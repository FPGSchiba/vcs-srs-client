//go:build linux

package joystick

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	log     *slog.Logger
	devices map[trigger.DeviceID]*linuxDevice
	// pollFail rate-limits the per-device Poll failure log. Without it the
	// only signal that a device had stopped answering was the ABSENCE of
	// presses, which is indistinguishable from "the user is not pressing
	// anything". See pollhealth.go.
	pollFail *pollFailures
}

type linuxDevice struct {
	dev  *evdev.InputDevice
	info Device
	// path is the /dev/input/eventN node this handle was opened on.
	//
	// It is NOT part of the DeviceID, deliberately: the ID is derived from
	// the /dev/input/by-id name (or the product name) precisely so that it
	// survives a replug and the user's bindings with it. But that same
	// path-independence is what let a STALE HANDLE survive a replug too --
	// see Devices() for the reuse rule this field exists to enforce.
	path string
	// hatAxes maps an ABS_HAT* axis code to its hat index (0..HatCount-1).
	hatAxes map[evdev.EvCode]int
	// buttons maps an EV_KEY code to our button index, in the order the
	// device declares them -- BTN_TRIGGER, BTN_THUMB, ... are not contiguous.
	buttons map[evdev.EvCode]trigger.Button
}

// inputDevDir is where Linux exposes evdev character devices.
const inputDevDir = "/dev/input"

// NewOSSource builds the platform joystick source.
//
// It deliberately does NOT enumerate here, and therefore never fails. On
// Linux the only way construction could fail was the permission path -- a
// user not in the 'input' group -- and failing for it destroyed the very
// distinction permissionError exists to make: main.go drops the manager on a
// construction error, so sb.joy stayed nil, GetJoystickState returned
// {Supported:false, Error:""}, and a denied Linux box was byte-identical to
// macOS's "no backend at all". The actionable message ("add your user to the
// 'input' group") only ever reached the log file, and Keybinds.tsx gates its
// banner on supported && error, which that state can never satisfy.
//
// Returning a live Source instead hands the enumeration -- and its error --
// to Manager.New's probe, which records ANY non-ErrUnsupported error in
// lastErr while leaving Supported() true. Denied then reads as
// {Supported:true, Error:"...input group..."} and the banner fires, while
// ErrUnsupported (macOS, source_other.go) still reads as
// {Supported:false, Error:""}. That split is spec sections 8 and 11.
func NewOSSource(log *slog.Logger) (Source, error) {
	if log == nil {
		log = slog.Default()
	}
	return &linuxSource{
		log:      log,
		devices:  map[trigger.DeviceID]*linuxDevice{},
		pollFail: newPollFailures(),
	}, nil
}

