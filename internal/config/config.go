package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config holds the persisted user/app settings. New fields MUST get a default
// in Default() so older config files still load.
type Config struct {
	LogLevel            string `toml:"log_level"`
	ServerURL           string `toml:"server_url"`
	PingIntervalSeconds int    `toml:"ping_interval_seconds"`
}

// Default returns the baseline config used when no file exists.
func Default() *Config {
	return &Config{LogLevel: "INFO", ServerURL: "", PingIntervalSeconds: 5}
}

// Load reads the TOML file at path. Missing file → defaults (no error).
func Load(path string) (*Config, error) {
	cfg := Default()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat config: %w", err)
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

// Save writes the config atomically (write-temp + rename).
func Save(path string, cfg *Config) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("encode config: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename temp config: %w", err)
	}
	return nil
}
