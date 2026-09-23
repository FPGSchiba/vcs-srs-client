package audio

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ringCapacityFrames bounds queued latency in either direction: enough
// headroom (~160 ms) to absorb OS scheduling jitter without letting a
// backlog become audible lag. See ring.go for why overflow drops the
// newest chunk instead of growing unbounded.
const ringCapacityFrames = 16

// reopenBackoffInitial/reopenBackoffMax bound how often the poll loop
// retries a direction that failed to open. Without a genuine backoff, a
// fixed fast retry against a genuinely-gone device would call
// Backend.OpenCapture/OpenPlayback on every poll tick -- and on the real
// malgo backend, a failed setDeviceID call still enumerates and takes a
// DeviceID.Pointer() for every candidate it checks, which allocates C
// memory malgo never frees (Task 8's finding). A fast fixed retry turns
// that per-open cost into an unbounded leak over an extended device-loss
// window; doubling backoff, capped, keeps the retry rate bounded instead.
const (
	reopenBackoffInitial = 1 * time.Second
	reopenBackoffMax     = 30 * time.Second
)

// Config is the user-tunable half of the pipeline, mirroring the [audio]
// settings table (design spec §10).
type Config struct {
	InputDevice, OutputDevice             string
	MicPassthrough, AGC, NoiseSuppression bool
	Gate                                  GateConfig
	Levels                                Levels
	VoiceEffect, ClippingEffect           string
	VOXNoiseCancel                        bool
}

// State is a point-in-time health snapshot, mirrored to the frontend via
// the audio:state event (design spec §13).
//
// InputDevice/OutputDevice report the device id actually in use for each
// direction; InputSubstituted/OutputSubstituted report whether that differs
// from what Config asked for -- i.e. resolveDevice fell back to the default
// because the saved id no longer enumerates (spec §13's "record the
// substitution" requirement).
type State struct {
	Running                             bool
	InputError, OutputError             string
	Overruns, Underruns                 uint64
	InputDevice, OutputDevice           string
	InputSubstituted, OutputSubstituted bool
}

// VU is the peak level observed on each bus since the last report, at
// ManagerOptions.VUInterval.
type VU struct {
	Input, Output float32
}

// Sink receives every gated, processed capture frame -- e.g. the Phase 5
// network encoder. WriteFrame must not retain the slice past the call: the
// DSP goroutine reuses the same backing array on the next frame.
type Sink interface {
	WriteFrame([]float32)
	Close() error
}

// ManagerOptions configures a Manager's callbacks and polling cadence.
//
// OnState may be invoked concurrently from more than one goroutine -- e.g.
// Start's tail call and a later pollLoop-driven emitState -- since nothing
// serializes Manager's own callers against its poll goroutine. Implement it
// to be safe under concurrent invocation (a mutex or a channel send are
// both fine); it must not assume a single caller.
type ManagerOptions struct {
	Log          *slog.Logger
	OnVU         func(VU)
	OnState      func(State)
	OnDevices    func(inputs, outputs []DeviceInfo)
	PollInterval time.Duration
	VUInterval   time.Duration
}

