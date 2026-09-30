package notify

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestZeroWindowEmitsImmediately(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { changes++ }})
	defer n.StopTimers()

	n.RaiseWindowed("hotkeys.global", hotkeyItem("x"), WindowHotkeys)

	if changes != 1 {
		t.Fatalf("OnChange fired %d times, want 1 -- a zero window must not defer anything", changes)
	}
}

func TestFlapInsideTheWindowCollapsesToOneEmit(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { changes++ }})
	defer n.StopTimers()

	// A joystick Poll() failing every other tick at 100 Hz: the manager's
	// edge-dedupe passes each edge through, so this is Raise/Resolve at
	// ~50 Hz. Simulated by advancing the injected clock 20ms per edge.
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, WindowJoystick)
		} else {
			n.ResolveWindowed("joystick.global", WindowJoystick)
		}
		now = now.Add(20 * time.Millisecond)
	}

	// 100 edges over 2s. The first opens the window and emits; everything
	// after is coalesced into the pending state.
	if changes > 2 {
		t.Fatalf("OnChange fired %d times for a 50 Hz flap, want at most 2 (the leading edge plus one settle)", changes)
	}
	if changes == 0 {
		t.Fatal("OnChange never fired -- the leading edge must be immediate, not deferred")
	}
}

func TestFlapThatStopsStillEmitsItsSettledState(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	settled := make(chan Snapshot, 8)
	n := New(Options{
		Now:      fixedClock(&now),
		OnChange: func(s Snapshot) { settled <- s },
	})
	defer n.StopTimers()

	// A short real window, so the trailing timer fires inside the test.
	const window = 40 * time.Millisecond

	// Leading edge: the condition is raised and published immediately.
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)
	<-settled

	// The flap then STOPS with the condition CLEARED. That final Resolve
	// lands inside the open window, so nothing emits it synchronously --
	// only the trailing timer can. Without one the UI would be left showing
	// a failure that has already gone away, for the life of the process.
	//
	// Note the settled state must DIFFER from what the leading edge
	// published, or there is genuinely nothing to emit: a flap that ends on
	// the same content it started with leaves the UI already correct, and
	// Raise's identity dedupe is right to stay silent.
	n.ResolveWindowed("joystick.global", window)

	select {
	case s := <-settled:
		if len(s.Items) == 0 {
			t.Fatal("settled snapshot is empty")
		}
		if !s.Items[0].Resolved {
			t.Fatal("settled state is not Resolved; the flap stopped with the condition CLEARED")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the trailing timer never fired -- a flap that stops would leave its final state never emitted")
	}
}

func TestTrailingTimerAfterClearDoesNotResurrectTheItem(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	defer n.StopTimers()

	const window = 40 * time.Millisecond

	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)
	// A second change lands inside the window, arming the trailing timer.
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable (2)", Severity: SeverityWarn}, window)

	// The user hits CLEAR ALL before the timer fires.
	n.Clear()

	time.Sleep(3 * window)

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- a trailing timer must not resurrect an item the user cleared", got)
	}
}

func TestTrailingTimerAfterDismissDoesNotResurrectTheItem(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	defer n.StopTimers()

	const window = 40 * time.Millisecond

	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)
	id := n.Snapshot().Items[0].ID
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable (2)", Severity: SeverityWarn}, window)

	n.Dismiss(id)
	time.Sleep(3 * window)

	items := n.Snapshot().Items
	for _, it := range items {
		if it.Key == "joystick.global" && !it.Resolved {
			t.Fatalf("a dismissed key came back through its trailing timer: %+v", it)
		}
	}
}

func TestStopTimersIsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.RaiseWindowed("k", Item{Title: "t", Severity: SeverityWarn}, 10*time.Second)
	n.RaiseWindowed("k", Item{Title: "t2", Severity: SeverityWarn}, 10*time.Second)

	n.StopTimers()
	n.StopTimers() // must not panic on a second call
}

func TestNoOpLeadingEdgeOpensNoWindow(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	defer n.StopTimers()

	// The healthy-system shape: the audio adapter calls raise-or-resolve for
	// every one of its four keys on EVERY audio:state emission, and on a
	// healthy system every one of those is a no-op Resolve against a key
	// that has never been raised.
	for i := 0; i < 5; i++ {
		n.ResolveWindowed("audio.input", 10*time.Second)
	}

	n.mu.Lock()
	open := len(n.pending)
	remembered := len(n.windows)
	n.mu.Unlock()

	if open != 0 {
		t.Fatalf("pending windows = %d, want 0 -- a call that published nothing is not an edge and must not open a window", open)
	}
	if remembered != 0 {
		t.Fatalf("remembered windows = %d, want 0 -- n.windows must not outlive the window it describes", remembered)
	}
}

