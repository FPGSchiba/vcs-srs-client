package hotkeys

import (
	"sort"
	"sync"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// keyEdge is the direction of a key transition.
type keyEdge uint8

const (
	edgeDown keyEdge = iota
	edgeUp
)

// keyEvent is one key transition, reduced to the only three things this
// package needs: which direction, which physical key, which modifiers were
// held. Everything else the OS stream carries -- the character, the keysym,
// mouse position, the raw platform payload -- is discarded at the boundary
// and never reaches this type. See registrar_gohook.go for why that matters.
type keyEvent struct {
	edge keyEdge
	code keyCode
	mods chord.Mod
}

// boundAction is one registered hotkey.
type boundAction struct {
	code keyCode
	mods chord.Mod
	hold bool
	h    Handler
}

// dispatcher matches key events against the registered chord set and drives
// Handler press/release edges. It is the whole behavioural core of the OS
// layer and deliberately knows nothing about the OS: everything here is
// driven by synthetic keyEvents in the tests.
//
// AUTO-REPEAT DEBOUNCE lives here, and it is the single most safety-critical
// thing in this file. gohook does not deduplicate a held key (issue #47): the
// macOS and Windows backends both re-deliver KeyDown while a key is held, and
// the X11 backend folds repeats into KeyHold which we ignore. Without the
// latch below, a held push-to-talk key would re-fire Pressed dozens of times
// a second. Worse, a Released that never arrives -- or arrives once for a
// key that latched twice -- leaves the microphone open with no indication to
// the user. So: exactly one Pressed on the first down, repeats swallowed,
// exactly one Released on the following up.
type dispatcher struct {
	mu      sync.Mutex
	binds   map[string]boundAction
	latched map[string]*latch

	// gen numbers latches so a stale watchdog timer cannot release a LATER
	// press of the same action. Stopping a timer is not enough on its own: a
	// timer that has already fired and is blocked on d.mu cannot be stopped,
	// and would otherwise wake up holding a stale intent.
	gen uint64

	// staleAfter is how long a hold binding may stay latched before the
	// watchdog force-releases it. Zero disables the watchdog entirely.
	staleAfter time.Duration
}

// latch is one held hotkey: its generation, and the watchdog timer armed for
// it (nil for a press-only binding, or when the watchdog is disabled).
type latch struct {
	gen   uint64
	timer *time.Timer
}

func newDispatcher(staleAfter time.Duration) *dispatcher {
	return &dispatcher{
		binds:      map[string]boundAction{},
		latched:    map[string]*latch{},
		staleAfter: staleAfter,
	}
}

// add registers one action, replacing any previous binding for that ID.
//
// A replaced binding that was HELD is released first, using the OLD binding's
// handler and hold flag. The new binding's key may be a different one
// entirely, so its latch cannot carry over -- and dropping the latch without
// the Released would be a silently open microphone.
//
// Nothing reaches this with a latch today: Manager.registerLocked always calls
// UnregisterAll first, and clear() drains. The release is here anyway so the
// invariant "a latch never disappears without its Released" is enforced where
// the latch is removed, rather than depending on a caller two layers up
// continuing to behave.
func (d *dispatcher) add(actionID string, b boundAction) {
	d.mu.Lock()
	// takeLatchLocked reads d.binds, which still holds the PREVIOUS binding
	// at this point -- which is the one that owes the Released.
	release := d.takeLatchLocked(actionID)
	d.binds[actionID] = b
	d.mu.Unlock()

	for _, r := range release {
		r.h.Released(r.id)
	}
}

// clear drops every registration and, crucially, RELEASES anything currently
// held. Suspend/Apply run through here on every rebind, and a hold binding
// that is latched when its registration disappears would otherwise never see
// its key-up -- a live microphone that no later event can close.
func (d *dispatcher) clear() {
	d.mu.Lock()
	release := d.drainLatchedLocked()
	d.binds = map[string]boundAction{}
	d.mu.Unlock()

	for _, r := range release {
		r.h.Released(r.id)
	}
}

// releaseHeld releases everything currently held but KEEPS the registrations.
//
// This is what the OS stream dying looks like from here. Once the stream is
// gone no KeyUp can ever arrive, so a hold binding latched at that instant
// would stay latched forever: Pressed was emitted, Released never would be,
// and the microphone stays open with nothing able to close it. The
// registrations themselves are still wanted -- the stream may be restarted --
// so this deliberately does not empty binds the way clear() does.
//
// Idempotent: the latch set is drained, so a second call finds nothing and
// fires nothing.
//
// It is also the only code-level closure available for a KeyUp that gohook
// DROPPED. gohook's send() discards events when its 1024-slot buffer is full
// (darwin.go:579-591, windows.go:666-678, x11.go:554-566) rather than stalling
// the OS input path -- a reasonable trade for it, but a dropped KeyUp strands
// a latch exactly like a dead stream does. That case self-heals on the next
// press/release of the same key; this one cannot.
func (d *dispatcher) releaseHeld() {
	d.mu.Lock()
	release := d.drainLatchedLocked()
	d.mu.Unlock()

	for _, r := range release {
		r.h.Released(r.id)
	}
}

// pending is one Handler call to make after the lock is dropped.
type pending struct {
	id string
	h  Handler
}

// takeLatchLocked removes ONE latch, cancels its watchdog timer, and returns
// the Released it owes (nil for a press-only binding, or when nothing was
// latched). Caller holds d.mu.
//
// This is the ONLY place a latch is removed. Every release path -- a real
// key-up, a rebind, Suspend, a dead stream, Close, and the stale-latch
// watchdog -- funnels through here, which is what makes "a latch never
// disappears without its Released" and "a cancelled latch never fires later"
// single facts rather than six coincidences.
func (d *dispatcher) takeLatchLocked(actionID string) []pending {
	l := d.latched[actionID]
	if l == nil {
		return nil
	}
	if l.timer != nil {
		l.timer.Stop()
	}
	delete(d.latched, actionID)

	if b, ok := d.binds[actionID]; ok && b.hold {
		return []pending{{id: actionID, h: b.h}}
	}
	return nil
}

// setLatchLocked marks an action held and, for a hold binding, arms its
// watchdog deadline. Caller holds d.mu.
func (d *dispatcher) setLatchLocked(actionID string, b boundAction) {
	d.gen++
	l := &latch{gen: d.gen}
	d.latched[actionID] = l

	if b.hold && d.staleAfter > 0 {
		gen := l.gen
		l.timer = time.AfterFunc(d.staleAfter, func() {
			d.forceRelease(actionID, gen)
		})
	}
}

// drainLatchedLocked empties the latch set and returns the hold bindings that
// owe a Released. Caller holds d.mu.
func (d *dispatcher) drainLatchedLocked() []pending {
	var out []pending
	// Deleting during range is defined behaviour in Go: entries removed
	// before they are reached are simply not produced.
	for id := range d.latched {
		out = append(out, d.takeLatchLocked(id)...)
	}
	sortPending(out)
	return out
}

// handle routes one key transition to the matching registrations.
//
// The two edges match DIFFERENTLY, on purpose:
//
//   - down requires an exact chord match, key AND modifier set. "Ctrl+E" must
//     not fire on a bare E, and a bare "E" binding must not fire when the user
//     is typing Ctrl+E at something else.
//
//   - up matches on the physical key ALONE, among actions that are currently
//     latched. A user who releases Ctrl before releasing E produces a key-up
//     with no modifiers; requiring an exact match there would drop the release
//     and strand push-to-talk open. The latch set is what makes this safe --
//     only a key that we ourselves latched can be released by it.
func (d *dispatcher) handle(e keyEvent) {
	var press, release []pending

	d.mu.Lock()
	switch e.edge {
	case edgeDown:
		for id, b := range d.binds {
			if b.code != e.code || b.mods != e.mods {
				continue
			}
			if d.latched[id] != nil {
				continue // auto-repeat of a key already held
			}
			d.setLatchLocked(id, b)
			press = append(press, pending{id: id, h: b.h})
		}
	case edgeUp:
		for id, b := range d.binds {
			if b.code != e.code || d.latched[id] == nil {
				continue
			}
			release = append(release, d.takeLatchLocked(id)...)
		}
	}
	d.mu.Unlock()

	// Handler calls happen outside the lock: Handler is documented as
	// non-blocking, but it reaches application code and the OS event stream
	// must not be able to stall behind it.
	sortPending(press)
	sortPending(release)
	for _, p := range press {
		p.h.Pressed(p.id)
	}
	for _, r := range release {
		r.h.Released(r.id)
	}
}

// forceRelease is the stale-latch watchdog firing: a hold binding has been
// latched for staleAfter with no key-up, so we release it as though one had
// arrived.
//
// THIS IS THE LAST LINE OF DEFENCE FOR AN OPEN MICROPHONE, and it is
// deliberately indifferent to WHY the release was lost. Every other guard in
// this package handles a cause we can observe -- the stream reporting itself
// dead, the channel closing, a rebind, a suspend. This one covers the ones we
// cannot: a KeyUp that gohook dropped from a full buffer, a key released while
// the app was backgrounded, an X11 backend that died silently without telling
// anyone (see hookRegistrar), and whatever else we have not thought of. If a
// hotkey has been "held" for two minutes, something upstream went wrong and
// the user's microphone has been open for two minutes.
//
// gen is what makes cancellation exact. Stopping the timer on a real key-up
// covers the ordinary case, but a timer that has ALREADY fired and is waiting
// on d.mu cannot be stopped -- it would wake up, find the action latched again
// from a later press, and release that one instead. Comparing generations
// makes the stale wake-up a no-op.
//
// Note it releases through takeLatchLocked like everything else, so a real
// key-up racing this can never produce two Released calls: whichever reaches
// the lock first removes the latch, and the loser finds nothing.
func (d *dispatcher) forceRelease(actionID string, gen uint64) {
	d.mu.Lock()
	l := d.latched[actionID]
	if l == nil || l.gen != gen {
		// Already released, or this timer belongs to an older press.
		d.mu.Unlock()
		return
	}
	release := d.takeLatchLocked(actionID)
	d.mu.Unlock()

	for _, r := range release {
		// Warn first, so the log explains the release that follows it. Action
		// ID only -- never a key identity, same rule as everywhere else in
		// this package.
		if sr, ok := r.h.(StaleReleaser); ok {
			sr.ForceReleased(r.id)
		}
		r.h.Released(r.id)
	}
}

// heldCount reports how many actions are currently latched down. Used by the
// tests to assert the debounce latch is actually released rather than merely
// looking released from the outside.
func (d *dispatcher) heldCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.latched)
}

// sortPending gives a deterministic Handler call order when one key is bound
// to several actions. Go map iteration is randomised, and a hotkey layer
// whose event order changes run to run is untestable and unpleasant to debug.
func sortPending(p []pending) {
	sort.Slice(p, func(i, j int) bool { return p[i].id < p[j].id })
}
