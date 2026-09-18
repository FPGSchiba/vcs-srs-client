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
	// UI can hide the affordance rather than showing a broken one.
	if _, err := src.Devices(); errors.Is(err, ErrUnsupported) {
		m.supported = false
		m.lastErr = err
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
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	started := m.started
	release := m.takeActiveLocked()
	m.mu.Unlock()

	m.emitReleases(release)

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
