package joystick

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

// DefaultPollInterval is how often the manager samples device state. DCS-SRS
// ships 40ms; 10ms costs nothing measurable and keeps the added push-to-talk
// latency inaudible.
const DefaultPollInterval = 10 * time.Millisecond

// DefaultRediscoverInterval is how often the manager re-enumerates devices so
// a stick plugged in after launch starts working without a restart.
const DefaultRediscoverInterval = 3 * time.Second

// Handler receives joystick activity. Implementations must not block.
//
// Structurally identical to hotkeys.Handler, and deliberately NOT that type:
// the two packages are siblings and neither should depend on the other. Go
// interfaces are structural, so one application type satisfies both.
type Handler interface {
	Pressed(actionID string)
	Released(actionID string)
}

// Manager owns the poll loop and the current joystick binding set.
//
// It deliberately has no stale-latch watchdog, unlike internal/hotkeys.
// Polling makes that failure mode self-healing: a device that disappears
// mid-transmission reads as all-buttons-up on the next poll and releases
// naturally, which covers unplug, sleep and driver reset alike and is
// strictly better than a timeout.
type Manager struct {
	mu sync.Mutex

	// notifyMu serialises every "mutate active state under mu -> unlock mu ->
	// call Handler" span across tick(), Apply(), Suspend() and Close(). It is
	// what makes the edges balanced under concurrency.
	//
	// WHY A SECOND LOCK. Each of those four methods, taken alone, is race
	// free: every shared field is read and written under mu. But each of
	// them COMMITS its state change (mu held) and then NOTIFIES the Handler
	// (mu released) as two separate steps, and two different goroutines can
	// run those steps interleaved: tick() can set m.active[id] = true, unlock
	// mu, and be preempted before it calls h.Pressed(id); in that window
	// Suspend() can acquire mu, see id already in active, take it via
	// takeActiveLocked, unlock, and call h.Released(id) -- so the Handler
	// observes Released BEFORE the Pressed that logically preceded it. -race
	// cannot see this: mu itself is never touched by two goroutines at once.
	// It is an ordering race between two independently-unlocked
	// commit-then-notify sequences, not a data race.
	//
	// notifyMu is held across the WHOLE span, including the Handler calls,
	// in each of the four methods, so only one such span can be in flight at
	// a time and Handler always sees the edges in true commit order. It is
	// deliberately a separate lock from mu: Devices(), LastErr() and
	// Supported() only ever need mu, and must not be blocked behind a slow
	// Handler.
	notifyMu sync.Mutex

	src Source
	h   Handler
	log *slog.Logger

	desired map[string][]Binding
	hold    map[string]bool // action ID -> owes a Released
	active  map[string]bool // action IDs currently held

	suspended bool
	closed    bool
	supported bool
	lastErr   error

	devices []Device

	// capture is the in-flight binding capture, if any. Touched only under
	// mu -- see capture.go. It deliberately never interacts with notifyMu:
	// nothing in the capture path reads or writes m.active/m.hold, so it
	// carries none of the Pressed/Released ordering risk notifyMu exists to
	// prevent.
	capture *captureState

	PollInterval       time.Duration
	RediscoverInterval time.Duration

	stop    chan struct{}
	done    chan struct{}
	started bool
}

// New constructs a Manager over a Source.
func New(src Source, h Handler, log *slog.Logger) *Manager {
	m := &Manager{
		src:                src,
		h:                  h,
		log:                log,
		desired:            map[string][]Binding{},
		hold:               map[string]bool{},
		active:             map[string]bool{},
		supported:          true,
		PollInterval:       DefaultPollInterval,
		RediscoverInterval: DefaultRediscoverInterval,
		stop:               make(chan struct{}),
		done:               make(chan struct{}),
	}
	// Probe once so Supported() is meaningful before the loop starts and the
	// UI can hide the affordance rather than showing a broken one. ANY error
	// here is recorded in lastErr -- including a transient enumeration
	// failure that is not ErrUnsupported -- so LastErr() is never silently
	// nil after a failed probe; only ErrUnsupported also flips supported to
	// false, since that is the one case the UI must not offer as retryable.
	if _, err := src.Devices(); err != nil {
		m.lastErr = err
		if errors.Is(err, ErrUnsupported) {
			m.supported = false
		}
	}
	return m
}

