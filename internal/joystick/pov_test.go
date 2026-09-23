package joystick

import "testing"

func TestPovDirection(t *testing.T) {
	// DirectInput reports POV angle in hundredths of a degree, clockwise
	// from north, and a centered hat as -1 (0xFFFFFFFF) -- though some
	// drivers report any value with the high word set.
	cases := []struct {
		raw     uint32
		wantDir int
		wantOK  bool
	}{
		{0, 0, true},           // up
		{4500, 1, true},        // up-right
		{9000, 2, true},        // right
		{13500, 3, true},       // down-right
		{18000, 4, true},       // down
		{22500, 5, true},       // down-left
		{27000, 6, true},       // left
		{31500, 7, true},       // up-left
		{35999, 0, true},       // wraps back to up
		{2200, 0, true},        // rounds down to up
		{2300, 1, true},        // rounds up to up-right
		{0xFFFFFFFF, 0, false}, // centered
		{0xFFFF, 0, false},     // centered, 16-bit form
	}
	for _, c := range cases {
		dir, ok := povDirection(c.raw)
		if ok != c.wantOK {
			t.Errorf("povDirection(%d) ok = %v, want %v", c.raw, ok, c.wantOK)
			continue
		}
		if ok && dir != c.wantDir {
			t.Errorf("povDirection(%d) = %d, want %d", c.raw, dir, c.wantDir)
		}
	}
}
