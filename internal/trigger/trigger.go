// Package trigger is the dependency-free value type for "what activates an
// action". It generalises internal/chord's keyboard-only Chord to a sum of a
// keyboard chord and a joystick binding, and owns the persisted string form
// of both.
//
// It deliberately imports nothing outside the standard library and
// internal/chord, so it is testable on any platform with no OS involvement.
package trigger

import "github.com/FPGSchiba/vcs-srs-client/internal/chord"

// Kind discriminates the Trigger sum.
type Kind uint8

const (
	// KindKey is a keyboard chord.
	KindKey Kind = iota
	// KindJoy is a joystick button or hat direction.
	KindJoy
)

// DeviceID is a backend-generated stable device identity. Constrained to
// [A-Za-z0-9_.-]+ (see ValidDeviceID) so it can never contain the ':' or '+'
// used as field separators in the persisted form. Backends sanitise.
type DeviceID string

// JoyButton names one physical input on one device.
type JoyButton struct {
	Device DeviceID
	Button Button
}

// JoyBinding is a button (or hat direction), optionally gated behind a
// modifier button. The modifier carries its own DeviceID because holding a
// throttle button to qualify a stick button is a normal HOTAS pattern.
type JoyBinding struct {
	Device   DeviceID
	Button   Button
	Modifier *JoyButton // nil = bare binding
}

// Main returns the binding's own button as a JoyButton.
func (b JoyBinding) Main() JoyButton {
	return JoyButton{Device: b.Device, Button: b.Button}
}

// Trigger is one way to activate an action.
type Trigger struct {
	Kind Kind
	Key  chord.Chord // valid when Kind == KindKey
	Joy  JoyBinding  // valid when Kind == KindJoy
}

// Key builds a keyboard trigger.
func Key(c chord.Chord) Trigger { return Trigger{Kind: KindKey, Key: c} }

// Joy builds a joystick trigger.
func Joy(b JoyBinding) Trigger { return Trigger{Kind: KindJoy, Joy: b} }

// IsZero reports whether the trigger is unset.
func (t Trigger) IsZero() bool {
	if t.Kind == KindKey {
		return t.Key.IsZero()
	}
	return t.Joy.Device == ""
}

// Equal compares by value. It exists because Trigger contains a pointer
// (JoyBinding.Modifier): == would compare pointer identity, so two loads of
// the same config would compare unequal and every conflict check would miss.
func (t Trigger) Equal(o Trigger) bool {
	if t.Kind != o.Kind {
		return false
	}
	if t.Kind == KindKey {
		return t.Key == o.Key
	}
	if t.Joy.Device != o.Joy.Device || t.Joy.Button != o.Joy.Button {
		return false
	}
	switch {
	case t.Joy.Modifier == nil && o.Joy.Modifier == nil:
		return true
	case t.Joy.Modifier == nil || o.Joy.Modifier == nil:
		return false
	default:
		return *t.Joy.Modifier == *o.Joy.Modifier
	}
}
