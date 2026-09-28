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

// pendingKind names WHAT a deferred change will do when its window closes.
//
// It exists because a bare closure is opaque, and one caller -- the
// dismissal path -- has to reason about the key's SETTLED state rather than
// its committed one. Dismiss and Clear suppress a key only while its
// condition STILL HOLDS (see store.go's Dismiss), and they decide that from
// the committed item's Resolved flag; but they call cancelPendingLocked
// first, destroying whatever the window was holding. When the thing
// destroyed was the Resolve, the committed item still read
// Resolved == false, so the dismissal suppressed a condition that had
// already cleared and swallowed the whole NEXT occurrence of the fault.
//
// Recording the intent alongside the closure is what lets those two ask
// "is a Resolve pending for this key?" under mu, with no lock widened and
// nothing flushed -- see pendingResolveLocked. It also makes
// cancelPendingLocked honest about what it is destroying.
type pendingKind uint8

const (
	// pendingNone is an armed window with nothing accumulated in it.
	pendingNone pendingKind = iota
	pendingRaise
	pendingResolve
)

// pendingChange is one key's coalescing state: the armed trailing timer and
// the change it will apply when the window closes.
type pendingChange struct {
	timer *time.Timer
	// gen identifies THIS entry, for the life of this entry only. It is what
	// lets a fired timer tell "the entry I was armed for" from "a SUCCESSOR
	// entry that reused my key".
	//
	// The successor is reachable: windowClosed releases mu to run apply(),
	// and in that gap a Dismiss or Clear can retire the key
	// (cancelPendingLocked) and a fresh coalesce can arm a brand-new entry
	// under the same key. Without the stamp, windowClosed re-locks, finds an
	// entry, and cannot tell it is not its own -- so it either arms over the
	// successor's LIVE timer (orphaning a timer that then fires into a
	// no-op) or retires it, closing a coalescing window it does not own a
	// full period early. Neither loses or duplicates a notification -- a
	// pending apply always eventually runs -- but both are wrong, and the
	// presence of a pending entry, which is all windowClosed used to check,
	// cannot distinguish the two cases.
	//
	// Guarded by Notifier.mu, like every other field here.
	gen uint64
	// apply performs the deferred mutation and reports whether it actually
	// published anything. nil means "nothing pending".
	//
	// kind describes what apply will do, and the two are ALWAYS written
	// together: apply == nil iff kind == pendingNone. Keep it that way --
	// pendingResolveLocked reads kind alone.
	apply func() bool
	kind  pendingKind
}

// pendingResolveLocked reports whether this key's open coalescing window is
// holding a deferred Resolve -- i.e. whether the key's SETTLED state is
// "cleared" even though the committed item still reads unresolved.
//
// Caller holds mu. See pendingKind for why this exists and store.go's
// Dismiss for the invariant it serves.
func (n *Notifier) pendingResolveLocked(key string) bool {
	p, ok := n.pending[key]
	return ok && p.kind == pendingResolve
}

