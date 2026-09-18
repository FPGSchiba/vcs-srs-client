package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

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

	// Keybinds is the raw action-ID -> trigger-string list. It is held raw
	// rather than typed so an unknown action ID written by a newer version
	// survives a load/save cycle. internal/keybinds owns interpretation, and
	// internal/trigger owns the string grammar -- this package deliberately
	// knows neither.
	Keybinds map[string]KeybindValue `toml:"keybinds"`

	// KeybindDevices maps a device id to its product name, purely for
	// display. It exists so a binding for a device that is NOT currently
	// attached can still say which device it wants -- without it, a chip for
	// an unplugged stick would render its raw id after a restart. Missing
	// entries are harmless: the UI falls back to the id and the binding
	// stays valid either way.
	KeybindDevices map[string]string `toml:"keybind_devices"`
}

// KeybindValue is one action's trigger list on disk. It accepts a bare string
// or an array of strings on read, and writes a bare string only when the
// action has exactly one KEYBOARD trigger -- so a config.toml belonging to a
// user who never binds a joystick is rewritten byte-identical.
type KeybindValue []string

// joyPrefix duplicates internal/trigger's constant rather than importing it.
// internal/config stays a raw-strings layer with no knowledge of trigger
// semantics; this one prefix is the only thing it needs, and importing the
// trigger package to get it would invert the dependency.
const joyPrefix = "joy:"

// UnmarshalTOML accepts a string or an array of strings.
func (k *KeybindValue) UnmarshalTOML(data any) error {
	switch v := data.(type) {
	case string:
		*k = KeybindValue{v}
		return nil
	case []any:
		out := make(KeybindValue, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("config: keybind entry is %T, want string", e)
			}
			out = append(out, s)
		}
		*k = out
		return nil
	default:
		return fmt.Errorf("config: keybind value is %T, want string or array of strings", data)
	}
}

// MarshalTOML writes the scalar form for a lone keyboard chord and the array
// form otherwise. See the type comment for why.
func (k KeybindValue) MarshalTOML() ([]byte, error) {
	if len(k) == 1 && !strings.HasPrefix(k[0], joyPrefix) {
		return []byte(strconv.Quote(k[0])), nil
	}
	parts := make([]string, len(k))
	for i, s := range k {
		parts[i] = strconv.Quote(s)
	}
	return []byte("[" + strings.Join(parts, ", ") + "]"), nil
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
		Keybinds:       map[string]KeybindValue{},
		KeybindDevices: map[string]string{},
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
