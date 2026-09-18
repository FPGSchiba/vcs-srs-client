package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// General holds the user-facing toggles from the Settings > General section.
// ShowTransmitterName, PlayConnectionSounds and RadioSwitchAsPTT are stored and
// exposed now; their consumers arrive in Phases 4/5.
type General struct {
	StartMinimized       bool `toml:"start_minimized"`
	MinimizeToTray       bool `toml:"minimize_to_tray"`
	ShowTransmitterName  bool `toml:"show_transmitter_name"`
	PlayConnectionSounds bool `toml:"play_connection_sounds"`
	RadioSwitchAsPTT     bool `toml:"radio_switch_as_ptt"`
}

// Config holds the persisted user/app settings. New fields MUST get a default
// in Default() so older config files still load.
type Config struct {
	LogLevel            string `toml:"log_level"`
	ServerURL           string `toml:"server_url"`
	PingIntervalSeconds int    `toml:"ping_interval_seconds"`

	General General `toml:"general"`

	// Keybinds is the raw action-ID -> chord-string map. It is held raw rather
	// than typed so that entries written by a newer client version survive a
	// load/save cycle. internal/keybinds owns interpretation.
	Keybinds map[string]string `toml:"keybinds"`
}

// Default returns the baseline config used when no file exists.
func Default() *Config {
	return &Config{
		LogLevel:            "INFO",
		ServerURL:           "",
		PingIntervalSeconds: 5,
		General: General{
			StartMinimized:       false,
			MinimizeToTray:       true,
			ShowTransmitterName:  true,
			PlayConnectionSounds: true,
			RadioSwitchAsPTT:     false,
		},
		Keybinds: map[string]string{},
	}
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

// LoadOrCreate loads the config at path, writing it with defaults first if the
// file does not yet exist. This guarantees a human-editable config.toml is
// present on disk after first startup.
func LoadOrCreate(path string) (*Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		if err := Save(path, cfg); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
		return cfg, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat config: %w", err)
	}
	return Load(path)
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
