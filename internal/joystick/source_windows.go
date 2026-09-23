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
//
// It deliberately does NOT probe enumeration here, and therefore fails only
// when there is genuinely no Source to return (no helper window, no
// DirectInput). The construction-time `s.Devices()` probe that used to live
// at the end of this function destroyed the very distinction the manager's
// health reporting exists to make: main.go drops the manager on a
// construction error, so sb.joy stayed nil, GetJoystickState returned
// {Supported:false, Error:""}, and a Windows box whose EnumDevices failed
// once was byte-identical to macOS's "no backend at all" -- affordance
// hidden, no banner, and NO RETRY, because the 3s rediscover loop only
// exists inside a Manager that was never built.
//
// Returning a live Source instead hands the enumeration -- and its error --
// to Manager.New's probe, which records ANY non-ErrUnsupported error in
// discoverErr while leaving Supported() true. A transient enumeration
// failure then reads as {Supported:true, Error:"joystick: enumerate
// devices: ..."}, the banner fires, and the next successful rediscover
// clears it on its own.
//
// This is the same reasoning source_linux.go carries; it is platform
// INDEPENDENT, so it must hold for every backend (darwin and other never
// fail construction at all) -- INCLUDING the two setup calls below, which is
// why they return a stub Source rather than an error. See winInitFailed.
func NewOSSource(log *slog.Logger) (Source, error) {
	if log == nil {
		log = slog.Default()
	}
	helper, err := newHelperWindow()
	if err != nil {
		return winInitFailed(log, err), nil
	}
	dinput, err := di8.Create(di8.HINSTANCE(helper.inst))
	if err != nil {
		helper.Close()
		return winInitFailed(log, fmt.Errorf("joystick: create DirectInput: %w", err)), nil
	}
	s := &winSource{
		log:        log,
		di:         dinput,
		helper:     helper,
		devices:    map[trigger.DeviceID]*winDevice{},
		openFailed: map[trigger.DeviceID]string{},
	}
	return s, nil
}

// winInitFailed returns a Source that reports err from Devices() forever,
// instead of failing construction.
//
// Returning an error from NewOSSource is what the previous wave's fix set out
// to remove, and newHelperWindow/di8.Create were the half it missed: main.go
// drops the manager on a construction error, so sb.joy stayed nil,
// GetJoystickState returned {Supported:false, Error:""} -- byte-identical to
// macOS's "no backend at all" -- and the user got no banner, no explanation
// and NO RETRY, because the 3s rediscover loop only exists inside a Manager
// that was never built.
//
// The error is deliberately NOT ErrUnsupported: Manager.New records any other
// error in discoverErr while leaving Supported() true, so the failure reads as
// {Supported:true, Error:"joystick: create helper window: ..."}, the Keybinds
// banner fires on supported && error, and the rediscover loop keeps retrying.
// That retry can genuinely succeed -- a CreateWindowEx or DirectInput8Create
// failure this early is usually resource pressure during startup -- but even
// when it cannot, a named cause beats silence.
//
// Poll returns an empty state and no error on purpose. Nothing can be held
// when nothing is open, and reporting a poll error every 10ms would hand
// lastErrLocked's poll-wins precedence a permanent, less useful message than
// the enumeration one that names the actual failed call.
func winInitFailed(log *slog.Logger, err error) Source {
	log.Warn("joystick backend could not start; keyboard binds are unaffected "+
		"and the client will keep retrying", "err", err)
	return winInitFailedSource{err: err}
}

type winInitFailedSource struct{ err error }

func (s winInitFailedSource) Devices() ([]Device, error) { return nil, s.err }
func (winInitFailedSource) Poll() (State, error) {
	return State{Held: map[trigger.JoyButton]struct{}{}}, nil
}
func (winInitFailedSource) Close() {}

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
			// povDirections, not povDirection: a diagonal reports its two
			// adjacent cardinals as well, so a PTT bound to hat1.up is not
			// cut by a nudge to up-right. See expandHatDirection in hat.go.
			for _, dir := range povDirections(angle) {
				st.Held[trigger.JoyButton{
					Device: id,
					Button: trigger.HatButton(hat, dir),
				}] = struct{}{}
			}
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
