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
	// apply performs the deferred mutation. nil means "nothing pending".
	apply func()
}

// coalesce either runs fn now (window open, or no window at all) or defers
// it to a trailing timer, resetting any timer already armed for this key.
//
// The trailing timer is NOT optional. Without it a flap that simply STOPS
// would leave its final state never emitted, so a device settling into a
// persistent error would show nothing at all.
//
// Caller holds neither mu nor emitMu.
func (n *Notifier) coalesce(key string, window time.Duration, fn func()) {
	if window <= 0 {
		fn()
		return
	}

	n.mu.Lock()
	p, open := n.pending[key]
	if !open {
		// Leading edge: apply immediately and open the window so the next
		// change within it is deferred.
		p = &pendingChange{}
		n.pending[key] = p
		p.timer = time.AfterFunc(window, func() { n.windowClosed(key) })
		n.mu.Unlock()
		fn()
		return
	}
	// Inside an open window: record this as the pending state, replacing any
	// earlier one. Only the LAST change in a window survives, which is the
	// whole point -- the settled state is what the user needs.
	p.apply = fn
	n.mu.Unlock()
}

// windowClosed runs when a key's window expires. It applies whatever change
// was pending and, if there was one, opens a fresh window; otherwise the key
// goes idle so the next change is again a leading edge.
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
		delete(n.pending, key)
		n.mu.Unlock()
		return
	}
	// Something is pending, so the source is still active: keep the window
	// open for another period.
	p.timer = time.AfterFunc(n.windowFor(key), func() { n.windowClosed(key) })
	n.mu.Unlock()

	apply()
	// apply() is the real Raise/Resolve, which only publishes when its own
	// content actually changed. A flap that ends back at the SAME content it
	// started with (error -> nil -> error, e.g.) is a legitimate settle, but
	// Raise's identity dedupe (see raiseLocked) sees no change from the last
	// committed state and stays silent -- exactly the "shows nothing at all"
	// failure this window exists to prevent. forceEmit re-confirms the
	// settled state unconditionally so the trailing edge is never silent,
	// at the cost of one harmless duplicate broadcast when apply() already
	// published (Snapshot is a full replace, so a repeat is a no-op for the
	// receiver).
	n.forceEmit()
}

// forceEmit publishes the current snapshot unconditionally, bypassing
// Raise/Resolve's publish-only-on-change gate. Caller holds neither mu nor
// emitMu. Never plays a sound -- only a genuinely new or changed item should
// be audible, and apply() already handled that for a real change.
func (n *Notifier) forceEmit() {
	n.emitMu.Lock()
	defer n.emitMu.Unlock()

	n.mu.Lock()
	snap := n.snapshotLocked()
	n.mu.Unlock()

	n.publish(snap)
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
func (n *Notifier) StopTimers() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for key, p := range n.pending {
		if p.timer != nil {
			p.timer.Stop()
		}
		delete(n.pending, key)
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
	delete(n.pending, key)
}
