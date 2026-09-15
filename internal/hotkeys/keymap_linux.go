//go:build (linux || openbsd) && cgo

package hotkeys

import (
	"fmt"

	"golang.design/x/hotkey"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// keyTable maps our canonical key names to golang.design/x/hotkey's X11
// keysym constants. As on the other platforms, the library only exposes a
// limited key set (letters, digits, a handful of named keys, arrows, and
// F1-F20): punctuation, navigation keys (Insert/Home/End/PageUp/PageDown),
// Backspace, the numpad, and F21-F24 have no equivalent here. Binding one of
// those keys returns an error from mapChord rather than silently failing to
// register.
var keyTable = buildKeyTable()

func buildKeyTable() map[string]hotkey.Key {
	m := map[string]hotkey.Key{
		"Space":      hotkey.KeySpace,
		"Enter":      hotkey.KeyReturn,
		"Escape":     hotkey.KeyEscape,
		"Tab":        hotkey.KeyTab,
		"Delete":     hotkey.KeyDelete,
		"ArrowUp":    hotkey.KeyUp,
		"ArrowDown":  hotkey.KeyDown,
		"ArrowLeft":  hotkey.KeyLeft,
		"ArrowRight": hotkey.KeyRight,
		"0":          hotkey.Key0,
		"1":          hotkey.Key1,
		"2":          hotkey.Key2,
		"3":          hotkey.Key3,
		"4":          hotkey.Key4,
		"5":          hotkey.Key5,
		"6":          hotkey.Key6,
		"7":          hotkey.Key7,
		"8":          hotkey.Key8,
		"9":          hotkey.Key9,
		"A":          hotkey.KeyA,
		"B":          hotkey.KeyB,
		"C":          hotkey.KeyC,
		"D":          hotkey.KeyD,
		"E":          hotkey.KeyE,
		"F":          hotkey.KeyF,
		"G":          hotkey.KeyG,
		"H":          hotkey.KeyH,
		"I":          hotkey.KeyI,
		"J":          hotkey.KeyJ,
		"K":          hotkey.KeyK,
		"L":          hotkey.KeyL,
		"M":          hotkey.KeyM,
		"N":          hotkey.KeyN,
		"O":          hotkey.KeyO,
		"P":          hotkey.KeyP,
		"Q":          hotkey.KeyQ,
		"R":          hotkey.KeyR,
		"S":          hotkey.KeyS,
		"T":          hotkey.KeyT,
		"U":          hotkey.KeyU,
		"V":          hotkey.KeyV,
		"W":          hotkey.KeyW,
		"X":          hotkey.KeyX,
		"Y":          hotkey.KeyY,
		"Z":          hotkey.KeyZ,
		"F1":         hotkey.KeyF1,
		"F2":         hotkey.KeyF2,
		"F3":         hotkey.KeyF3,
		"F4":         hotkey.KeyF4,
		"F5":         hotkey.KeyF5,
		"F6":         hotkey.KeyF6,
		"F7":         hotkey.KeyF7,
		"F8":         hotkey.KeyF8,
		"F9":         hotkey.KeyF9,
		"F10":        hotkey.KeyF10,
		"F11":        hotkey.KeyF11,
		"F12":        hotkey.KeyF12,
		"F13":        hotkey.KeyF13,
		"F14":        hotkey.KeyF14,
		"F15":        hotkey.KeyF15,
		"F16":        hotkey.KeyF16,
		"F17":        hotkey.KeyF17,
		"F18":        hotkey.KeyF18,
		"F19":        hotkey.KeyF19,
		"F20":        hotkey.KeyF20,
	}
	return m
}

// mapChord translates a chord.Chord into the modifier and key constants used
// by golang.design/x/hotkey on Linux/X11.
//
// Unlike macOS and Windows, X11 does not have a fixed, portable Alt/Super
// modifier bit: the upstream package's own docs warn that "some keys may be
// mapped to multiple Mod keys" and that a real Ctrl+Alt+S might need to be
// registered as Ctrl+Mod2+Mod4+S, depending on the X server's modifier map.
// We use the near-universal X.Org default (Mod1 = Alt, Mod4 = Super/Meta);
// this can be wrong on X servers with a customized modifier mapping, and is
// not exercised by any Wayland compositor's global-shortcut portal. This is
// a known limitation of golang.design/x/hotkey on Linux, tracked for the
// Phase 4/5 library re-evaluation alongside R11 (see task-5-report.md).
func mapChord(c chord.Chord) ([]hotkey.Modifier, hotkey.Key, error) {
	key, ok := keyTable[c.Key]
	if !ok {
		return nil, 0, fmt.Errorf("hotkeys: no OS key mapping for %q", c.Key)
	}

	var mods []hotkey.Modifier
	if c.Mods&chord.ModCtrl != 0 {
		mods = append(mods, hotkey.ModCtrl)
	}
	if c.Mods&chord.ModAlt != 0 {
		mods = append(mods, hotkey.Mod1) // X.Org default: Mod1 = Alt
	}
	if c.Mods&chord.ModShift != 0 {
		mods = append(mods, hotkey.ModShift)
	}
	if c.Mods&chord.ModSuper != 0 {
		mods = append(mods, hotkey.Mod4) // X.Org default: Mod4 = Super/Meta
	}
	return mods, key, nil
}
