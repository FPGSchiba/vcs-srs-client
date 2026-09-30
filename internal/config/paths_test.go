package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
)

func TestAppDataDir_ReturnsPlatformAppropriatePath(t *testing.T) {
	dir, err := config.AppDataDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("expected absolute path, got %q", dir)
	}
	if !strings.HasSuffix(filepath.Clean(dir), "VCS") {
		t.Fatalf("expected path to end with VCS, got %q", dir)
	}
	t.Logf("os=%s dir=%s", runtime.GOOS, dir)
}

func TestConfigFilePath_IsUnderAppDataDir(t *testing.T) {
	cp, err := config.ConfigFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dir, err := config.AppDataDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(filepath.Clean(cp), filepath.Clean(dir)) {
		t.Fatalf("expected config path %q under app-data dir %q", cp, dir)
	}
	if filepath.Base(cp) != "config.toml" {
		t.Fatalf("expected basename config.toml, got %q", filepath.Base(cp))
	}
}

func TestLogFilePath_IsUnderAppDataDir(t *testing.T) {
	lp, err := config.LogFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dir, err := config.AppDataDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(filepath.Clean(lp), filepath.Clean(dir)) {
		t.Fatalf("expected log path %q under app-data dir %q", lp, dir)
	}
	if filepath.Base(lp) != "vcs-client.log" {
		t.Fatalf("expected basename vcs-client.log, got %q", filepath.Base(lp))
	}
	// The containing log directory must exist after the call.
	if _, err := os.Stat(filepath.Dir(lp)); err != nil {
		t.Fatalf("expected log directory to exist: %v", err)
	}
}

func TestProfilesDirPathHonoursOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "ops")
	got, err := config.ProfilesDirPath(want)
	if err != nil {
		t.Fatalf("ProfilesDirPath: %v", err)
	}
	if got != want {
		t.Fatalf("ProfilesDirPath(%q) = %q, want the override verbatim", want, got)
	}
}

func TestProfilesDirPathDefaultsUnderAppData(t *testing.T) {
	got, err := config.ProfilesDirPath("")
	if err != nil {
		t.Fatalf("ProfilesDirPath: %v", err)
	}
	if filepath.Base(got) != "profiles" {
		t.Fatalf("default = %q, want a 'profiles' dir under AppDataDir", got)
	}
}

func TestProfilesDirPathTrimsWhitespaceOverride(t *testing.T) {
	// An override of "   " is a user who cleared the field, not a request
	// for a directory named three spaces.
	got, err := config.ProfilesDirPath("   ")
	if err != nil {
		t.Fatalf("ProfilesDirPath: %v", err)
	}
	if filepath.Base(got) != "profiles" {
		t.Fatalf("blank override = %q, want the AppData default", got)
	}
}

func TestHistoryFilePath(t *testing.T) {
	got, err := config.HistoryFilePath()
	if err != nil {
		t.Fatalf("HistoryFilePath: %v", err)
	}
	if filepath.Base(got) != "history.json" {
		t.Fatalf("HistoryFilePath = %q", got)
	}
}
