package trigger

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// joyPrefix marks a persisted value as a joystick binding. Anything without
// it is a keyboard chord and goes to chord.Parse.
const joyPrefix = "joy:"

var (
	// ErrEmpty means the input string was empty.
	ErrEmpty = errors.New("trigger: empty")
	// ErrBadDevice means the device id was missing or contained an illegal character.
	ErrBadDevice = errors.New("trigger: bad device id")
	// ErrBadInput means the input part was not btn<n> or hat<n>.<dir>.
	ErrBadInput = errors.New("trigger: bad input")
	// ErrBadModifier means more than one modifier was supplied.
	ErrBadModifier = errors.New("trigger: at most one modifier is supported")
)

// hatDirNames maps the persisted direction names to their index, clockwise
// from up. Never reorder: these strings are written to config.toml.
var hatDirNames = [HatDirs]string{
	"up", "up_right", "right", "down_right", "down", "down_left", "left", "up_left",
}

// Parse reads a persisted trigger string. Values carrying the "joy:" prefix
// are joystick bindings; everything else is a keyboard chord.
func Parse(s string) (Trigger, error) {
	if strings.TrimSpace(s) == "" {
		return Trigger{}, ErrEmpty
	}
	if !strings.HasPrefix(s, joyPrefix) {
		c, err := chord.Parse(s)
		if err != nil {
			return Trigger{}, err
		}
		return Key(c), nil
	}

	parts := strings.Split(strings.TrimPrefix(s, joyPrefix), "+")
	if len(parts) > 2 {
		return Trigger{}, ErrBadModifier
	}

	main, err := parseRef(parts[len(parts)-1])
	if err != nil {
		return Trigger{}, err
	}
	b := JoyBinding{Device: main.Device, Button: main.Button}
	if len(parts) == 2 {
		mod, err := parseRef(parts[0])
		if err != nil {
			return Trigger{}, err
		}
		b.Modifier = &mod
	}
	return Joy(b), nil
}

// parseRef reads one "<device-id>:<input>" reference.
func parseRef(s string) (JoyButton, error) {
	dev, input, ok := strings.Cut(s, ":")
	if !ok {
		return JoyButton{}, ErrBadInput
	}
	if !ValidDeviceID(dev) {
		return JoyButton{}, fmt.Errorf("%w: %q", ErrBadDevice, dev)
	}
	btn, err := parseInput(input)
	if err != nil {
		return JoyButton{}, err
	}
	return JoyButton{Device: DeviceID(dev), Button: btn}, nil
}

// parseInput reads "btn<n>" or "hat<n>.<dir>". Both n values are 1-indexed in
// text and 0-indexed internally, matching Button.Label and how joystick
// vendors number their inputs.
func parseInput(s string) (Button, error) {
	switch {
	case strings.HasPrefix(s, "btn"):
		n, err := strconv.Atoi(strings.TrimPrefix(s, "btn"))
		if err != nil || n < 1 || n > int(MaxButton)+1 {
			return 0, fmt.Errorf("%w: %q", ErrBadInput, s)
		}
		return Button(n - 1), nil

	case strings.HasPrefix(s, "hat"):
		rest := strings.TrimPrefix(s, "hat")
		numStr, dirStr, ok := strings.Cut(rest, ".")
		if !ok {
			return 0, fmt.Errorf("%w: %q", ErrBadInput, s)
		}
		n, err := strconv.Atoi(numStr)
		if err != nil || n < 1 || n > HatCount {
			return 0, fmt.Errorf("%w: %q", ErrBadInput, s)
		}
		for dir, name := range hatDirNames {
			if name == dirStr {
				return HatButton(n-1, dir), nil
			}
		}
		return 0, fmt.Errorf("%w: %q", ErrBadInput, s)

	default:
		return 0, fmt.Errorf("%w: %q", ErrBadInput, s)
	}
}

// String renders the persisted form. Zero triggers render "".
func (t Trigger) String() string {
	if t.IsZero() {
		return ""
	}
	if t.Kind == KindKey {
		return t.Key.String()
	}
	var b strings.Builder
	b.WriteString(joyPrefix)
	if t.Joy.Modifier != nil {
		b.WriteString(refString(*t.Joy.Modifier))
		b.WriteByte('+')
	}
	b.WriteString(refString(t.Joy.Main()))
	return b.String()
}

func refString(j JoyButton) string {
	return string(j.Device) + ":" + inputString(j.Button)
}

func inputString(b Button) string {
	if b.IsHat() {
		hat, dir := b.Hat()
		return fmt.Sprintf("hat%d.%s", hat+1, hatDirNames[dir])
	}
	return fmt.Sprintf("btn%d", int(b)+1)
}