// Supported reports whether this platform has a real backend.
func (m *Manager) Supported() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.supported
}

// LastErr returns the most recent source error, if any.
func (m *Manager) LastErr() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

// Devices returns the most recently enumerated device list.
func (m *Manager) Devices() []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Device(nil), m.devices...)
}

// Apply replaces the desired binding set. Any action that was held and is no
// longer bound to what is currently held is released first, so the edge
// balance the app-level refcount depends on is never broken by a rebind.
func (m *Manager) Apply(binds map[string][]Binding) error {
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()

	m.mu.Lock()
	next := make(map[string][]Binding, len(binds))
	hold := make(map[string]bool, len(binds))
	for id, list := range binds {
		next[id] = append([]Binding(nil), list...)
		for _, b := range list {
			if b.Hold {
				hold[id] = true
			}
		}
	}
	m.desired = next
	// Release everything currently held: the next tick re-presses whatever is
	// still legitimately active under the NEW table. Releasing then
	// re-pressing is correct and cheap; trying to diff old against new here
	// is where an unbalanced edge would hide.
	release := m.takeActiveLocked()
	m.hold = hold
	m.mu.Unlock()

	m.emitReleases(release)
	return nil
}

// Suspend stops driving handlers and releases anything held, so a capture
// cannot transmit while the user is binding a button.
func (m *Manager) Suspend() {
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()

	m.mu.Lock()
	if m.suspended {
		m.mu.Unlock()
		return
	}
	m.suspended = true
	release := m.takeActiveLocked()
	m.mu.Unlock()

	m.emitReleases(release)
}

// Resume re-enables handler dispatch. The next tick re-presses whatever is
// genuinely held.
func (m *Manager) Resume() {
	m.mu.Lock()
	m.suspended = false
	m.mu.Unlock()
}

// IsSuspendedForTest reports whether the manager is currently suspended. A
// read-only accessor for tests -- it changes no behaviour. Named distinctly
// from hotkeys.Manager.Suspended so the two are never confused across the two
// sibling packages in a test that wires both.
func (m *Manager) IsSuspendedForTest() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.suspended
}

// Start begins the poll loop. Safe to call once; later calls are no-ops.
func (m *Manager) Start() {
	m.mu.Lock()
	if m.started || m.closed || !m.supported {
		m.mu.Unlock()
		return
	}
	m.started = true
	poll, rediscover := m.PollInterval, m.RediscoverInterval
	m.mu.Unlock()

	go m.loop(poll, rediscover)
}

// Close stops the loop, releases anything held and closes the source. Safe to
// call more than once.
func (m *Manager) Close() {
	// notifyMu is released BEFORE waiting on <-m.done below: holding it
	// across that wait would deadlock against a concurrently-running tick()
	// that is blocked acquiring notifyMu and can never reach the m.closed
	// check that lets it return.
	m.notifyMu.Lock()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		m.notifyMu.Unlock()
		return
	}
	m.closed = true
	started := m.started
	release := m.takeActiveLocked()
	m.mu.Unlock()

	m.emitReleases(release)
	m.notifyMu.Unlock()

	if started {
		close(m.stop)
		<-m.done
	}
	m.src.Close()
}

func (m *Manager) loop(poll, rediscover time.Duration) {
	defer close(m.done)
	pollTick := time.NewTicker(poll)
	defer pollTick.Stop()
	discoverTick := time.NewTicker(rediscover)
	defer discoverTick.Stop()

	m.rediscover()
	for {
		select {
		case <-m.stop:
			return
		case <-pollTick.C:
			m.tick()
		case <-discoverTick.C:
			m.rediscover()
		}
	}
}

// rediscover re-enumerates devices so hot-plugged hardware starts working.
func (m *Manager) rediscover() {
	devs, err := m.src.Devices()
	m.mu.Lock()
	if err != nil {
		m.lastErr = err
	} else {
		m.devices = devs
	}
	m.mu.Unlock()
}

