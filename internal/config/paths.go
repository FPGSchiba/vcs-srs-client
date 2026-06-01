// Package config resolves the app-data directory and TOML config files.
package config

import (
	"os"
	"path/filepath"
	"runtime"
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
