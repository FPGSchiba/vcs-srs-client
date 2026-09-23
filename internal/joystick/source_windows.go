//go:build windows

package joystick

import (
	"fmt"
	"log/slog"
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
	log     *slog.Logger
	di      *di8.DirectInput
	helper  *helperWindow
	devices map[trigger.DeviceID]*winDevice

	// openFailed records, per device, WHICH DirectInput call last failed to
	// open it. Opening a device can fail for a perfectly ordinary reason --
	// something else already holds it -- and Devices() is re-run every
	// RediscoverInterval (3s) forever, so logging unconditionally would spam
	// the file at 20 lines a minute for as long as the app runs. Keying on
	// the failing call means one line per device per TRANSITION: the first
	// failure, and again only if the failure MOVES to a different call or
	// recurs after a success.
	//
	// This is the most likely real-hardware failure mode, and without a log
	// line "never enumerated" and "enumerated but SetCooperativeLevel
	// failed" are indistinguishable from the outside -- which is exactly the
	// question hardware verification has to answer.
	openFailed map[trigger.DeviceID]string
}

type winDevice struct {
	dev  *di8.Device
	info Device
}

// NewOSSource builds the platform joystick source.
func NewOSSource(log *slog.Logger) (Source, error) {
	if log == nil {
		log = slog.Default()
	}
	helper, err := newHelperWindow()
	if err != nil {
		return nil, err
	}
	dinput, err := di8.Create(di8.HINSTANCE(helper.inst))
	if err != nil {
		helper.Close()
		return nil, fmt.Errorf("joystick: create DirectInput: %w", err)
	}
	s := &winSource{
		log:        log,
		di:         dinput,
		helper:     helper,
		devices:    map[trigger.DeviceID]*winDevice{},
		openFailed: map[trigger.DeviceID]string{},
	}
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
		name := inst.GetProductName()
		dev, err := s.di.CreateDevice(inst.GuidInstance)
		if err != nil {
			s.noteOpenFailure(id, name, "CreateDevice", err)
			return di8.ENUM_CONTINUE // skip this device, keep enumerating
		}
		if err := dev.SetDataFormat(&di8.Joystick2); err != nil {
			s.noteOpenFailure(id, name, "SetDataFormat", err)
			dev.Release()
			return di8.ENUM_CONTINUE
		}
		// THE line. See the type comment.
		if err := dev.SetCooperativeLevel(di8.HWND(s.helper.hwnd), di8.SCL_NONEXCLUSIVE|di8.SCL_BACKGROUND); err != nil {
			s.noteOpenFailure(id, name, "SetCooperativeLevel", err)
			dev.Release()
			return di8.ENUM_CONTINUE
		}
		if err := dev.Acquire(); err != nil {
			s.noteOpenFailure(id, name, "Acquire", err)
			dev.Release()
			return di8.ENUM_CONTINUE
		}
		// Opened cleanly: forget any past failure so a LATER one is logged
		// again rather than suppressed as a repeat.
		delete(s.openFailed, id)
		info := Device{ID: id, Name: name}
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
	// A device that is unplugged while failing must forget its failure too,
	// so plugging it back in logs the next failure instead of silently
	// treating it as the same one.
	for id := range s.openFailed {
		if !seen[id] {
			delete(s.openFailed, id)
		}
	}
	return out, nil
}

// noteOpenFailure logs one per-device open failure, at most once per
// transition. Caller holds s.mu (Devices holds it across the whole
// enumeration, and the EnumDevices callback runs synchronously inside it).
//
// Warn, not Error: a device held exclusively by something else is a normal
// state of the world, and the rest of the joystick subsystem keeps working.
// It is still worth a line, because from outside this function a device that
// failed SetCooperativeLevel and a device that was never enumerated at all
// look identical.
func (s *winSource) noteOpenFailure(id trigger.DeviceID, name, call string, err error) {
	if s.openFailed[id] == call {
		return // already reported this exact failure; do not spam every 3s
	}
	s.openFailed[id] = call
	s.log.Warn("joystick device could not be opened; it will be retried on the next rediscover",
		"device", string(id), "name", name, "call", call, "err", err)
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
