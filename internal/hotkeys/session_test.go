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
		{
			// The false positive the WAYLAND_DISPLAY heuristic has on its own.
			// systemd's user-environment import carries WAYLAND_DISPLAY across
			// sessions, nested compositors set it, and some Flatpak/snap
			// wrappers export it unconditionally. Refusing here would cost a
			// real X11 user every hotkey and tell them to log in to X11 --
			// which is what they did. XDG_SESSION_TYPE is authoritative and
			// wins.
			name:        "X11 session carrying a stale WAYLAND_DISPLAY",
			sessionType: "x11", wayDisplay: "wayland-0", displa: ":0",
			want: nil,
		},
		{
			name:        "X11 session type, capitalised, stale WAYLAND_DISPLAY",
			sessionType: "X11", wayDisplay: "wayland-1", displa: ":1",
			want: nil,
		},
		{
			// XDG_SESSION_TYPE being authoritative must not excuse a missing
			// DISPLAY -- there would be nothing for XRecord to attach to.
			name:        "X11 session type but no DISPLAY",
			sessionType: "x11", wayDisplay: "", displa: "",
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