// isJoystickButton reports whether c is one of the EV_KEY codes the Linux
// kernel reserves for joystick/gamepad buttons. It is the SINGLE definition
// of "this is a button, not a key" and MUST be used everywhere that
// distinction matters -- both in looksLikeJoystick's device-shape gate and in
// the button-map loop in Devices() that decides which codes actually become
// addressable trigger.Button values.
//
// Those two call sites used to disagree: looksLikeJoystick gated correctly,
// but the button map took every EV_KEY code the device declared, unfiltered.
// A composite evdev node -- ABS_X, a joystick button, AND literal KEY_*
// codes on one node -- would pass the gate, then have its KEY_* codes mapped
// as buttons and reported held from Poll(). manager.go's logEdge would then
// write those "buttons" to the log by label, which is exactly the keylog
// logEdge's own doc comment says this package must never become: "a
// joystick backend sees joystick buttons and nothing else" is the entire
// reason logging input identities is permitted here when internal/hotkeys is
// forbidden from doing it. Routing both call sites through one predicate
// means they cannot drift apart the way they did before.
//
// The eligible set is the union of the THREE kernel-defined blocks that
// actually contain joystick and gamepad buttons, and nothing else:
//
//   - BTN_JOYSTICK..BTN_THUMBR (0x120-0x13e): the standard joystick block
//     (0x120-0x12f) followed by the gamepad block (BTN_GAMEPAD 0x130 ..
//     BTN_THUMBR 0x13e). The gamepad block ENDS at BTN_THUMBR -- this used
//     to read BTN_GAMEPAD+0x7f (0x1AF), which overshot by 113 codes and
//     swallowed the whole digitiser block: BTN_DIGI 0x140, BTN_TOOL_FINGER
//     0x145, BTN_TOUCH 0x14a, BTN_TOOL_DOUBLETAP 0x14d, BTN_WHEEL 0x150.
//     Every mainstream laptop touchpad declares EV_ABS/ABS_X together with
//     BTN_TOUCH and BTN_TOOL_FINGER, so it passed the device gate, its
//     BTN_TOUCH became an addressable trigger.Button held whenever a finger
//     was down, and a user who clicked "+" and then touched the trackpad
//     bound their trackpad to the action. Do not widen this bound again.
//   - BTN_DPAD_UP..BTN_DPAD_RIGHT (0x220-0x223): the separate D-pad block
//     some gamepads report their hat on instead of ABS_HAT0X/Y.
//   - BTN_TRIGGER_HAPPY1..BTN_TRIGGER_HAPPY40 (0x2c0-0x2e7): the kernel's
//     overflow block for devices with more buttons than the standard block
//     has room for -- exactly what a high-button-count HOTAS (e.g. a
//     Warthog-class throttle) uses. Dropping it would trade the digitiser
//     bug for a "half my buttons vanished" bug, which is why the union
//     exists at all.
//
// Everything outside those three blocks -- the digitiser block, the
// multimedia/consumer KEY_* space between 0x1AF and 0x2c0, KEY_OK,
// KEY_KBD_LCD_MENU* and so on -- stays excluded, which is what keeps the
// keylog closure intact.
func isJoystickButton(c evdev.EvCode) bool {
	switch {
	case c >= evdev.BTN_JOYSTICK && c <= evdev.BTN_THUMBR:
		return true
	case c >= evdev.BTN_DPAD_UP && c <= evdev.BTN_DPAD_RIGHT:
		return true
	case c >= evdev.BTN_TRIGGER_HAPPY1 && c <= evdev.BTN_TRIGGER_HAPPY40:
		return true
	default:
		return false
	}
}

// isDigitiserButton reports whether c is one of the codes that mark a device
// as a touchpad, touchscreen or graphics tablet rather than a joystick.
//
// This is how the kernel's own joydev driver tells the two apart, and it is
// needed IN ADDITION to isJoystickButton's narrowed ranges: narrowing alone
// only stops a digitiser's codes from becoming bindable buttons, it does not
// stop a device that declares BOTH a joystick button and touch capability
// from being opened and listed as a joystick in the first place.
func isDigitiserButton(c evdev.EvCode) bool {
	switch c {
	case evdev.BTN_DIGI, evdev.BTN_TOOL_FINGER, evdev.BTN_TOUCH:
		return true
	default:
		return false
	}
}

