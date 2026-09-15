package hotkeys

import (
	"fmt"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// unmappable lists, per GOOS, the canonical keys that platform genuinely
// cannot express, with the reason. Anything NOT in here must be mappable --
// which is what makes TestKeyTablesCoverTheCanonicalSet a real assertion
// rather than a restatement of whatever the tables happen to contain.
//
// Keeping this list explicit is the point: a key that silently falls out of a
// table is a binding a user cannot make, and nothing else in the system would
// notice.
var unmappable = map[string]map[string]string{
	"darwin": {
		"F21": "macOS defines no kVK_ constant for F21-F24",
		"F22": "macOS defines no kVK_ constant for F21-F24",
		"F23": "macOS defines no kVK_ constant for F21-F24",
		"F24": "macOS defines no kVK_ constant for F21-F24",
	},
	"windows": {
		"NumpadEnter": "Win32 reports it as VK_RETURN + LLKHF_EXTENDED, and gohook " +
			"does not carry the extended flag onto hook.Event",
	},
	"linux": {},
}

// TestKeyTablesCoverTheCanonicalSet checks every platform table against
// internal/chord's canonical key set -- the set the UI will actually let a
// user bind. Run on any host, it validates all three tables, so a typo in the
// Windows table is caught by a Linux CI run.
func TestKeyTablesCoverTheCanonicalSet(t *testing.T) {
	canonical := chord.Keys()
	if len(canonical) == 0 {
		t.Fatal("chord.Keys() is empty")
	}

	for goos, table := range keyTables {
		exempt, ok := unmappable[goos]
		if !ok {
			t.Fatalf("keyTables has a %q table with no entry in unmappable; add one (empty if it covers everything)", goos)
		}
		for _, key := range canonical {
			_, mapped := table[key]
			_, excused := exempt[key]
			switch {
			case mapped && excused:
				t.Errorf("%s: %q is both mapped and listed as unmappable", goos, key)
			case !mapped && !excused:
				t.Errorf("%s: canonical key %q has no key code and no documented reason", goos, key)
			}
		}
	}
}

// TestKeyTablesContainOnlyCanonicalKeys is the other direction: an entry
// whose name is not a canonical key is dead weight that can never be matched,
// and is usually a typo in the name.
func TestKeyTablesContainOnlyCanonicalKeys(t *testing.T) {
	canonical := map[string]bool{}
	for _, k := range chord.Keys() {
		canonical[k] = true
	}
	for goos, table := range keyTables {
		for key := range table {
			if !canonical[key] {
				t.Errorf("%s: key table has %q, which is not a canonical chord key", goos, key)
			}
		}
	}
}

// TestKeyTablesHaveNoDuplicateCodes catches the classic copy-paste error:
// two keys mapped to the same physical code. The symptom in the field is one
// binding firing another binding's action, which is very hard to diagnose.
func TestKeyTablesHaveNoDuplicateCodes(t *testing.T) {
	for goos, table := range keyTables {
		owner := map[keyCode]string{}
		for key, code := range table {
			if prev, dup := owner[code]; dup {
				t.Errorf("%s: %q and %q both map to code %#x", goos, prev, key, code)
				continue
			}
			owner[code] = key
		}
	}
}

// TestKeyTableSpotChecks pins a sample of each table against the OS headers
// by hand. The structural tests above prove the tables are self-consistent;
// only literal values checked against an external source prove they are
// RIGHT, and a table that is uniformly wrong would pass every other test
// here.
func TestKeyTableSpotChecks(t *testing.T) {
	cases := []struct {
		goos, key string
		want      keyCode
		source    string
	}{
		// Carbon HIToolbox Events.h
		{"darwin", "A", 0x00, "kVK_ANSI_A"},
		{"darwin", "Z", 0x06, "kVK_ANSI_Z"},
		{"darwin", "Space", 0x31, "kVK_Space"},
		{"darwin", "Escape", 0x35, "kVK_Escape"},
		{"darwin", "Enter", 0x24, "kVK_Return"},
		{"darwin", "Backspace", 0x33, "kVK_Delete"},
		{"darwin", "Delete", 0x75, "kVK_ForwardDelete"},
		{"darwin", "F1", 0x7A, "kVK_F1"},
		{"darwin", "F12", 0x6F, "kVK_F12"},
		{"darwin", "F20", 0x5A, "kVK_F20"},
		{"darwin", "ArrowUp", 0x7E, "kVK_UpArrow"},
		{"darwin", "Numpad0", 0x52, "kVK_ANSI_Keypad0"},
		{"darwin", "NumpadEnter", 0x4C, "kVK_ANSI_KeypadEnter"},
		{"darwin", "Semicolon", 0x29, "kVK_ANSI_Semicolon"},

		// Win32 winuser.h
		{"windows", "A", 0x41, "'A'"},
		{"windows", "Z", 0x5A, "'Z'"},
		{"windows", "0", 0x30, "'0'"},
		{"windows", "Space", 0x20, "VK_SPACE"},
		{"windows", "Escape", 0x1B, "VK_ESCAPE"},
		{"windows", "Enter", 0x0D, "VK_RETURN"},
		{"windows", "Backspace", 0x08, "VK_BACK"},
		{"windows", "Delete", 0x2E, "VK_DELETE"},
		{"windows", "F1", 0x70, "VK_F1"},
		{"windows", "F24", 0x87, "VK_F24"},
		{"windows", "ArrowUp", 0x26, "VK_UP"},
		{"windows", "Numpad0", 0x60, "VK_NUMPAD0"},
		{"windows", "Semicolon", 0xBA, "VK_OEM_1"},

		// linux/input-event-codes.h
		{"linux", "A", 30, "KEY_A"},
		{"linux", "Z", 44, "KEY_Z"},
		{"linux", "1", 2, "KEY_1"},
		{"linux", "0", 11, "KEY_0"},
		{"linux", "Space", 57, "KEY_SPACE"},
		{"linux", "Escape", 1, "KEY_ESC"},
		{"linux", "Enter", 28, "KEY_ENTER"},
		{"linux", "Backspace", 14, "KEY_BACKSPACE"},
		{"linux", "Delete", 111, "KEY_DELETE"},
		{"linux", "F1", 59, "KEY_F1"},
		{"linux", "F11", 87, "KEY_F11"},
		{"linux", "F13", 183, "KEY_F13"},
		{"linux", "F24", 194, "KEY_F24"},
		{"linux", "ArrowUp", 103, "KEY_UP"},
		{"linux", "Numpad0", 82, "KEY_KP0"},
		{"linux", "NumpadEnter", 96, "KEY_KPENTER"},
		{"linux", "Semicolon", 39, "KEY_SEMICOLON"},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s/%s", c.goos, c.key), func(t *testing.T) {
			got, ok := keyTables[c.goos][c.key]
			if !ok {
				t.Fatalf("%s table has no %q", c.goos, c.key)
			}
			if got != c.want {
				t.Errorf("%s %q = %#x, want %#x (%s)", c.goos, c.key, got, c.want, c.source)
			}
		})
	}
}

// TestOSKeyCodeRejectsUnknownKeys: osKeyCode is the gate that turns a key the
// platform cannot express into a named per-action failure. It must not
// silently return a zero code, which on Linux is a real key code.
func TestOSKeyCodeRejectsUnknownKeys(t *testing.T) {
	for _, key := range []string{"", "NotAKey", "Ctrl", "F99", "numpad0"} {
		if _, ok := osKeyCode(key); ok {
			t.Errorf("osKeyCode(%q) reported a mapping", key)
		}
	}
}

// TestSupportedPlatform: the three targets have tables; the fallback path
// exists for anything else.
func TestSupportedPlatform(t *testing.T) {
	if !supportedPlatform() {
		t.Fatalf("no key table for the host platform")
	}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		if _, ok := keyTables[goos]; !ok {
			t.Errorf("no key table for %s", goos)
		}
	}
}
