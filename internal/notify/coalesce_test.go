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

	// Use a short real window so the trailing timer fires inside the test.
	const window = 40 * time.Millisecond

	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)
	<-settled // the leading edge

	// Flap, then STOP with the condition true. The final Raise lands inside
	// the window, so nothing emits it synchronously -- only the trailing
	// timer can, and without one the error would never be shown.
	n.ResolveWindowed("joystick.global", window)
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)

	select {
	case s := <-settled:
		if len(s.Items) == 0 {
			t.Fatal("settled snapshot is empty")
		}
		if s.Items[0].Resolved {
			t.Fatal("settled state is Resolved; the flap stopped with the condition TRUE")
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
