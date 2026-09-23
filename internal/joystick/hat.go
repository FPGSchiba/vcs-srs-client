package joystick

// Hat direction handling, shared by every backend.
//
// This file carries NO build tag on purpose. The two backends used to derive
// hat directions independently -- povDirection (Windows) rounded a POV angle
// to one of 8 points including diagonals, while the Linux path mapped each
// ABS_HAT axis separately and could only ever produce the 4 cardinals -- and
// the two disagreed on identical hardware. Putting the decision here means
// there is exactly one answer to "what does a deflected hat report held", and
// it is testable on any host rather than only on the platform whose build tag
// happens to be active.
//
// DIRECTION ENCODING. Indices are the 8 compass points clockwise from up, the
// same encoding trigger.HatButton persists and trigger.Parse accepts:
//
//	0 up          1 up_right    2 right       3 down_right
//	4 down        5 down_left   6 left        7 up_left
//
// Even indices are cardinals, odd indices are diagonals. Nothing below
// hardcodes that beyond `dir%2`, but every table in this package relies on it.
//
// A DIAGONAL REPORTS THREE DIRECTIONS, NOT ONE. See expandHatDirection.

// expandHatDirection returns every direction index a hat deflected to dir
// reports held. A cardinal reports only itself; a diagonal reports itself AND
// both adjacent cardinals.
//
// WHY. Spec section 8 originally said only "round to the nearest 45 degrees",
// and the two backends resolved the silence differently:
//
//   - Windows emitted the diagonal ALONE. Bind global.ptt to hat1.up -- the
//     canonical HOTAS PTT placement -- transmit, and nudge the hat to
//     up-right, which is trivially easy on a 4-contact 8-way hat. The POV
//     read 4500, the only held button became hat1.up_right, Active() no
//     longer matched the binding, the refcount fell to zero and the MIC CLOSED
//     WITH THE HAT STILL DEFLECTED.
//   - Linux emitted both cardinals and no diagonal, so the same physical input
//     kept transmitting -- but the four diagonal grammar values parsed,
//     round-tripped and rendered while being unreachable on the platform, so a
//     Windows-authored config was silently inert.
//
// Emitting the diagonal AND both adjacent cardinals is the only option that
// fixes both: a "Hat up = PTT" bind survives a diagonal nudge, a deliberate
// hat1.up_right bind still gets its precise trigger, no grammar value is dead
// on either platform, and the backends become identical -- which was the
// actual defect.
//
// THE COST, ACCEPTED DELIBERATELY: a diagonal activates three buttons, so a
// user who binds BOTH hat1.up_right and hat1.up fires both actions on a
// diagonal. Active()'s specificity pass only suppresses a bare binding when
// some MODIFIER binding claims the same button, and neither of those is a
// modifier binding, so no suppression applies. That is the intended reading:
// the hat really is deflected up, and an action bound to "up" should fire.
// Capture is handled separately -- see suppressDiagonalCardinals in capture.go
// -- because binding three buttons at once is NOT what the user meant.
func expandHatDirection(dir int) []int {
	if dir%2 == 0 {
		return []int{dir}
	}
	// (dir+7)%8 and (dir+1)%8 are the cardinals either side, wrapping so
	// up_left (7) yields left (6) and up (0).
	return []int{dir, (dir + 7) % 8, (dir + 1) % 8}
}

// hatAxisDirection maps one hat's x/y deflection pair to a single direction
// index, before expansion. It is the axis-pair equivalent of povDirection.
//
// The sign convention is evdev's, which is also the screen convention: x > 0
// is right, y > 0 is DOWN. A hat at rest reads (0, 0) and returns false.
func hatAxisDirection(x, y int32) (int, bool) {
	switch {
	case y < 0 && x == 0:
		return 0, true // up
	case y < 0 && x > 0:
		return 1, true // up-right
	case y == 0 && x > 0:
		return 2, true // right
	case y > 0 && x > 0:
		return 3, true // down-right
	case y > 0 && x == 0:
		return 4, true // down
	case y > 0 && x < 0:
		return 5, true // down-left
	case y == 0 && x < 0:
		return 6, true // left
	case y < 0 && x < 0:
		return 7, true // up-left
	}
	return 0, false // centered
}

// hatAxisDirections is hatAxisDirection composed with expandHatDirection: the
// complete set of direction indices an x/y deflection pair reports held. A
// centered hat returns nil.
func hatAxisDirections(x, y int32) []int {
	dir, ok := hatAxisDirection(x, y)
	if !ok {
		return nil
	}
	return expandHatDirection(dir)
}