func TestGenuineFaultAfterNoOpCallsIsNotDeferred(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	emitted := make(chan Snapshot, 8)
	n := New(Options{Now: fixedClock(&now), OnChange: func(s Snapshot) { emitted <- s }})
	defer n.StopTimers()

	// Scaled down from the real 10s audio window so the test is fast; the
	// deferral this guards against is a FULL window either way.
	const window = 100 * time.Millisecond

	for i := 0; i < 5; i++ {
		n.ResolveWindowed("audio.input", window)
	}
	select {
	case s := <-emitted:
		t.Fatalf("a no-op Resolve published a snapshot: %+v", s)
	default:
	}

	// The microphone dies. This must reach the user NOW, not a window later:
	// audio:state fires every 2s while the engine is glitching, which is the
	// state a device is most likely to fail from, and audio.input is the most
	// urgent thing this channel carries.
	n.RaiseWindowed("audio.input", Item{
		Title:    "Microphone unavailable",
		Severity: SeverityError,
	}, window)

	select {
	case s := <-emitted:
		if len(s.Items) != 1 || s.Items[0].Title != "Microphone unavailable" {
			t.Fatalf("published %+v, want the microphone failure", s.Items)
		}
	default:
		t.Fatal("a genuine failure was DEFERRED by a full window: the preceding no-op calls held the coalescing window open on nothing")
	}
}

func TestWindowClosingOnANoOpDoesNotHoldItselfOpen(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	emitted := make(chan Snapshot, 8)
	n := New(Options{Now: fixedClock(&now), OnChange: func(s Snapshot) { emitted <- s }})
	defer n.StopTimers()

	const window = 40 * time.Millisecond

	// A real edge, so the window legitimately opens, plus an IDENTICAL
	// re-raise inside it -- the level-triggered shape: every source here
	// re-emits its whole state far more often than that state changes.
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)
	<-emitted
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)

	// Let the window close. Its pending change is a no-op, so nothing is
	// published -- and with nothing published there is no committed edge for
	// a further window to protect.
	time.Sleep(window + window/2)
	select {
	case s := <-emitted:
		t.Fatalf("the no-op trailing apply published %+v", s)
	default:
	}

	// The condition now genuinely changes. This is a leading edge and must
	// be immediate; a window re-armed on the no-op above would defer it.
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable (device gone)", Severity: SeverityWarn}, window)
	select {
	case s := <-emitted:
		if s.Items[0].Title != "Joystick unavailable (device gone)" {
			t.Fatalf("published %q, want the changed title", s.Items[0].Title)
		}
	default:
		t.Fatal("a genuine change was deferred: a window that closes on a no-op must retire rather than re-arm itself")
	}
}

// TestDismissDuringTheLeadingEdgeDoesNotArmAnEmptyWindow closes the narrow
// hole the leading-edge ordering left open: a Dismiss (or Clear, or
// StopTimers) landing AFTER the leading edge published and BEFORE coalesce
// armed its window.
//
// cancelPendingLocked runs while there is nothing to cancel -- the window
// does not exist yet -- so, before the cancelGen check, coalesce went on to
// arm one anyway. The window then stood open for a full period carrying
// apply == nil. It retires harmlessly when the timer fires, but until then
// the key looks busy, and the next genuine change is deferred to the trailing
// edge instead of being the immediate leading edge -- up to 10s for audio,
// i.e. a narrow reappearance of exactly what the fn()-before-armLocked
// ordering exists to prevent.
//
// The interleaving is forced by calling the unexported coalesce directly with
// an fn that dismisses what it just raised. That is the real sequence, with
// the timing hazard removed.
func TestDismissDuringTheLeadingEdgeDoesNotArmAnEmptyWindow(t *testing.T) {
	n := New(Options{})

	n.coalesce("audio.input", time.Hour, pendingRaise, func() bool {
		n.Raise("audio.input", Item{Title: "Microphone unavailable", Severity: SeverityError})
		var id string
		for _, it := range n.Snapshot().Items {
			if it.Key == "audio.input" {
				id = it.ID
			}
		}
		if id == "" {
			t.Fatal("the raise inside the leading edge produced no item")
		}
		n.Dismiss(id)
		return true // it genuinely published
	})

	n.mu.Lock()
	p, open := n.pending["audio.input"]
	var empty bool
	if open {
		empty = p.apply == nil
	}
	n.mu.Unlock()

	if open {
		n.StopTimers() // do not leave an hour-long timer behind
		t.Fatalf("a window is open for a key dismissed during the leading edge (carrying no pending change: %v) -- the next genuine change would be deferred by a full period", empty)
	}
}

