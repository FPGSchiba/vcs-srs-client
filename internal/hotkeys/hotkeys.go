// Package hotkeys registers global OS hotkeys. It deliberately knows nothing
// about internal/keybinds: callers hand it a map of action ID -> Binding, and it
// registers what it is given. The OS layer sits behind the Registrar interface
// so tests never touch real hotkeys.
package hotkeys

import (
	"fmt"
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// Binding is one registerable hotkey. Hold means the action needs key release
// as well as key press (push-to-talk semantics).
type Binding struct {
	Chord chord.Chord
	Hold  bool
}

// Handler receives hotkey activity. Implementations must not block.
type Handler interface {
	Pressed(actionID string)
	Released(actionID string)
}

// Registrar is the OS seam.
type Registrar interface {
	Register(actionID string, c chord.Chord, hold bool, h Handler) error
	UnregisterAll()
}

// Manager owns the current registration set and the suspend/resume state.
type Manager struct {
	mu        sync.Mutex
	reg       Registrar
	handler   Handler
	desired   map[string]Binding
	suspended bool
	lastErr   error
	failed    map[string]error
}

// New constructs a Manager over a Registrar.
func New(r Registrar, h Handler) *Manager {
	return &Manager{reg: r, handler: h, desired: map[string]Binding{}, failed: map[string]error{}}
}

// Apply replaces the desired binding set and re-registers. While suspended it
// records the set without touching the OS; Resume registers it.
func (m *Manager) Apply(binds map[string]Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.desired = make(map[string]Binding, len(binds))
	for k, v := range binds {
		m.desired[k] = v
	}
	if m.suspended {
		return nil
	}
	return m.registerLocked()
}

// registerLocked clears and re-registers the desired set. Caller holds m.mu.
func (m *Manager) registerLocked() error {
	m.reg.UnregisterAll()
	m.lastErr = nil
	m.failed = map[string]error{}
	var firstErr error
	for id, b := range m.desired {
		if b.Chord.IsZero() {
			continue // unbound action
		}
		if err := m.reg.Register(id, b.Chord, b.Hold, m.handler); err != nil {
			wrapped := fmt.Errorf("register %s (%s): %w", id, b.Chord, err)
			m.failed[id] = wrapped
			if firstErr == nil {
				firstErr = wrapped
			}
		}
	}
	m.lastErr = firstErr
	return firstErr
}

// Suspend releases every OS registration. Used while the UI captures a keypress,
// so a registered hotkey does not swallow the key being rebound. Idempotent.
func (m *Manager) Suspend() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.suspended {
		return
	}
	m.suspended = true
	m.failed = map[string]error{}
	m.reg.UnregisterAll()
}

// Resume re-registers the desired set. Idempotent.
func (m *Manager) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.suspended {
		return nil
	}
	m.suspended = false
	return m.registerLocked()
}

// State is a consistent snapshot of registration health, read under one lock
// so callers never see Registered disagree with Failed.
type State struct {
	Registered bool
	LastError  error
	Failed     map[string]string
}

// Registered reports whether OS hotkey registration is usable AT ALL.
//
// It is false only when nothing is live: there is at least one bound action
// and every one of them failed to register. That is what a missing backend
// (registrar_nocgo.go), a denied macOS Accessibility permission or an
// unreachable X display looks like, and it is the only case the UI's
// "global hotkeys unavailable" banner should describe.
//
// Deliberately NOT false for:
//   - a partial failure. internal/chord accepts keys the OS layer cannot
//     register, so one bad binding among nineteen good ones is normal; those
//     surface per action through Failed(), not as a blanket banner that
//     declares every working hotkey dead.
//   - suspension. Capture releases every registration for a moment (see
//     Suspend); reporting "unavailable" for that window would flash the
//     banner on every rebind. Use Suspended() to observe that state.
func (m *Manager) Registered() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registeredLocked()
}

// registeredLocked is Registered without the lock. Caller holds m.mu.
func (m *Manager) registeredLocked() bool {
	bound := 0
	for _, b := range m.desired {
		if !b.Chord.IsZero() {
			bound++
		}
	}
	return bound == 0 || len(m.failed) < bound
}

// Suspended reports whether registrations are currently released for a UI
// capture. Kept separate from Registered() because suspension is a transient
// internal state, not a user-facing failure.
func (m *Manager) Suspended() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.suspended
}

// State returns Registered, LastError and Failed together under a single
// lock. Reading them through three separate calls can tear -- the banner
// saying "unavailable" while Failed is already empty, or vice versa.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	failed := make(map[string]string, len(m.failed))
	for id, err := range m.failed {
		failed[id] = err.Error()
	}
	return State{Registered: m.registeredLocked(), LastError: m.lastErr, Failed: failed}
}

// LastError returns the most recent registration failure, if any.
func (m *Manager) LastError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

// Failed returns the reason each action's binding could not be registered,
// keyed by action ID. Empty when everything registered cleanly. Callers use
// it to tell the user WHICH keybind did not take effect -- chord accepts keys
// the OS layer cannot register, so a silent failure is otherwise invisible.
func (m *Manager) Failed() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.failed))
	for id, err := range m.failed {
		out[id] = err.Error()
	}
	return out
}
