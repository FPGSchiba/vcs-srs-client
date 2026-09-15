package hotkeys

import (
	"sort"
	"sync"

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
	latched map[string]bool
}

func newDispatcher() *dispatcher {
	return &dispatcher{binds: map[string]boundAction{}, latched: map[string]bool{}}
}

// add registers one action. Replacing an existing action ID drops its latch,
// because the new binding's key may be a different one entirely.
func (d *dispatcher) add(actionID string, b boundAction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.binds[actionID] = b
	delete(d.latched, actionID)
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

// pending is one Handler call to make after the lock is dropped.
type pending struct {
	id string
	h  Handler
}

// drainLatchedLocked empties the latch set and returns the hold bindings that
// owe a Released. Caller holds d.mu.
func (d *dispatcher) drainLatchedLocked() []pending {
	var out []pending
	for id := range d.latched {
		if b, ok := d.binds[id]; ok && b.hold {
			out = append(out, pending{id: id, h: b.h})
		}
	}
	d.latched = map[string]bool{}
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
			if d.latched[id] {
				continue // auto-repeat of a key already held
			}
			d.latched[id] = true
			press = append(press, pending{id: id, h: b.h})
		}
	case edgeUp:
		for id, b := range d.binds {
			if b.code != e.code || !d.latched[id] {
				continue
			}
			delete(d.latched, id)
			if b.hold {
				release = append(release, pending{id: id, h: b.h})
			}
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
