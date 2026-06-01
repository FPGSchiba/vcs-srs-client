package config_test

import (
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
