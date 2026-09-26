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

// Source supplies the received-voice bus: one frame of already-decoded PCM
// per DSP tick, summed onto the voice bus alongside the local monitor. The
// Phase 5 implementation is internal/voice's Session.
//
// The direction of the seam is what keeps the dependency acyclic. package
// voice imports package audio (it needs Effect for its per-stream radio
// colouration); audio must therefore never import voice, so the RX path
// arrives here as an interface satisfied from the outside rather than as a
// concrete type.
//
// ReadInto is called ON THE DSP GOROUTINE, inside the same 10 ms budget as
// Sink.WriteFrame, and carries the same prohibitions: no blocking, no
// allocation, no contended lock, and in particular no decoding. It must
// OVERWRITE buf rather than sum into it -- dspLoop hands it a scratch
// buffer whose previous contents are the last tick's received audio, so a
// summing implementation would smear a tail across every subsequent frame.
//
// Like Sink, it may be invoked concurrently: Stop()'s bounded join can
// leave an abandoned generation's dspLoop running alongside a live one, and
// both call ReadInto on the same Source.
type Source interface {
	ReadInto(buf []float32)
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

	// source is the received-voice bus, nil until one is registered. It is
	// control-plane state, not per-generation state, so dspLoop reads it
	// live through this atomic exactly as it reads sinks -- the "take it as
	// a parameter" rule above applies to the rings, the voicePool and the
	// tick, which a later Start() REPLACES; a Source survives a
	// Stop()/Start() cycle by design, so an abandoned generation reading
	// the current one is the intended behaviour rather than a crossing of
	// generations.
	source atomic.Pointer[Source]

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

	// inputTargetID/outputTargetID record the RESOLVED device id the LAST
	// open ATTEMPT was made against; inputAttemptCfg/outputAttemptCfg record
	// the CONFIGURED id (cfg.InputDevice/cfg.OutputDevice, "" meaning follow
	// the system default) that attempt was made under. Both differ from
	// inputID/outputID, which record the device currently OPEN.
	//
	// They exist so closeSupersededStreams can tell three situations apart
	// while no stream is open -- which is precisely when a backoff is in
	// force, and precisely the case the old reset (which required an open
	// stream) never ran for:
	//
	//  1. The CONFIGURED id moved: a deliberate user action in Settings.
	//     The accumulated backoff belongs to a device nobody is asking for
	//     any more, so it is cleared outright.
	//  2. The configured id is unchanged but the RESOLVED target went from
	//     nothing to a real device: System Default plus a hot-plug. The
	//     retry is made due immediately, but the accumulated backoff is
	//     KEPT -- see closeSupersededStreams for why that asymmetry is the
	//     whole design.
	//  3. The configured id is unchanged and the resolved target merely
	//     moved between two real devices: a flapping enumeration, not a
	//     user decision. Nothing is reset.
	//
	// inputID/outputID cannot stand in for the target: a direction that has
	// never successfully opened leaves those empty, so the very scenario the
	// reset is for -- a device that keeps failing -- is the one they carry
	// no information about. And the target cannot stand in for the
	// configured id either: under System Default cfg.InputDevice stays ""
	// across a hot-plug, so only the pair distinguishes (1) from (2)/(3).
	// All four are written under mu at every attempt site (Start's publish,
	// maybeReopenCapture/maybeReopenPlayback) and read under mu in
	// closeSupersededStreams.
	inputTargetID, outputTargetID     string
	inputAttemptCfg, outputAttemptCfg string

	// lastState is the last snapshot handed to opts.OnState, so
	// emitStateIfChanged can suppress identical repeats. Nil until the
	// first emission; State is all-comparable by construction, so this is
	// a plain == against a value, not a field-by-field diff that would
	// silently miss a field added later.
	lastState *State

	// stopDSP/stopPoll/dspDone/pollDone are written ONLY inside the same mu
	// critical section that sets running = true in Start, and read only
	// under mu in Stop -- see Start's doc for why this matters (Fix 1).
	stopDSP, stopPoll chan struct{}
	dspDone, pollDone chan struct{}

	// dspTick, when non-nil, replaces dspLoop's internal time.Ticker so a
	// test can advance the loop exactly one tick at a time. Production
	// never sets it: NewManager leaves it nil and dspLoop builds a real
	// FrameDuration ticker.
	//
	// It exists because dspLoop's catch-up behaviour (see maxCatchUpFrames)
	// is defined in terms of "what one tick does", and a real ticker can
	// only be observed by sleeping -- which turns a correctness assertion
	// about backlog absorption into a race between the test and the
	// scheduler, on the one path in this package where a flaky test would
	// be worse than no test.
	//
	// It is read ONCE, in Start, and handed to dspLoop as a PARAMETER --
	// dspLoop never reads this field. That is not fussiness: dspLoop's doc
	// explains at length why every piece of per-generation state it touches
	// is a parameter (an abandoned generation must never be able to reach
	// the next generation's fields), and an exception to that rule is worth
	// more as a grep-clean invariant than as one saved argument. It is only
	// ever set before Start, so Start's single read races nothing and the
	// goroutine-creation edge publishes it.
	dspTick <-chan time.Time
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

// SetSource registers (or, with nil, clears) the received-voice bus. Like
// AddSink it is Manager-lifetime and survives a Stop()/Start() cycle: the
// Phase 5 wiring registers exactly one Source at startup whose own session
// pointer goes nil on disconnect, rather than registering and clearing one
// per connection.
//
// A single slot rather than AddSink's copy-on-write slice because there is
// exactly one received-voice bus and the mixer has exactly one input for
// it; two Sources would need a summing order and a shared scratch buffer
// that ReadInto's no-allocation contract has nowhere to put.
func (m *Manager) SetSource(s Source) {
	if s == nil {
		m.source.Store(nil)
		return
	}
	m.source.Store(&s)
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

// EffectIDs returns the SFX manifest's slot ids, in the same stable display
// order SFX.EffectIDs() does -- the backend's single source of truth for
// the Radio Effects panel's row set, so the frontend has no reason to keep
// its own copy of the manifest. m.sfx is immutable after NewManager (see
// Manager's own field-ownership doc), so this needs no lock.
func (m *Manager) EffectIDs() []string { return m.sfx.EffectIDs() }

// EffectLabel returns one effect slot's display label (or the id itself if
// unknown), delegating to the same SFX manifest PreviewEffect/PlayEffect
// already use for the sample lookup.
func (m *Manager) EffectLabel(id string) string { return m.sfx.Label(id) }

// EffectAvailable reports whether a decoded sample currently backs the
// given effect slot -- false for every slot until the SFX sample pack
// lands (internal/audio/assets/README.md), which is the true, unfaked
// answer today.
func (m *Manager) EffectAvailable(id string) bool { return m.sfx.Available(id) }

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

// emitState pushes the current snapshot unconditionally. Used by Start and
// Stop, where the transition itself is the news even if the snapshot happens
// to match the previous one.
func (m *Manager) emitState() {
	if m.opts.OnState == nil {
		return
	}
	st := m.State()
	m.mu.Lock()
	s := st
	m.lastState = &s
	m.mu.Unlock()
	m.opts.OnState(st)
}

// emitStateIfChanged pushes the current snapshot only when it differs from
// the last one emitted.
//
// This is what makes the health surface reachable at all between Start and
// Stop. Those two were previously emitState's ONLY call sites besides a
// device-list change, so a reopen failure, a backoff cycle, a recovery, a
// device substitution and every Overruns/Underruns movement were all
// invisible to the frontend -- the counters simply froze at whatever they
// read when the manager started. Calling this at the end of every poll tick
// gives all of them a trigger, and the equality check keeps an idle manager
// from pushing an identical payload every PollInterval forever (the same
// discipline dspLoop's VU suppression already follows).
//
// epoch identifies the generation the caller belongs to: an abandoned
// generation (Stop()'s bounded joins) must not publish over the current
// one, so a stale epoch discards the emit entirely rather than racing
// m.lastState. This mirrors the epoch discipline at every other Manager
// field write-back site in this file -- pollOnce is the only caller, it
// reaches here AFTER Enumerate/OpenCapture/OpenPlayback (any of which can
// be the thing blocked when Stop() gives up on this goroutine), and
// m.lastState is a Manager field write-back like any other. Without the
// check a zombie poll tick could overwrite lastState with a dead
// generation's snapshot -- which then also SUPPRESSES the live
// generation's next identical-to-the-zombie emit, so the damage outlives
// the one bad event.
func (m *Manager) emitStateIfChanged(epoch uint64) {
	if m.opts.OnState == nil {
		return
	}
	st := m.State()
	m.mu.Lock()
	if m.epoch != epoch {
		m.mu.Unlock()
		return
	}
	unchanged := m.lastState != nil && *m.lastState == st
	if !unchanged {
		s := st
		m.lastState = &s
	}
	m.mu.Unlock()
	if !unchanged {
		m.opts.OnState(st)
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
	// Start's OpenCapture/OpenPlayback above ARE open attempts, so they
	// stamp the target and the configured id they were made under too --
	// otherwise a Start whose open failed would leave both empty and the
	// first device change after it would have nothing to compare against
	// (see the inputTargetID field doc).
	m.inputTargetID, m.outputTargetID = inID, outID
	m.inputAttemptCfg, m.outputAttemptCfg = cfg.InputDevice, cfg.OutputDevice
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
	// See Manager.dspTick: nil in production, and only ever set before
	// Start, so reading it here (and never inside dspLoop) keeps the
	// "per-generation state is a parameter" rule exceptionless.
	go m.dspLoop(denoiser, stopDSP, dspDone, captureRing, playbackRing, sfxVoices, m.dspTick)
	go m.pollLoop(stopPoll, pollDone, captureRing, playbackRing, epoch)

	m.emitState()
	return nil
}

// Stop tears the manager down. It is idempotent and orders teardown as:
// stop DSP goroutine -> stop poll goroutine -> stop streams. The poll
// goroutine must be joined before the streams are read/stopped: otherwise a
// concurrent bounded-backoff reopen could store a freshly-opened stream into
// captureStream/playbackStream after Stop has already snapshotted (and is
// about to discard) the old one, leaking it.
//
// Stop deliberately does NOT call Backend.Close(). The Manager does not OWN
// its Backend -- it is injected by whoever constructed it (main.go), and
// that constructor is the only party that knows when the process is really
// done with it. Closing here was a genuine bug on two levels:
//
//   - Backend.Close() is terminal, not a pause. The malgo backend Uninits
//     and Frees its miniaudio context and nils the pointer
//     (backend_malgo.go), so a later Start() -> Enumerate() nil-derefs.
//     Every other part of this type treats Stop/Start as a supported cycle
//     (the whole epoch/generation machinery exists for exactly that), so a
//     restart panic was reachable by design, not by misuse.
//
//   - It was a fifth instance of the abandoned-generation class the epoch
//     discipline exists to close. The bounded joins above can ABANDON the
//     poll goroutine while it is still parked inside
//     Backend.OpenCapture/OpenPlayback; Freeing the context out from under
//     a goroutine that is inside ma_device_init on it is a C-level
//     use-after-free, and the `b.ctx = nil` that followed raced that
//     goroutine's read of it. Epoch checks guard Manager FIELD write-backs;
//     the Backend is not a field write, it is the shared resource itself,
//     so no epoch check could ever have covered it.
//
//     Not closing a resource we do not own removes the hazard FROM THIS
//     FUNCTION. It does not remove it from the process. main.go still does
//     `defer backend.Close()` before `defer am.Stop()`, so the backend is
//     closed AFTER Stop() returns -- and Stop()'s joins below are BOUNDED,
//     so they can return while dspLoop is still running. A slow
//     Sink.WriteFrame is exactly what makes that happen, and Phase 5
//     registers the first Sink that can be slow (it writes to a socket).
//     The ordering in main.go is therefore load-bearing and is pinned by
//     TestMainWiringClosesBackendAfterManagerStop. If you reorder those
//     defers, an abandoned dspLoop can touch a freed malgo context.
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
// producer for the playback ring. It owns AGC, Gate, Mixer, the monitor
// Effect and the Denoiser exclusively: nothing else in this file touches
// them, so there is nothing to synchronise here beyond the atomic reads of
// cfg/ptt/muted/sinks/source that cross in from the control plane.
//
// stopDSP/dspDone/captureRing/playbackRing/sfxVoices/dspTick are
// PARAMETERS, not read from the Manager's own fields, and that is
// load-bearing (Fix A):
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
func (m *Manager) dspLoop(denoiser *Denoiser, stopDSP, dspDone chan struct{}, captureRing, playbackRing *Ring, sfxVoices *voicePool, dspTick <-chan time.Time) {
	defer close(dspDone)
	defer denoiser.Close()

	agc := NewAGC()
	gate := NewGate(GateConfig{})
	mixer := NewMixer()
	// monitorEffect colours the LOCAL MONITOR ONLY. Phase 5 moved the radio
	// effect off the transmit path (design decision D7); see the sink loop
	// below for why. Every RECEIVED stream carries its own instance in
	// internal/voice/rx.go, because a biquad's state is per-signal and one
	// shared filter fed alternating talkers would smear each into the next.
	monitorEffect := NewEffect("", "")
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
	// rxBuf is the received-voice bus. It is filled once per TICK (not once
	// per catch-up frame like monitorBuf): received audio is paced by the
	// remote senders and by the RX path's own jitter buffers, not by how far
	// behind our capture ring happens to be, so pulling it more than once on
	// a catch-up tick would run every talker fast.
	rxBuf := make([]float32, FrameSamples)
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

	// maxCatchUpFrames bounds how much backlog one tick may absorb. A
	// time.Ticker coalesces missed ticks into one, so after a scheduling
	// stall the ring holds several frames and a strict one-frame-per-tick
	// read can NEVER recover -- the backlog is permanent latency until
	// Drain() dumps it in one audible ~160 ms jump. Draining a bounded
	// number of extra frames per tick lets latency decay smoothly instead.
	// The bound exists so a pathological stall cannot turn one tick into an
	// unbounded burst of encode work on this goroutine.
	const maxCatchUpFrames = 3

	ticker := time.NewTicker(FrameDuration)
	defer ticker.Stop()
	// See Manager.dspTick: production always takes the real ticker; only a
	// test ever substitutes a hand-driven channel, which Start captured and
	// passed in as the dspTick parameter.
	tickC := ticker.C
	if dspTick != nil {
		tickC = dspTick
	}

	for {
		select {
		case <-stopDSP:
			return
		case <-tickC:
		}

		cfg := m.cfg.Load()
		if cfg != lastCfg {
			lastCfg = cfg
			gate.SetConfig(cfg.Gate)
			mixer.SetLevels(cfg.Levels)
			if vID, cID := monitorEffect.IDs(); vID != cfg.VoiceEffect || cID != cfg.ClippingEffect {
				monitorEffect = NewEffect(cfg.VoiceEffect, cfg.ClippingEffect)
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

		// The CAPTURE side catches up; the PLAYBACK side below does not.
		// That asymmetry is the whole point. Capture backlog is ours to
		// absorb -- nothing downstream is paced by our read rate, and a
		// frame we leave in the ring is latency we have chosen to keep.
		// Playback is paced by the OUTPUT DEVICE, which pulls from
		// playbackRing on its own callback schedule; writing two frames on
		// one tick because capture was behind would run playback fast and
		// overrun the ring, an artefact no listener can un-hear. So: read
		// at least one frame (a short/empty read still runs the chain on a
		// zero-filled frame, exactly as before, so the gate, VOX and VU
		// keep advancing on the fixed 10 ms grid), and at most
		// 1+maxCatchUpFrames.
		//
		// NOTE for the reader comparing this against task-2's brief: the
		// brief's prose ("process every whole frame the ring currently
		// holds, up to a bounded catch-up limit") and the formula it
		// sketched (`1 + min(Available()/FrameSamples, maxCatchUpFrames)`)
		// disagree, and the prose is the correct one. The sketch adds one
		// to a count that is ALREADY the number of whole frames present,
		// so a ring holding exactly one frame would be read twice: once for
		// real and once into an underrun, injecting a silent frame into
		// every Sink. That is precisely the bursty, spurious-packet TX
		// cadence this fix exists to avoid, so the clamp below is
		// max(1, min(present, 1+maxCatchUpFrames)) instead.
		frames := captureRing.Available() / FrameSamples
		if frames > 1+maxCatchUpFrames {
			frames = 1 + maxCatchUpFrames
		}
		if frames < 1 {
			frames = 1
		}

		for f := 0; f < frames; f++ {
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
			if p := peak(inFrame); p > vuIn {
				vuIn = p
			}

			// The radio effect used to run here, before the sinks -- which
			// meant we TRANSMITTED pre-effected audio. The C# peer applies
			// effects on RECEIVE, so that left a C# listener hearing us
			// double-effected while we heard them dry, and made the "no
			// effects on global frequencies" rule unimplementable from the
			// sending side (we cannot know each listener's frequency).
			// Effects now live on the RX path, one instance per stream, in
			// internal/voice/rx.go. monitorEffect below colours ONLY the
			// local monitor, so self-monitoring still sounds like the radio.
			//
			// vuIn above therefore now measures the signal we actually send
			// rather than the effected copy, which is what an INPUT meter
			// should have read all along.
			if gateOpen {
				sinks := *m.sinks.Load()
				for _, s := range sinks {
					s.WriteFrame(inFrame)
				}
			}
			// monitorBuf is written once per CAPTURE frame but consumed
			// once per TICK, so during a catch-up the last frame of the
			// burst is the one that reaches the monitor bus. That is the
			// right trade: mic passthrough is a local sidetone, and there
			// is exactly one playback slot to put it in -- the alternative
			// would be to mix the burst together, which is a comb filter,
			// or to drop the sidetone entirely, which is worse than
			// shortening it by a few milliseconds.
			if gateOpen && cfg.MicPassthrough {
				copy(monitorBuf, inFrame)
				monitorEffect.Process(monitorBuf)
			} else {
				clear(monitorBuf)
			}
		}

		// The Source OVERWRITES rxBuf (see the interface's contract), so
		// there is nothing to clear first; the nil branch has to, because
		// last tick's received audio is still sitting in it.
		if src := m.source.Load(); src != nil {
			(*src).ReadInto(rxBuf)
		} else {
			clear(rxBuf)
		}

		clear(sfxBuf)
		sfxVoices.mixInto(sfxBuf, m.sfx.sampleFor)

		mixer.Mix(outFrame, monitorBuf, rxBuf, sfxBuf, notifBuf)
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
	// Must run BEFORE the reopen pair: it is what nils a stream whose
	// device is no longer the right one, which is the only condition
	// maybeReopen* acts on.
	m.closeSupersededStreams(cfg, inputs, outputs, epoch)
	m.maybeReopenCapture(cfg, inputs, now, captureRing, epoch)
	m.maybeReopenPlayback(cfg, outputs, now, playbackRing, epoch)

	m.emitStateIfChanged(epoch)
}

// closeSupersededStreams stops and nils any open stream whose device is no
// longer the one this generation should be using, leaving the reopen pair
// below to bring the correct device up on this very same tick.
//
// It is the trigger for two behaviours the rest of the machinery could
// already perform but was never asked to:
//
//   - A DEVICE-SELECTION CHANGE (design DoD 18.2, "selecting one takes
//     effect without restarting the app"). Start() was the only place
//     cfg.InputDevice/OutputDevice were ever resolved into an open stream,
//     and maybeReopen* only fires on a nil stream -- so SetConfig stored a
//     new id and absolutely nothing reopened. Changing the microphone in
//     Settings did nothing, ever, until the next process launch.
//   - DEVICE LOSS WHILE OPEN (design spec 8 and 13, DoD 18.3). pollOnce
//     diffed the enumeration and emitted audio:devices_changed, and that
//     was all: nothing observed that the OPEN device had vanished from the
//     list, so there was no fallback and no error -- the device just went
//     quiet.
//
// Both reduce to ONE comparison, which is why they share an implementation:
// resolveDevice(cfg.X, list) is by construction the id this generation
// SHOULD be on, and it can only return an id that is currently enumerable.
// So a user picking a different device and the open device disappearing
// from the enumeration are the same event seen from two sides -- in either
// case the resolved id stops matching m.inputID/m.outputID.
//
// Epoch discipline matches every other write-back site in this file: the
// check and the writes happen in ONE mu critical section, so an abandoned
// generation can never tear down the current generation's streams. The
// Stop() calls themselves happen after the unlock (Stop can block on the
// OS) -- safe, because nilling the field under mu already took exclusive
// ownership of those pointers; nobody else can reach them any more.
func (m *Manager) closeSupersededStreams(cfg Config, inputs, outputs []DeviceInfo, epoch uint64) {
	m.mu.Lock()
	if m.epoch != epoch {
		m.mu.Unlock()
		return
	}
	var capStream, playStream Stream
	wantIn := resolveDevice(cfg.InputDevice, inputs)
	if m.captureStream != nil && wantIn != m.inputID {
		capStream = m.captureStream
		m.captureStream = nil
		// Clear the backoff: this is a fresh target, not a retry of the
		// failure the current backoff was accumulated for. Without this a
		// direction that had been failing could sit out its (up to 30s)
		// backoff before honouring the user's brand-new selection.
		m.inputBackoff, m.inputRetryAt = 0, time.Time{}
	} else if m.captureStream == nil {
		// No stream to close. This is the only case in which a backoff is
		// actually in force -- a stream that is open by definition has none
		// -- so gating the reset above on `m.captureStream != nil` made it
		// a no-op for every situation it was written to fix.
		//
		// Two DIFFERENT signals reach this branch and they need different
		// answers (see the inputTargetID field doc for the three cases):
		switch {
		case cfg.InputDevice != m.inputAttemptCfg:
			// (1) The user changed the selection in Settings. The backoff
			// was accumulated against a device nobody is asking for any
			// more, so it is irrelevant in full: clear it, and retry on
			// this very tick. Otherwise a user whose microphone kept
			// failing would pick a working one and then hear nothing for
			// up to 30 s, with no indication anything was pending.
			m.inputBackoff, m.inputRetryAt = 0, time.Time{}
		case m.inputTargetID == "" && wantIn != "":
			// (2) The selection is unchanged (typically System Default, so
			// cfg.InputDevice is "" on both sides of a hot-plug) but the
			// last attempt had NOTHING to aim at -- resolveDevice returns
			// "" when nothing enumerates -- and a device has since
			// appeared. Boot with the USB headset unplugged, plug it in a
			// minute later: without this the retry sits out a backoff that
			// escalated while there was no hardware to open at all.
			//
			// Only the DUE TIME is cleared; m.inputBackoff is deliberately
			// KEPT. Clearing the backoff here too is what would reopen the
			// hole this case sits next to: a device that flaps in and out
			// of the enumeration drives the target between "" and a real
			// id, so a full reset would fire on every flap and pin the
			// retry rate at reopenBackoffInitial forever. Per
			// reopenBackoffInitial/Max's doc, a pinned retry rate is a
			// correspondingly faster leak of the per-open C allocation
			// malgo never frees, so the rate must keep decaying even while
			// hardware is flapping.
			//
			// RESIDUAL, accepted deliberately: a hot-plug gets exactly ONE
			// prompt attempt. If the newly appeared device also fails to
			// open, the next retry is nextBackoff(the accumulated value) --
			// up to reopenBackoffMax -- rather than a fresh 1 s. The normal
			// case (the device opens) resets the backoff to 0 on success,
			// so the user-visible behaviour is "plug in, mic works within a
			// poll"; the slow path is reserved for hardware that appears
			// and still cannot be opened, which is exactly the case the
			// bound exists for.
			m.inputRetryAt = time.Time{}
		}
		// (3) Configured id unchanged and the target merely moved between
		// two real devices: a flapping enumeration, not a user decision.
		// Nothing is reset, so nextBackoff keeps escalating toward
		// reopenBackoffMax.
	}
	wantOut := resolveDevice(cfg.OutputDevice, outputs)
	if m.playbackStream != nil && wantOut != m.outputID {
		playStream = m.playbackStream
		m.playbackStream = nil
		m.outputBackoff, m.outputRetryAt = 0, time.Time{}
	} else if m.playbackStream == nil {
		// Symmetric with capture above -- see that branch for the full
		// reasoning behind the two different answers.
		switch {
		case cfg.OutputDevice != m.outputAttemptCfg:
			m.outputBackoff, m.outputRetryAt = 0, time.Time{}
		case m.outputTargetID == "" && wantOut != "":
			m.outputRetryAt = time.Time{}
		}
	}
	m.mu.Unlock()

	if capStream != nil {
		capStream.Stop()
	}
	if playStream != nil {
		playStream.Stop()
	}
}

func (m *Manager) maybeReopenCapture(cfg Config, inputs []DeviceInfo, now time.Time, captureRing *Ring, epoch uint64) {
	id := resolveDevice(cfg.InputDevice, inputs)

	m.mu.Lock()
	ready := m.captureStream == nil && !now.Before(m.inputRetryAt) && m.epoch == epoch
	if ready {
		// Stamp both the device this attempt is aimed at and the configured
		// id it is being made under, in the SAME critical section that
		// decides to make it. closeSupersededStreams reads the pair on a
		// later tick to tell "the user picked something else" from "the
		// enumeration moved under us" from "nothing changed". Stamping only
		// on SUCCESS would leave them empty for exactly the case that needs
		// them -- a device that never opens at all.
		m.inputTargetID = id
		m.inputAttemptCfg = cfg.InputDevice
	}
	m.mu.Unlock()
	if !ready {
		return
	}

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
	id := resolveDevice(cfg.OutputDevice, outputs)

	m.mu.Lock()
	ready := m.playbackStream == nil && !now.Before(m.outputRetryAt) && m.epoch == epoch
	if ready {
		// Symmetric with maybeReopenCapture: see its comment.
		m.outputTargetID = id
		m.outputAttemptCfg = cfg.OutputDevice
	}
	m.mu.Unlock()
	if !ready {
		return
	}

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
