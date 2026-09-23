//go:build linux

package joystick

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	evdev "github.com/holoplot/go-evdev"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// linuxSource reads joysticks through evdev.
//
// Reading evdev is inherently non-exclusive -- we never EVIOCGRAB -- so a game
// running under Proton reads the same devices undisturbed. That is the same
// property the Windows backend has to work for, and here it is free.
//
// DEVICE FILTER. Only devices that look like joysticks are opened. This is a
// correctness requirement, not a tidiness one: the manager logs which button
// fired an action, which is safe precisely because a joystick backend cannot
// observe typing. Opening a keyboard here would turn those log lines into a
// keylog.
type linuxSource struct {
	mu      sync.Mutex
	devices map[trigger.DeviceID]*linuxDevice
}

type linuxDevice struct {
	dev  *evdev.InputDevice
	info Device
	// hatAxes maps an ABS_HAT* axis code to its hat index (0..HatCount-1).
	hatAxes map[evdev.EvCode]int
	// buttons maps an EV_KEY code to our button index, in the order the
	// device declares them -- BTN_TRIGGER, BTN_THUMB, ... are not contiguous.
	buttons map[evdev.EvCode]trigger.Button
}

// inputDevDir is where Linux exposes evdev character devices.
const inputDevDir = "/dev/input"

// NewOSSource builds the platform joystick source.
func NewOSSource() (Source, error) {
	s := &linuxSource{devices: map[trigger.DeviceID]*linuxDevice{}}
	if _, err := s.Devices(); err != nil {
		return nil, err
	}
	return s, nil
}

// looksLikeJoystick reports whether the device declares joystick-shaped
// capabilities: an absolute X axis plus at least one gamepad/joystick button.
// See the type comment for why this filter is load-bearing: a keyboard
// declares EV_KEY capability but no ABS_X, and reports none of the codes in
// BTN_JOYSTICK..BTN_GAMEPAD's range, so it can never pass this check.
func looksLikeJoystick(d *evdev.InputDevice) bool {
	hasAbsX := false
	for _, c := range d.CapableEvents(evdev.EV_ABS) {
		if c == evdev.ABS_X {
			hasAbsX = true
			break
		}
	}
	if !hasAbsX {
		return false
	}
	for _, c := range d.CapableEvents(evdev.EV_KEY) {
		if c >= evdev.BTN_JOYSTICK && c <= evdev.BTN_GAMEPAD+0x7f {
			return true
		}
	}
	return false
}

// listEventPaths enumerates /dev/input/eventN nodes.
//
// This is deliberately NOT evdev.ListDevicePaths: that helper opens every
// node itself (read-only) purely to read its name, and silently drops any
// node it cannot open rather than returning an error -- which would swallow
// exactly the permission failure permissionError below exists to report (a
// user who is not in the 'input' group would see an empty, error-free device
// list instead of an actionable message). Listing the directory ourselves
// keeps every open attempt, and its error, visible to Devices.
func listEventPaths() ([]string, error) {
	entries, err := os.ReadDir(inputDevDir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		paths = append(paths, filepath.Join(inputDevDir, e.Name()))
	}
	return paths, nil
}

// stableID prefers the /dev/input/by-id symlink, which carries the device
// model and serial and therefore survives a replug. It falls back to the
// device name when no symlink exists.
func stableID(path, name string) trigger.DeviceID {
	const byID = "/dev/input/by-id"
	entries, err := os.ReadDir(byID)
	if err == nil {
		for _, e := range entries {
			link := filepath.Join(byID, e.Name())
			if target, err := filepath.EvalSymlinks(link); err == nil && target == path {
				return sanitiseLinuxID(e.Name())
			}
		}
	}
	return sanitiseLinuxID(name)
}

// sanitiseLinuxID enforces global constraint 7: ':' and '+' can never appear
// in a DeviceID, because they are the persisted form's field separators.
// /dev/input/by-id names and kernel device names both routinely contain
// spaces, colons and other characters outside trigger.ValidDeviceID's
// [A-Za-z0-9_.-] charset, so every non-matching rune is mapped to '-' rather
// than passed through.
func sanitiseLinuxID(s string) trigger.DeviceID {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if !trigger.ValidDeviceID(out) {
		return "unknown-device"
	}
	return trigger.DeviceID(out)
}