// Manager owns the capture -> process -> playback pipeline: device
// lifecycle, hot-plug, device-loss fallback, and the DSP goroutine that is
// the only place any of Tasks 2-9's building blocks actually run.
//
// Field ownership is split three ways, and that split is what keeps the
// realtime path lock-free:
//
//   - captureRing / playbackRing: the Ring's own Read/Write/Dropped/Drain
//     operations are lock-free by construction (ring.go). The POINTER
//     FIELDS on Manager are a different matter: Start publishes them (along
//     with stopDSP/dspDone/stopPoll/pollDone) inside the same mu critical
//     section that sets running = true, so a concurrent State() call --
//     which reads them under mu -- has a proper happens-before edge against
//     Start's write. Holding mu only on State's side would NOT be enough by
//     itself; it is the write side being under mu too that closes the race
//     (see Start's doc).
//   - cfg / ptt / muted / sinks: written by any control-plane goroutine,
//     read by the DSP (and, for cfg, the poll) goroutine via atomic loads
//     of an immutable value or an immutable-slice pointer. Nothing here is
//     ever mutated in place, so a reader never observes a torn value and
//     never blocks a writer.
//   - mu: guards run/error/backoff bookkeeping that only Start, Stop and
//     the poll goroutine touch. The DSP goroutine and the two OS callbacks
//     never take it, so it is never contended by the realtime path.
type Manager struct {
	backend Backend
	opts    ManagerOptions
	log     *slog.Logger

	captureRing  *Ring
	playbackRing *Ring

	cfg   atomic.Pointer[Config]
	ptt   atomic.Bool
	muted atomic.Bool
	sinks atomic.Pointer[[]Sink]

	sfx *SFX

	mu                            sync.Mutex
	running                       bool
	starting                      bool // claimed inside the same critical section as the running check, so a second concurrent Start() returns immediately instead of racing the first (Fix 2).
	captureStream, playbackStream Stream
	inputErr, outputErr           string
	inputID, outputID             string
	inputSubstituted              bool // resolveDevice fell back to the default because Config's saved input id no longer enumerates.
	outputSubstituted             bool // same, for output.
	lastInputs, lastOutputs       []DeviceInfo
	inputRetryAt, outputRetryAt   time.Time
	inputBackoff, outputBackoff   time.Duration

	// stopDSP/stopPoll/dspDone/pollDone are written ONLY inside the same mu
	// critical section that sets running = true in Start, and read only
	// under mu in Stop -- see Start's doc for why this matters (Fix 1).
	stopDSP, stopPoll chan struct{}
	dspDone, pollDone chan struct{}
}

// NewManager builds a Manager against the given Backend. Start must be
// called before any audio flows.
func NewManager(b Backend, opts ManagerOptions) *Manager {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second // design spec §8: ~2s hot-plug poll
	}
	if opts.VUInterval <= 0 {
		opts.VUInterval = 50 * time.Millisecond // design spec §11: ~20Hz
	}
	m := &Manager{
		backend: b,
		opts:    opts,
		log:     opts.Log,
		sfx:     NewSFX(),
	}
	m.sinks.Store(&[]Sink{})
	// Mirrors config's own Default() (design spec §10) so a Manager that
	// never receives an explicit SetConfig still mixes at a sane level
	// instead of silently muting every bus at Levels{}'s zero value.
	m.cfg.Store(&Config{Levels: Levels{Master: 0.75, Voice: 1, SFX: 0.8, Notification: 0.8}})
	return m
}

func (m *Manager) currentConfig() Config {
	if c := m.cfg.Load(); c != nil {
		return *c
	}
	return Config{}
}

// SetConfig replaces the pipeline configuration. Safe from any goroutine:
// the DSP and poll goroutines pick it up via an atomic pointer swap, never
// a mutation in place.
func (m *Manager) SetConfig(cfg Config) {
	c := cfg
	m.cfg.Store(&c)
}

// SetPTT reports the refcounted, cross-source push-to-talk state.
func (m *Manager) SetPTT(held bool) { m.ptt.Store(held) }

// SetMuted reports push-to-mute / mute-toggle state. Mute wins over PTT and
// VOX unconditionally -- see gate.go.
func (m *Manager) SetMuted(muted bool) { m.muted.Store(muted) }

// Muted reports the current mute state.
func (m *Manager) Muted() bool { return m.muted.Load() }

// AddSink registers a Sink to receive every gated capture frame. The
// backing slice is copy-on-write so the DSP goroutine never takes a lock to
// read it.
func (m *Manager) AddSink(s Sink) {
	for {
		old := m.sinks.Load()
		next := make([]Sink, 0, len(*old)+1)
		next = append(next, *old...)
		next = append(next, s)
		if m.sinks.CompareAndSwap(old, &next) {
			return
		}
	}
}

// PlayEffect starts an SFX one-shot. Unknown or absent ids are ignored by
// SFX itself.
func (m *Manager) PlayEffect(id string) { m.sfx.Play(id) }

// State returns a snapshot of the manager's health.
func (m *Manager) State() State {
	m.mu.Lock()
	st := State{
		Running:           m.running,
		InputError:        m.inputErr,
		OutputError:       m.outputErr,
		InputDevice:       m.inputID,
		OutputDevice:      m.outputID,
		InputSubstituted:  m.inputSubstituted,
		OutputSubstituted: m.outputSubstituted,
	}
	capRing, playRing := m.captureRing, m.playbackRing
	m.mu.Unlock()

	if capRing != nil {
		st.Overruns = capRing.Dropped()
	}
	if playRing != nil {
		st.Underruns = playRing.Underruns()
	}
	return st
}

