package config_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
)

func TestLoad_ReturnsDefaultsWhenFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "INFO" {
		t.Fatalf("default LogLevel: expected INFO, got %q", cfg.LogLevel)
	}
	if cfg.PingIntervalSeconds != 5 {
		t.Fatalf("default PingIntervalSeconds: expected 5, got %d", cfg.PingIntervalSeconds)
	}
}

func TestLoad_ReadsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "log_level = \"DEBUG\"\nserver_url = \"127.0.0.1:50051\"\nping_interval_seconds = 2\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Fatalf("expected DEBUG, got %q", cfg.LogLevel)
	}
	if cfg.ServerURL != "127.0.0.1:50051" {
		t.Fatalf("expected 127.0.0.1:50051, got %q", cfg.ServerURL)
	}
	if cfg.PingIntervalSeconds != 2 {
		t.Fatalf("expected 2, got %d", cfg.PingIntervalSeconds)
	}
}

func TestSave_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{LogLevel: "WARN", ServerURL: "vcs.example:443", PingIntervalSeconds: 10}
	cfg.General = config.Default().General
	cfg.Keybinds = config.Default().Keybinds
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.LogLevel != cfg.LogLevel || got.ServerURL != cfg.ServerURL || got.PingIntervalSeconds != cfg.PingIntervalSeconds {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, cfg)
	}
	if got.General != cfg.General {
		t.Errorf("General lost in round trip: got %+v, want %+v", got.General, cfg.General)
	}
	if !maps.EqualFunc(got.Keybinds, cfg.Keybinds, slices.Equal) {
		t.Errorf("Keybinds lost in round trip: got %v, want %v", got.Keybinds, cfg.Keybinds)
	}
}

func TestLoadOrCreate_WritesDefaultsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg, err := config.LoadOrCreate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	def := config.Default()
	if cfg.LogLevel != def.LogLevel || cfg.ServerURL != def.ServerURL || cfg.PingIntervalSeconds != def.PingIntervalSeconds {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
	if cfg.General != def.General {
		t.Errorf("expected General defaults, got %+v", cfg.General)
	}
	if !maps.EqualFunc(cfg.Keybinds, def.Keybinds, slices.Equal) {
		t.Errorf("expected Keybinds defaults, got %v", cfg.Keybinds)
	}
	// The file must now exist on disk.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to be created: %v", err)
	}
	// And re-loading it must yield the same defaults.
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.LogLevel != def.LogLevel || reloaded.ServerURL != def.ServerURL || reloaded.PingIntervalSeconds != def.PingIntervalSeconds {
		t.Fatalf("reloaded config differs from defaults: %+v", reloaded)
	}
	if reloaded.General != def.General {
		t.Errorf("expected General to survive reload, got %+v", reloaded.General)
	}
	if !maps.EqualFunc(reloaded.Keybinds, def.Keybinds, slices.Equal) {
		t.Errorf("expected Keybinds to survive reload, got %v", reloaded.Keybinds)
	}
}

func TestLoadOrCreate_LeavesExistingFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "log_level = \"WARN\"\nserver_url = \"existing:443\"\nping_interval_seconds = 9\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cfg, err := config.LoadOrCreate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "WARN" || cfg.ServerURL != "existing:443" || cfg.PingIntervalSeconds != 9 {
		t.Fatalf("expected existing values preserved, got %+v", cfg)
	}
}

func TestDefaultGeneral(t *testing.T) {
	g := config.Default().General
	if g.StartMinimized {
		t.Error("StartMinimized should default false")
	}
	if !g.MinimizeToTray {
		t.Error("MinimizeToTray should default true")
	}
	if !g.ShowTransmitterName {
		t.Error("ShowTransmitterName should default true")
	}
	if !g.PlayConnectionSounds {
		t.Error("PlayConnectionSounds should default true")
	}
	if g.RadioSwitchAsPTT {
		t.Error("RadioSwitchAsPTT should default false")
	}
}

