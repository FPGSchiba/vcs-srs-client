package app

import "testing"

func TestNotificationsWindowHasItsOwnURLAndGeometry(t *testing.T) {
	if got := windowURL("notifications"); got != "/notifications.html" {
		t.Fatalf("windowURL(\"notifications\") = %q, want \"/notifications.html\"", got)
	}

	g := defaultGeometry("notifications")
	main := defaultGeometry("unknown-id")
	if g == main {
		t.Fatal("the notifications popout fell through to the main-window geometry; it needs its own arm")
	}
	if g.W <= 0 || g.H <= 0 {
		t.Fatalf("geometry = %+v, want positive dimensions", g)
	}
}

func TestCommsWindowIsUnchanged(t *testing.T) {
	// Regression guard: adding an arm to either switch must not disturb the
	// one popout that already works.
	if got := windowURL("comms"); got != "/comms.html" {
		t.Fatalf("windowURL(\"comms\") = %q, want \"/comms.html\"", got)
	}
	if got := defaultGeometry("comms"); got.W != 540 || got.H != 720 {
		t.Fatalf("comms geometry = %+v, want 540x720", got)
	}
}