// Devices returns the most recently enumerated device lists.
func (m *Manager) Devices() (inputs, outputs []DeviceInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DeviceInfo(nil), m.lastInputs...), append([]DeviceInfo(nil), m.lastOutputs...)
}

func (m *Manager) emitState() {
	if m.opts.OnState != nil {
		m.opts.OnState(m.State())
	}
}

// resolveDevice maps a persisted device id to one that can actually be
// opened right now: an empty id (design's first-class "follow system
// default"), or one that no longer enumerates, resolves to the entry
// marked IsDefault, then to the first enumerated entry -- never an error.
// See backend.go's DeviceInfo.ID doc and design spec §13.
func resolveDevice(id string, devices []DeviceInfo) string {
	if id != "" {
		for _, d := range devices {
			if d.ID == id {
				return id
			}
		}
	}
	for _, d := range devices {
		if d.IsDefault {
			return d.ID
		}
	}
	if len(devices) > 0 {
		return devices[0].ID
	}
	return ""
}

func sameDevices(a, b []DeviceInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Start enumerates devices, resolves the configured input/output ids,
// opens both directions and launches the DSP and poll goroutines.
//
// Start never fails on a bad or absent saved device (resolveDevice falls
// back to the default) and never fails on a device that refuses to open
// (the error is recorded in State and the other direction still runs) --
// see the rules on manager.go's Start doc and task-10-brief.md rules 5-6.
// It is idempotent while already running, and safe against concurrent
// invocation: a second caller that arrives while the first is still setting
// up observes m.starting and returns immediately rather than racing it
// (Fix 2).
//
// Concurrency note (Fix 1): every field a concurrent State() or Stop() call
// can observe -- captureRing, playbackRing, captureStream, playbackStream,
// inputID/outputID (+substituted), the error strings, and the four
// lifecycle channels -- is written in ONE mu-guarded critical section, the
// same one that flips running to true. The Go memory model gives no
// happens-before edge from an unguarded write to a guarded read: a mutex
// held only on the reader's side (as State() does) does not make a writer's
// unsynchronized store visible, and does not order it against the reader.
// Publishing the whole set together under mu, before any other goroutine
// can legally observe running == true, is what makes them visible as a
// single atomic unit rather than individually-torn fields.
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.running || m.starting {
		m.mu.Unlock()
		return nil
	}
	m.starting = true
	m.mu.Unlock()

	inputs, outputs, err := m.backend.Enumerate()
	if err != nil {
		// Nothing to resolve against; leave both directions to the poll
		// loop's bounded-backoff reopen once enumeration recovers, rather
		// than aborting Start (spec §13: the app stays usable).
		m.log.Warn("audio: enumerate failed at start", "err", err)
		inputs, outputs = nil, nil
	}

	captureRing := NewRing(ringCapacityFrames)
	playbackRing := NewRing(ringCapacityFrames)

	cfg := m.currentConfig()
	inID := resolveDevice(cfg.InputDevice, inputs)
	outID := resolveDevice(cfg.OutputDevice, outputs)
	inputSubstituted := cfg.InputDevice != "" && cfg.InputDevice != inID
	outputSubstituted := cfg.OutputDevice != "" && cfg.OutputDevice != outID

	// The two OS callbacks stay trivial: no cgo, no locks, no allocation,
	// no logging. All they do is move data through the ring. They close
	// over the LOCAL captureRing/playbackRing (not the not-yet-published
	// m.captureRing/m.playbackRing), so they need no synchronisation of
	// their own either.
	capStream, capErr := m.backend.OpenCapture(inID, func(frame []float32) {
		captureRing.Write(frame)
	})
	playStream, playErr := m.backend.OpenPlayback(outID, func(dst []float32) {
		n := playbackRing.Read(dst)
		for i := n; i < len(dst); i++ {
			dst[i] = 0
		}
	})

	denoiser, err := NewDenoiser()
	if err != nil {
		// Noise suppression is a quality feature, not a correctness one
		// (rnnoise_stub.go) -- never fails Start.
		m.log.Warn("audio: denoiser unavailable", "err", err)
		denoiser = &Denoiser{}
	}
	// Prime the denoiser: RNNoise attenuates its very first frame by
	// roughly 60x while its internal state initialises (confirmed against
	// the C source in Task 7 -- cold start, not a bug). Run one throwaway
	// frame through it now so the live path never sees the artifact; a
	// user's first live syllable would otherwise be swallowed.
	if denoiser.Available() {
		denoiser.Process(make([]float32, FrameSamples))
	}

	stopDSP := make(chan struct{})
	dspDone := make(chan struct{})
	stopPoll := make(chan struct{})
	pollDone := make(chan struct{})

	m.mu.Lock()
	m.captureRing, m.playbackRing = captureRing, playbackRing
	m.lastInputs, m.lastOutputs = inputs, outputs
	m.captureStream, m.playbackStream = capStream, playStream
	m.inputID, m.outputID = inID, outID
	m.inputSubstituted, m.outputSubstituted = inputSubstituted, outputSubstituted
	m.inputBackoff, m.outputBackoff = 0, 0
	m.inputRetryAt, m.outputRetryAt = time.Time{}, time.Time{}
	if capErr != nil {
		m.inputErr = capErr.Error()
		m.log.Warn("audio: input device failed to open", "device", inID, "err", capErr)
	} else {
		m.inputErr = ""
	}
	if playErr != nil {
		m.outputErr = playErr.Error()
		m.log.Warn("audio: output device failed to open", "device", outID, "err", playErr)
	} else {
		m.outputErr = ""
	}
	m.stopDSP, m.dspDone = stopDSP, dspDone
	m.stopPoll, m.pollDone = stopPoll, pollDone
	m.running = true
	m.starting = false
	m.mu.Unlock()

	go m.dspLoop(denoiser)
	go m.pollLoop()

	m.emitState()
	return nil
}

