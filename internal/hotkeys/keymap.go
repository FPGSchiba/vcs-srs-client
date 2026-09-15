package hotkeys

import "runtime"

// keyCode is a PHYSICAL key code in the host OS's own numbering. It is
// deliberately NOT a cross-platform abstraction: see keyTables below for why
// one does not exist here.
type keyCode uint32

// eventKeyCode extracts the stable physical key code from the two numeric
// fields github.com/robotn/gohook puts on every key event.
//
// WHICH FIELD IS AUTHORITATIVE DIFFERS PER BACKEND, and getting this wrong is
// silent — a chord simply never matches. Verified by reading gohook
// v1.0.0-beta1's three purego backends:
//
//   - darwin (darwin.go:418-421, makeKeyEvent at :461) — Rawcode is the raw
//     Quartz/HIToolbox keycode straight out of
//     CGEventGetIntegerValueField(kCGKeyboardEventKeycode). Keycode is
//     rawToKeyDarwin[raw] looked up in github.com/vcaesar/keycode, which has
//     no entry for Backspace, Insert, Home, End, PageUp, PageDown, F13+ or
//     the numpad, and FALLS BACK TO THE RAW CODE when the lookup misses
//     (darwin.go:473-479). That fallback collides with the codes it is
//     supposed to be translating into: Backspace's raw 51 is the same number
//     as VC_COMMA (0x33). Rawcode is the only unambiguous field here.
//
//   - windows (windows.go:352-392) — Rawcode is the Win32 virtual-key code
//     from the WH_KEYBOARD_LL struct. Keycode is winVKToKeycode[vk], a
//     generated VC_* table that is mostly right but nibble-swaps the
//     navigation cluster (PageUp maps to 0x0E49 where VC_PAGE_UP is 0xE049)
//     and has no entry at all for numpad Enter. VK is stable and documented;
//     use it.
//
//   - linux/X11 (x11.go:358-368) — INVERTED relative to the other two.
//     Keycode is the evdev code (X keycode - 8), which is the physical key.
//     Rawcode is the X KEYSYM resolved under the event's modifier state, so
//     it changes with the keyboard layout and with Shift. Keycode is the only
//     usable field here.
//
// This is the concrete form of the divergence gohook issue #41 describes, and
// it is why the migration spike's hope of collapsing three key tables into
// one VC_* table does not survive contact with the beta's source.
func eventKeyCode(keycode, rawcode uint16) keyCode {
	if runtime.GOOS == "linux" {
		return keyCode(keycode)
	}
	return keyCode(rawcode)
}

// osKeyCode maps one canonical internal/chord key name to the running
// platform's physical key code. The second result is false for a key this
// platform cannot express; callers turn that into a per-action registration
// error so hotkeys.Manager.Failed() can name the binding that did not take.
func osKeyCode(key string) (keyCode, bool) {
	table, ok := keyTables[runtime.GOOS]
	if !ok {
		return 0, false
	}
	code, ok := table[key]
	return code, ok
}

// supportedPlatform reports whether this GOOS has a key table at all.
func supportedPlatform() bool {
	_, ok := keyTables[runtime.GOOS]
	return ok
}

// keyTables maps GOOS -> canonical chord key name -> physical key code.
//
// All three tables are compiled into every binary rather than split behind
// build tags. They are a few kilobytes of static data, and keeping them
// together buys something worth far more: every table is exercised by the
// tests on every platform, so a typo in the Windows table is caught by a CI
// run on Linux instead of by a user who cannot bind their PTT key.
//
// The codes are the OPERATING SYSTEM's own constants, not gohook's. gohook
// passes them through untranslated in the field eventKeyCode picks (see
// above), and OS key codes are ABI-stable in a way a beta library's
// hand-maintained translation table is not.
var keyTables = map[string]map[string]keyCode{
	"darwin":  darwinKeys,
	"windows": windowsKeys,
	"linux":   linuxKeys,
}