// looksLikeJoystick reports whether the device declares joystick-shaped
// capabilities: an absolute X axis plus at least one gamepad/joystick
// button, and NO digitiser capability. See the type comment for why this
// filter is load-bearing: a keyboard declares EV_KEY capability but no
// ABS_X, and reports no isJoystickButton code, so it can never pass this
// check.
//
// The digitiser rejection is the second half of that, and it is not
// redundant with the button-range narrowing: a touchpad declares ABS_X and
// BTN_TOUCH, and a composite node could declare a real joystick button
// alongside touch capability. Rejecting the whole DEVICE -- rather than just
// declining to map its touch codes -- is what keeps a laptop trackpad out of
// the device list the capture UI shows, which is the difference between
// "joystick capture works on a laptop" and "the first thing the user touches
// gets bound".
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
	// The whole EV_KEY set is scanned before answering: a digitiser code
	// anywhere disqualifies the device, so returning true on the first
	// joystick button would let a composite touch device through.
	hasJoyButton := false
	for _, c := range d.CapableEvents(evdev.EV_KEY) {
		if isDigitiserButton(c) {
			return false
		}
		if isJoystickButton(c) {
			hasJoyButton = true
		}
	}
	return hasJoyButton
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
		// Sanitised at the boundary: an evdev name is a raw char name[80]
		// with no encoding guarantee, and it is PERSISTED to config.toml.
		// See sanitiseDeviceName for what an invalid byte costs there.
		name, _ := dev.Name()
		name = sanitiseDeviceName(name)
		id := stableID(p, name)
		if seen[id] {
			dev.Close()
			continue
		}
		seen[id] = true

		if existing, ok := s.devices[id]; ok {
			if reuseCachedHandle(existing.path, p, handleAlive(existing.dev)) {
				dev.Close()
				out = append(out, existing.info)
				continue
			}
			// The cached handle is stale: the device re-enumerated inside
			// one rediscover window. Close it and fall through to rebuild
			// from the handle we just opened.
			//
			// This is the I1 fix, and the failure it closes is not exotic --
			// a laptop suspend/resume tears down and re-adds USB well inside
			// the 3s ticker, as does a cable reseat or a hub glitch. The old
			// code took the cache hit unconditionally, closed the FRESH
			// handle and kept the dead one; because stableID is
			// path-independent by design the id was identical, so seen[id]
			// stayed true on every subsequent pass and the staleness sweep
			// below never dropped it. Poll() then got ENODEV from every
			// ioctl and swallowed it, so pollErr and discoverErr stayed nil,
			// Devices() kept reporting the stick attached, TriggerChip kept
			// rendering its chips CONNECTED, and every binding on it was
			// silently dead until the app was restarted. That directly
			// contradicts the self-healing property spec section 7 claims
			// for the polling design.
			s.log.Info("joystick device re-enumerated; reopening it",
				"device", string(id), "name", existing.info.Name,
				"was", existing.path, "now", p)
			existing.dev.Close()
			delete(s.devices, id)
			s.pollFail.forget(id)
		}

		ld := &linuxDevice{
			dev:     dev,
			path:    p,
			hatAxes: map[evdev.EvCode]int{},
			buttons: map[evdev.EvCode]trigger.Button{},
		}

		// isJoystickButton is the SAME predicate looksLikeJoystick's gate 2
		// uses. This must filter here too, not just gate the device: without
		// it, a composite node's literal KEY_* codes would be mapped as
		// buttons and reported held from Poll(), which is precisely the
		// keylog logEdge's doc comment says this package must not become.
		// See isJoystickButton's doc for the two-range union and why a
		// single range is wrong.
		//
		// The bounds check is defence in depth, not a reachable path
		// today: isJoystickButton's eligible set is 31 codes
		// (BTN_JOYSTICK..BTN_THUMBR) + 4 (the D-pad block) + 40
		// (BTN_TRIGGER_HAPPY1..40) = 75, comfortably inside
		// trigger.MaxButton+1 (128) slots. It WAS genuinely reachable while
		// the first range ran to 0x1AF; it is kept because every caller
		// building a trigger.Button must clamp, and this is that clamp. On
		// overflow the first 128 eligible codes, in the order CapableEvents
		// reports them, keep their indices; anything past that is dropped
		// rather than misindexed -- the break happens before any write past
		// next==127.
		var next trigger.Button
		for _, c := range dev.CapableEvents(evdev.EV_KEY) {
			if !isJoystickButton(c) {
				continue
			}
			if next > trigger.MaxButton {
				break
			}
			ld.buttons[c] = next
			next++
		}

		// The hatIdx >= trigger.HatCount guard is defence in depth rather
		// than a reachable path today: evdev defines exactly
		// ABS_HAT0X..ABS_HAT3Y (4 hats), which already equals
		// trigger.HatCount, so hatIdx can never exceed 3. It is kept for the
		// same reason the button guard above is kept: callers of
		// trigger.HatButton must clamp, and this is that clamp.
		for _, c := range dev.CapableEvents(evdev.EV_ABS) {
			if c < evdev.ABS_HAT0X || c > evdev.ABS_HAT3Y {
				continue
			}
			hatIdx := int(c-evdev.ABS_HAT0X) / 2
			if hatIdx >= trigger.HatCount {
				continue
			}
			ld.hatAxes[c] = hatIdx
		}

		ld.info = Device{ID: id, Name: name}
		s.devices[id] = ld
		out = append(out, ld.info)
	}

	// Drop devices that went away so a replug re-opens cleanly.
	for id, d := range s.devices {
		if !seen[id] {
			d.dev.Close()
			delete(s.devices, id)
			s.pollFail.forget(id)
		}
	}

	if len(out) == 0 && permErr != nil {
		return nil, permErr
	}
	return out, nil
}