// Stop tears the manager down. It is idempotent and orders teardown as:
// stop DSP goroutine -> stop poll goroutine -> stop streams ->
// Backend.Close(). The poll goroutine must be joined before the streams
// are read/stopped: otherwise a concurrent bounded-backoff reopen could
// store a freshly-opened stream into captureStream/playbackStream after
// Stop has already snapshotted (and is about to discard) the old one,
// leaking it.
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	stopDSP, dspDone := m.stopDSP, m.dspDone
	stopPoll, pollDone := m.stopPoll, m.pollDone
	m.mu.Unlock()

	if stopDSP != nil {
		close(stopDSP)
		m.joinOrAbandon("dsp", dspDone)
	}
	if stopPoll != nil {
		close(stopPoll)
		m.joinOrAbandon("poll", pollDone)
	}

	m.mu.Lock()
	capStream, playStream := m.captureStream, m.playbackStream
	m.captureStream, m.playbackStream = nil, nil
	m.mu.Unlock()

	if capStream != nil {
		capStream.Stop()
	}
	if playStream != nil {
		playStream.Stop()
	}
	m.backend.Close()

	m.emitState()
}

// stopJoinTimeout bounds how long Stop() waits for the DSP/poll goroutines
// to exit before giving up on them. A wedged Backend.Open call in flight
// inside a bounded-backoff reopen (poll goroutine) or a stalled Backend call
// (either goroutine) could otherwise hang shutdown forever; an app that logs
// an abandoned goroutine and exits is better than one that never quits.
const stopJoinTimeout = 5 * time.Second

// joinOrAbandon waits for done to close, up to stopJoinTimeout, logging and
// returning instead of blocking forever if it never does. The goroutine
// itself is not killed -- Go has no mechanism for that -- so a timeout here
// means Stop proceeds to close the backend and streams while that goroutine
// may still be running; this is judged the lesser risk versus an
// unresponsive app (see Fix 7 in task-10 review).
func (m *Manager) joinOrAbandon(name string, done <-chan struct{}) {
	select {
	case <-done:
	case <-time.After(stopJoinTimeout):
		m.log.Warn("audio: goroutine did not exit before Stop's timeout; abandoning it", "goroutine", name, "timeout", stopJoinTimeout)
	}
}

