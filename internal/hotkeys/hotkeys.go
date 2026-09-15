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
}

// New constructs a Manager over a Registrar.
func New(r Registrar, h Handler) *Manager {
	return &Manager{reg: r, handler: h, desired: map[string]Binding{}}
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
	var firstErr error
	for id, b := range m.desired {
		if b.Chord.IsZero() {
			continue // unbound action
		}
		if err := m.reg.Register(id, b.Chord, b.Hold, m.handler); err != nil {
			wrapped := fmt.Errorf("register %s (%s): %w", id, b.Chord, err)
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

// Registered reports whether the desired set is currently live with no errors.
func (m *Manager) Registered() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.suspended && m.lastErr == nil
}

// LastError returns the most recent registration failure, if any.
func (m *Manager) LastError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}