func (s *linuxSource) Devices() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths, err := listEventPaths()
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, permissionError(err)
		}
		return nil, fmt.Errorf("joystick: list input devices: %w", err)
	}

	seen := map[trigger.DeviceID]bool{}
	var out []Device
	var permErr error

	for _, p := range paths {
		// Read-only: we only ever query state (EVIOCGKEY, EVIOCGABS,
		// EVIOCGBIT), never write an event or take the exclusive grab, so
		// there is no reason to ask for more than read access.
		dev, err := evdev.OpenWithFlags(p, os.O_RDONLY)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				permErr = permissionError(err)
			}
			continue
		}
		if !looksLikeJoystick(dev) {
			dev.Close()
			continue
		}
		name, _ := dev.Name()
		id := stableID(p, name)
		if seen[id] {
			dev.Close()
			continue
		}
		seen[id] = true

		if existing, ok := s.devices[id]; ok {
			dev.Close()
			out = append(out, existing.info)
			continue
		}

		ld := &linuxDevice{
			dev:     dev,
			hatAxes: map[evdev.EvCode]int{},
			buttons: map[evdev.EvCode]trigger.Button{},
		}

		// Bounds check is genuinely reachable here, unlike a purely
		// defensive one: evdev's joystick/gamepad button range
		// (BTN_JOYSTICK..BTN_GAMEPAD+0x7f, ~144 codes) is wider than
		// trigger.MaxButton+1 (128) slots, so a device declaring capability
		// across most of that range would overflow without this guard.
		var next trigger.Button
		for _, c := range dev.CapableEvents(evdev.EV_KEY) {
			if next > trigger.MaxButton {
				break
			}
			ld.buttons[c] = next
			next++
		}

		// hatSeen counts DISTINCT hats, not axis codes: each hat reports an
		// X and a Y code, so counting codes would double the real total.
		// The hatIdx >= trigger.HatCount guard is defence in depth rather
		// than a reachable path today: evdev defines exactly
		// ABS_HAT0X..ABS_HAT3Y (4 hats), which already equals
		// trigger.HatCount, so hatIdx can never exceed 3. It is kept for the
		// same reason the button guard above is kept for real: callers of
		// trigger.HatButton must clamp, and this is that clamp.
		hatSeen := map[int]bool{}
		for _, c := range dev.CapableEvents(evdev.EV_ABS) {
			if c < evdev.ABS_HAT0X || c > evdev.ABS_HAT3Y {
				continue
			}
			hatIdx := int(c-evdev.ABS_HAT0X) / 2
			if hatIdx >= trigger.HatCount {
				continue
			}
			ld.hatAxes[c] = hatIdx
			hatSeen[hatIdx] = true
		}

		ld.info = Device{
			ID:      id,
			Name:    name,
			Buttons: len(ld.buttons),
			Hats:    len(hatSeen),
		}
		s.devices[id] = ld
		out = append(out, ld.info)
	}

	// Drop devices that went away so a replug re-opens cleanly.
	for id, d := range s.devices {
		if !seen[id] {
			d.dev.Close()
			delete(s.devices, id)
		}
	}

	if len(out) == 0 && permErr != nil {
		return nil, permErr
	}
	return out, nil
}

// permissionError names the remedy. /dev/input/event* is typically
// 0660 root:input, so a user who has never played a game under Steam may
// simply not be in the input group. Deliberately NOT routed through the
// keyboard path's Accessibility messaging: different cause, different fix.
func permissionError(err error) error {
	return fmt.Errorf(
		"joystick: cannot read /dev/input (add your user to the 'input' group, then log out and back in): %w",
		err)
}

func (s *linuxSource) Poll() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := State{Held: map[trigger.JoyButton]struct{}{}}
	for id, d := range s.devices {
		keys, err := d.dev.State(evdev.EV_KEY)
		if err != nil {
			continue
		}
		for code, pressed := range keys {
			if !pressed {
				continue
			}
			btn, ok := d.buttons[code]
			if !ok {
				continue
			}
			st.Held[trigger.JoyButton{Device: id, Button: btn}] = struct{}{}
		}

		if len(d.hatAxes) == 0 {
			continue
		}
		// AbsInfos reads every absolute axis the device supports in one
		// ioctl round trip per axis; go-evdev has no single-axis
		// equivalent, so this is called once per device per poll rather
		// than once per hat.
		absInfos, err := d.dev.AbsInfos()
		if err != nil {
			continue
		}
		for code, hat := range d.hatAxes {
			info, ok := absInfos[code]
			if !ok || info.Value == 0 {
				continue
			}
			dir, ok := hatDirection(code, info.Value)
			if !ok {
				continue
			}
			st.Held[trigger.JoyButton{Device: id, Button: trigger.HatButton(hat, dir)}] = struct{}{}
		}
	}
	return st, nil
}

// hatDirection maps one hat axis deflection to a direction index. Diagonals
// are not represented: evdev reports X and Y separately, so a diagonal press
// registers as two directions held at once, which is the behaviour a user
// binding "hat up" expects anyway.
func hatDirection(code evdev.EvCode, value int32) (int, bool) {
	isX := (code-evdev.ABS_HAT0X)%2 == 0
	switch {
	case isX && value > 0:
		return 2, true // right
	case isX && value < 0:
		return 6, true // left
	case !isX && value < 0:
		return 0, true // up
	case !isX && value > 0:
		return 4, true // down
	}
	return 0, false
}

func (s *linuxSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.devices {
		d.dev.Close()
		delete(s.devices, id)
	}
}
