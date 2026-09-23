package config

import (
	"errors"
	"fmt"
	"os"
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
		return []byte(quoteTOML(k[0])), nil
	}
	parts := make([]string, len(k))
	for i, s := range k {
		parts[i] = quoteTOML(s)
	}
	return []byte("[" + strings.Join(parts, ", ") + "]"), nil
}

// quoteTOML renders s as a TOML basic string.
//
// Deliberately NOT strconv.Quote, which is a GO literal quoter: it escapes an
// ASCII control character as \xNN, and TOML has no \x escape at all. That is
// reachable, not theoretical -- keybinds.Store passes the values of
// unrecognised action IDs through verbatim, so a config file (hand-edited, or
// written by a future version) carrying U+0007 in one of them came back out as
// "\a", which the TOML parser then rejects. The next Load fails, main.go falls
// back to in-memory defaults, and the first subsequent Save overwrites every
// setting and keybind the user had. Tiny probability, total cost.
//
// Control characters therefore go out as \uNNNN (or their TOML shorthand),
// which is what the format specifies. U+007F counts as one; so does every
// code point below U+0020 other than tab.
//
// Invalid UTF-8 becomes U+FFFD. TOML is defined over valid UTF-8 and has no
// way to spell a lone byte, so substituting is the only option that still
// produces a loadable file -- which is the whole point of this function.
func quoteTOML(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
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
		Keybinds: map[string]KeybindValue{},
		// KeybindDevices is deliberately left NIL, not an empty map. The
		// TOML encoder writes a bare "[keybind_devices]" table header for an
		// empty-but-non-nil map and omits it entirely for a nil one, so
		// initialising it here made the first Save after upgrading append a
		// table header to every existing keyboard-only config.toml -- a
		// diff on a file the user never asked to change, and a breach of the
		// Definition of Done's byte-identical-rewrite requirement.
		//
		// Nothing needs it non-nil: every read is a range or a len (both
		// nil-safe), and rememberDevice -- the only writer -- allocates a
		// fresh map before copying into it.
		KeybindDevices: nil,
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
