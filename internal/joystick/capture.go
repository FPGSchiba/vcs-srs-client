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
	//
	// nil is a sentinel, distinct from an empty-but-initialised map: it means
	// BeginCapture could not poll a baseline itself (the poll loop owned
	// Source exclusively) and feedCapture's first call must populate it from
	// that tick's own sample instead. See BeginCapture and feedCapture.
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
//
// The app layer is still expected to call Suspend() around a capture
// session, per spec section 9 -- that is what stops OTHER actions from
// transmitting while the dialog is open. What BeginCapture does here, on top
// of that, is force-release whatever is active RIGHT NOW so a capture is
// correct even if the caller forgets Suspend(): defence in depth, not a
// replacement for it. Without this, an action already held when the dialog
// opens would stay latched active (armed capture suppresses tick()'s
// dispatch entirely, so nothing would ever reconcile it) for the whole
// capture window, and its Released could be lost outright if the physical
// button comes up while still armed.
func (m *Manager) BeginCapture(done func(Captured)) {
	m.mu.Lock()
	started := m.started
	m.mu.Unlock()

	// Source may only ever be called from the poll loop's own goroutine once
	// that loop exists -- see the Source doc comment: the real backends
	// Tasks 8/9 add (vendored DirectInput COM objects, evdev file handles)
	// are not merely "not safe for concurrent calls", some are apartment-
	// threaded and require the SAME goroutine/thread every time. So while
	// the loop is running, BeginCapture must not poll itself: it arms with a
	// nil (pending) baseline instead, and the first feedCapture call --
	// which runs on the poll goroutine, inside tick() -- fills it in from
	// that tick's own sample. That makes "already held" mean "held as of
	// the poll immediately after BeginCapture" rather than "held at the
	// exact BeginCapture call", i.e. correct to within one poll interval,
	// which is the same bound every other observation in this package is
	// already subject to.
	//
	// Before Start() there IS no poll goroutine yet, so polling here directly
	// cannot race anything -- and every test in this package that arms a
	// capture and drives tick() manually relies on exactly that: it is what
	// makes "a button already held at BeginCapture time" precisely knowable
	// with no interval fuzz.
	baseline := map[trigger.JoyButton]struct{}{}
	if !started {
		if state, err := m.src.Poll(); err == nil {
			for b := range state.Held {
				baseline[b] = struct{}{}
			}
		}
	} else {
		baseline = nil // pending; see feedCapture.
	}

	// notifyMu, then mu: the same order Suspend/Apply/Close use. Safe here
	// because BeginCapture is called from the app goroutine, never from
	// inside tick() -- there is no reentrancy risk of the kind that made the
	// capturing()/feedCapture() check in tick() avoid notifyMu.
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()

	m.mu.Lock()
	m.capture = &captureState{
		baseline: baseline,
		seen:     map[trigger.JoyButton]struct{}{},
		done:     done,
	}
	release := m.takeActiveLocked()
	m.mu.Unlock()

	m.emitReleases(release)
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

	if c.baseline == nil {
		// BeginCapture armed this while the poll loop owned Source
		// exclusively and could not poll a baseline itself (see
		// BeginCapture). This tick's own sample becomes the baseline
		// instead, and nothing can be captured on the same sample that
		// establishes the baseline -- otherwise a button already held would
		// look identical to one pressed in this very instant.
		baseline := make(map[trigger.JoyButton]struct{}, len(s.Held))
		for b := range s.Held {
			baseline[b] = struct{}{}
		}
		c.baseline = baseline
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