func TestLoadPrePhase3ConfigGetsDefaults(t *testing.T) {
	// A config.toml written before Phase 3 has neither [general] nor
	// [keybinds]. It must still load, with defaults filled in.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	old := "log_level = \"DEBUG\"\nserver_url = \"localhost:5002\"\nping_interval_seconds = 7\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "DEBUG" || cfg.PingIntervalSeconds != 7 {
		t.Errorf("existing values lost: %+v", cfg)
	}
	if !cfg.General.MinimizeToTray {
		t.Error("missing [general] should fall back to defaults")
	}
}

func TestKeybindsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.Default()
	cfg.Keybinds = map[string]config.KeybindValue{
		"global.ptt":     {"F1"},
		"radio.1.select": {"Alt+1"},
	}
	cfg.General.StartMinimized = true
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v := got.Keybinds["global.ptt"]; len(v) != 1 || v[0] != "F1" {
		t.Errorf("keybinds lost in round trip: %v", got.Keybinds)
	}
	if v := got.Keybinds["radio.1.select"]; len(v) != 1 || v[0] != "Alt+1" {
		t.Errorf("keybinds lost in round trip: %v", got.Keybinds)
	}
	if !got.General.StartMinimized {
		t.Error("general lost in round trip")
	}
}

func TestGeneralRoundTripAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.Default()
	// Verify all General fields serialize/deserialize correctly through Save/Load.
	// Sets each field to a non-default value as a sanity check for round-trip
	// encoding/decoding (not tag verification — symmetric round trips cannot catch
	// tag typos since Save and Load use the same struct tags).
	cfg.General = config.General{
		StartMinimized:       true,
		MinimizeToTray:       false,
		ShowTransmitterName:  false,
		PlayConnectionSounds: false,
		RadioSwitchAsPTT:     true,
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.General != cfg.General {
		t.Errorf("General round trip = %+v, want %+v", got.General, cfg.General)
	}
}

func TestGeneralKeyNamesMatchOnDiskContract(t *testing.T) {
	// A literal fixture, not a Save() round trip: Save and Load share the same
	// struct tags, so a round trip passes even when a tag is misspelled. Only
	// asserting against hand-written key names pins the on-disk contract.
	// Every value here is the OPPOSITE of its default, so a tag that fails to
	// match leaves the field at its default and the assertion fails.
	body := `
[general]
start_minimized = true
minimize_to_tray = false
show_transmitter_name = false
play_connection_sounds = false
radio_switch_as_ptt = true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.General{
		StartMinimized:       true,
		MinimizeToTray:       false,
		ShowTransmitterName:  false,
		PlayConnectionSounds: false,
		RadioSwitchAsPTT:     true,
	}
	if got.General != want {
		t.Errorf("General from literal TOML = %+v, want %+v", got.General, want)
	}
}

func TestKeybindsKeyNameMatchesOnDiskContract(t *testing.T) {
	// Literal fixture to pin the on-disk key name for keybinds. A mistyped
	// table name leaves Keybinds at its default empty map.
	body := `
[keybinds]
"global.ptt" = "F1"
"radio.1.select" = "Alt+1"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if v := got.Keybinds["global.ptt"]; len(v) != 1 || v[0] != "F1" {
		t.Errorf("keybinds from literal TOML = %v, want F1 and Alt+1 present", got.Keybinds)
	}
	if v := got.Keybinds["radio.1.select"]; len(v) != 1 || v[0] != "Alt+1" {
		t.Errorf("keybinds from literal TOML = %v, want F1 and Alt+1 present", got.Keybinds)
	}
}