// darwinKeys maps canonical key names to Quartz/HIToolbox virtual key codes
// (kVK_* in <Carbon/HIToolbox/Events.h>). These are the values gohook's
// darwin purego backend puts in Event.Rawcode.
//
// Cross-checked against gohook's own rawToKeyDarwin table (tables.go), which
// agrees on every entry it covers.
//
// NOT PRESENT: F21-F24. macOS defines no kVK_ constant for them and the
// window server has no keycode to report, so a user binding F21 on macOS gets
// a named failure in Manager.Failed() rather than a hotkey that never fires.
// Windows and Linux do support them.
//
// "Insert" is kVK_Help (0x72). That is the code a PC keyboard's Insert key
// produces on macOS; Apple keyboards have no Insert key at all.
// "Backspace" is kVK_Delete (0x33) and "Delete" is kVK_ForwardDelete (0x75):
// Apple's naming is one key off from the PC naming this project's canonical
// set uses.
var darwinKeys = map[string]keyCode{
	"A": 0x00, "B": 0x0B, "C": 0x08, "D": 0x02, "E": 0x0E, "F": 0x03,
	"G": 0x05, "H": 0x04, "I": 0x22, "J": 0x26, "K": 0x28, "L": 0x25,
	"M": 0x2E, "N": 0x2D, "O": 0x1F, "P": 0x23, "Q": 0x0C, "R": 0x0F,
	"S": 0x01, "T": 0x11, "U": 0x20, "V": 0x09, "W": 0x0D, "X": 0x07,
	"Y": 0x10, "Z": 0x06,

	"0": 0x1D, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15,
	"5": 0x17, "6": 0x16, "7": 0x1A, "8": 0x1C, "9": 0x19,

	"F1": 0x7A, "F2": 0x78, "F3": 0x63, "F4": 0x76, "F5": 0x60,
	"F6": 0x61, "F7": 0x62, "F8": 0x64, "F9": 0x65, "F10": 0x6D,
	"F11": 0x67, "F12": 0x6F, "F13": 0x69, "F14": 0x6B, "F15": 0x71,
	"F16": 0x6A, "F17": 0x40, "F18": 0x4F, "F19": 0x50, "F20": 0x5A,

	"Space":     0x31,
	"Escape":    0x35,
	"Enter":     0x24,
	"Tab":       0x30,
	"Backspace": 0x33,
	"Delete":    0x75,
	"Insert":    0x72,
	"Home":      0x73,
	"End":       0x77,
	"PageUp":    0x74,
	"PageDown":  0x79,

	"ArrowUp":    0x7E,
	"ArrowDown":  0x7D,
	"ArrowLeft":  0x7B,
	"ArrowRight": 0x7C,

	"Minus":        0x1B,
	"Equal":        0x18,
	"BracketLeft":  0x21,
	"BracketRight": 0x1E,
	"Semicolon":    0x29,
	"Quote":        0x27,
	"Backquote":    0x32,
	"Backslash":    0x2A,
	"Comma":        0x2B,
	"Period":       0x2F,
	"Slash":        0x2C,

	"Numpad0": 0x52, "Numpad1": 0x53, "Numpad2": 0x54, "Numpad3": 0x55,
	"Numpad4": 0x56, "Numpad5": 0x57, "Numpad6": 0x58, "Numpad7": 0x59,
	"Numpad8": 0x5B, "Numpad9": 0x5C,

	"NumpadAdd":      0x45,
	"NumpadSubtract": 0x4E,
	"NumpadMultiply": 0x43,
	"NumpadDivide":   0x4B,
	"NumpadDecimal":  0x41,
	"NumpadEnter":    0x4C,
}

// windowsKeys maps canonical key names to Win32 virtual-key codes (VK_*,
// <winuser.h>). These are the values gohook's windows purego backend puts in
// Event.Rawcode, straight from KBDLLHOOKSTRUCT.vkCode.
//
// NOT PRESENT: "NumpadEnter". Win32 reports the numpad Enter as VK_RETURN
// (0x0D) with LLKHF_EXTENDED set in KBDLLHOOKSTRUCT.flags, and gohook's
// keyboardProc does not carry that flag onto the Event (windows.go:338-392) —
// there is nowhere in hook.Event for it to live. Aliasing NumpadEnter to
// VK_RETURN would make a NumpadEnter binding fire on the MAIN Enter key,
// which is worse than not binding it, so it is omitted and reported through
// Manager.Failed(). macOS and Linux distinguish the two keys.
//
// Also inherent to Win32 rather than to gohook: with NumLock OFF the numpad
// digits report the navigation VKs (Numpad7 arrives as VK_HOME), so a numpad
// digit binding needs NumLock on. That is how every Windows application
// behaves and is not something this layer can correct.
var windowsKeys = map[string]keyCode{
	"A": 0x41, "B": 0x42, "C": 0x43, "D": 0x44, "E": 0x45, "F": 0x46,
	"G": 0x47, "H": 0x48, "I": 0x49, "J": 0x4A, "K": 0x4B, "L": 0x4C,
	"M": 0x4D, "N": 0x4E, "O": 0x4F, "P": 0x50, "Q": 0x51, "R": 0x52,
	"S": 0x53, "T": 0x54, "U": 0x55, "V": 0x56, "W": 0x57, "X": 0x58,
	"Y": 0x59, "Z": 0x5A,

	"0": 0x30, "1": 0x31, "2": 0x32, "3": 0x33, "4": 0x34,
	"5": 0x35, "6": 0x36, "7": 0x37, "8": 0x38, "9": 0x39,

	"F1": 0x70, "F2": 0x71, "F3": 0x72, "F4": 0x73, "F5": 0x74,
	"F6": 0x75, "F7": 0x76, "F8": 0x77, "F9": 0x78, "F10": 0x79,
	"F11": 0x7A, "F12": 0x7B, "F13": 0x7C, "F14": 0x7D, "F15": 0x7E,
	"F16": 0x7F, "F17": 0x80, "F18": 0x81, "F19": 0x82, "F20": 0x83,
	"F21": 0x84, "F22": 0x85, "F23": 0x86, "F24": 0x87,

	"Space":     0x20,
	"Escape":    0x1B,
	"Enter":     0x0D,
	"Tab":       0x09,
	"Backspace": 0x08,
	"Delete":    0x2E,
	"Insert":    0x2D,
	"Home":      0x24,
	"End":       0x23,
	"PageUp":    0x21,
	"PageDown":  0x22,

	"ArrowUp":    0x26,
	"ArrowDown":  0x28,
	"ArrowLeft":  0x25,
	"ArrowRight": 0x27,

	"Minus":        0xBD, // VK_OEM_MINUS
	"Equal":        0xBB, // VK_OEM_PLUS
	"BracketLeft":  0xDB, // VK_OEM_4
	"BracketRight": 0xDD, // VK_OEM_6
	"Semicolon":    0xBA, // VK_OEM_1
	"Quote":        0xDE, // VK_OEM_7
	"Backquote":    0xC0, // VK_OEM_3
	"Backslash":    0xDC, // VK_OEM_5
	"Comma":        0xBC, // VK_OEM_COMMA
	"Period":       0xBE, // VK_OEM_PERIOD
	"Slash":        0xBF, // VK_OEM_2

	"Numpad0": 0x60, "Numpad1": 0x61, "Numpad2": 0x62, "Numpad3": 0x63,
	"Numpad4": 0x64, "Numpad5": 0x65, "Numpad6": 0x66, "Numpad7": 0x67,
	"Numpad8": 0x68, "Numpad9": 0x69,

	"NumpadAdd":      0x6B, // VK_ADD
	"NumpadSubtract": 0x6D, // VK_SUBTRACT
	"NumpadMultiply": 0x6A, // VK_MULTIPLY
	"NumpadDivide":   0x6F, // VK_DIVIDE
	"NumpadDecimal":  0x6E, // VK_DECIMAL
}

