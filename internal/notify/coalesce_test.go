package notify

import (
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
