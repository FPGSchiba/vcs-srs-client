// Package joystick reads gamepad, joystick and HOTAS input and turns it into
// the same Pressed/Released edges internal/hotkeys produces for the keyboard.
//
// It is a SIBLING of internal/hotkeys, not a change to it: the keyboard path
// is already shipped and tested, and joystick support is additive.
//
// The OS sits behind Source. Everything above that seam -- edge detection,
// specificity resolution, capture -- is a pure function over successive State
// values, so the entire behavioural core is testable with no device attached.
package joystick

import (
	"errors"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// ErrUnsupported is returned by backends on platforms where joystick input is
// not implemented. It is deliberately distinct from a permission denial: it
// is not something the user can fix, and the UI must not offer a grant
// affordance for it.
var ErrUnsupported = errors.New("joystick: not supported on this platform")

// Device is one attached input device.
//
// It carries NOTHING but the id and the name. Button and hat COUNTS were
// removed for the same reason State.Connected was: both backends populated
// them and nothing ever read them -- JoystickDeviceDTO carries only the id and
// the name, and binding is driven by what a button actually reports held,
// never by a declared count. A field no consumer reads is a field that
// silently rots.
type Device struct {
	// ID is stable across replug where the OS permits. Always a legal
	// trigger.DeviceID -- backends sanitise.
	ID trigger.DeviceID
	// Name is the human-readable product name, shown in the UI and persisted
	// as display metadata so an absent device is still nameable.
	Name string
}

// State is a snapshot of every held input across all connected devices.
//
// State deliberately does NOT carry per-device connectivity. Every consumer
// -- Active, Manager.tick, Manager.rediscover -- only ever needs to know
// which buttons are currently held, and a vanished device already reads as
// "nothing held" on its own: that is what makes release-on-unplug
// self-healing without a separate connected flag to keep in sync. Device
// presence for the UI comes from Source.Devices, a separate call.
type State struct {
	// Held is the set of currently-held inputs.
	Held map[trigger.JoyButton]struct{}
}

// IsHeld reports whether b is currently held.
func (s State) IsHeld(b trigger.JoyButton) bool {
	_, ok := s.Held[b]
	return ok
}

// Source is the OS seam. Implementations are not required to be safe for
// concurrent use: Manager serialises every call.
type Source interface {
	// Devices enumerates what is attached. Called on a slow timer for
	// hot-plug, and by the capture UI.
	Devices() ([]Device, error)
	// Poll returns the current held-input snapshot.
	Poll() (State, error)
	// Close releases the OS resources. Safe to call more than once and safe
	// on a Source that never opened anything.
	Close()
}
