package trigger

import "fmt"

// Button indexes one physical input on a device.
//
//	0..127   buttons
//	128..159 hat directions, encoded 128 + hat*8 + dir
//
// Encoding hats as high button indices (the DCS-SRS approach) means one
// edge-detection path and one capture path serve buttons and hats alike.
// This encoding is PERSISTED -- changing it silently reinterprets every
// stored hat binding.
type Button uint16

const (
	// MaxButton is the highest plain button index.
	MaxButton Button = 127
	// hatBase is the first hat-direction index.
	hatBase Button = 128
	// HatCount is how many hats a device may report (DIJOYSTATE2 has 4).
	HatCount = 4
	// HatDirs is how many directions a hat resolves to.
	HatDirs = 8
)

// hatArrows renders the 8 hat directions clockwise from up.
var hatArrows = [HatDirs]string{"↑", "↗", "→", "↘", "↓", "↙", "←", "↖"}

// HatButton encodes a hat direction as a Button. hat and dir are assumed in
// range; callers that take them from a device clamp first.
func HatButton(hat, dir int) Button {
	return hatBase + Button(hat*HatDirs+dir)
}

// IsHat reports whether b encodes a hat direction rather than a plain button.
func (b Button) IsHat() bool { return b >= hatBase }

// Hat decodes a hat-direction Button into its hat index and direction.
// Meaningless unless IsHat.
func (b Button) Hat() (hat, dir int) {
	n := int(b - hatBase)
	return n / HatDirs, n % HatDirs
}

// Label renders a human-readable name. Buttons and hats are 1-indexed for
// display because that is how every joystick vendor numbers them, while the
// wire encoding stays 0-indexed.
func (b Button) Label() string {
	if b.IsHat() {
		hat, dir := b.Hat()
		return fmt.Sprintf("Hat %d %s", hat+1, hatArrows[dir])
	}
	return fmt.Sprintf("Btn %d", int(b)+1)
}

// ValidDeviceID reports whether s is a legal DeviceID: one or more of
// [A-Za-z0-9_.-]. The excluded ':' and '+' are the persisted form's field
// separators (see parse.go), so allowing them would let a device id forge a
// field boundary or a modifier.
func ValidDeviceID(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '_', c == '-', c == '.':
		default:
			return false
		}
	}
	return true
}