// TestWindowClosedLeavesASuccessorsWindowAlone pins pendingChange.gen.
//
// windowClosed releases mu to run apply(). In that gap the user can Dismiss
// or Clear the key -- cancelPendingLocked retires the entry -- and a new
// change can arm a brand-new entry under the SAME key. Before the generation
// stamp, windowClosed re-locked, found *an* entry, and assumed it was its
// own:
//
//   - apply() published -> it re-armed, overwriting the successor's LIVE
//     timer, orphaning it. The orphan fires into a no-op, and the successor's
//     window now runs from the wrong instant.
//   - apply() published nothing -> it retired the successor outright,
//     closing a coalescing window it did not own, a full period early.
//
// Neither loses or duplicates a notification, which is why this is a Minor;
// both are still the wrong entry being mutated by a timer that does not own
// it, and "is there a pending entry" cannot tell the two apart.
//
// White-box by necessity: the gap is between two mu acquisitions inside one
// unexported function, and no public call sequence can hold it open.
func TestWindowClosedLeavesASuccessorsWindowAlone(t *testing.T) {
	const key = "k"
	for _, tc := range []struct {
		name      string
		published bool // what the victim's apply() reports
	}{
		{"apply published: must not arm over the successor's live timer", true},
		{"apply published nothing: must not retire the successor", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := New(Options{})
			defer n.StopTimers()

			entered := make(chan struct{})
			release := make(chan struct{})

			n.mu.Lock()
			n.pendingGen++
			gen := n.pendingGen
			n.pending[key] = &pendingChange{gen: gen, apply: func() bool {
				close(entered)
				<-release
				return tc.published
			}}
			n.windows[key] = time.Hour
			n.mu.Unlock()

			done := make(chan struct{})
			go func() {
				defer close(done)
				n.windowClosed(key, gen) // as its timer would
			}()
			<-entered // windowClosed has dropped mu and is inside apply()

			// The user dismisses the item (retiring the key), and the source
			// then reports a fresh change, which arms a NEW entry.
			n.mu.Lock()
			n.cancelPendingLocked(key)
			n.armLocked(key, time.Hour)
			successor := n.pending[key]
			successorTimer := successor.timer
			successorGen := successor.gen
			n.mu.Unlock()
			if successorGen == gen {
				t.Fatal("the successor reused the victim's generation; the stamp is not unique per entry")
			}

			close(release)
			<-done

			n.mu.Lock()
			got, ok := n.pending[key]
			var gotTimer *time.Timer
			if ok {
				gotTimer = got.timer
			}
			n.mu.Unlock()

			if !ok {
				t.Fatal("the successor's pending entry was retired by a timer belonging to the previous entry -- its coalescing window closed a full period early")
			}
			if got != successor {
				t.Fatal("the successor's pending entry was replaced by a timer belonging to the previous entry")
			}
			if gotTimer != successorTimer {
				t.Fatal("the successor's LIVE timer was armed over by a timer belonging to the previous entry -- the orphan still fires, and the successor's window now runs from the wrong instant")
			}
		})
	}
}

