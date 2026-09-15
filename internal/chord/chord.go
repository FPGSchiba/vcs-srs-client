// Package chord is a dependency-free keyboard-chord value type: parsing,
// canonical formatting, and physical-key-code mapping. It deliberately imports
// nothing outside the standard library so it is testable on any platform with
// no OS involvement.
package chord

import (
	"errors"
	"strings"
)

// Mod is a bitmask of chord modifiers.
type Mod uint8

const (
	ModCtrl Mod = 1 << iota
	ModAlt
	ModShift
	ModSuper
)

var (
	// ErrEmpty means the input string was empty.
	ErrEmpty = errors.New("chord: empty")
	// ErrModifierOnly means the chord had modifiers but no key.
	ErrModifierOnly = errors.New("chord: modifier-only chord is not bindable")
	// ErrUnknownKey means the key name is not in the canonical key set.
	ErrUnknownKey = errors.New("chord: unknown key")
	// ErrDuplicateModifier means the same modifier appeared more than once.
	ErrDuplicateModifier = errors.New("chord: duplicate modifier")
)

// Chord is a set of modifiers plus one canonical key name.
type Chord struct {
	Mods Mod
	Key  string
}

// IsZero reports whether the chord is unset.
func (c Chord) IsZero() bool { return c.Key == "" }

// modOrder is the canonical modifier ordering. Never reorder this — the string
// form is persisted to config.toml and compared for equality.
var modOrder = []struct {
	bit  Mod
	name string
}{
	{ModCtrl, "Ctrl"},
	{ModAlt, "Alt"},
	{ModShift, "Shift"},
	{ModSuper, "Super"},
}

// String renders the canonical form, e.g. "Ctrl+Alt+F1". Zero chords render "".
func (c Chord) String() string {
	if c.IsZero() {
		return ""
	}
	var b strings.Builder
	for _, m := range modOrder {
		if c.Mods&m.bit != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	b.WriteString(c.Key)
	return b.String()
}

// Parse reads a canonical (or differently-ordered) chord string. Modifier order
// in the input is not significant; the result is always canonical.
func Parse(s string) (Chord, error) {
	if strings.TrimSpace(s) == "" {
		return Chord{}, ErrEmpty
	}
	parts := strings.Split(s, "+")
	var c Chord
	for i, p := range parts {
		switch p {
		case "Ctrl":
			if c.Mods&ModCtrl != 0 {
				return Chord{}, ErrDuplicateModifier
			}
			c.Mods |= ModCtrl
			continue
		case "Alt":
			if c.Mods&ModAlt != 0 {
				return Chord{}, ErrDuplicateModifier
			}
			c.Mods |= ModAlt
			continue
		case "Shift":
			if c.Mods&ModShift != 0 {
				return Chord{}, ErrDuplicateModifier
			}
			c.Mods |= ModShift
			continue
		case "Super":
			if c.Mods&ModSuper != 0 {
				return Chord{}, ErrDuplicateModifier
			}
			c.Mods |= ModSuper
			continue
		}
		if i != len(parts)-1 {
			return Chord{}, ErrUnknownKey
		}
		if !validKey(p) {
			return Chord{}, ErrUnknownKey
		}
		c.Key = p
	}
	if c.Key == "" {
		return Chord{}, ErrModifierOnly
	}
	return c, nil
}
