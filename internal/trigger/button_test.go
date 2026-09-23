package trigger_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func TestHatButtonRoundTrip(t *testing.T) {
	for hat := 0; hat < trigger.HatCount; hat++ {
		for dir := 0; dir < trigger.HatDirs; dir++ {
			b := trigger.HatButton(hat, dir)
			if !b.IsHat() {
				t.Fatalf("HatButton(%d,%d) = %d, IsHat() = false, want true", hat, dir, b)
			}
			gotHat, gotDir := b.Hat()
			if gotHat != hat || gotDir != dir {
				t.Errorf("HatButton(%d,%d).Hat() = (%d,%d), want (%d,%d)",
					hat, dir, gotHat, gotDir, hat, dir)
			}
		}
	}
}

func TestPlainButtonIsNotHat(t *testing.T) {
	for _, b := range []trigger.Button{0, 1, 42, 127} {
		if b.IsHat() {
			t.Errorf("Button(%d).IsHat() = true, want false", b)
		}
	}
}

func TestHatEncodingIsContiguousFrom128(t *testing.T) {
	// Pins the on-the-wire encoding: 128 + hat*8 + dir. A change here
	// silently reinterprets every persisted hat binding.
	cases := map[trigger.Button][2]int{
		128: {0, 0},
		129: {0, 1},
		135: {0, 7},
		136: {1, 0},
		159: {3, 7},
	}
	for b, want := range cases {
		hat, dir := b.Hat()
		if hat != want[0] || dir != want[1] {
			t.Errorf("Button(%d).Hat() = (%d,%d), want (%d,%d)", b, hat, dir, want[0], want[1])
		}
		if got := trigger.HatButton(want[0], want[1]); got != b {
			t.Errorf("HatButton(%d,%d) = %d, want %d", want[0], want[1], got, b)
		}
	}
}

func TestButtonLabel(t *testing.T) {
	cases := []struct {
		b    trigger.Button
		want string
	}{
		{0, "Btn 1"}, // 0-indexed internally, 1-indexed for humans
		{11, "Btn 12"},
		{127, "Btn 128"},
		{trigger.HatButton(0, 0), "Hat 1 ↑"},
		{trigger.HatButton(0, 1), "Hat 1 ↗"},
		{trigger.HatButton(1, 2), "Hat 2 →"},
		{trigger.HatButton(3, 7), "Hat 4 ↖"},
	}
	for _, c := range cases {
		if got := c.b.Label(); got != c.want {
			t.Errorf("Button(%d).Label() = %q, want %q", c.b, got, c.want)
		}
	}
}

func TestValidDeviceID(t *testing.T) {
	valid := []string{"abc", "A-1_2.3", "6F1D2B70-D5A0-11CF", "x"}
	for _, s := range valid {
		if !trigger.ValidDeviceID(s) {
			t.Errorf("ValidDeviceID(%q) = false, want true", s)
		}
	}
	// ':' and '+' are the persisted-form field separators and MUST be rejected,
	// or a device id could forge a modifier or a field boundary.
	invalid := []string{"", "a:b", "a+b", "a b", "a/b", "ä"}
	for _, s := range invalid {
		if trigger.ValidDeviceID(s) {
			t.Errorf("ValidDeviceID(%q) = true, want false", s)
		}
	}
}

func TestTriggerEqual(t *testing.T) {
	mod := trigger.JoyButton{Device: "d1", Button: 5}
	a := trigger.Joy(trigger.JoyBinding{Device: "d1", Button: 3, Modifier: &mod})
	// Distinct pointer, same value: Equal must compare the POINTEE, not the
	// pointer, or two loads of the same config compare unequal.
	mod2 := trigger.JoyButton{Device: "d1", Button: 5}
	b := trigger.Joy(trigger.JoyBinding{Device: "d1", Button: 3, Modifier: &mod2})
	if !a.Equal(b) {
		t.Error("triggers with equal modifier values compare unequal")
	}
	c := trigger.Joy(trigger.JoyBinding{Device: "d1", Button: 3})
	if a.Equal(c) {
		t.Error("modifier and bare triggers compare equal")
	}
}