// peak reports the largest absolute sample value in frame, for VU metering.
func peak(frame []float32) float32 {
	var p float32
	for _, v := range frame {
		a := v
		if a < 0 {
			a = -a
		}
		if a > p {
			p = a
		}
	}
	return p
}

// dspLoop is the only place Tasks 2-9's building blocks run, and the only
// producer for the playback ring. It owns AGC, Gate, Mixer, the voice
// Effect and the Denoiser exclusively: nothing else in this file touches
// them, so there is nothing to synchronise here beyond the atomic reads of
// cfg/ptt/muted/sinks that cross in from the control plane.
func (m *Manager) dspLoop(denoiser *Denoiser) {
	defer close(m.dspDone)
	defer denoiser.Close()

	agc := NewAGC()
	gate := NewGate(GateConfig{})
	mixer := NewMixer()
	effect := NewEffect("", "")
	var lastCfg *Config

	// All scratch buffers allocated exactly once, here -- nothing in this
	// loop (or in either OS callback) allocates again. outFrame is its own
	// buffer, distinct from inFrame: Phase 5 adds a network encoder Sink
	// that may queue frames rather than consume them synchronously, and
	// aliasing outFrame onto inFrame (handed to every Sink.WriteFrame just
	// above) would let the mixer overwrite a frame a queuing sink hasn't
	// read yet. One extra 480-float32 buffer is the whole fix (Fix 6).
	inFrame := make([]float32, FrameSamples)
	outFrame := make([]float32, FrameSamples)
	monitorBuf := make([]float32, FrameSamples)
	sfxBuf := make([]float32, FrameSamples)
	notifBuf := make([]float32, FrameSamples) // no notification engine yet (Phase 4 scope); always silent.

	var lastCaptureDropped uint64
	var vuIn, vuOut float32
	vuLastEmit := time.Now()

	ticker := time.NewTicker(FrameDuration)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopDSP:
			return
		case <-ticker.C:
		}

		cfg := m.cfg.Load()
		if cfg != lastCfg {
			lastCfg = cfg
			gate.SetConfig(cfg.Gate)
			mixer.SetLevels(cfg.Levels)
			if vID, cID := effect.IDs(); vID != cfg.VoiceEffect || cID != cfg.ClippingEffect {
				effect = NewEffect(cfg.VoiceEffect, cfg.ClippingEffect)
			}
		}

		// The ring drops the NEWEST samples on overflow (see ring.go) --
		// race-free, but it means a sustained overrun leaves a permanent
		// backlog unless something advances the read index past it. Only
		// the consumer (this goroutine) may legally do that, so this is
		// the one place latency can recover: observe Dropped() climbing
		// and Drain() the backlog it left behind.
		if d := m.captureRing.Dropped(); d != lastCaptureDropped {
			lastCaptureDropped = d
			m.captureRing.Drain()
		}

		n := m.captureRing.Read(inFrame)
		for i := n; i < len(inFrame); i++ {
			inFrame[i] = 0
		}

		// Chain order NS -> AGC -> gate -> voice effect is load-bearing:
		// NS runs before AGC so AGC cannot amplify the noise floor during
		// silence, and gate runs after AGC because it decides on a
		// levelled signal. VOX level measurement is the one thing that
		// moves: VOXNoiseCancel decides whether the gate sees the level
		// pre- or post-denoise.
		var level float32
		if cfg.VOXNoiseCancel {
			if cfg.NoiseSuppression {
				denoiser.Process(inFrame)
			}
			level = rms(inFrame)
		} else {
			level = rms(inFrame)
			if cfg.NoiseSuppression {
				denoiser.Process(inFrame)
			}
		}
		if cfg.AGC {
			agc.Process(inFrame)
		}
		gateOpen := gate.Step(GateInput{
			PTT:   m.ptt.Load(),
			Muted: m.muted.Load(),
			Level: level,
		})
		effect.Process(inFrame)

		if p := peak(inFrame); p > vuIn {
			vuIn = p
		}

		if gateOpen {
			sinks := *m.sinks.Load()
			for _, s := range sinks {
				s.WriteFrame(inFrame)
			}
		}
		if gateOpen && cfg.MicPassthrough {
			copy(monitorBuf, inFrame)
		} else {
			clear(monitorBuf)
		}

		clear(sfxBuf)
		m.sfx.MixInto(sfxBuf)

		mixer.Mix(outFrame, monitorBuf, sfxBuf, notifBuf)
		m.playbackRing.Write(outFrame)

		if p := peak(outFrame); p > vuOut {
			vuOut = p
		}

		if m.opts.OnVU != nil {
			if now := time.Now(); now.Sub(vuLastEmit) >= m.opts.VUInterval {
				m.opts.OnVU(VU{Input: vuIn, Output: vuOut})
				vuIn, vuOut = 0, 0
				vuLastEmit = now
			}
		}
	}
}