// coalesce either runs fn now (no window open, or no window at all) or defers
// it to the trailing timer already armed for this key.
//
// This is a FIXED window, NOT a debounce, and the difference is the whole
// point. A change landing inside an open window only overwrites p.apply and
// p.kind; it deliberately does NOT reset the timer (armLocked is never
// reached here, and neither is time.Timer.Reset). The timer therefore fires
// one window after the edge that opened it, whatever the source does in
// between, and windowClosed re-arms a FRESH window only if something was
// actually applied or is still waiting.
//
// Resetting the timer per change -- a debounce -- would mean a source
// flapping faster than its own window emitted NOTHING until the flap
// stopped: the joystick's failing Poll() toggles the error edge at up to
// ~50 Hz against a 2s window, so the user would be told nothing at all for
// as long as the stick was broken. That is precisely the outcome the
// trailing timer exists to prevent (see below), and DoD 3's "at most one
// emit per window" is an upper bound with no matching lower one, so the
// suite cannot catch it by counting. Pinned instead by
// TestAFlapFasterThanItsWindowStillEmitsOnTheBoundary.
//
// A window is opened only on a REAL EDGE -- fn runs first, and a fn that
// published nothing leaves the key idle. That ordering is load-bearing, not
// incidental. The adapters are level-triggered: notifyAudioState calls
// raise-or-resolve for all four audio keys on every audio:state emission, so
// on a healthy system every call is a no-op Resolve. Arming the window
// before learning that would open a speculative 10s window on nothing, and
// -- because windowClosed re-arms whenever something is still pending -- a
// source emitting faster than its own window would keep that window open
// period after period, deferring the first genuine device fault by a full
// window. The audio poll is 2s and the audio window is 10s, so that is a
// device failure the user is told about up to ten seconds late, starting
// with the first ten seconds after startup.
//
// The trailing timer is NOT optional. Without it a flap that simply STOPS
// would leave its final state never emitted, so a device settling into a
// persistent error would show nothing at all.
//
// kind records what fn WILL do, so a dismissal can see the key's settled
// state rather than only its committed one -- see pendingKind.
//
// Caller holds neither mu nor emitMu.
func (n *Notifier) coalesce(key string, window time.Duration, kind pendingKind, fn func() bool) {
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
		p.apply, p.kind = fn, kind
		n.mu.Unlock()
		return
	}
	gen := n.cancelGen
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
	if n.cancelGen != gen {
		// A Dismiss, Clear or StopTimers landed in the gap between fn()
		// publishing and this arm. For THIS key, that means
		// cancelPendingLocked ran and found nothing to cancel, because the
		// window did not exist yet -- so arming now would leave a window
		// OPEN carrying apply == nil. The timer retires harmlessly when it
		// fires, but until then the key looks busy, and the next genuine
		// change would be deferred to the trailing edge (up to 10s, for
		// audio) instead of being the immediate leading edge this function's
		// ordering exists to guarantee. Skipping the arm errs the safe way:
		// the next change is a leading edge, which is the no-coalescing
		// default.
		//
		// cancelGen is deliberately GLOBAL, not per key, so this is COARSER
		// than the paragraph above describes: a Dismiss of item A that lands
		// in the gap also skips the arm for an unrelated key B. That is
		// accepted, not overlooked. The cost is bounded and self-healing --
		// one human click can cost at most the one window that was mid-arm,
		// and the very next change on that key opens a window normally --
		// while a per-key counter would add a second map with exactly
		// n.suppressed's "there is no moment at which pruning would be
		// correct" problem, for a defect whose entire symptom is that one
		// coalescing window did not open. Do not narrow this without a
		// measured reason; err-safe and simple beats precise and unpruned.
		return
	}
	n.armLocked(key, window)
}

// armLocked opens (or re-opens) key's window, recording the period so a
// re-armed trailing timer reuses it. Caller holds mu.
//
// A NEW entry gets a fresh generation; re-arming an existing one keeps its
// generation, because it is the same window continuing. The timer closes
// over that generation so windowClosed can prove the entry it finds later is
// still the one it was armed for -- see pendingChange.gen.
//
// The Stop() on re-arm makes this primitive safe on its own rather than
// safe-by-caller. Today the only re-arm reaches here from windowClosed with
// a timer that has already fired, so Stop is a no-op; but a future caller
// arming over a live timer would otherwise leak it, and that is exactly the
// defect the generation stamp exists to make impossible.
func (n *Notifier) armLocked(key string, window time.Duration) {
	p, ok := n.pending[key]
	if !ok {
		n.pendingGen++
		p = &pendingChange{gen: n.pendingGen}
		n.pending[key] = p
	} else if p.timer != nil {
		p.timer.Stop()
	}
	n.windows[key] = window
	gen := p.gen
	p.timer = time.AfterFunc(window, func() { n.windowClosed(key, gen) })
}