// TestWindowClosedIgnoresAStrangersEntryOnTheWayIN closes the other half of
// pendingChange.gen. TestWindowClosedLeavesASuccessorsWindowAlone reaches
// only the check AFTER apply(), because it constructs the successor while
// the victim is already inside apply(); deleting the way-IN check left the
// whole suite green at -race -count=3, which for a guard whose commit
// message advertises "both sides" is exactly the vacuity this branch has
// produced six times already.
//
// The way-in path is reached when a timer FIRES for an entry that is then
// retired and recreated before that timer manages to take mu -- a Dismiss
// and a fresh change landing in the microseconds between AfterFunc's
// callback starting and its first Lock. Without the stamp, windowClosed
// finds *an* entry, takes the SUCCESSOR's pending change as its own and
// runs it a full window early, and the successor is left armed with nothing
// to apply.
//
// White-box for the same reason as its sibling: no public call sequence can
// hold two mu acquisitions of one unexported function apart.
func TestWindowClosedIgnoresAStrangersEntryOnTheWayIn(t *testing.T) {
	const key = "k"
	n := New(Options{})
	defer n.StopTimers()

	applied := make(chan struct{}, 1)

	n.mu.Lock()
	n.pendingGen++
	stale := n.pendingGen // the entry this timer was armed for; since retired
	n.pendingGen++
	successorGen := n.pendingGen
	n.pending[key] = &pendingChange{gen: successorGen, kind: pendingRaise, apply: func() bool {
		applied <- struct{}{}
		return true
	}}
	n.windows[key] = time.Hour
	n.mu.Unlock()

	// The retired entry's timer fires late and takes mu only now.
	n.windowClosed(key, stale)

	select {
	case <-applied:
		t.Fatal("windowClosed ran the SUCCESSOR's pending change -- a timer armed for a retired entry applied a change a full window early")
	default:
	}

	n.mu.Lock()
	got, ok := n.pending[key]
	var gotGen uint64
	var hasApply bool
	if ok {
		gotGen, hasApply = got.gen, got.apply != nil
	}
	n.mu.Unlock()

	if !ok {
		t.Fatal("the successor's entry was retired by a timer belonging to a previous entry")
	}
	if gotGen != successorGen {
		t.Fatalf("pending[%q].gen = %d, want the successor's %d", key, gotGen, successorGen)
	}
	if !hasApply {
		t.Fatal("the successor's pending change was consumed by a stranger's timer -- its window is now armed with nothing to apply")
	}
}

// TestAFlapFasterThanItsWindowStillEmitsOnTheBoundary pins the FIXED-window
// semantics coalesce actually implements, against the debounce its own doc
// comment used to claim ("resetting any timer already armed for this key").
//
// The two readings are indistinguishable to every other test here, which is
// the finding: the reviewer made the CODE match the old comment -- one
// p.timer.Reset(window) in the open-window branch -- and the whole suite
// stayed green. DoD 3's "at most one emit per window" is an upper bound, and
// nothing else stated a lower one, so a source flapping faster than its own
// window emitting NOTHING AT ALL until it stopped went unnoticed.
//
// That is not a theoretical shape. A joystick with a failing Poll() toggles
// the error edge at up to ~50 Hz against notify.WindowJoystick's 2s, and the
// audio adapter re-asserts all four of its keys every 2s against a 10s
// window. Under a debounce the user would be told nothing for as long as the
// hardware stayed broken -- exactly the outcome the trailing timer exists to
// prevent.
func TestAFlapFasterThanItsWindowStillEmitsOnTheBoundary(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) {
		mu.Lock()
		changes++
		mu.Unlock()
	}})
	defer n.StopTimers()

	const window = 40 * time.Millisecond
	const step = window / 8 // the source is EIGHT times faster than its window

	// The leading edge opens the window and publishes immediately.
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable #0", Severity: SeverityWarn}, window)
	mu.Lock()
	leading := changes
	mu.Unlock()
	if leading != 1 {
		t.Fatalf("leading edge published %d times, want 1", leading)
	}

	// Now flap continuously, with genuinely different content every time so
	// identity dedupe can never be the reason nothing is published, for five
	// whole windows. A fixed window emits on every boundary; a debounce emits
	// on none of them, because every change pushes the timer out again.
	deadline := time.Now().Add(5 * window)
	for i := 1; time.Now().Before(deadline); i++ {
		n.RaiseWindowed("joystick.global", Item{
			Title:    "Joystick unavailable #" + strconv.Itoa(i),
			Severity: SeverityWarn,
		}, window)
		time.Sleep(step)
	}

	mu.Lock()
	got := changes
	mu.Unlock()
	if got <= leading {
		t.Fatalf("OnChange fired %d times in all, i.e. nothing after the leading edge, "+
			"over five windows of continuous flapping -- the window is behaving as a "+
			"DEBOUNCE, so a source that flaps faster than its own window tells the user "+
			"nothing for as long as the hardware stays broken", got)
	}
}

