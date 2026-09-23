package audio

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ErrStartInProgress is returned by Start when a previous call is still
// setting up (see Start's doc, Fix B). It is distinct from the ordinary
// idempotent no-op Start returns when the manager is already running: this
// means a start was already underway and this call did nothing, so a
// caller that needs the manager running should retry rather than assume
// success.
var ErrStartInProgress = errors.New("audio: Start already in progress")

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
//
// Starting reports whether a Start() call is currently underway (claimed
// but not yet resolved into Running). It exists so a wedged Start (backend
// call that never returns) is at least OBSERVABLE from outside rather than
// indistinguishable from "never started" -- see Fix B.
type State struct {
	Running                             bool
	Starting                            bool
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
//
// WriteFrame may be invoked concurrently from more than one goroutine.
// m.sinks is deliberately Manager-lifetime (a registered Sink should
// survive a Stop()/Start() restart), but Stop()'s bounded join (Fix 7) can
// leave a previous generation's dspLoop still running (abandoned, not
// stopped) at the same time a new generation's dspLoop is live -- and both
// call WriteFrame on the SAME Sink. Implementations must be safe under
// concurrent invocation; this package's own test sinks happen to be
// internally synchronised, but that is not part of the interface's
// contract elsewhere, so don't rely on it.
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
//
// OnVU carries the identical exposure: an abandoned generation's dspLoop
// (Stop()'s bounded join, Fix 7) can still be running, calling OnVU on its
// own schedule, at the same time a new generation's dspLoop is doing the
// same -- both from the same Manager, on the same callback. Implement it to
// be safe under concurrent invocation too.
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

	// sfx holds the shared, immutable-after-construction sample store only.
	// Its MUTABLE mixing state is deliberately NOT here -- see sfxVoices
	// below and voicePool's doc (sfx.go) for why a single Manager-lifetime
	// voicePool would have been just as broken as reading m.captureRing
	// live from dspLoop (Fix A's precise bug, rediscovered in round 2 via
	// -race on this exact scenario: an abandoned generation's zombie
	// dspLoop and the next generation's real one both calling MixInto on
	// the one shared pool concurrently).
	sfx *SFX

	mu                            sync.Mutex
	running                       bool
	starting                      bool       // claimed inside the same critical section as the running check, so a second concurrent Start() returns immediately instead of racing the first (Fix 2).
	epoch                         uint64     // GENERATION IDENTITY, not "a Start happened": incremented in Start's mu-guarded publish AND in Stop's first mu section (Fix A round 4 -- Stop alone, with no subsequent Start, must also invalidate the generation it's tearing down, or a reopen it abandoned and gives up waiting on can still publish after Stop returns). Round 2 parameterised the READ side of the poll goroutine (the rings); it missed that maybeReopenCapture/maybeReopenPlayback/pollOnce also WRITE BACK into Manager fields after a Backend call that can itself be the thing blocked when Stop() abandons this goroutine. mu makes those writes race-free, not current: a late write-back is checked against the CURRENT m.epoch before being published, and discarded (with the freshly-opened stream stopped) if this goroutine's generation is no longer it (Fix A round 3). Every stamped goroutine now carries one uniform invariant -- "my epoch must still be the live one" -- that covers being superseded by a later Start() and being torn down by a Stop() with no successor, via the same check, with no special-casing.
	sfxVoices                     *voicePool // current generation's mixing state; read fresh under mu by PlayEffect (Fix A round 2). Published under mu in Start alongside the rings, for the same reason.
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

// PTT reports the current push-to-talk state, mirroring Muted below.
func (m *Manager) PTT() bool { return m.ptt.Load() }

// SetMuted reports push-to-mute / mute-toggle state. Mute wins over PTT and
// VOX unconditionally -- see gate.go.
func (m *Manager) SetMuted(muted bool) { m.muted.Store(muted) }

// Muted reports the current mute state.
func (m *Manager) Muted() bool { return m.muted.Load() }

// ToggleMuted flips the mute state and reports the new value.
//
// This is a CAS loop, not the more obvious `m.SetMuted(!m.Muted())`.
// global.mute_toggle is a PRESS action, deliberately unrefcounted (unlike a
// HOLD action's press/release, which internal/app's pressCount joins into
// one edge across sources before either Manager method is ever called) --
// so two independent sources bound to it (a keyboard chord and a joystick
// button, both fully supported bind targets for the same action) can call
// this from two separate goroutines with no coordination above this layer.
// A plain load-then-store lets both goroutines read the same starting value
// before either stores, so both flip to the same target and one toggle is
// silently lost: the UI ends up disagreeing with the user's last press
// until they press again. The CAS retry closes that window -- a losing
// goroutine re-reads the value the winner just stored and flips THAT
// instead, so no toggle is ever dropped.
func (m *Manager) ToggleMuted() bool {
	for {
		old := m.muted.Load()
		next := !old
		if m.muted.CompareAndSwap(old, next) {
			return next
		}
	}
}

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

// PlayEffect starts an SFX one-shot. Unknown or absent ids are ignored.
// Also a silent no-op if called while the manager isn't running -- there is
// no current generation's voicePool to queue into, and letting a request
// linger until some later, unrelated Start() drained it would be more
// surprising than dropping it.
func (m *Manager) PlayEffect(id string) {
	if !m.sfx.Available(id) {
		return
	}
	m.mu.Lock()
	v, running := m.sfxVoices, m.running
	m.mu.Unlock()
	if running && v != nil {
		v.play(id)
	}
}

// State returns a snapshot of the manager's health.
func (m *Manager) State() State {
	m.mu.Lock()
	st := State{
		Running:           m.running,
		Starting:          m.starting,
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
// It is idempotent while already running: a second call returns nil once
// running is true, same as before.
//
// A second call that arrives WHILE a first call is still setting up is
// different: it returns ErrStartInProgress rather than nil (Fix B). Silent
// success there would be a lie -- the manager did not just start, someone
// else's in-flight Start() might still fail, hang, or resolve a different
// device set. m.starting is cleared via defer, so even a panic mid-setup
// cannot wedge it permanently; State().Starting also exposes it, so a
// truly wedged Start (a Backend call that never returns -- Backend has no
// cancellation hook, so this can't be turned into a bounded error) is at
// least observable from outside instead of indistinguishable from
// never having been called.
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
	if m.running {
		m.mu.Unlock()
		return nil
	}
	if m.starting {
		m.mu.Unlock()
		m.log.Warn("audio: Start called while a previous Start is still in progress")
		return ErrStartInProgress
	}
	m.starting = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
	}()

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
	// A fresh voicePool per generation -- see sfx.go's voicePool doc for
	// why m.sfx's SAMPLE STORE stays shared but its MIXING STATE cannot:
	// an abandoned generation's dspLoop must never be able to mix into the
	// same pool a later generation's dspLoop is using.
	sfxVoices := m.sfx.NewVoicePool()

	m.mu.Lock()
	m.captureRing, m.playbackRing = captureRing, playbackRing
	m.sfxVoices = sfxVoices
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
	m.epoch++
	epoch := m.epoch
	m.running = true
	m.mu.Unlock()

	// dspLoop/pollLoop take THIS generation's rings and channels as
	// parameters -- exactly as the OS callbacks above already do -- rather
	// than reading m.captureRing/m.playbackRing/m.stopDSP live. See Fix A's
	// note on the Manager struct doc for why: Stop()'s bounded join (Fix 7)
	// means a goroutine can outlive Stop() and still be running when the
	// NEXT Start() publishes a new generation's rings into those fields.
	// Passed-by-value locals can never be repointed by a later Start(), so
	// an abandoned generation stays permanently wired to its OWN rings and
	// can never cross into the next generation's state.
	go m.dspLoop(denoiser, stopDSP, dspDone, captureRing, playbackRing, sfxVoices)
	go m.pollLoop(stopPoll, pollDone, captureRing, playbackRing, epoch)

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
	// Bump epoch here too, not just in Start (Fix A round 4): epoch means
	// "generation identity", not "a Start happened". Without this, a
	// reopen abandoned by the bounded joins below (still parked in
	// Backend.OpenCapture/OpenPlayback/Enumerate) that finally returns
	// AFTER this Stop() has already nilled captureStream/playbackStream
	// and closed the backend would still read m.epoch == epoch as true --
	// its stamped generation was never invalidated, just torn down -- and
	// would publish a live, backend-registered stream into a Manager that
	// believes itself fully stopped. No future Stop() could ever reach it
	// (this Stop already returned), and the next Start would silently
	// overwrite the pointer without stopping what's actually there: an OS
	// microphone stream kept running after the user is told audio is off.
	// Bumping here gives every write-back site (pollOnce,
	// maybeReopenCapture, maybeReopenPlayback) one uniform invariant --
	// "my stamped epoch must still be the live one" -- that covers BOTH "a
	// newer generation replaced me" (Start's bump) AND "the manager was
	// torn down with no replacement" (this bump), with no special-casing
	// needed at the check sites themselves.
	m.epoch++
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
	m.sfxVoices = nil // PlayEffect also gates on m.running, so this is belt-and-suspenders: don't hold a reference to a generation that may still be a zombie any longer than necessary.
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
//
// A var, not a const, solely so a test can shorten it to deterministically
// force an abandonment (Fix A's test) without a real 5s wait. Tests in this
// package never run in parallel, so temporarily overriding it is safe.
var stopJoinTimeout = 5 * time.Second

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

// vuMeterSegments is the number of discrete segments the frontend VU meter
// renders (frontend/src/shared/components/VU.tsx's default `segs`). It is
// the resolution EventAudioVU suppression quantizes to -- see
// quantizeVUSegment.
const vuMeterSegments = 16

// quantizeVUSegment buckets a normalized 0..1 level into the same discrete
// segment the frontend VU meter would light for it, so the dspLoop's
// "emit only when changed" check compares what the user would actually
// SEE rather than raw floats that are effectively never bit-identical
// between two intervals of live capture. Pure integer/float arithmetic on
// its parameter -- no allocation, no logging, no locking -- so it is safe
// to call from the DSP loop on every tick.
func quantizeVUSegment(level float32) int32 {
	clamped := level
	if clamped < 0 {
		clamped = 0
	} else if clamped > 1 {
		clamped = 1
	}
	return int32(clamped * vuMeterSegments)
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
//
// stopDSP/dspDone/captureRing/playbackRing/sfxVoices are PARAMETERS, not
// read from the Manager's own fields, and that is load-bearing (Fix A):
// Stop()'s bounded join (Fix 7) means this goroutine can be abandoned --
// still running after Stop() gives up waiting on it -- and a later Start()
// then publishes a NEW generation's rings (and voicePool) into
// m.captureRing/m.playbackRing/m.sfxVoices. If this loop read those fields
// live, an abandoned instance that eventually unblocks (e.g. from a slow
// Sink.WriteFrame or OnVU call) would resume operating on the NEW
// generation's state -- an unguarded read racing a guarded write, AND a
// second producer/consumer violating the Ring's documented single-consumer
// invariant (ring.go) or the voicePool's single-goroutine invariant
// (sfx.go) -- sfxVoices needed exactly the same treatment as the rings once
// -race caught two generations' dspLoops both calling MixInto on one
// shared voicePool during this fix's own testing. Taking them all as
// parameters means this instance is permanently wired to the generation it
// was launched for: even if it never returns, it can only ever touch its
// OWN orphaned state, never the next generation's. See dspLoop's call site
// in Start for how the OS callbacks already used this pattern.
func (m *Manager) dspLoop(denoiser *Denoiser, stopDSP, dspDone chan struct{}, captureRing, playbackRing *Ring, sfxVoices *voicePool) {
	defer close(dspDone)
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
	// lastVUInSeg/lastVUOutSeg are the quantized (see quantizeVUSegment)
	// buckets of the last EMITTED VU, not the last computed one --
	// EventAudioVU's doc says the event is suppressed when unchanged, but
	// nothing enforced that (see Fix 1's report). These are locals of this
	// goroutine's loop, not Manager fields: the DSP loop must never
	// allocate, log, block, or take a contended lock, and a shared field
	// would need synchronisation against OnState/OnDevices callers that
	// don't otherwise touch dspLoop's state. -1 is not a reachable
	// quantizeVUSegment result, so the very first tick always emits and
	// establishes a baseline.
	lastVUInSeg, lastVUOutSeg := int32(-1), int32(-1)

	ticker := time.NewTicker(FrameDuration)
	defer ticker.Stop()

	for {
		select {
		case <-stopDSP:
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
		if d := captureRing.Dropped(); d != lastCaptureDropped {
			lastCaptureDropped = d
			captureRing.Drain()
		}

		n := captureRing.Read(inFrame)
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
		sfxVoices.mixInto(sfxBuf, m.sfx.sampleFor)

		mixer.Mix(outFrame, monitorBuf, sfxBuf, notifBuf)
		playbackRing.Write(outFrame)

		if p := peak(outFrame); p > vuOut {
			vuOut = p
		}

		if m.opts.OnVU != nil {
			if now := time.Now(); now.Sub(vuLastEmit) >= m.opts.VUInterval {
				// EventAudioVU's doc promises suppression when unchanged.
				// An exact float== comparison would almost never suppress
				// anything against a live mic -- capture noise flickers the
				// low bits of peak() every interval -- so quantize to the
				// same 16-segment resolution the frontend VU meter actually
				// renders (frontend/src/shared/components/VU.tsx) before
				// comparing: two payloads that would paint the same meter
				// are "unchanged" even if their raw floats differ in the
				// noise floor. The emitted payload still carries the raw,
				// unquantized level.
				inSeg, outSeg := quantizeVUSegment(vuIn), quantizeVUSegment(vuOut)
				if inSeg != lastVUInSeg || outSeg != lastVUOutSeg {
					m.opts.OnVU(VU{Input: vuIn, Output: vuOut})
					lastVUInSeg, lastVUOutSeg = inSeg, outSeg
				}
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
//
// stopPoll/pollDone/captureRing/playbackRing are parameters for the exact
// same reason as dspLoop's (Fix A): maybeReopenCapture/maybeReopenPlayback
// open NEW OS streams whose callbacks close over whichever ring they're
// given, and Stop()'s bounded join can abandon this goroutine too. Reading
// m.captureRing/m.playbackRing live here would let an abandoned poll
// goroutine reopen a device against the NEXT generation's ring after a
// Stop()+Start() cycle -- the same cross-generation entanglement dspLoop
// was fixed for, just reached through the reopen path instead of the
// steady-state read/write path.
//
// epoch is this generation's stamp (Manager.epoch's value at the Start()
// call that launched this goroutine). Round 2 parameterised the READ side
// here (the rings) but missed that pollOnce/maybeReopenCapture/
// maybeReopenPlayback also WRITE BACK into Manager fields (captureStream,
// inputID, inputErr, lastInputs, ...) -- and those writes happen AFTER a
// Backend call (Enumerate/OpenCapture/OpenPlayback) that can itself be the
// thing blocked when Stop() gives up and abandons this goroutine. mu makes
// those write-backs race-free, not current: it stops two goroutines from
// tearing the same field, but nothing stopped an abandoned goroutine's
// STALE result from being published as if it were fresh once the blocked
// call finally returned. Every write-back site below re-checks m.epoch ==
// epoch under the SAME mu critical section as the write, immediately
// before committing, and discards (stopping any freshly-opened stream
// first) if this goroutine's generation is no longer current. See the
// Manager.epoch field doc.
func (m *Manager) pollLoop(stopPoll, pollDone chan struct{}, captureRing, playbackRing *Ring, epoch uint64) {
	defer close(pollDone)
	ticker := time.NewTicker(m.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopPoll:
			return
		case <-ticker.C:
		}
		m.pollOnce(captureRing, playbackRing, epoch)
	}
}

func (m *Manager) pollOnce(captureRing, playbackRing *Ring, epoch uint64) {
	inputs, outputs, err := m.backend.Enumerate()
	if err != nil {
		m.log.Warn("audio: enumerate failed during poll", "err", err)
		return
	}

	m.mu.Lock()
	if m.epoch != epoch {
		// Enumerate was in flight (possibly blocked) when Stop() abandoned
		// this goroutine and a later Start() ran. Discard: publishing
		// inputs/outputs here would overwrite the CURRENT generation's
		// device snapshot with a stale one, and any reopen below would be
		// working from stale data too -- skip the rest of this poll tick
		// entirely.
		m.mu.Unlock()
		return
	}
	changed := !sameDevices(m.lastInputs, inputs) || !sameDevices(m.lastOutputs, outputs)
	m.lastInputs, m.lastOutputs = inputs, outputs
	m.mu.Unlock()

	if changed && m.opts.OnDevices != nil {
		m.opts.OnDevices(inputs, outputs)
	}

	now := time.Now()
	cfg := m.currentConfig()
	m.maybeReopenCapture(cfg, inputs, now, captureRing, epoch)
	m.maybeReopenPlayback(cfg, outputs, now, playbackRing, epoch)

	if changed {
		m.emitState()
	}
}

func (m *Manager) maybeReopenCapture(cfg Config, inputs []DeviceInfo, now time.Time, captureRing *Ring, epoch uint64) {
	m.mu.Lock()
	ready := m.captureStream == nil && !now.Before(m.inputRetryAt) && m.epoch == epoch
	m.mu.Unlock()
	if !ready {
		return
	}

	id := resolveDevice(cfg.InputDevice, inputs)
	stream, err := m.backend.OpenCapture(id, func(frame []float32) {
		captureRing.Write(frame)
	})

	m.mu.Lock()
	if m.epoch != epoch {
		// OpenCapture (possibly blocked on a wedged device) outlived this
		// generation: Stop() abandoned this goroutine and a later Start()
		// is now current. Publishing a phantom stream/inputID/inputErr
		// here would make gen 2's REAL capture stream unreachable from
		// m.captureStream forever (no future Stop() could ever stop it --
		// an OS microphone stream kept open after the user believes audio
		// has stopped, for a voice-comms client), and would make
		// `ready := m.captureStream == nil` permanently false so gen 2
		// could never recover capture either. Discard entirely, and stop
		// the stream we just opened so it isn't leaked.
		m.mu.Unlock()
		if stream != nil {
			stream.Stop()
		}
		return
	}
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

func (m *Manager) maybeReopenPlayback(cfg Config, outputs []DeviceInfo, now time.Time, playbackRing *Ring, epoch uint64) {
	m.mu.Lock()
	ready := m.playbackStream == nil && !now.Before(m.outputRetryAt) && m.epoch == epoch
	m.mu.Unlock()
	if !ready {
		return
	}

	id := resolveDevice(cfg.OutputDevice, outputs)
	stream, err := m.backend.OpenPlayback(id, func(dst []float32) {
		n := playbackRing.Read(dst)
		for i := n; i < len(dst); i++ {
			dst[i] = 0
		}
	})

	m.mu.Lock()
	if m.epoch != epoch {
		// Symmetric with maybeReopenCapture above: discard a late result
		// from an abandoned generation rather than publish a phantom
		// playbackStream over the current generation's real one.
		m.mu.Unlock()
		if stream != nil {
			stream.Stop()
		}
		return
	}
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