// windowClosed runs when a key's window expires. It applies whatever change
// was pending and keeps the window open only while something is still
// happening; otherwise the key goes idle so the next change is again a
// leading edge.
//
// gen is the generation of the entry this timer was armed for. Every lookup
// below checks it, on both sides of the apply() call: a retired-and-recreated
// key leaves an entry that is present but NOT ours, and acting on it would
// steal a successor's pending change on the way in, or orphan its live timer
// on the way out. See pendingChange.gen.
func (n *Notifier) windowClosed(key string, gen uint64) {
	n.mu.Lock()
	p, ok := n.pending[key]
	if !ok || p.gen != gen {
		// Either the key was retired (Dismiss, Clear, StopTimers) or it was
		// retired and re-armed by a later change. In both cases this timer
		// has nothing left to do, and the successor owns its own timer.
		n.mu.Unlock()
		return
	}
	apply := p.apply
	p.apply, p.kind = nil, pendingNone
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
	if !ok || p.gen != gen {
		// Dismiss or Clear retired the key while apply() ran. Do not re-arm:
		// a timer for a key the user has just cleared has nothing to defend.
		// The generation half covers the same thing followed by a NEW change
		// arming a fresh entry under this key: that successor has its own
		// live timer and its own pending change, and neither arming over it
		// nor retiring it would be ours to do.
		return
	}
	if window <= 0 {
		// windowFor found nothing recorded -- unreachable today (see its
		// doc). Retire rather than arm a zero-delay timer whose only effect
		// would be to re-enter this function.
		n.retirePendingLocked(key)
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

// windowFor recalls the window a key was last coalesced with, so a re-armed
// trailing timer uses the same period. Caller holds mu.
//
// A miss returns ZERO, and deliberately not some default. This package is
// source-agnostic -- see the window constants above, whose whole rule is that
// a new source declares its own window rather than inheriting one tuned for
// someone else's loop -- and a fallback here would be the one place that rule
// was silently broken, re-arming (say) an audio key on the joystick's 2s.
//
// The miss is unreachable today: armLocked is the only writer of n.windows
// and writes it while creating the n.pending entry, retirePendingLocked
// deletes the two together, and both happen under the same mu that
// windowClosed holds when it calls this after finding the pending entry. Zero
// is therefore a "cannot happen" answer, and windowClosed treats it the way
// coalesce treats a non-positive window: no window, so nothing to keep open.
func (n *Notifier) windowFor(key string) time.Duration {
	return n.windows[key]
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
// inverts this package's lock order (see Notifier.emitMu).
//
// What shutdown ordering actually gives, in main.go, is narrower than an
// earlier version of this comment claimed. `defer notifier.StopTimers()` is
// registered BEFORE `defer jm.Close()` and `defer am.Stop()`, and defers run
// LIFO, so the joystick and audio poll goroutines are stopped FIRST and this
// runs last: nothing can arm a new window while or after it cancels, which
// is what makes "cancels every armed timer" a final statement rather than a
// racy one. That registration order is load-bearing and is pinned by
// TestNotifierStopTimersIsRegisteredBeforeTheSources in main_wiring_test.go.
//
// It does NOT run before the event bus is torn down: every one of those
// defers runs only after wailsApp.Run() has returned. A timer already inside
// the windowClosed/apply() gap when Run() returns still publishes, into a
// Wails application that has stopped. Recorded rather than claimed away --
// the gap is bounded by the two Stop/Close calls ahead of it and nothing in
// this package can close it.
func (n *Notifier) StopTimers() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cancelGen++ // see coalesce: a leading edge in flight must not re-arm
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
//
// It DESTROYS the pending change rather than flushing it, so a caller whose
// own decision depends on that change must read it FIRST -- see
// pendingResolveLocked and the two call sites in store.go. Flushing here is
// not an option: apply is a closure over the exported Raise/Resolve, which
// re-acquire emitMu, and both callers already hold emitMu and mu.
//
// The generation bump is unconditional, BEFORE the "is there anything to
// cancel" check, because the case that needs it is precisely the one where
// there is nothing: a Dismiss landing between a leading edge publishing and
// coalesce arming its window finds no pending entry, and without the bump
// coalesce would go on to arm one for a key the user has just silenced. See
// coalesce.
func (n *Notifier) cancelPendingLocked(key string) {
	n.cancelGen++
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
