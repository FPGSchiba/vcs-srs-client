package app

import (
	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// RadioDTO is the binding-facing radio shape.
type RadioDTO struct {
	ID         uint32  `json:"id"`
	Name       string  `json:"name"`
	Frequency  float32 `json:"frequency"`
	Enabled    bool    `json:"enabled"`
	IsIntercom bool    `json:"is_intercom"`
}

// RadioInfoDTO is the binding-facing radio-set shape.
type RadioInfoDTO struct {
	Radios []RadioDTO `json:"radios"`
	Muted  bool       `json:"muted"`
}

// ClientInfoDTO is the binding-facing client shape.
type ClientInfoDTO struct {
	Name      string `json:"name"`
	Coalition string `json:"coalition"`
	UnitID    string `json:"unit_id"`
	RoleID    uint32 `json:"role_id"`
}

// ClientStateSnapshot is returned by GetClientState.
type ClientStateSnapshot struct {
	Clients  map[string]ClientInfoDTO `json:"clients"`
	Radios   map[string]RadioInfoDTO  `json:"radios"`
	SelfGUID string                   `json:"self_guid"`
	Self     *ClientInfoDTO           `json:"self"`
}

// VoiceStateDTO is the binding-facing snapshot App.VoiceState returns.
type VoiceStateDTO struct {
	// SelectedRadio is the radio id global.ptt currently targets, 0 if none.
	SelectedRadio uint32 `json:"selected_radio"`
	// Connected reports whether a voice session is currently live. It is
	// deliberately narrower than the control connection: the control plane
	// can be connected while voice failed to dial (no secret yet, a bad
	// self GUID, or a dial error -- see App.startVoiceSession in voice.go),
	// and the UI needs to be able to tell the two apart.
	Connected bool `json:"connected"`
}

// BuildInfoDTO carries version identifiers for display in the UI.
type BuildInfoDTO struct {
	ClientVersion   string `json:"client_version"`
	ProtocolVersion string `json:"protocol_version"`
	Build           string `json:"build"`
}

// RadioInfoToProto maps a DTO to the proto message.
func RadioInfoToProto(d RadioInfoDTO) *srspb.RadioInfo {
	out := &srspb.RadioInfo{Muted: d.Muted}
	for _, r := range d.Radios {
		out.Radios = append(out.Radios, &srspb.Radio{
			Id: r.ID, Name: r.Name, Frequency: r.Frequency,
			Enabled: r.Enabled, IsIntercom: r.IsIntercom,
		})
	}
	return out
}

// RadioInfoFromProto maps a proto message to a DTO.
func RadioInfoFromProto(p *srspb.RadioInfo) RadioInfoDTO {
	out := RadioInfoDTO{Muted: p.GetMuted()}
	for _, r := range p.GetRadios() {
		out.Radios = append(out.Radios, RadioDTO{
			ID: r.GetId(), Name: r.GetName(), Frequency: r.GetFrequency(),
			Enabled: r.GetEnabled(), IsIntercom: r.GetIsIntercom(),
		})
	}
	return out
}

func clientInfoFromProto(p *srspb.ClientInfo) ClientInfoDTO {
	return ClientInfoDTO{
		Name: p.GetName(), Coalition: p.GetCoalition(),
		UnitID: p.GetUnitId(), RoleID: p.GetRoleId(),
	}
}

// SnapshotFromProto builds a snapshot DTO from proto maps.
func SnapshotFromProto(clients map[string]*srspb.ClientInfo, radios map[string]*srspb.RadioInfo) ClientStateSnapshot {
	snap := ClientStateSnapshot{
		Clients: map[string]ClientInfoDTO{},
		Radios:  map[string]RadioInfoDTO{},
	}
	for g, c := range clients {
		snap.Clients[g] = clientInfoFromProto(c)
	}
	for g, r := range radios {
		snap.Radios[g] = RadioInfoFromProto(r)
	}
	return snap
}

// SettingsDTO is the binding-facing shape of config.General plus
// config.Audio (Audio). One settings path, one settings:changed event -- see
// audio.go's package doc for why Audio does not get its own
// Get/SetAudioSettings pair.
type SettingsDTO struct {
	StartMinimized       bool `json:"start_minimized"`
	MinimizeToTray       bool `json:"minimize_to_tray"`
	ShowTransmitterName  bool `json:"show_transmitter_name"`
	PlayConnectionSounds bool `json:"play_connection_sounds"`
	RadioSwitchAsPTT     bool `json:"radio_switch_as_ptt"`

	Audio AudioSettingsDTO `json:"audio"`
}

// AudioSettingsDTO is the binding-facing shape of config.Audio. Levels and
// the VOX threshold are normalized 0-1 here; the UI renders the design's
// 0-100 scale. Keeping the scale conversion in one place (the React
// component) stops the two representations from drifting.
type AudioSettingsDTO struct {
	InputDevice       string                    `json:"input_device"`
	OutputDevice      string                    `json:"output_device"`
	InputDeviceName   string                    `json:"input_device_name"`
	OutputDeviceName  string                    `json:"output_device_name"`
	MicPassthrough    bool                      `json:"mic_passthrough"`
	AGC               bool                      `json:"agc"`
	NoiseSuppression  bool                      `json:"noise_suppression"`
	VOX               bool                      `json:"vox"`
	VOXThreshold      float32                   `json:"vox_threshold"`
	VOXMinLengthMS    int                       `json:"vox_min_length_ms"`
	VOXHangMS         int                       `json:"vox_hang_ms"`
	VOXNoiseCancel    bool                      `json:"vox_noise_cancel"`
	PTTStartDelayMS   int                       `json:"ptt_start_delay_ms"`
	PTTReleaseDelayMS int                       `json:"ptt_release_delay_ms"`
	VoiceEffect       string                    `json:"voice_effect"`
	ClippingEffect    string                    `json:"clipping_effect"`
	Levels            AudioLevelsDTO            `json:"levels"`
	Effects           map[string]AudioEffectDTO `json:"effects"`
	// EffectOrder is Effects' keys in the manifest's display order (see
	// audio.SFX.EffectIDs' doc) -- a JSON object's key order is not
	// guaranteed (and encoding/json sorts map keys alphabetically in
	// practice), so a stable render order for the frontend's Radio Effects
	// panel has to travel as its own array. Derived and read-only, like
	// Label/Available: not persisted, and configAudioFromDTO ignores it.
	EffectOrder []string `json:"effect_order"`
}

// AudioLevelsDTO holds the four mixer bus positions, 0-1.
type AudioLevelsDTO struct {
	Master       float32 `json:"master"`
	Voice        float32 `json:"voice"`
	SFX          float32 `json:"sfx"`
	Notification float32 `json:"notification"`
}

// AudioEffectDTO is one Radio Effects slot's persisted state, merged with
// its live manifest metadata: Enabled/File round-trip through
// config.Audio.Effects (SetSettings persists them), while Label/Available
// are read fresh off audio.Manager's SFX manifest on every GetSettings
// call (audioSettingsDTO) and are NOT persisted -- config.Audio.Effects has
// no fields for them, and configAudioFromDTO ignores them on the way back.
//
// Available is false for every slot today: the SFX sample pack
// (internal/audio/assets/README.md) has not landed, so nothing is
// available regardless of id -- that is the true, unfaked answer, not a
// placeholder. When no audio backend is wired at all (main.go's
// NewMalgoBackend failed, or a test never called SetAudioBackend), Label
// falls back to the slot id and Available stays false, since there is no
// Manager to ask.
type AudioEffectDTO struct {
	Enabled bool   `json:"enabled"`
	File    string `json:"file"`
	Label   string `json:"label"`
	// Available is false when no sample is behind the slot -- expected
	// until the sample pack lands. The UI greys the row rather than
	// presenting a PREVIEW button that does nothing.
	Available bool `json:"available"`
}

// AudioEffectPresetDTO is one selectable DSP preset (a Voice Effect or
// Clipping Effect dropdown option), mirroring audio.EffectPreset. Value is
// what SetSettings persists into AudioSettingsDTO.VoiceEffect/
// ClippingEffect; Label is display-only.
type AudioEffectPresetDTO struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// AudioEffectPresetsDTO carries both built-in DSP preset lists in one
// binding call, mirroring AudioDevicesDTO's inputs/outputs pairing for the
// same reason: the frontend's Radio Effects screen always wants both
// together. This is static data (internal/audio's voiceBands/
// clippingDrives), not per-Manager state, so it is available even when no
// audio backend is wired -- unlike GetAudioDevices/GetAudioState.
type AudioEffectPresetsDTO struct {
	Voice    []AudioEffectPresetDTO `json:"voice"`
	Clipping []AudioEffectPresetDTO `json:"clipping"`
}

// AudioDeviceDTO is one selectable endpoint.
type AudioDeviceDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// AudioDevicesDTO is the full enumerated device list for both directions.
type AudioDevicesDTO struct {
	Inputs  []AudioDeviceDTO `json:"inputs"`
	Outputs []AudioDeviceDTO `json:"outputs"`
}

// AudioStateDTO reports the audio subsystem's health, mirroring
// HotkeyStateDTO and JoystickStateDTO so Phase 7's notification channel can
// absorb all three the same way.
//
// InputError/OutputError are shared by two very different failure modes,
// and Running is what tells them apart -- consumers MUST check Running
// before rendering either error:
//
//   - Running == false: there is no audio engine at all. This is main.go's
//     whole-backend construction failure path (audio.NewMalgoBackend
//     returned an error, e.g. no sound card, a denied OS permission, a
//     broken driver) -- there was never a Manager to report per-direction
//     health, so both InputError and OutputError carry the SAME
//     backend-level error message. A UI that shows InputError here as "your
//     microphone failed" is wrong: the honest message is "no audio backend
//     is available on this machine" (nothing works, not just the mic).
//   - Running == true: a real Manager is up and polling. InputError and
//     OutputError are now independent per-direction results -- either can
//     be set on its own (one direction opened fine, the other didn't) or
//     both, and any message here is specific to that one direction (e.g.
//     "device busy", a saved device that vanished and had no fallback).
//     THIS is the "your microphone failed to open" case.
//
// InputDevice/OutputDevice/InputSubstituted/OutputSubstituted/Starting are
// carried straight off audio.State. They were dropped here once already, and
// that dropped exactly the thing Task 10's review had just been fixed to
// produce: spec 13's "record the substitution in State". A saved device that
// no longer enumerates, or an open device that vanished mid-session, is
// silently replaced by the default -- InputSubstituted is the ONLY signal
// that the user is not on the device they picked, and InputDevice is the only
// way to say which one they are on instead. Without both, the poll loop's
// fallback and the device picker's reopen are invisible to the user.
type AudioStateDTO struct {
	Running     bool   `json:"running"`
	Starting    bool   `json:"starting"`
	InputError  string `json:"input_error"`
	OutputError string `json:"output_error"`
	Overruns    uint64 `json:"overruns"`
	Underruns   uint64 `json:"underruns"`
	// InputDevice/OutputDevice are the device ids actually in use, which is
	// not necessarily what the settings asked for -- see the Substituted
	// pair.
	InputDevice  string `json:"input_device"`
	OutputDevice string `json:"output_device"`
	// InputSubstituted/OutputSubstituted report that the id above differs
	// from the configured one because resolveDevice fell back to the
	// default.
	InputSubstituted  bool `json:"input_substituted"`
	OutputSubstituted bool `json:"output_substituted"`
}

// CaptureDTO is a raw {code, modifiers} capture from the frontend's keydown
// listener. The physical-key mapping table lives only in internal/chord, so
// this carries the browser KeyboardEvent.code rather than a chord string.
type CaptureDTO struct {
	Code  string `json:"code"`
	Ctrl  bool   `json:"ctrl"`
	Alt   bool   `json:"alt"`
	Shift bool   `json:"shift"`
	Super bool   `json:"super"`
}

// TriggerDTO is one way to activate an action, as the frontend sees it.
//
// Display labels are rendered HERE, in Go, not in the frontend: the physical
// naming of buttons and hats has exactly one home, the same discipline that
// keeps canonical chord formatting inside internal/chord.
type TriggerDTO struct {
	Kind string `json:"kind"` // "key" | "joy"
	// Chord is the canonical chord form; set only when Kind == "key".
	Chord string `json:"chord"`
	// Device is the stable device id; set only when Kind == "joy".
	Device string `json:"device"`
	// DeviceName is the product name for display. Falls back to Device when
	// the device has never been seen.
	DeviceName string `json:"device_name"`
	// Label is the rendered input name, e.g. "Btn 12", "Hat 1 ↑",
	// "Btn 5 + Btn 3".
	Label string `json:"label"`
	// Connected reports whether the device is attached right now. A binding
	// for an absent device is still valid and renders muted rather than
	// vanishing.
	Connected bool `json:"connected"`
}

// KeybindDTO is one row of the joined action-registry + bound-trigger list.
type KeybindDTO struct {
	ActionID string       `json:"action_id"`
	Label    string       `json:"label"`
	Desc     string       `json:"desc"`
	Category string       `json:"category"` // "global" | "channel" | "per_radio" | "status"
	Kind     string       `json:"kind"`     // "hold" | "press"
	Triggers []TriggerDTO `json:"triggers"`
}

// StolenDTO reports which action lost a trigger to a new binding.
type StolenDTO struct {
	ActionID string     `json:"action_id"`
	Label    string     `json:"label"`
	Trigger  TriggerDTO `json:"trigger"`
}

// SetKeybindResult is the result of AddTrigger.
type SetKeybindResult struct {
	Stolen *StolenDTO `json:"stolen"` // nil when there was no conflict
}

// JoystickStateDTO reports the joystick subsystem's health.
type JoystickStateDTO struct {
	// Supported is false where there is no backend (macOS). The UI must hide
	// the joystick affordance rather than showing a broken one, and must NOT
	// offer a permission grant: unsupported is not denied.
	Supported bool `json:"supported"`
	// Error is the most recent source error, "" when healthy. On Linux this
	// carries the actionable 'input' group message.
	Error string `json:"error"`
	// Devices is the attached device list, for display.
	Devices []JoystickDeviceDTO `json:"devices"`
}

// JoystickDeviceDTO is one attached device.
type JoystickDeviceDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// HotkeyStateDTO reports whether OS hotkey registration is currently healthy.
type HotkeyStateDTO struct {
	Registered bool   `json:"registered"`
	Error      string `json:"error"`
	// Failed maps action ID -> reason for every binding that could not be
	// registered. Needed because internal/chord accepts keys the OS layer
	// cannot register (Numpad, F21-F24, punctuation, navigation keys), so the
	// UI must say WHICH binding did not take effect, not just that something
	// failed.
	Failed map[string]string `json:"failed"`
	// Permission is the OS grant state for global hotkey capture:
	// "unknown" | "granted" | "denied" | "not_applicable" (see
	// hotkeys.Permission.String). Present so the frontend branches on a
	// STATE rather than parsing Error's text -- Error carries whatever the
	// platform registrar said and is not a contract. Only macOS can report
	// "denied"; Windows and Linux/X11 report "not_applicable", which is the
	// UI's signal to offer no permission affordance at all.
	Permission string `json:"permission"`
}

// ConnLinkDTO is one transport plane's health, mirroring connhealth.Link.
//
// RTTMs is -1 when nothing has been measured, never 0 -- see
// connhealth.RTTUnknown for why the distinction is load-bearing. The
// frontend renders -1 as an em dash.
type ConnLinkDTO struct {
	State     string `json:"state"`
	RTTMs     int64  `json:"rtt_ms"`
	Healthy   bool   `json:"healthy"`
	Available bool   `json:"available"`
	Error     string `json:"error"`
}

// ConnectionStateDTO is the connection:state event payload and
// GetConnectionState's return, mirroring connhealth.Snapshot.
type ConnectionStateDTO struct {
	Server  string      `json:"server"`
	Control ConnLinkDTO `json:"control"`
	Voice   ConnLinkDTO `json:"voice"`
}

// ConnectionStateDTOFrom converts a connhealth.Snapshot to its wire shape.
// One shared converter for the event path and the getter, for exactly the
// reason AudioStateDTOFrom exists: a hand-written mapping on one of the two
// paths is how fields silently go missing from the other.
func ConnectionStateDTOFrom(s connhealth.Snapshot) ConnectionStateDTO {
	return ConnectionStateDTO{
		Server:  s.Server,
		Control: connLinkDTOFrom(s.Control),
		Voice:   connLinkDTOFrom(s.Voice),
	}
}

func connLinkDTOFrom(l connhealth.Link) ConnLinkDTO {
	return ConnLinkDTO{
		State:     l.State,
		RTTMs:     l.RTTMs,
		Healthy:   l.Healthy,
		Available: l.Available,
		Error:     l.Error,
	}
}

// HotkeyPermissionResultDTO is the result of RequestHotkeyPermission.
type HotkeyPermissionResultDTO struct {
	// Prompted reports what the OS request call returned. It is NOT the
	// user's answer and must never be rendered as one -- see
	// hotkeys.PermissionChecker.Request. Its only use is choosing the next
	// button: false alongside a still-denied Permission means the one-shot
	// prompt is spent and the user has to be sent to System Settings.
	Prompted bool `json:"prompted"`
	// Permission is the grant state read back immediately after the request.
	// On the happy path it is still "denied" here -- TCC answers the prompt
	// asynchronously -- and flips later through the bounded re-check poll or
	// the window-focus re-check, both of which emit hotkeys:state.
	Permission string `json:"permission"`
}
