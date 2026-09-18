package chord

import (
	"sort"
	"strings"
)

// codeToKey maps a browser KeyboardEvent.code (a PHYSICAL key, layout
// independent) to our canonical key name. We use .code rather than .key because
// OS-level hotkey registration matches physical keys; .key varies by layout.
var codeToKey = func() map[string]string {
	m := map[string]string{
		"Space":          "Space",
		"Escape":         "Escape",
		"Enter":          "Enter",
		"Tab":            "Tab",
		"Backspace":      "Backspace",
		"Delete":         "Delete",
		"Insert":         "Insert",
		"Home":           "Home",
		"End":            "End",
		"PageUp":         "PageUp",
		"PageDown":       "PageDown",
		"ArrowUp":        "ArrowUp",
		"ArrowDown":      "ArrowDown",
		"ArrowLeft":      "ArrowLeft",
		"ArrowRight":     "ArrowRight",
		"Minus":          "Minus",
		"Equal":          "Equal",
		"BracketLeft":    "BracketLeft",
		"BracketRight":   "BracketRight",
		"Semicolon":      "Semicolon",
		"Quote":          "Quote",
		"Backquote":      "Backquote",
		"Backslash":      "Backslash",
		"Comma":          "Comma",
		"Period":         "Period",
		"Slash":          "Slash",
		"NumpadAdd":      "NumpadAdd",
		"NumpadSubtract": "NumpadSubtract",
		"NumpadMultiply": "NumpadMultiply",
		"NumpadDivide":   "NumpadDivide",
		"NumpadDecimal":  "NumpadDecimal",
		"NumpadEnter":    "NumpadEnter",
	}
	for c := 'A'; c <= 'Z'; c++ {
		m["Key"+string(c)] = string(c)
	}
	for d := '0'; d <= '9'; d++ {
		m["Digit"+string(d)] = string(d)
		m["Numpad"+string(d)] = "Numpad" + string(d)
	}
	for i := 1; i <= 24; i++ {
		name := "F" + itoa(i)
		m[name] = name
	}
	return m
}()

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// modifierCodes are physical modifier keys. Pressing one alone must never
// produce a binding.
var modifierCodes = map[string]bool{
	"ControlLeft": true, "ControlRight": true,
	"AltLeft": true, "AltRight": true,
	"ShiftLeft": true, "ShiftRight": true,
	"MetaLeft": true, "MetaRight": true,
	"CapsLock": true,
}

func validKey(k string) bool {
	for _, v := range codeToKey {
		if v == k {
			return true
		}
	}
	return false
}

// Keys returns every canonical key name, sorted.
//
// Exported so the OS layer can prove its per-platform key tables cover this
// set rather than asserting it against a second, hand-copied list that would
// drift the moment a key is added here.
func Keys() []string {
	seen := make(map[string]bool, len(codeToKey))
	out := make([]string, 0, len(codeToKey))
	for _, v := range codeToKey {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// FromCode builds a Chord from a browser KeyboardEvent.code plus modifier flags.
func FromCode(code string, ctrl, alt, shift, super bool) (Chord, error) {
	if strings.TrimSpace(code) == "" {
		return Chord{}, ErrEmpty
	}
	if modifierCodes[code] {
		return Chord{}, ErrModifierOnly
	}
	key, ok := codeToKey[code]
	if !ok {
		return Chord{}, ErrUnknownKey
	}
	var c Chord
	if ctrl {
		c.Mods |= ModCtrl
	}
	if alt {
		c.Mods |= ModAlt
	}
	if shift {
		c.Mods |= ModShift
	}
	if super {
		c.Mods |= ModSuper
	}
	c.Key = key
	return c, nil
}