func TestKeybindValueAcceptsStringOrArray(t *testing.T) {
	// Literal fixture, not a symmetric round trip: Phase 3 proved a
	// round-trip test stays green with a mistyped struct tag.
	const in = `
[keybinds]
"global.push_to_mute" = "V"
"global.ptt" = ["F1", "joy:throttle-a1:btn12"]
"radio.1.ptt" = ["joy:throttle-a1:btn7+stick-c3:btn3"]
`
	var got config.Config
	if _, err := toml.Decode(in, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v := got.Keybinds["global.push_to_mute"]; len(v) != 1 || v[0] != "V" {
		t.Errorf("scalar keybind = %v, want [V]", v)
	}
	if v := got.Keybinds["global.ptt"]; len(v) != 2 ||
		v[0] != "F1" || v[1] != "joy:throttle-a1:btn12" {
		t.Errorf("array keybind = %v, want [F1 joy:throttle-a1:btn12]", v)
	}
	if v := got.Keybinds["radio.1.ptt"]; len(v) != 1 ||
		v[0] != "joy:throttle-a1:btn7+stick-c3:btn3" {
		t.Errorf("modifier keybind = %v", v)
	}
}

func TestSingleKeyboardChordStaysAScalarOnDisk(t *testing.T) {
	// Global constraint 8: a user who never binds a joystick must never see
	// their config.toml change shape.
	cfg := config.Default()
	cfg.Keybinds = map[string]config.KeybindValue{
		"global.push_to_mute": {"V"},
		"global.ptt":          {"F1", "joy:throttle-a1:btn12"},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"global.push_to_mute" = "V"`) {
		t.Errorf("single keyboard chord was not written as a scalar:\n%s", text)
	}
	if !strings.Contains(text, `"global.ptt" = ["F1", "joy:throttle-a1:btn12"]`) {
		t.Errorf("multi-trigger action was not written as an array:\n%s", text)
	}
}

func TestLoneJoyTriggerIsWrittenAsArray(t *testing.T) {
	// A single JOYSTICK trigger must still be an array: writing it bare
	// would be legal TOML but would make the file's shape depend on which
	// kind of trigger happens to be first, which is harder to reason about
	// than "keyboard-only files never change".
	cfg := config.Default()
	cfg.Keybinds = map[string]config.KeybindValue{
		"global.ptt": {"joy:throttle-a1:btn12"},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"global.ptt" = ["joy:throttle-a1:btn12"]`) {
		t.Errorf("lone joy trigger not written as array:\n%s", raw)
	}
}

func TestKeybindValueRejectsNonString(t *testing.T) {
	const in = `
[keybinds]
global.ptt = [1, 2]
`
	var got config.Config
	if _, err := toml.Decode(in, &got); err == nil {
		t.Error("decode of numeric keybind entries = nil error, want failure")
	}
}

// TestSaveLoadRoundTripsAControlCharacterInAnUnknownKeybind is the end-to-end
// half of the M2 guard: the file Save writes must be readable by Load. With
// strconv.Quote it was not -- a control character came out as a Go escape TOML
// rejects, and the next startup silently fell back to defaults and then
// overwrote the user's real settings and keybinds on the first Save.
func TestSaveLoadRoundTripsAControlCharacterInAnUnknownKeybind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg := config.Default()
	cfg.Keybinds = map[string]config.KeybindValue{
		"future.action":  {"\a"},                        // the bell, the original report
		"future.action2": {"Ctrl+\x1b", "joy:x\x00y"},   // array form, ESC and NUL
		"future.action3": {"quote\"back\\slash\ttab\n"}, // the ordinary escapes
		"global.ptt":     {"Ctrl+Shift+T"},              // an unremarkable neighbour
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v -- the file Save wrote is not valid TOML", err)
	}
	for id, want := range cfg.Keybinds {
		have, ok := got.Keybinds[id]
		if !ok {
			t.Errorf("keybind %q missing after round trip", id)
			continue
		}
		if !slices.Equal([]string(have), []string(want)) {
			t.Errorf("keybind %q = %q, want %q", id, have, want)
		}
	}
}

