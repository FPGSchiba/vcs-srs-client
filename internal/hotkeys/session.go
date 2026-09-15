package hotkeys

import (
	"fmt"
	"os"
	"strings"
)

// ErrWaylandUnsupported is returned by every Register on a Wayland session.
//
// This is NOT a gap in gohook and not something a different library would
// fix. Wayland has no global key-listening primitive BY DESIGN -- a wl_seat
// delivers keyboard events only to the surface that currently holds input
// focus, and there is no XRecord equivalent. Push-to-talk is defined by
// working while the GAME has focus, so a focus-scoped listener inverts the
// requirement. Neither Discord, Mumble nor OBS solves this either: OBS's
// Wayland hotkey backend is a deliberate stub whose comment reads "wayland
// never delivers key events when out of focus", and the standing answer for
// Discord is XWayland or an evdev shim that needs the user in the `input`
// group.
//
// The xdg-desktop-portal GlobalShortcuts portal does not help: every backend
// implements it as a compositor GRAB, so the game would not receive the key
// either -- and the user cannot even choose which key.
//
// What a Wayland user must not get is SILENCE, and that is the entire reason
// this error exists. On a Wayland session gohook's X11 backend connects
// happily to XWayland, reports HookEnabled, and then never sees a keystroke
// from any native Wayland client. Failing every Register instead turns that
// into "no binding registered" -- Manager.Registered() goes false and the
// existing hotkeys:state surface shows the banner with this sentence as the
// reason.
var ErrWaylandUnsupported = fmt.Errorf(
	"%w: global hotkeys are not available in a Wayland session, because Wayland "+
		"provides no global key-listening API. Log in to an X11/Xorg session to use them",
	ErrBackendUnavailable)

// ErrNoDisplay is returned when there is no X display to connect to at all,
// e.g. a headless or plain-SSH session.
var ErrNoDisplay = fmt.Errorf(
	"%w: no X11 display found (DISPLAY is unset)", ErrBackendUnavailable)

// linuxSessionCheck is the Linux session pre-flight, taking its inputs
// explicitly so it is testable from any host rather than only under CI's
// Linux runner.
//
// It runs BEFORE the stream is started, so the answer is synchronous and
// lands on the very first Apply. Waiting for gohook to tell us would not
// work: it does no session detection of its own (grepping its tree for
// XDG_SESSION_TYPE and WAYLAND_DISPLAY returns nothing), and under Wayland
// its X11 backend succeeds against XWayland.
//
// A Wayland session is claimed when XDG_SESSION_TYPE says so, OR when a
// Wayland display is present at all -- that second case is the XWayland trap
// above, where DISPLAY is also set and everything looks healthy.
func linuxSessionCheck(sessionType, waylandDisplay, display string) error {
	if strings.EqualFold(strings.TrimSpace(sessionType), "wayland") {
		return ErrWaylandUnsupported
	}
	if strings.TrimSpace(waylandDisplay) != "" {
		return ErrWaylandUnsupported
	}
	if strings.TrimSpace(display) == "" {
		return ErrNoDisplay
	}
	return nil
}

// linuxSession reads the session out of the environment.
func linuxSession() error {
	return linuxSessionCheck(
		os.Getenv("XDG_SESSION_TYPE"),
		os.Getenv("WAYLAND_DISPLAY"),
		os.Getenv("DISPLAY"),
	)
}
