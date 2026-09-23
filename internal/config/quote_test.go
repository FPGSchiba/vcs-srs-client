package config

import (
	"fmt"
	"testing"
)

// ctl renders a single control character by code point. Written this way and
// not as a literal because a raw NUL byte in a .go file is a compile error.
func ctl(code rune) string { return string([]rune{code}) }

// tomlU renders the escape TOML requires for a control code point: a
// backslash, 'u', and four uppercase hex digits, inside quotes. Assembled
// rather than written out so this file's own source does not contain the
// escape sequence it is asserting about.
func tomlU(code rune) string { return fmt.Sprintf(`"\`+`u%04X"`, code) }

// TestQuoteTOMLEmitsUnicodeEscapesNotHex is the M2 guard.
//
// MarshalTOML used to quote with strconv.Quote, a GO literal quoter: it
// renders the bell as a backslash-a escape and other ASCII control characters
// as backslash-x-NN, neither of which TOML defines. TOML requires
// backslash-u-NNNN. keybinds.Store passes the values of unrecognised action
// IDs through VERBATIM, so such a value can reach the file, after which Load
// fails, main.go falls back to in-memory defaults, and the first subsequent
// Save overwrites every setting and keybind the user had.
//
// This pins the escape FORM, so a future "simplify back to strconv.Quote" has
// something to fail against.
func TestQuoteTOMLEmitsUnicodeEscapesNotHex(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bell U+0007", ctl(0x07), tomlU(0x07)},
		{"nul U+0000", ctl(0x00), tomlU(0x00)},
		{"escape U+001B", ctl(0x1b), tomlU(0x1b)},
		{"unit separator U+001F", ctl(0x1f), tomlU(0x1f)},
		{"delete U+007F", ctl(0x7f), tomlU(0x7f)},

		// The five code points TOML gives a shorthand of their own.
		{"tab", "\t", `"\t"`},
		{"newline", "\n", `"\n"`},
		{"carriage return", "\r", `"\r"`},
		{"backspace", "\b", `"\b"`},
		{"form feed", "\f", `"\f"`},

		{"quote and backslash", `a"b\c`, `"a\"b\\c"`},
		{"an ordinary chord", "Ctrl+Shift+T", `"Ctrl+Shift+T"`},
		{"a joystick trigger", "joy:stick-c3:btn3", `"joy:stick-c3:btn3"`},
		{"non-ASCII passes through unescaped", "Warthog-" + ctl(0xdc), `"Warthog-` + ctl(0xdc) + `"`},
	}
	for _, tc := range cases {
		if got := quoteTOML(tc.in); got != tc.want {
			t.Errorf("quoteTOML(%s) = %s, want %s", tc.name, got, tc.want)
		}
	}
}