// TestStopTimersDuringTheLeadingEdgeDoesNotArmAWindow is StopTimers' half of
// the cancelGen guard. Dismiss's and Clear's half is already covered by
// TestDismissDuringTheLeadingEdgeDoesNotArmAnEmptyWindow; deleting the
// `n.cancelGen++` from StopTimers alone left the whole suite green.
//
// StopTimers is the one method whose stated purpose is that a pending emit
// cannot fire into a torn-down event bus. A leading edge in flight when it
// runs finds nothing to cancel -- the window does not exist yet -- and
// without the bump goes on to arm one AFTER shutdown has cancelled
// everything, falsifying exactly that headline claim.
//
// Forced by calling the unexported coalesce directly with an fn that shuts
// the notifier down, which is the real interleaving with the timing hazard
// removed -- the same technique its Dismiss sibling uses.
func TestStopTimersDuringTheLeadingEdgeDoesNotArmAWindow(t *testing.T) {
	n := New(Options{})

	n.coalesce("audio.input", time.Hour, pendingRaise, func() bool {
		n.Raise("audio.input", Item{Title: "Microphone unavailable", Severity: SeverityError})
		n.StopTimers() // shutdown lands between the publish and the arm
		return true
	})

	n.mu.Lock()
	_, open := n.pending["audio.input"]
	n.mu.Unlock()

	if open {
		n.StopTimers() // do not leave an hour-long timer behind
		t.Fatal("a coalescing window was armed AFTER StopTimers ran -- shutdown's one " +
			"promise is that no pending emit survives it, and this timer would fire " +
			"into a torn-down event bus")
	}
}

// TestRetiringAKeyDrainsBothMaps pins retirePendingLocked's
// delete(n.windows, key), which a paragraph of doc justifies as 7.3/7.4 leak
// prevention and which nothing failed on when removed.
//
// n.windows' lifetime is exactly n.pending's -- armLocked is its only writer
// and writes it while creating the pending entry. Today's key set is small
// and fixed, so an unpruned map merely looks untidy; Phase 7.3/7.4's
// per-client and per-frequency keys would make it an unbounded map keyed on
// a REMOTE party's identity. Every retirement path is covered, because the
// leak only needs one of them to miss.
func TestRetiringAKeyDrainsBothMaps(t *testing.T) {
	const key = "joystick.global"
	const window = 30 * time.Millisecond

	item := func(n int) Item {
		return Item{Title: "Joystick unavailable " + strconv.Itoa(n), Severity: SeverityWarn}
	}

	for _, tc := range []struct {
		name   string
		retire func(t *testing.T, n *Notifier)
	}{
		{
			name: "the trailing timer closes on a settled flap",
			retire: func(t *testing.T, n *Notifier) {
				t.Helper()
				// Leading edge only: nothing accumulates, so the window closes
				// on an empty pending change and retires the key.
				n.RaiseWindowed(key, item(1), window)
				deadline := time.Now().Add(2 * time.Second)
				for {
					n.mu.Lock()
					done := len(n.pending) == 0
					n.mu.Unlock()
					if done || time.Now().After(deadline) {
						return
					}
					time.Sleep(time.Millisecond)
				}
			},
		},
		{
			name: "the user dismisses the item",
			retire: func(t *testing.T, n *Notifier) {
				t.Helper()
				n.RaiseWindowed(key, item(1), time.Hour)
				n.RaiseWindowed(key, item(2), time.Hour) // accumulate, so there is something to cancel
				n.Dismiss(n.Snapshot().Items[0].ID)
			},
		},
		{
			name: "the user clears the list",
			retire: func(t *testing.T, n *Notifier) {
				t.Helper()
				n.RaiseWindowed(key, item(1), time.Hour)
				n.RaiseWindowed(key, item(2), time.Hour)
				n.Clear()
			},
		},
		{
			name: "shutdown stops every timer",
			retire: func(t *testing.T, n *Notifier) {
				t.Helper()
				n.RaiseWindowed(key, item(1), time.Hour)
				n.RaiseWindowed(key, item(2), time.Hour)
				n.StopTimers()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
			n := New(Options{Now: fixedClock(&now)})
			defer n.StopTimers()

			tc.retire(t, n)

			n.mu.Lock()
			pending := len(n.pending)
			remembered := len(n.windows)
			n.mu.Unlock()

			if pending != 0 {
				t.Fatalf("pending = %d, want 0 -- the key was not retired at all", pending)
			}
			if remembered != 0 {
				t.Fatalf("windows = %d with pending = 0 -- n.windows outlived the window it "+
					"describes. The two are written and pruned in lockstep by design; 7.3/7.4 "+
					"key this map on a remote party's identity, at which point an entry that "+
					"is never deleted is an unbounded leak", remembered)
			}
		})
	}
}
