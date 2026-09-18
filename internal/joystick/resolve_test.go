package joystick_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/joystick"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

const dev = trigger.DeviceID("stick-c3")
const dev2 = trigger.DeviceID("throttle-a1")

func jb(d trigger.DeviceID, b trigger.Button) trigger.JoyButton {
	return trigger.JoyButton{Device: d, Button: b}
}

// state builds a State with the given buttons held.
func state(held ...trigger.JoyButton) joystick.State {
	s := joystick.State{
		Held: map[trigger.JoyButton]struct{}{},
	}
	for _, h := range held {
		s.Held[h] = struct{}{}
	}
	return s
}

func bare(d trigger.DeviceID, b trigger.Button) joystick.Binding {
	return joystick.Binding{Joy: trigger.JoyBinding{Device: d, Button: b}, Hold: true}
}

func withMod(d trigger.DeviceID, b trigger.Button, mod trigger.JoyButton) joystick.Binding {
	m := mod
	return joystick.Binding{
		Joy:  trigger.JoyBinding{Device: d, Button: b, Modifier: &m},
		Hold: true,
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestActiveBareBinding(t *testing.T) {
	binds := map[string][]joystick.Binding{
		"global.ptt": {bare(dev, 3)},
	}
	if got := joystick.Active(binds, state(jb(dev, 3))); !got["global.ptt"] {
		t.Errorf("Active = %v, want global.ptt held", keys(got))
	}
	if got := joystick.Active(binds, state(jb(dev, 4))); got["global.ptt"] {
		t.Errorf("Active = %v, want nothing held", keys(got))
	}
}

func TestModifierMustAlsoBeHeld(t *testing.T) {
	binds := map[string][]joystick.Binding{
		"radio.1.ptt": {withMod(dev, 3, jb(dev, 5))},
	}
	if got := joystick.Active(binds, state(jb(dev, 3))); got["radio.1.ptt"] {
		t.Error("modifier binding fired without its modifier held")
	}
	if got := joystick.Active(binds, state(jb(dev, 3), jb(dev, 5))); !got["radio.1.ptt"] {
		t.Error("modifier binding did not fire with both held")
	}
}

func TestSpecificitySuppressesTheBareBinding(t *testing.T) {
	// The worked example from spec section 5:
	//   global.ptt  = Btn3
	//   radio.1.ptt = Btn5 + Btn3
	binds := map[string][]joystick.Binding{
		"global.ptt":  {bare(dev, 3)},
		"radio.1.ptt": {withMod(dev, 3, jb(dev, 5))},
	}

	got := joystick.Active(binds, state(jb(dev, 3)))
	if !got["global.ptt"] || got["radio.1.ptt"] {
		t.Errorf("Btn3 alone: Active = %v, want only global.ptt", keys(got))
	}

	got = joystick.Active(binds, state(jb(dev, 3), jb(dev, 5)))
	if !got["radio.1.ptt"] {
		t.Errorf("Btn5+Btn3: Active = %v, want radio.1.ptt", keys(got))
	}
	if got["global.ptt"] {
		t.Errorf("Btn5+Btn3: global.ptt was NOT suppressed; Active = %v", keys(got))
	}
}

func TestSuppressionAlsoAppliesWhenTheBareBindingIsTheModifier(t *testing.T) {
	// global.ptt is bound to the very button another binding uses AS its
	// modifier. Holding the combo must not also fire global.ptt.
	binds := map[string][]joystick.Binding{
		"global.ptt":  {bare(dev, 5)},
		"radio.1.ptt": {withMod(dev, 3, jb(dev, 5))},
	}
	got := joystick.Active(binds, state(jb(dev, 3), jb(dev, 5)))
	if !got["radio.1.ptt"] {
		t.Errorf("Active = %v, want radio.1.ptt", keys(got))
	}
	if got["global.ptt"] {
		t.Errorf("bare binding on the modifier button was not suppressed: %v", keys(got))
	}
}

func TestCrossDeviceModifier(t *testing.T) {
	// Throttle button qualifying a stick button -- a normal HOTAS pattern.
	binds := map[string][]joystick.Binding{
		"radio.2.ptt": {withMod(dev, 3, jb(dev2, 7))},
	}
	if got := joystick.Active(binds, state(jb(dev, 3))); got["radio.2.ptt"] {
		t.Error("fired without the cross-device modifier")
	}
	if got := joystick.Active(binds, state(jb(dev, 3), jb(dev2, 7))); !got["radio.2.ptt"] {
		t.Error("cross-device modifier did not fire")
	}
}

func TestUnrelatedModifierBindingDoesNotSuppress(t *testing.T) {
	// A modifier binding on entirely different buttons must leave the bare
	// binding alone -- suppression is targeted, not global.
	binds := map[string][]joystick.Binding{
		"global.ptt":  {bare(dev, 3)},
		"radio.1.ptt": {withMod(dev, 9, jb(dev, 8))},
	}
	got := joystick.Active(binds, state(jb(dev, 3), jb(dev, 8), jb(dev, 9)))
	if !got["global.ptt"] {
		t.Errorf("unrelated modifier binding suppressed global.ptt: %v", keys(got))
	}
	if !got["radio.1.ptt"] {
		t.Errorf("Active = %v, want radio.1.ptt too", keys(got))
	}
}

func TestActionWithSeveralTriggersIsHeldByAnyOfThem(t *testing.T) {
	binds := map[string][]joystick.Binding{
		"global.ptt": {bare(dev, 3), bare(dev2, 7)},
	}
	if got := joystick.Active(binds, state(jb(dev2, 7))); !got["global.ptt"] {
		t.Error("second trigger of the same action did not hold it")
	}
}

func TestEmptyStateHoldsNothing(t *testing.T) {
	binds := map[string][]joystick.Binding{
		"global.ptt": {bare(dev, 3)},
	}
	empty := joystick.State{
		Held: map[trigger.JoyButton]struct{}{},
	}
	if got := joystick.Active(binds, empty); len(got) != 0 {
		t.Errorf("Active on empty state = %v, want nothing", keys(got))
	}
}
