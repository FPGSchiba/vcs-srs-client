// Package config resolves the app-data directory and TOML config files.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AppDataDir returns the per-user directory where VCS stores configs, profiles,
// logs, and session state. The folder is created if it does not exist.
//
//	Windows: %APPDATA%\VCS
//	macOS:   ~/Library/Application Support/VCS
//	Linux:   ${XDG_CONFIG_HOME:-~/.config}/VCS
func AppDataDir() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("APPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
	}
	dir := filepath.Join(base, "VCS")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ConfigFilePath returns the path to config.toml under AppDataDir.
func ConfigFilePath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// WindowStateFilePath returns the path to windows.json under AppDataDir.
func WindowStateFilePath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "windows.json"), nil
}

// LogFilePath returns the path to the rotating client log under AppDataDir
// (in a "log" subdirectory, which is created if it does not exist).
func LogFilePath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(dir, "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(logDir, "vcs-client.log"), nil
}

// ProfilesDirPath resolves where radio profiles live. A non-blank
// `configured` (config.toml's profiles_dir) is returned verbatim; anything
// else falls back to a "profiles" directory under AppDataDir, which is
// created if absent.
//
// A whitespace-only override is treated as empty: that is a user who
// cleared the field, not a request for a directory named three spaces.
func ProfilesDirPath(configured string) (string, error) {
	if c := strings.TrimSpace(configured); c != "" {
		return c, nil
	}
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	return out, nil
}

// HistoryFilePath returns the path to history.json under AppDataDir.
func HistoryFilePath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.json"), nil
}
