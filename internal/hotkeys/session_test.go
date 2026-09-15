package hotkeys

import (
	"errors"
	"strings"
	"testing"
)

// TestLinuxSessionCheck pins what a Linux user actually experiences.
//
// The failure this guards against is silence: under Wayland, gohook's X11
// backend connects to XWayland, reports itself enabled, and then never
// delivers a keystroke from a native Wayland client. That looks perfect in
// development (where the VCS window has focus) and is dead in real use. Every
// Wayland row below must be an ERROR, because an error is what reaches the
// user through Manager.Registered() and the hotkeys:state banner.
func TestLinuxSessionCheck(t *testing.T) {
	cases := []struct {
		name                            string
		sessionType, wayDisplay, displa string
		want                            error
	}{
		{
			name:        "X11 session",
			sessionType: "x11", displa: ":0",
			want: nil,
		},
		{
			name:        "X11 session, session type not set",
			sessionType: "", displa: ":0",
			want: nil,
		},
		{
			name:        "Wayland session",
			sessionType: "wayland", wayDisplay: "wayland-0", displa: "",
			want: ErrWaylandUnsupported,
		},
		{
			// The trap: XWayland means DISPLAY is set and the X11 backend
			// starts cleanly, so nothing downstream would ever report a
			// problem.
			name:        "Wayland session with XWayland",
			sessionType: "wayland", wayDisplay: "wayland-0", displa: ":0",
			want: ErrWaylandUnsupported,
		},
		{
			name:        "Wayland display present but session type unset",
			sessionType: "", wayDisplay: "wayland-0", displa: ":0",
			want: ErrWaylandUnsupported,
		},
		{
			name:        "session type capitalised",
			sessionType: "Wayland", wayDisplay: "wayland-0", displa: ":0",
			want: ErrWaylandUnsupported,
		},
		{
			name:        "headless: no display at all",
			sessionType: "tty", displa: "",
			want: ErrNoDisplay,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := linuxSessionCheck(c.sessionType, c.wayDisplay, c.displa)
			if !errors.Is(got, c.want) {
				t.Fatalf("linuxSessionCheck = %v, want %v", got, c.want)
			}
			if c.want != nil && !errors.Is(got, ErrBackendUnavailable) {
				t.Errorf("a session failure must report as ErrBackendUnavailable so "+
					"Manager.Registered() goes false; got %v", got)
			}
		})
	}
}

// TestWaylandMessageIsActionable: the banner shows this string verbatim, so a
// user has to be able to act on it. "It doesn't work" is not enough.
func TestWaylandMessageIsActionable(t *testing.T) {
	msg := ErrWaylandUnsupported.Error()
	for _, want := range []string{"Wayland", "X11"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Wayland error message does not mention %q: %q", want, msg)
		}
	}
}