// tick samples the source once and drives the resulting edges. It is the
// whole behavioural core, and the tests call it directly rather than waiting
// on the ticker.
func (m *Manager) tick() {
	m.mu.Lock()
	if m.suspended || m.closed || !m.supported {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	state, err := m.src.Poll()

	// A capture owns the device while it is armed: pressing a button to bind
	// it must never also fire the action being bound. The app layer is still
	// expected to call Suspend() around a capture session, per spec section
	// 9 -- that is what stops OTHER, already-suspended actions from
	// transmitting elsewhere while the dialog is open. This check, and the
	// force-release BeginCapture does on arming (see capture.go), are
	// defence in depth on top of that, not a replacement for it: they make
	// capture itself correct -- no suppressed dispatch stranding an action
	// mid-hold, no suppressed dispatch swallowing the action being bound --
	// even in the window before Suspend() is called or if the app layer
	// forgets it entirely.
	//
	// This runs BEFORE notifyMu is acquired below, and returns without ever
	// touching notifyMu. That is deliberate, not merely "not yet needed":
	// capture's completion callback commonly calls straight back into the
	// Manager on this same goroutine (e.g. Apply, to persist the newly
	// captured binding immediately). notifyMu is not reentrant, and this
	// callback runs on the very goroutine that would be holding it if the
	// check were moved after the Lock below -- a reentrant Apply/Suspend/
	// Close from inside the callback would then deadlock the poll loop
	// against itself. feedCapture only ever takes m.mu (see capture.go), and
	// capture never reads or writes m.active/m.hold, so skipping notifyMu
	// here does not reopen the Pressed/Released ordering bug notifyMu exists
	// to close.
	if err == nil && m.capturing() {
		if done, result, ok := m.feedCapture(state); ok && done != nil {
			done(result)
		}
		return
	}

	// notifyMu is acquired for the rest of this call, INCLUDING the Handler
	// calls at the bottom: this is the commit-then-notify span that must not
	// interleave with Suspend()/Apply()/Close() doing the same. See the
	// notifyMu field doc for why a data-race-free mu is not enough on its
	// own.
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()

	if err != nil {
		// A failing poll means we can no longer prove anything is down.
		// Release everything: a stuck-open microphone is the worst outcome
		// available here.
		m.mu.Lock()
		m.lastErr = err
		release := m.takeActiveLocked()
		m.mu.Unlock()
		m.emitReleases(release)
		return
	}

	m.mu.Lock()
	if m.suspended || m.closed {
		m.mu.Unlock()
		return
	}
	want := Active(m.desired, state)

	var press []string
	var release []string
	for id := range want {
		if !m.active[id] {
			m.active[id] = true
			press = append(press, id)
		}
	}
	for id := range m.active {
		if !want[id] {
			delete(m.active, id)
			if m.hold[id] {
				release = append(release, id)
			}
		}
	}
	binds := m.desired
	m.mu.Unlock()

	// Handler calls happen outside the lock: Handler is documented as
	// non-blocking, but it reaches application code and the poll loop must
	// not be able to stall behind it.
	for _, id := range press {
		m.logEdge(id, binds[id], state)
		m.h.Pressed(id)
	}
	for _, id := range release {
		m.h.Released(id)
	}
}

// takeActiveLocked empties the active set and returns the HOLD actions that
// owe a Released. Caller holds m.mu.
func (m *Manager) takeActiveLocked() []string {
	var out []string
	for id := range m.active {
		if m.hold[id] {
			out = append(out, id)
		}
		delete(m.active, id)
	}
	return out
}

func (m *Manager) emitReleases(ids []string) {
	for _, id := range ids {
		m.h.Released(id)
	}
}

// logEdge records which physical input fired an action.
//
// Unlike internal/hotkeys, this DOES name the input. That rule exists there
// because the keyboard listener sees every keystroke on the machine, so a log
// naming keys would be a keylog. A joystick backend sees joystick buttons and
// nothing else, so the same reasoning does not apply -- and naming the button
// is what makes "my HOTAS bind does nothing" diagnosable at all.
func (m *Manager) logEdge(actionID string, binds []Binding, s State) {
	for _, b := range binds {
		if !s.IsHeld(b.Joy.Main()) {
			continue
		}
		if b.Joy.Modifier != nil && !s.IsHeld(*b.Joy.Modifier) {
			continue
		}
		attrs := []any{
			"action", actionID,
			"device", string(b.Joy.Device),
			"input", b.Joy.Button.Label(),
		}
		if b.Joy.Modifier != nil {
			attrs = append(attrs,
				"modifier_device", string(b.Joy.Modifier.Device),
				"modifier_input", b.Joy.Modifier.Button.Label())
		}
		m.log.Info("joystick bind fired", attrs...)
		return
	}
}
