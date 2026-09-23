package joystick

// povDirection converts a DirectInput POV reading into one of the 8 hat
// directions. The angle is in hundredths of a degree clockwise from north;
// a centered hat reads as -1, which arrives as 0xFFFFFFFF or 0xFFFF
// depending on the driver, so anything at or above a full circle is treated
// as centered rather than trusting one sentinel.
func povDirection(raw uint32) (int, bool) {
	if raw >= 36000 {
		return 0, false
	}
	// Round to the nearest 45 degrees (4500 hundredths), then wrap 8 -> 0.
	return int((raw+2250)/4500) % 8, true
}
