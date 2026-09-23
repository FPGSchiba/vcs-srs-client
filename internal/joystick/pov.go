package joystick

// povDirection converts a DirectInput POV reading into one of the 8 hat
// directions. The angle is in hundredths of a degree clockwise from north;
// a centered hat reads as -1, which arrives as 0xFFFFFFFF or 0xFFFF
// depending on the driver, so anything at or above a full circle is treated
// as centered rather than trusting one sentinel.
//
// This answers "which of the 8 points is the hat nearest", NOT "which buttons
// are held" -- a diagonal reports its two adjacent cardinals as well. Callers
// driving State want povDirections; see expandHatDirection in hat.go for why.
func povDirection(raw uint32) (int, bool) {
	if raw >= 36000 {
		return 0, false
	}
	// Round to the nearest 45 degrees (4500 hundredths), then wrap 8 -> 0.
	return int((raw+2250)/4500) % 8, true
}

// povDirections returns every direction index a POV reading reports held: the
// nearest point, plus both adjacent cardinals when that point is a diagonal.
// A centered hat returns nil.
//
// This is the Windows half of the one-answer rule hat.go exists to enforce;
// the Linux half is hatAxisDirections. Both go through expandHatDirection, so
// the two backends cannot drift apart again the way they had.
func povDirections(raw uint32) []int {
	dir, ok := povDirection(raw)
	if !ok {
		return nil
	}
	return expandHatDirection(dir)
}