func TestDefaultAudioMatchesTheSpec(t *testing.T) {
	a := config.Default().Audio
	if a.Levels.Master != 0.75 || a.Levels.Voice != 1.0 || a.Levels.SFX != 0.8 || a.Levels.Notification != 0.8 {
		t.Fatalf("default levels = %+v", a.Levels)
	}
	if !a.AGC || !a.NoiseSuppression {
		t.Fatal("AGC and noise suppression must default on")
	}
	if a.VOX {
		t.Fatal("VOX must default off")
	}
	if a.PTTReleaseDelayMS != 120 {
		t.Fatalf("ptt_release_delay_ms = %d, want 120", a.PTTReleaseDelayMS)
	}
	if a.Effects != nil {
		t.Fatal("Effects must default nil, not an empty map -- see the KeybindDevices comment")
	}
}

func TestLoadConfigWithoutAudioTableGetsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// A pre-Phase-4 config file.
	if err := os.WriteFile(path, []byte("log_level = \"INFO\"\nserver_url = \"\"\nping_interval_seconds = 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audio.Levels.Master != 0.75 {
		t.Fatalf("missing [audio] did not fall back to defaults: %+v", cfg.Audio)
	}
}

func TestAudioRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	in := config.Default()
	in.Audio.InputDevice = "mic-7"
	in.Audio.InputDeviceName = "Procyon Headset"
	in.Audio.VOX = true
	in.Audio.VOXThreshold = 0.42
	in.Audio.Levels.SFX = 0.5
	in.Audio.Effects = map[string]config.AudioEffect{"tx_start": {Enabled: false, File: "custom.wav"}}
	if err := config.Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Audio.InputDevice != "mic-7" || out.Audio.VOXThreshold != 0.42 || out.Audio.Levels.SFX != 0.5 {
		t.Fatalf("round trip lost values: %+v", out.Audio)
	}
	if e := out.Audio.Effects["tx_start"]; e.Enabled || e.File != "custom.wav" {
		t.Fatalf("effect round trip = %+v", e)
	}
}

// TestAudioEffectsTableAbsentWhenNil is an explicit guard for Audio.Effects,
// which -- like KeybindDevices below -- must default nil rather than an
// empty map: the TOML encoder writes a bare "[audio.effects]" table header
// for an empty-but-non-nil map and omits it entirely for a nil one, so a
// non-nil default would add a noise diff to every user's config on their
// first save after upgrading.
//
// This exact trap already shipped once in this repo with KeybindDevices (see
// TestRewritingAKeyboardOnlyConfigIsByteIdentical immediately below, whose
// keybind_devices check already guards Audio.Effects TRANSITIVELY: a non-nil
// Effects map would break that test's byte-identity assertion too). This
// test exists so that guard is not the only place the reasoning survives --
// someone repairing the keybind-only test later would otherwise have no way
// to know audio depends on it as well.
func TestAudioEffectsTableAbsentWhenNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg := config.Default()
	if cfg.Audio.Effects != nil {
		t.Fatal("Default().Audio.Effects must be nil for this test to prove anything")
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after Save: %v", err)
	}
	if strings.Contains(string(saved), "[audio.effects]") {
		t.Errorf("a config with nil Audio.Effects must not contain an [audio.effects] table, got:\n%s", saved)
	}
}

// TestRewritingAKeyboardOnlyConfigIsByteIdentical is the M1 guard.
//
// Default() used to initialise KeybindDevices to an empty, non-nil map. The
// TOML encoder writes a bare "[keybind_devices]" table header for that and
// omits the key entirely for a nil map, so the first Save after upgrading
// appended a table header to every existing keyboard-only config.toml -- a
// diff on a file the user never asked to change.
func TestRewritingAKeyboardOnlyConfigIsByteIdentical(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg := config.Default()
	cfg.Keybinds = map[string]config.KeybindValue{"global.ptt": {"Ctrl+Shift+T"}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first Save: %v", err)
	}
	if strings.Contains(string(first), "keybind_devices") {
		t.Errorf("a keyboard-only config must not mention keybind_devices, got:\n%s", first)
	}

	// Load and re-save, which is exactly what startup followed by any
	// settings change does.
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := config.Save(path, loaded); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second Save: %v", err)
	}
	if string(second) != string(first) {
		t.Errorf("load-then-save changed the file.\nbefore:\n%s\nafter:\n%s", first, second)
	}
}
