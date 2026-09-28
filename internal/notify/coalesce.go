package notify

import "time"

// Coalescing windows, per source.
//
// Deliberately NOT one global constant. The three sources have genuinely
// different cadences, and a window has to be several times its source's own
// period to coalesce anything rather than beat against it:
//
//   - Hotkeys are event-driven with no poll at all. Identity dedupe (see
//     Raise) is the whole defence; a window would only delay an honest edge.
//   - The joystick manager polls at 100 Hz, and a failing Poll() toggles the
//     error edge at up to ~50 Hz.
//   - The audio manager polls at 2s (audio.ManagerOptions.PollInterval). A
//     2s window here would beat against that, letting a device flapping at
//     the poll rate through roughly every other tick.
//
// A new source in a later phase declares its own rather than inheriting a
// value tuned for someone else's loop.
const (
	WindowHotkeys  = time.Duration(0)
	WindowJoystick = 2 * time.Second
	WindowAudio    = 10 * time.Second
)

// pendingChange is one key's coalescing state: the armed trailing timer and
// the change it will apply when the window closes.
type pendingChange struct {
	timer *time.Timer
	// apply performs the deferred mutation and reports whether it actually
	// published anything. nil means "nothing pending".
	apply func() bool
}

// coalesce either runs fn now (no window open, or no window at all) or defers
// it to a trailing timer, resetting any timer already armed for this key.
//
// A window is opened only on a REAL EDGE -- fn runs first, and a fn that
// published nothing leaves the key idle. That ordering is load-bearing, not
// incidental. The adapters are level-triggered: NotifyAudioState calls
// raise-or-resolve for all four audio keys on every audio:state emission, so
// on a healthy system every call is a no-op Resolve. Arming the window
// before learning that would open a speculative 10s window on nothing, and
// -- because a pending change re-arms the timer -- a source emitting faster
// than its own window would hold that window open indefinitely, deferring
// the first genuine device fault by a full window. The audio poll is 2s and
// the audio window is 10s, so that is a device failure the user is told
// about up to ten seconds late, starting with the first ten seconds after
// startup.
//
// The trailing timer is NOT optional. Without it a flap that simply STOPS
// would leave its final state never emitted, so a device settling into a
// persistent error would show nothing at all.
//
// Caller holds neither mu nor emitMu.
func (n *Notifier) coalesce(key string, window time.Duration, fn func() bool) {
	if window <= 0 {
		fn()
		return
	}

	n.mu.Lock()
	p, open := n.pending[key]
	if open {
		// Inside an open window: record this as the pending state, replacing
		// any earlier one. Only the LAST change in a window survives, which
		// is the whole point -- the settled state is what the user needs.
		p.apply = fn
		n.mu.Unlock()
		return
	}
	n.mu.Unlock()

	// Leading edge. Apply first, and open the window only if that was a real
	// change; a no-op must leave the next change a leading edge too.
	if !fn() {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if _, raced := n.pending[key]; raced {
		// Another goroutine opened the window while fn ran. Its timer is as
		// good as ours would have been; a second one would double-fire.
		return
	}
	n.armLocked(key, window)
}

// armLocked opens (or re-opens) key's window, recording the period so a
// re-armed trailing timer reuses it. Caller holds mu.
func (n *Notifier) armLocked(key string, window time.Duration) {
	p, ok := n.pending[key]
	if !ok {
		p = &pendingChange{}
		n.pending[key] = p
	}
	n.windows[key] = window
	p.timer = time.AfterFunc(window, func() { n.windowClosed(key) })
}

// windowClosed runs when a key's window expires. It applies whatever change
// was pending and keeps the window open only while something is still
// happening; otherwise the key goes idle so the next change is again a
// leading edge.
func (n *Notifier) windowClosed(key string) {
	n.mu.Lock()
	p, ok := n.pending[key]
	if !ok {
		n.mu.Unlock()
		return
	}
	apply := p.apply
	p.apply = nil
	if apply == nil {
		// Nothing accumulated: the flap has stopped. Retire the key.
		n.retirePendingLocked(key)
		n.mu.Unlock()
		return
	}
	window := n.windowFor(key)
	n.mu.Unlock()

	changed := apply()
	// apply() is the real Raise/Resolve, which publishes only when its own
	// content actually changed. A flap that settles back onto EXACTLY the
	// content already committed (and thus already published) correctly
	// publishes nothing here -- the UI already shows the right thing, and
	// broadcasting an identical Snapshot would be the same bus noise
	// EventAudioVU and the joystick manager are documented to suppress. The
	// trailing timer's job is only to make sure a settle that DOES differ
	// from the last published state -- clearing, changing, or newly
	// occurring -- gets its own emit instead of being silently dropped by
	// the coalescing window; it is not a guarantee that every window close
	// broadcasts something.

	n.mu.Lock()
	defer n.mu.Unlock()
	p, ok = n.pending[key]
	if !ok {
		// Dismiss or Clear retired the key while apply() ran. Do not re-arm:
		// a timer for a key the user has just cleared has nothing to defend.
		return
	}
	if !changed && p.apply == nil {
		// The window closed on a no-op and nothing new arrived while it ran,
		// so the published state already IS the source's state. Retire the
		// key rather than re-arming: holding a window open on nothing is
		// exactly what defers the next genuine fault by a full window.
		n.retirePendingLocked(key)
		return
	}
	// Either something was published (so the next change deserves
	// coalescing) or a further change landed while apply() ran (so the
	// source is still active). Keep the window open for another period.
	n.armLocked(key, window)
}

// windowFor recalls the window a key was last coalesced with, so a
// re-armed trailing timer uses the same period. Caller holds mu.
func (n *Notifier) windowFor(key string) time.Duration {
	if w, ok := n.windows[key]; ok {
		return w
	}
	return WindowJoystick
}

// StopTimers cancels every armed trailing timer. Called on shutdown so a
// pending emit cannot fire into a torn-down event bus. Safe to call more
// than once and from any goroutine.
//
// "Cancels every armed timer" is the honest claim, and it is narrower than
// "no emit can follow". windowClosed releases mu before calling apply(), so
// a timer that has already fired and is inside that gap is past cancelling:
// its apply() will still run, publishing one final Snapshot. Nothing here
// can close that window -- the emit would have to be blocked rather than the
// timer stopped, and blocking would mean holding mu across a publish, which
// inverts this package's lock order (see Notifier.emitMu). Shutdown ordering
// is what covers it: StopTimers runs before the event bus is torn down.
func (n *Notifier) StopTimers() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for key, p := range n.pending {
		if p.timer != nil {
			p.timer.Stop()
		}
		n.retirePendingLocked(key)
	}
}

// cancelPendingLocked drops a key's armed timer and pending change, so a
// window opened before the user dismissed or cleared the item cannot fire
// afterwards and resurrect it. Caller holds mu.
func (n *Notifier) cancelPendingLocked(key string) {
	p, ok := n.pending[key]
	if !ok {
		return
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	n.retirePendingLocked(key)
}

// retirePendingLocked forgets everything this key's coalescing state holds:
// the pendingChange AND the remembered window.
//
// n.windows is pruned here rather than left to grow, because its lifetime is
// exactly n.pending's: armLocked is the only writer and it writes only while
// opening a window, and windowFor is consulted only by windowClosed, which
// requires one to be open. Today the key set is a small
// fixed one -- four audio keys, two globals, one per binding -- so the map
// could not grow without bound anyway; Phase 7.3/7.4 may add per-client or
// per-frequency keys, at which point an unpruned map WOULD be an unbounded
// leak keyed on a remote party's identity. Pruning now costs one delete and
// removes the question. Caller holds mu.
func (n *Notifier) retirePendingLocked(key string) {
	delete(n.pending, key)
	delete(n.windows, key)
}