// pollLoop re-enumerates devices on a fixed cadence, diffs against the
// last snapshot for hot-plug notification, and drives bounded-backoff
// reopen attempts for any direction that failed to open (or lost its
// device). It never touches the DSP goroutine's state.
func (m *Manager) pollLoop() {
	defer close(m.pollDone)
	ticker := time.NewTicker(m.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopPoll:
			return
		case <-ticker.C:
		}
		m.pollOnce()
	}
}

func (m *Manager) pollOnce() {
	inputs, outputs, err := m.backend.Enumerate()
	if err != nil {
		m.log.Warn("audio: enumerate failed during poll", "err", err)
		return
	}

	m.mu.Lock()
	changed := !sameDevices(m.lastInputs, inputs) || !sameDevices(m.lastOutputs, outputs)
	m.lastInputs, m.lastOutputs = inputs, outputs
	m.mu.Unlock()

	if changed && m.opts.OnDevices != nil {
		m.opts.OnDevices(inputs, outputs)
	}

	now := time.Now()
	cfg := m.currentConfig()
	m.maybeReopenCapture(cfg, inputs, now)
	m.maybeReopenPlayback(cfg, outputs, now)

	if changed {
		m.emitState()
	}
}

func (m *Manager) maybeReopenCapture(cfg Config, inputs []DeviceInfo, now time.Time) {
	m.mu.Lock()
	ready := m.captureStream == nil && !now.Before(m.inputRetryAt)
	m.mu.Unlock()
	if !ready {
		return
	}

	id := resolveDevice(cfg.InputDevice, inputs)
	captureRing := m.captureRing
	stream, err := m.backend.OpenCapture(id, func(frame []float32) {
		captureRing.Write(frame)
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.inputErr = err.Error()
		m.inputBackoff = nextBackoff(m.inputBackoff)
		m.inputRetryAt = now.Add(m.inputBackoff)
		return
	}
	m.captureStream = stream
	m.inputID = id
	m.inputSubstituted = cfg.InputDevice != "" && cfg.InputDevice != id
	m.inputErr = ""
	m.inputBackoff = 0
}

func (m *Manager) maybeReopenPlayback(cfg Config, outputs []DeviceInfo, now time.Time) {
	m.mu.Lock()
	ready := m.playbackStream == nil && !now.Before(m.outputRetryAt)
	m.mu.Unlock()
	if !ready {
		return
	}

	id := resolveDevice(cfg.OutputDevice, outputs)
	playbackRing := m.playbackRing
	stream, err := m.backend.OpenPlayback(id, func(dst []float32) {
		n := playbackRing.Read(dst)
		for i := n; i < len(dst); i++ {
			dst[i] = 0
		}
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.outputErr = err.Error()
		m.outputBackoff = nextBackoff(m.outputBackoff)
		m.outputRetryAt = now.Add(m.outputBackoff)
		return
	}
	m.playbackStream = stream
	m.outputID = id
	m.outputSubstituted = cfg.OutputDevice != "" && cfg.OutputDevice != id
	m.outputErr = ""
	m.outputBackoff = 0
}

// nextBackoff doubles (or seeds) a bounded reopen backoff. See the
// reopenBackoffInitial/Max doc for why this must not be a fixed fast
// retry.
func nextBackoff(cur time.Duration) time.Duration {
	if cur <= 0 {
		return reopenBackoffInitial
	}
	next := cur * 2
	if next > reopenBackoffMax {
		next = reopenBackoffMax
	}
	return next
}
