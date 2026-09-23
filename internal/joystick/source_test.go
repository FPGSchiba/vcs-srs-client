package joystick

import (
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
)

// TestSanitiseDeviceNameReplacesInvalidUTF8 pins the invariant Device.Name
// documents: whatever the OS handed over, what comes out is valid UTF-8.
func TestSanitiseDeviceNameReplacesInvalidUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain ascii passes through", "Thrustmaster T.16000M", "Thrustmaster T.16000M"},
		{"valid multi-byte passes through", "Vücht Störöm ⚙", "Vücht Störöm ⚙"},
		{"empty stays empty", "", ""},
		{"lone continuation byte", "Bad\xffName", "Bad�Name"},
		{"truncated multi-byte sequence", "\xe2\x82", "�"},
		{"a whole name of garbage", "\xff\xfe\xfd", "�"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitiseDeviceName(tc.in)
			if got != tc.want {
				t.Errorf("sanitiseDeviceName(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("sanitiseDeviceName(%q) = %q, which is not valid UTF-8", tc.in, got)
			}
		})
	}
}

// TestSanitisedDeviceNameSurvivesAConfigRoundTrip is the test that gives the
// sanitiser its reason to exist, and it is deliberately an integration test
// against the real encoder rather than a unit test of the substitution.
//
// A Device.Name is persisted verbatim under [keybind_devices]. BurntSushi's
// default encoder handles control characters, quotes and newlines correctly
// but writes an invalid UTF-8 byte out RAW, so config.toml then fails to
// parse with "invalid UTF-8 byte: 0xff". main.go falls back to
// config.Default() with cfgPath still set, and the first subsequent Save
// rewrites the file -- every setting and keybind the user had, gone.
// config.quoteTOML exists for exactly that hazard but covers only
// KeybindValue.
//
// The second half asserts the unsanitised name really does break the file,
// so this cannot quietly degrade into a tautology if the encoder ever starts
// substituting on its own.
func TestSanitisedDeviceNameSurvivesAConfigRoundTrip(t *testing.T) {
	const raw = "HOTAS \xffThrottle"

	t.Run("sanitised", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		cfg := config.Default()
		cfg.KeybindDevices = map[string]string{"stick-c3": sanitiseDeviceName(raw)}
		if err := config.Save(path, cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}
		back, err := config.LoadOrCreate(path)
		if err != nil {
			t.Fatalf("a sanitised device name must survive a save/load cycle, got: %v", err)
		}
		if got := back.KeybindDevices["stick-c3"]; got != "HOTAS �Throttle" {
			t.Errorf("round-tripped name = %q, want %q", got, "HOTAS �Throttle")
		}
	})

	t.Run("unsanitised is what we are protecting against", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		cfg := config.Default()
		cfg.KeybindDevices = map[string]string{"stick-c3": raw}
		if err := config.Save(path, cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if _, err := config.LoadOrCreate(path); err == nil {
			t.Skip("the TOML encoder now handles invalid UTF-8 itself; " +
				"sanitiseDeviceName is still the right boundary, but this half no longer bites")
		}
	})
}
