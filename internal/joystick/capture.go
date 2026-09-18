package joystick

import (
	"sort"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// Captured is the result of a completed capture.
type Captured struct {
	Binding trigger.JoyBinding
}

// captureState tracks one in-flight capture.
//
// Capture works by DELTA against a baseline taken when it begins, so a button
// already down when the user opened the dialog cannot register -- otherwise
// someone holding push-to-talk while opening settings would bind it instantly.
type captureState struct {
	// baseline is everything held when capture began. Ignored throughout.
	baseline map[trigger.JoyButton]struct{}
	// order is the newly-pressed inputs in the order they first appeared.
	order []trigger.JoyButton
	// seen dedupes order.
	seen map[trigger.JoyButton]struct{}
	// done is the caller's completion callback.
	done func(Captured)
}

// BeginCapture arms capture. The callback fires once, on release, on the poll
// goroutine. Arming a capture while one is in flight replaces it.
func (m *Manager) BeginCapture(done func(Captured)) {
	state, err := m.src.Poll()
	baseline := map[trigger.JoyButton]struct{}{}
	if err == nil {
		for b := range state.Held {
			baseline[b] = struct{}{}
		}
	}

	m.mu.Lock()
	m.capture = &captureState{
		baseline: baseline,
		seen:     map[trigger.JoyButton]struct{}{},
		done:     done,
	}
	m.mu.Unlock()
}

// CancelCapture disarms capture without completing it.
func (m *Manager) CancelCapture() {
	m.mu.Lock()
	m.capture = nil
	m.mu.Unlock()
}

// capturing reports whether a capture is armed.
func (m *Manager) capturing() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capture != nil
}

// feedCapture advances an in-flight capture with one poll sample and returns
// the completion callback plus its result when the capture finished.
//
// Completion is ON RELEASE: it needs the full hold order to tell a modifier
// from a main input, and that is only known once the user lets go. Harmless
// in a binding dialog, where nothing is transmitting.
//
// feedCapture only ever takes m.mu, never m.notifyMu -- see the call site in
// tick() for why that matters.
func (m *Manager) feedCapture(s State) (func(Captured), Captured, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.capture
	if c == nil {
		return nil, Captured{}, false
	}

	// Anything in the baseline that has been released stops being ignored,
	// so a button held at the start can still be bound on its NEXT press.
	for b := range c.baseline {
		if !s.IsHeld(b) {
			delete(c.baseline, b)
		}
	}

	// Collect newly-held inputs. Within one sample the order is unknowable,
	// so sort by (Device, Button) to keep capture deterministic rather than
	// dependent on poll alignment or map iteration order.
	var fresh []trigger.JoyButton
	for b := range s.Held {
		if _, ignored := c.baseline[b]; ignored {
			continue
		}
		if _, dup := c.seen[b]; dup {
			continue
		}
		fresh = append(fresh, b)
	}
	sort.Slice(fresh, func(i, j int) bool {
		if fresh[i].Device != fresh[j].Device {
			return fresh[i].Device < fresh[j].Device
		}
		return fresh[i].Button < fresh[j].Button
	})
	for _, b := range fresh {
		c.seen[b] = struct{}{}
		c.order = append(c.order, b)
	}

	if len(c.order) == 0 {
		return nil, Captured{}, false // nothing pressed yet
	}
	// Still holding something we captured: wait for release.
	for _, b := range c.order {
		if s.IsHeld(b) {
			return nil, Captured{}, false
		}
	}

	// Released. First-held is the modifier, last-pressed is the main input;
	// anything in between is ignored (spec section 9).
	main := c.order[len(c.order)-1]
	binding := trigger.JoyBinding{Device: main.Device, Button: main.Button}
	if len(c.order) > 1 {
		mod := c.order[0]
		binding.Modifier = &mod
	}
	done := c.done
	m.capture = nil
	return done, Captured{Binding: binding}, true
}
