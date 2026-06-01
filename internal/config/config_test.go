package config_test

import (
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
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if *got != *cfg {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, cfg)
	}
}

func TestLoadOrCreate_WritesDefaultsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg, err := config.LoadOrCreate(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *cfg != *config.Default() {
		t.Fatalf("expected defaults, got %+v", cfg)
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
	if *reloaded != *config.Default() {
		t.Fatalf("reloaded config differs from defaults: %+v", reloaded)
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
