//go:build linux

package joystick

import (
	"io"
	"log/slog"
	"testing"

	evdev "github.com/holoplot/go-evdev"
)

// TestIsJoystickButtonExcludesDigitisers is the I2 guard.
//
// The first eligible range used to run BTN_JOYSTICK..BTN_GAMEPAD+0x7f
// (0x120-0x1AF), which swallowed the entire digitiser block. Every mainstream
// laptop touchpad declares EV_ABS/ABS_X plus BTN_TOUCH and BTN_TOOL_FINGER, so
// it passed looksLikeJoystick, its BTN_TOUCH became an addressable
// trigger.Button reported held whenever a finger was down, and clicking "+"
// then touching the trackpad bound the trackpad to the action.
//
// Every constant below is the kernel's, as pinned go-evdev's codes.go spells
// it; the hex is repeated in the table only so a future widening of a bound
// has to argue with a number rather than with a name.
func TestIsJoystickButtonExcludesDigitisers(t *testing.T) {
	cases := []struct {
		name string
		code evdev.EvCode
		want bool
	}{
		// The joystick/gamepad block, at both ends and in the middle.
		{"BTN_JOYSTICK (0x120)", evdev.BTN_JOYSTICK, true},
		{"BTN_GAMEPAD (0x130)", evdev.BTN_GAMEPAD, true},
		{"BTN_THUMBR (0x13e, last gamepad code)", evdev.BTN_THUMBR, true},

		// The digitiser block: touchpads, touchscreens, graphics tablets.
		// These are the codes the old upper bound of 0x1AF admitted.
		{"BTN_DIGI (0x140)", evdev.BTN_DIGI, false},
		{"BTN_TOOL_FINGER (0x145)", evdev.BTN_TOOL_FINGER, false},
		{"BTN_TOUCH (0x14a)", evdev.BTN_TOUCH, false},
		{"BTN_TOOL_DOUBLETAP (0x14d)", evdev.BTN_TOOL_DOUBLETAP, false},
		{"BTN_WHEEL (0x150)", evdev.BTN_WHEEL, false},

		// The D-pad block, which the old two-range union excluded entirely.
		{"BTN_DPAD_UP (0x220)", evdev.BTN_DPAD_UP, true},
		{"BTN_DPAD_RIGHT (0x223, last D-pad code)", evdev.BTN_DPAD_RIGHT, true},

		// The HOTAS overflow block. Dropping this is the opposite failure:
		// half a Warthog's buttons silently vanishing.
		{"BTN_TRIGGER_HAPPY1 (0x2c0)", evdev.BTN_TRIGGER_HAPPY1, true},
		{"BTN_TRIGGER_HAPPY40 (0x2e7)", evdev.BTN_TRIGGER_HAPPY40, true},

		// Ordinary keyboard space, on both sides of the eligible blocks.
		// Admitting any of these would turn the joystick log lines into a
		// keylog -- see isJoystickButton's doc comment.
		{"KEY_A", evdev.KEY_A, false},
		{"KEY_OK (0x160)", evdev.KEY_OK, false},
		{"just below BTN_DPAD_UP (0x21f)", evdev.BTN_DPAD_UP - 1, false},
		{"just above BTN_DPAD_RIGHT (0x224)", evdev.BTN_DPAD_RIGHT + 1, false},
		{"just below BTN_TRIGGER_HAPPY1 (0x2bf)", evdev.BTN_TRIGGER_HAPPY1 - 1, false},
		{"just above BTN_TRIGGER_HAPPY40 (0x2e8)", evdev.BTN_TRIGGER_HAPPY40 + 1, false},
	}

	for _, tc := range cases {
		if got := isJoystickButton(tc.code); got != tc.want {
			t.Errorf("isJoystickButton(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestIsDigitiserButton pins the three codes looksLikeJoystick rejects a
// whole DEVICE for. Narrowing isJoystickButton alone is not enough: a device
// declaring BOTH a real joystick button and touch capability would still be
// opened and listed without this.
func TestIsDigitiserButton(t *testing.T) {
	for _, c := range []evdev.EvCode{evdev.BTN_DIGI, evdev.BTN_TOOL_FINGER, evdev.BTN_TOUCH} {
		if !isDigitiserButton(c) {
			t.Errorf("isDigitiserButton(%#x) = false, want true", c)
		}
	}
	for _, c := range []evdev.EvCode{evdev.BTN_JOYSTICK, evdev.BTN_THUMBR, evdev.BTN_TRIGGER_HAPPY1} {
		if isDigitiserButton(c) {
			t.Errorf("isDigitiserButton(%#x) = true, want false -- a real joystick button", c)
		}
	}
}

// TestNewOSSourceDoesNotFailOnEnumeration is the I3 guard on the Linux side.
//
// NewOSSource used to enumerate eagerly and return the error, which on Linux
// meant a permission denial destroyed the manager before it existed --
// collapsing "denied, here is the fix" into the same {Supported:false,
// Error:""} macOS reports for "no backend at all". The enumeration error now
// belongs to Manager.New's probe, which keeps Supported() true and surfaces
// the message. Construction must therefore succeed whatever /dev/input says.
func TestNewOSSourceDoesNotFailOnEnumeration(t *testing.T) {
	// A real discard logger, not nil. The Linux backend ignores the argument
	// today, so nil would not panic -- but the signature exists because the
	// WINDOWS backend logs through it, and a test that passes nil here is one
	// refactor away from a nil dereference. Every other test in this package
	// constructs its logger the same way.
	src, err := NewOSSource(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewOSSource() error = %v, want nil -- an enumeration or permission "+
			"failure must reach the UI through Manager.lastErr, not delete the manager", err)
	}
	if src == nil {
		t.Fatal("NewOSSource() returned a nil Source")
	}
	src.Close()
}