// linuxKeys maps canonical key names to Linux evdev key codes (KEY_*,
// <linux/input-event-codes.h>). These are the values gohook's X11 purego
// backend puts in Event.Keycode: it subtracts the standard offset of 8 from
// the X keycode (x11.go:55-58, :352-356), which yields the evdev code on any
// modern X server.
//
// This table is complete for the canonical key set — Linux is the only one of
// the three platforms with no gaps.
var linuxKeys = map[string]keyCode{
	"A": 30, "B": 48, "C": 46, "D": 32, "E": 18, "F": 33,
	"G": 34, "H": 35, "I": 23, "J": 36, "K": 37, "L": 38,
	"M": 50, "N": 49, "O": 24, "P": 25, "Q": 16, "R": 19,
	"S": 31, "T": 20, "U": 22, "V": 47, "W": 17, "X": 45,
	"Y": 21, "Z": 44,

	"0": 11, "1": 2, "2": 3, "3": 4, "4": 5,
	"5": 6, "6": 7, "7": 8, "8": 9, "9": 10,

	"F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63,
	"F6": 64, "F7": 65, "F8": 66, "F9": 67, "F10": 68,
	"F11": 87, "F12": 88,
	"F13": 183, "F14": 184, "F15": 185, "F16": 186, "F17": 187, "F18": 188,
	"F19": 189, "F20": 190, "F21": 191, "F22": 192, "F23": 193, "F24": 194,

	"Space":     57,
	"Escape":    1,
	"Enter":     28,
	"Tab":       15,
	"Backspace": 14,
	"Delete":    111,
	"Insert":    110,
	"Home":      102,
	"End":       107,
	"PageUp":    104,
	"PageDown":  109,

	"ArrowUp":    103,
	"ArrowDown":  108,
	"ArrowLeft":  105,
	"ArrowRight": 106,

	"Minus":        12,
	"Equal":        13,
	"BracketLeft":  26,
	"BracketRight": 27,
	"Semicolon":    39,
	"Quote":        40, // KEY_APOSTROPHE
	"Backquote":    41, // KEY_GRAVE
	"Backslash":    43,
	"Comma":        51,
	"Period":       52, // KEY_DOT
	"Slash":        53,

	"Numpad0": 82, "Numpad1": 79, "Numpad2": 80, "Numpad3": 81,
	"Numpad4": 75, "Numpad5": 76, "Numpad6": 77, "Numpad7": 71,
	"Numpad8": 72, "Numpad9": 73,

	"NumpadAdd":      78, // KEY_KPPLUS
	"NumpadSubtract": 74, // KEY_KPMINUS
	"NumpadMultiply": 55, // KEY_KPASTERISK
	"NumpadDivide":   98, // KEY_KPSLASH
	"NumpadDecimal":  83, // KEY_KPDOT
	"NumpadEnter":    96, // KEY_KPENTER
}
