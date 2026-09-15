package config_test

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

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
	if !maps.Equal(got.Keybinds, cfg.Keybinds) {
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
	if !maps.Equal(cfg.Keybinds, def.Keybinds) {
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
	if !maps.Equal(reloaded.Keybinds, def.Keybinds) {
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
	cfg.Keybinds = map[string]string{
		"global.ptt":     "F1",
		"radio.1.select": "Alt+1",
	}
	cfg.General.StartMinimized = true
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Keybinds["global.ptt"] != "F1" || got.Keybinds["radio.1.select"] != "Alt+1" {
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
	// Every field flipped away from its default, so a mistyped toml tag on any
	// one of them shows up as a value reverting rather than passing silently.
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
