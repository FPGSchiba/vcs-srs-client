//go:build windows

package joystick

import (
	"fmt"
	"strings"
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/joystick/di8"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// winSource reads joysticks through DirectInput8.
//
// COOPERATIVE LEVEL IS LOAD-BEARING. Every device is opened
// SCL_NONEXCLUSIVE | SCL_BACKGROUND:
//
//   - NONEXCLUSIVE because DirectInput allows exactly one exclusive owner per
//     device, and force feedback REQUIRES exclusive access. Taking it here
//     would mean Star Citizen could not have it -- the user would silently
//     lose force feedback on their HOTAS because a voice-comms app was
//     running. SDL takes exclusive unconditionally, which is exactly why we
//     do not use SDL.
//   - BACKGROUND because the whole point is reading push-to-talk while the
//     game has focus.
//
// This is the combination DCS-SRS ships and Microsoft documents as the
// default. If you are changing this line, you are introducing a defect.
type winSource struct {
	mu      sync.Mutex
	di      *di8.DirectInput
	helper  *helperWindow
	devices map[trigger.DeviceID]*winDevice
}

type winDevice struct {
	dev  *di8.Device
	info Device
}

// NewOSSource builds the platform joystick source.
func NewOSSource() (Source, error) {
	helper, err := newHelperWindow()
	if err != nil {
		return nil, err
	}
	dinput, err := di8.Create(di8.HINSTANCE(helper.inst))
	if err != nil {
		helper.Close()
		return nil, fmt.Errorf("joystick: create DirectInput: %w", err)
	}
	s := &winSource{di: dinput, helper: helper, devices: map[trigger.DeviceID]*winDevice{}}
	if _, err := s.Devices(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// sanitiseID reduces a GUID string to a legal trigger.DeviceID. Global
// constraint 7: the id must never contain ':' or '+', which are the
// persisted form's field separators.
func sanitiseID(s string) trigger.DeviceID {
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

// guidString renders a DirectInput GUID in the standard textual form. The
// vendored di8.GUID has no String() method, so this is done by hand; the
// result already satisfies trigger.ValidDeviceID (hex digits and hyphens
// only), but it is still routed through sanitiseID as a defensive backstop.
func guidString(g di8.GUID) string {
	return fmt.Sprintf("%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		g.Data1, g.Data2, g.Data3,
		g.Data4[0], g.Data4[1], g.Data4[2], g.Data4[3],
		g.Data4[4], g.Data4[5], g.Data4[6], g.Data4[7])
}

func (s *winSource) Devices() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := map[trigger.DeviceID]bool{}
	var out []Device

	err := s.di.EnumDevices(di8.DEVCLASS_GAMECTRL, func(inst *di8.DEVICEINSTANCE, _ uintptr) uintptr {
		id := sanitiseID(guidString(inst.GuidInstance))
		seen[id] = true
		if existing, ok := s.devices[id]; ok {
			out = append(out, existing.info)
			return di8.ENUM_CONTINUE
		}
		dev, err := s.di.CreateDevice(inst.GuidInstance)
		if err != nil {
			return di8.ENUM_CONTINUE // skip this device, keep enumerating
		}
		if err := dev.SetDataFormat(&di8.Joystick2); err != nil {
			dev.Release()
			return di8.ENUM_CONTINUE
		}
		// THE line. See the type comment.
		if err := dev.SetCooperativeLevel(di8.HWND(s.helper.hwnd), di8.SCL_NONEXCLUSIVE|di8.SCL_BACKGROUND); err != nil {
			dev.Release()
			return di8.ENUM_CONTINUE
		}
		if err := dev.Acquire(); err != nil {
			dev.Release()
			return di8.ENUM_CONTINUE
		}
		info := Device{
			ID:      id,
			Name:    inst.GetProductName(),
			Buttons: int(trigger.MaxButton) + 1,
			Hats:    trigger.HatCount,
		}
		s.devices[id] = &winDevice{dev: dev, info: info}
		out = append(out, info)
		return di8.ENUM_CONTINUE
	}, 0, di8.EDFL_ATTACHEDONLY)
	if err != nil {
		return nil, fmt.Errorf("joystick: enumerate devices: %w", err)
	}

	// Drop devices that went away so a replug re-acquires cleanly.
	for id, d := range s.devices {
		if !seen[id] {
			d.dev.Unacquire()
			d.dev.Release()
			delete(s.devices, id)
		}
	}
	return out, nil
}

func (s *winSource) Poll() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := State{
		Held: map[trigger.JoyButton]struct{}{},
	}
	for id, d := range s.devices {
		var raw di8.JOYSTATE2
		// Poll gives polling-model devices a chance to refresh their state
		// before we read it; on the interrupt-driven devices that make up
		// the overwhelming majority of modern HOTAS hardware it is a
		// documented no-op that returns DIERR_UNSUPPORTED, which is exactly
		// why the error is discarded here rather than swallowed silently --
		// it is not an error condition, it is the expected outcome for a
		// device that never needed this call. Skipping Poll entirely would
		// instead risk a polling-model device silently never updating.
		_ = d.dev.Poll()
		if err := d.dev.GetDeviceState(&raw); err != nil {
			_ = d.dev.Acquire()
			continue
		}

		for i, pressed := range raw.Buttons {
			if pressed&0x80 == 0 {
				continue
			}
			if trigger.Button(i) > trigger.MaxButton {
				break
			}
			st.Held[trigger.JoyButton{Device: id, Button: trigger.Button(i)}] = struct{}{}
		}
		for hat, angle := range raw.POV {
			if hat >= trigger.HatCount {
				break
			}
			dir, ok := povDirection(angle)
			if !ok {
				continue
			}
			st.Held[trigger.JoyButton{
				Device: id,
				Button: trigger.HatButton(hat, dir),
			}] = struct{}{}
		}
	}
	return st, nil
}

func (s *winSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.devices {
		d.dev.Unacquire()
		d.dev.Release()
		delete(s.devices, id)
	}
	if s.di != nil {
		s.di.Release()
		s.di = nil
	}
	s.helper.Close()
}
