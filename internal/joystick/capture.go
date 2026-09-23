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
	// It ALWAYS starts nil: BeginCapture never calls Source itself (see its
	// doc comment), so it can never populate this directly. nil is a
	// sentinel, distinct from an empty-but-initialised map: it means
	// feedCapture's first call for this capture must populate it from that
	// tick's own sample instead. See BeginCapture and feedCapture.
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
	// BeginCapture never touches Source, unconditionally -- not even before
	// Start() has been called. Source may ONLY ever be called from the poll
	// loop's own goroutine: see the Source doc comment. The real backends
	// Tasks 8/9 add (vendored DirectInput COM objects, evdev file handles)
	// are not merely "not safe for concurrent calls" -- some require the
	// SAME goroutine/thread on every call (COM apartment threading), which
	// no amount of mutual exclusion from a second goroutine can satisfy.
	//
	// So capture arms with a PENDING baseline (nil), and the first
	// feedCapture call for it -- which only ever runs on the poll goroutine,
	// inside tick() -- fills it in from that tick's own sample. In
	// production the loop is already running by the time any UI can call
	// BeginCapture (Task 11 calls Start() at wiring time), and it polls
	// every PollInterval (10ms by default): the baseline is established
	// within one tick of arming, long before a human can react by pressing
	// anything. "Already held" therefore means "held as of the poll
	// immediately after BeginCapture", not "held at the exact BeginCapture
	// call" -- a bound every other observation in this package already
	// lives with, and the only one obtainable without ever letting a second
	// goroutine touch Source.

	// notifyMu, then mu: the same order Suspend/Apply/Close use. Safe here
	// because BeginCapture is called from the app goroutine, never from
	// inside tick() -- there is no reentrancy risk of the kind that made the
	// capturing()/feedCapture() check in tick() avoid notifyMu.
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()

	m.mu.Lock()
	m.capture = &captureState{
		// baseline starts nil (pending): see the field doc and feedCapture.
		seen: map[trigger.JoyButton]struct{}{},
		done: done,
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
		// BeginCapture never polls Source itself (see its doc comment), so
		// every capture starts pending. This tick's own sample becomes the
		// baseline instead, and nothing can be captured on the same sample
		// that establishes the baseline -- otherwise a button already held
		// would look identical to one pressed in this very instant.
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
	fresh = suppressDiagonalCardinals(fresh, s.Held)
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

// suppressDiagonalCardinals drops, from one sample's freshly-held inputs, the
// two adjacent cardinals of any hat diagonal present in that same sample.
//
// DISPATCH and CAPTURE want opposite things from a diagonal. Dispatch wants
// all three directions held, so a PTT bound to hat1.up is not cut when the
// user nudges the hat off-axis (expandHatDirection in hat.go has the whole
// argument). Capture wants ONE binding, because the user physically pressed
// one thing: without this, a diagonal arrives as three inputs in a single
// poll, the (Device, Button) tie-break sorts hat1.up (128) below hat1.up_right
// (129), and the captured binding is the nonsense
// `joy:dev:hat1.up+dev:hat1.right` -- a modifier pair the user never pressed
// and cannot press deliberately.
//
// The drop set is derived from everything HELD this sample, not just from
// fresh. Deriving it from fresh alone would only defer the bug by one tick:
// the diagonal joins c.seen on the sample it arrives, so on the NEXT sample it
// is no longer fresh, no diagonal would be found, and the still-held cardinals
// -- never committed, therefore still fresh -- would be admitted after all.
//
// It filters the fresh set and never touches c.order, so a cardinal already
// committed by an EARLIER sample keeps its place: a user who genuinely holds
// hat1.up first and then rolls to up-right has expressed two presses over
// time, and rewriting committed history to guess otherwise is worse than
// honouring what was seen.
//
// Returns fresh unchanged (not a copy) when no diagonal is held, which is
// every sample that does not involve a hat.
func suppressDiagonalCardinals(
	fresh []trigger.JoyButton, held map[trigger.JoyButton]struct{},
) []trigger.JoyButton {
	var drop map[trigger.JoyButton]struct{}
	for b := range held {
		if !b.Button.IsHat() {
			continue
		}
		hat, dir := b.Button.Hat()
		if dir%2 == 0 {
			continue // a cardinal claims nothing
		}
		if drop == nil {
			drop = map[trigger.JoyButton]struct{}{}
		}
		for _, adj := range expandHatDirection(dir)[1:] {
			drop[trigger.JoyButton{Device: b.Device, Button: trigger.HatButton(hat, adj)}] = struct{}{}
		}
	}
	if drop == nil {
		return fresh
	}
	out := make([]trigger.JoyButton, 0, len(fresh))
	for _, b := range fresh {
		if _, skip := drop[b]; skip {
			continue
		}
		out = append(out, b)
	}
	return out
}