// handleAlive reports whether a cached evdev handle still refers to a live
// device.
//
// EVIOCGNAME is the cheapest ioctl that touches the device: go-evdev's Name()
// issues it, and on a file descriptor whose device the kernel has already
// unbound it fails with ENODEV rather than returning stale data. One ioctl
// per open device per 3s rediscover is negligible next to the 100 polls a
// second the same handles already serve.
//
// This is the SECOND half of the reuse rule, and it is not redundant with the
// path comparison in Devices(). The path check catches the common
// re-enumeration, where the node moves (event5 -> event14) because lower
// numbers are still in use; it cannot catch a re-enumeration that lands back
// on the SAME node number, which is exactly what happens on a suspend/resume
// of a machine with one stick attached and nothing else competing for the
// number. Only an ioctl against the retained fd distinguishes that case.
// Together the two cover every re-enumeration; either alone leaves a hole.
func handleAlive(d *evdev.InputDevice) bool {
	_, err := d.Name()
	return err == nil
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
			s.notePollFailure(id, d, "State", err)
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
			s.pollFail.ok(id)
			continue
		}
		// AbsInfos reads every absolute axis the device supports in one
		// ioctl round trip per axis; go-evdev has no single-axis
		// equivalent, so this is called once per device per poll rather
		// than once per hat.
		absInfos, err := d.dev.AbsInfos()
		if err != nil {
			s.notePollFailure(id, d, "AbsInfos", err)
			continue
		}
		// The X and Y axes of one hat MUST be read together, not mapped
		// independently. evdev reports them as two separate absolute axes,
		// and the old code turned each into its own direction -- which made
		// a diagonal indistinguishable from two cardinals held at once and
		// left the four diagonal grammar values unreachable on Linux, while
		// Windows reported the diagonal alone. Pairing them here and routing
		// through hatAxisDirections is what makes the two backends agree.
		// See expandHatDirection in hat.go.
		type hatXY struct{ x, y int32 }
		pairs := map[int]*hatXY{}
		for code, hat := range d.hatAxes {
			info, ok := absInfos[code]
			if !ok {
				continue
			}
			p := pairs[hat]
			if p == nil {
				p = &hatXY{}
				pairs[hat] = p
			}
			if isHatXAxis(code) {
				p.x = info.Value
			} else {
				p.y = info.Value
			}
		}
		for hat, p := range pairs {
			for _, dir := range hatAxisDirections(p.x, p.y) {
				st.Held[trigger.JoyButton{Device: id, Button: trigger.HatButton(hat, dir)}] = struct{}{}
			}
		}
		s.pollFail.ok(id)
	}
	return st, nil
}

// notePollFailure logs one per-device poll failure, at most once per
// transition. Caller holds s.mu (Poll holds it across the whole sample).
//
// Warn, not Error: the rest of the joystick subsystem keeps working, and the
// next rediscover revalidates this handle and reopens the device (see
// handleAlive). The line exists because without it the ONLY symptom of a
// device that has stopped answering is the absence of presses, which is
// indistinguishable from a user who is not pressing anything -- the silence,
// not the staleness, is what made I1 undiagnosable.
//
// Naming the device and the node here is the same disclosure Poll's own
// caller already makes when it logs which button fired an action: this
// backend can only ever see joystick hardware (see isJoystickButton), which
// is what permits it. No key identity can reach this line.
func (s *linuxSource) notePollFailure(id trigger.DeviceID, d *linuxDevice, call string, err error) {
	logIt, n := s.pollFail.note(id, call)
	if !logIt {
		return // already reported; do not write 100 lines a second
	}
	s.log.Warn("joystick device stopped responding; it will be revalidated and "+
		"reopened on the next rediscover",
		"device", string(id), "name", d.info.Name, "path", d.path,
		"call", call, "consecutive", n, "err", err)
}

// isHatXAxis reports whether an ABS_HAT* code is the X half of its hat pair.
// evdev lays them out as ABS_HAT0X, ABS_HAT0Y, ABS_HAT1X, ... so the X axes
// are the even offsets from ABS_HAT0X.
//
// This is all that remains of the old hatDirection: the DIRECTION decision
// moved to hatAxisDirection in hat.go, where it is shared with the Windows
// backend and testable without this file's build tag.
func isHatXAxis(code evdev.EvCode) bool {
	return (code-evdev.ABS_HAT0X)%2 == 0
}

func (s *linuxSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.devices {
		d.dev.Close()
		delete(s.devices, id)
		s.pollFail.forget(id)
	}
}
