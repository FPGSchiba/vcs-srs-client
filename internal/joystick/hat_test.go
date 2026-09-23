package joystick

import (
	"sort"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func sortedInts(in []int) []int {
	out := append([]int(nil), in...)
	sort.Ints(out)
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestExpandHatDirection pins the ruling in spec section 8: a cardinal reports
// itself alone, a diagonal reports itself PLUS both adjacent cardinals.
//
// The diagonal rows are the fix for the Windows PTT cut -- without the two
// cardinals, a hat nudged from up to up-right stops satisfying a binding on
// hat1.up and closes the mic mid-transmission. The cardinal rows are what
// stops the fix from over-reaching: pressing straight up must not also report
// the two diagonals either side.
func TestExpandHatDirection(t *testing.T) {
	cases := []struct {
		dir  int
		want []int
	}{
		{0, []int{0}},       // up
		{2, []int{2}},       // right
		{4, []int{4}},       // down
		{6, []int{6}},       // left
		{1, []int{0, 1, 2}}, // up-right + up + right
		{3, []int{2, 3, 4}}, // down-right + right + down
		{5, []int{4, 5, 6}}, // down-left + down + left
		{7, []int{0, 6, 7}}, // up-left + left + up -- the wrap case
	}
	for _, c := range cases {
		got := sortedInts(expandHatDirection(c.dir))
		if !equalInts(got, c.want) {
			t.Errorf("expandHatDirection(%d) = %v, want %v", c.dir, got, c.want)
		}
	}
}

// TestPovDirectionsExpandsDiagonals is the Windows half of the one-answer
// rule. povDirection itself is unchanged and still tested by TestPovDirection;
// this covers what Poll() actually puts in State.Held.
func TestPovDirectionsExpandsDiagonals(t *testing.T) {
	cases := []struct {
		raw  uint32
		want []int
	}{
		{0, []int{0}},           // up
		{4500, []int{0, 1, 2}},  // up-right: THE regression. Must include up.
		{9000, []int{2}},        // right
		{13500, []int{2, 3, 4}}, // down-right
		{18000, []int{4}},       // down
		{22500, []int{4, 5, 6}}, // down-left
		{27000, []int{6}},       // left
		{31500, []int{0, 6, 7}}, // up-left
		{0xFFFFFFFF, nil},       // centered
		{0xFFFF, nil},           // centered, 16-bit form
	}
	for _, c := range cases {
		got := sortedInts(povDirections(c.raw))
		if !equalInts(got, sortedInts(c.want)) {
			t.Errorf("povDirections(%d) = %v, want %v", c.raw, got, c.want)
		}
	}
}

// TestHatAxisDirections is the Linux half. The old hatDirection mapped each
// ABS_HAT axis independently and could only ever return a cardinal, which made
// the four diagonal grammar values unreachable on Linux -- a Windows-authored
// config was silently inert. Pairing X and Y and routing both backends through
// expandHatDirection is what makes these two tables agree.
//
// Sign convention is evdev's: x > 0 right, y > 0 DOWN.
func TestHatAxisDirections(t *testing.T) {
	cases := []struct {
		x, y int32
		want []int
	}{
		{0, 0, nil},              // centered
		{0, -1, []int{0}},        // up
		{1, 0, []int{2}},         // right
		{0, 1, []int{4}},         // down
		{-1, 0, []int{6}},        // left
		{1, -1, []int{0, 1, 2}},  // up-right
		{1, 1, []int{2, 3, 4}},   // down-right
		{-1, 1, []int{4, 5, 6}},  // down-left
		{-1, -1, []int{0, 6, 7}}, // up-left
	}
	for _, c := range cases {
		got := sortedInts(hatAxisDirections(c.x, c.y))
		if !equalInts(got, sortedInts(c.want)) {
			t.Errorf("hatAxisDirections(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// TestBackendsAgreeOnEveryHatDirection is the actual defect this wave fixes:
// the two backends resolved the same physical deflection differently. For all
// 8 points, the POV angle and the axis pair that describe the same deflection
// must produce the same held-direction set.
func TestBackendsAgreeOnEveryHatDirection(t *testing.T) {
	// The axis pair for each of the 8 points, in direction order.
	axes := [8][2]int32{
		{0, -1},  // 0 up
		{1, -1},  // 1 up-right
		{1, 0},   // 2 right
		{1, 1},   // 3 down-right
		{0, 1},   // 4 down
		{-1, 1},  // 5 down-left
		{-1, 0},  // 6 left
		{-1, -1}, // 7 up-left
	}
	for dir := 0; dir < 8; dir++ {
		win := sortedInts(povDirections(uint32(dir) * 4500))
		lin := sortedInts(hatAxisDirections(axes[dir][0], axes[dir][1]))
		if !equalInts(win, lin) {
			t.Errorf("direction %d: windows reports %v, linux reports %v -- the backends must agree",
				dir, win, lin)
		}
	}
}

// holdHatAt makes the fake source report exactly what a real backend reports
// for hat 0 deflected to the given POV angle. The held set is DERIVED from
// povDirections rather than hand-listed, so these dispatch tests cannot pass
// against a held set the shipped backend would never produce.
func holdHatAt(src *fakeSource, raw uint32) {
	for d := 0; d < trigger.HatDirs; d++ {
		src.release(trigger.JoyButton{Device: tdev, Button: trigger.HatButton(0, d)})
	}
	for _, d := range povDirections(raw) {
		src.hold(trigger.JoyButton{Device: tdev, Button: trigger.HatButton(0, d)})
	}
}

const povCentered = uint32(0xFFFFFFFF)

// TestHatDiagonalDoesNotCutAPTTBoundToTheCardinal is THE regression test for
// this wave.
//
// hat1.up is the canonical HOTAS PTT placement, and a 4-contact 8-way hat is
// trivially easy to nudge off-axis mid-transmission. Before the fix the
// Windows backend reported the diagonal ALONE: Held lost button 128 and gained
// 129, Active() stopped matching, the refcount fell 1->0 and HotkeyReleased
// fired with the hat still physically deflected. The mic closed mid-word.
func TestHatDiagonalDoesNotCutAPTTBoundToTheCardinal(t *testing.T) {
	m, src, rec := testManager(t)
	if err := m.Apply(map[string][]Binding{
		"global.ptt": {holdBind(trigger.HatButton(0, 0))},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	holdHatAt(src, 0) // straight up
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt"})

	holdHatAt(src, 4500) // nudged to up-right, still very much deflected
	m.tick()
	m.tick() // two ticks: a cut would show on either
	eq(t, rec.events(), []string{"down:global.ptt"})

	holdHatAt(src, povCentered) // released
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
}

// TestHatDiagonalBindingStillFires is the other half of the ruling: emitting
// the adjacent cardinals must not cost a user who deliberately bound the
// diagonal its precise trigger.
func TestHatDiagonalBindingStillFires(t *testing.T) {
	m, src, rec := testManager(t)
	if err := m.Apply(map[string][]Binding{
		"global.ptt": {holdBind(trigger.HatButton(0, 1))}, // hat1.up_right
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	holdHatAt(src, 0) // straight up must NOT fire an up_right binding
	m.tick()
	eq(t, rec.events(), nil)

	holdHatAt(src, 4500)
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt"})

	holdHatAt(src, povCentered)
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
}

// TestCaptureOfADiagonalYieldsTheDiagonalAlone covers the half of the ruling
// that goes the other way. Dispatch wants three directions held; capture wants
// one binding, because the user pressed one thing. Without
// suppressDiagonalCardinals the three arrive in one poll sample, the
// (Device, Button) tie-break makes hat1.up (128) the modifier and hat1.right
// (130) the main input, and the user gets `joy:dev:hat1.up+dev:hat1.right` --
// a combination they never pressed and cannot press on purpose.
func TestCaptureOfADiagonalYieldsTheDiagonalAlone(t *testing.T) {
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	m.tick() // baseline

	holdHatAt(src, 4500) // up-right: three inputs in one sample
	m.tick()
	m.tick() // a second sample: the cardinals must stay suppressed, not
	// slip in once the diagonal is no longer "fresh"

	holdHatAt(src, povCentered)
	m.tick()

	if got == nil {
		t.Fatal("capture did not complete on release")
	}
	if want := trigger.HatButton(0, 1); got.Binding.Button != want {
		t.Errorf("captured button %v (%s), want %v (%s)",
			got.Binding.Button, got.Binding.Button.Label(), want, want.Label())
	}
	if got.Binding.Modifier != nil {
		t.Errorf("captured a modifier %v from a single diagonal press; want none",
			got.Binding.Modifier)
	}
}

// TestCaptureOfACardinalIsUnaffected pins that the diagonal suppression does
// not reach an ordinary hat press.
func TestCaptureOfACardinalIsUnaffected(t *testing.T) {
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	m.tick() // baseline
	holdHatAt(src, 0)
	m.tick()
	holdHatAt(src, povCentered)
	m.tick()

	if got == nil {
		t.Fatal("capture did not complete on release")
	}
	if want := trigger.HatButton(0, 0); got.Binding.Button != want {
		t.Errorf("captured button %v, want %v", got.Binding.Button, want)
	}
	if got.Binding.Modifier != nil {
		t.Errorf("captured a modifier %v from a single cardinal press", got.Binding.Modifier)
	}
}
