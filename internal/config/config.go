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

	// Audio holds Settings > Audio & Sounds. It is a raw-values layer, same
	// discipline as Keybinds/KeybindDevices above: internal/config does not
	// import internal/audio, so this stays plain fields with TOML tags and
	// no knowledge of engine semantics.
	Audio Audio `toml:"audio"`

	// Voice holds the UDP voice engine's network/buffering tuning. Same raw-
	// values discipline as Audio above: internal/config does not import
	// internal/voice.
	Voice Voice `toml:"voice"`

	// Radios is the persisted radio set. See the Radio type comment for why
	// this exists at all -- in short, the server hands every freshly
	// connected client zero radios and that state does not survive
	// disconnect, so without a local seed there is nothing to transmit on.
	Radios []Radio `toml:"radios"`

	// SelectedRadioID is the persisted radio id global.ptt targets, restored
	// into state.Store at startup (internal/app.SetSettingsBackend) and
	// kept current by every selection -- clicking a radio card
	// (App.SelectRadio) and the radio.<n>.select hotkey action alike. Zero
	// means "none selected", matching state.Store.SelectedRadio's own
	// zero-value meaning. Without this, selectedRadio lived only in
	// state.Store (in-memory), so global.ptt -- the primary PTT -- resolved
	// to nothing on every fresh launch until the user clicked a radio card.
	SelectedRadioID uint32 `toml:"selected_radio_id"`
}

// Voice holds Settings > Voice network/buffering tuning for the UDP voice
// engine. It is a raw-values layer, same discipline as Audio: internal/config
// does not import internal/voice, so this stays plain fields with TOML tags
// and no knowledge of engine semantics.
//
// Host and Port default to empty/0, which internal/voice reads as "derive
// the voice endpoint from server_url, on the server's UDP voice port" --
// what every standalone deployment needs with zero configuration. An
// explicit host/port here overrides that derivation, for a deployment that
// runs voice on a different host or port than the control connection.
type Voice struct {
	Host           string `toml:"host"`
	Port           int    `toml:"port"`
	JitterBufferMS int    `toml:"jitter_buffer_ms"`
	MaxBufferMS    int    `toml:"max_buffer_ms"`
}

// Radio is one persisted radio preset.
//
// The server creates every client with ZERO radios (state.AddClient sets
// Radios: []), and that state is scoped to the session -- it is gone the
// moment the client disconnects. Nothing server-side, and nothing else in
// the client, can create a radio, so without a local seed a freshly
// connected user has literally nothing to transmit on. Radios therefore live
// here and are re-pushed to the server on every connect; as a side effect
// the radio stack now survives a client restart too, and this is the honest
// seed for Phase 7's profiles without building profiles now.
type Radio struct {
	ID   uint32 `toml:"id"`
	Name string `toml:"name"`

	// FrequencyKHz is the canonical stored form: a plain integer, never a
	// float. internal/voice.KHz is the canonical TYPE elsewhere in the
	// client, but this package must not import internal/voice, so this is
	// just a uint32 with the same meaning. It matters that it is an integer
	// and not a float: the server decides whether to relay a transmission by
	// comparing the frequency we advertise against the sender's using EXACT
	// float32 equality, so the client must derive both wire forms it needs
	// from this one integer rather than round-tripping a value through a
	// float and risking a rounding difference that silently drops the radio
	// out of range.
	FrequencyKHz uint32 `toml:"frequency_khz"`
	Enabled      bool   `toml:"enabled"`
	IsIntercom   bool   `toml:"is_intercom"`
}

// AudioLevels holds the four mixer bus positions from Settings > Audio.
// These are KNOB POSITIONS in [0,1], not gains: internal/audio applies the
// perceptual taper. Storing gains here would bake a UI decision into the
// file format.
type AudioLevels struct {
	Master       float32 `toml:"master"`
	Voice        float32 `toml:"voice"`
	SFX          float32 `toml:"sfx"`
	Notification float32 `toml:"notification"`
}

// AudioEffect is one Radio Effects slot's persisted state.
type AudioEffect struct {
	Enabled bool   `toml:"enabled"`
	File    string `toml:"file"`
}

// Audio holds Settings > Audio & Sounds.
//
// Device identity is stored as ID PLUS display name, the same shape as
// KeybindDevices and for the same reason: a device that is unplugged right
// now must still render a meaningful name rather than a raw id. An EMPTY
// device id means "follow the system default" and is a real choice, not a
// missing value.
type Audio struct {
	InputDevice      string `toml:"input_device"`
	OutputDevice     string `toml:"output_device"`
	InputDeviceName  string `toml:"input_device_name"`
	OutputDeviceName string `toml:"output_device_name"`

	MicPassthrough   bool `toml:"mic_passthrough"`
	AGC              bool `toml:"agc"`
	NoiseSuppression bool `toml:"noise_suppression"`

	VOX            bool    `toml:"vox"`
	VOXThreshold   float32 `toml:"vox_threshold"`
	VOXMinLengthMS int     `toml:"vox_min_length_ms"`
	VOXNoiseCancel bool    `toml:"vox_noise_cancel"`
	VOXHangMS      int     `toml:"vox_hang_ms"`

	PTTStartDelayMS   int `toml:"ptt_start_delay_ms"`
	PTTReleaseDelayMS int `toml:"ptt_release_delay_ms"`

	VoiceEffect    string `toml:"voice_effect"`
	ClippingEffect string `toml:"clipping_effect"`

	Levels AudioLevels `toml:"levels"`

	// Effects is nil until the user customises a slot. Deliberately NOT an
	// empty map: the TOML encoder writes table headers for an empty-but-
	// non-nil map, which would add noise to every config file on first save
	// for no gain. Same reasoning as KeybindDevices.
	Effects map[string]AudioEffect `toml:"effects"`
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
		Audio: Audio{
			AGC:               true,
			NoiseSuppression:  true,
			VOX:               false,
			VOXThreshold:      0.35,
			VOXMinLengthMS:    220,
			VOXHangMS:         300,
			VOXNoiseCancel:    true,
			PTTStartDelayMS:   0,
			PTTReleaseDelayMS: 120,
			VoiceEffect:       "comms_filter_mid",
			ClippingEffect:    "",
			Levels: AudioLevels{
				Master:       0.75,
				Voice:        1.0,
				SFX:          0.8,
				Notification: 0.8,
			},
			// Effects is deliberately left NIL, not an empty map -- see the
			// field comment on Audio.Effects for why (same trap as
			// KeybindDevices above).
			Effects: nil,
		},
		Voice: Voice{
			// Host and Port are left at their zero values on purpose -- see
			// the Voice type comment: empty/0 means "derive from
			// server_url", which is what a standalone deployment needs.
			JitterBufferMS: 60,
			MaxBufferMS:    500,
		},
		// Radios seeds three ordinary radios plus an intercom so the Comms
		// window is never empty on first run -- see the Radio type comment
		// for why that seed has to happen at all. The IDs, names and
		// frequencies below are only a STARTING POINT for the user to edit;
		// they are not meaningful, standards-derived, or tied to anything
		// server-side.
		Radios: []Radio{
			{ID: 1, Name: "Radio 1", FrequencyKHz: 30000, Enabled: true, IsIntercom: false},
			{ID: 2, Name: "Radio 2", FrequencyKHz: 141000, Enabled: true, IsIntercom: false},
			{ID: 3, Name: "Radio 3", FrequencyKHz: 251000, Enabled: true, IsIntercom: false},
			{ID: 4, Name: "Intercom", FrequencyKHz: 1000, Enabled: true, IsIntercom: true},
		},
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
