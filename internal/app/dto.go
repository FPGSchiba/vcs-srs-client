package app

import srspb "github.com/FPGSchiba/vcs-srs-client/srspb"

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

// SettingsDTO is the binding-facing shape of config.General.
type SettingsDTO struct {
	StartMinimized       bool `json:"start_minimized"`
	MinimizeToTray       bool `json:"minimize_to_tray"`
	ShowTransmitterName  bool `json:"show_transmitter_name"`
	PlayConnectionSounds bool `json:"play_connection_sounds"`
	RadioSwitchAsPTT     bool `json:"radio_switch_as_ptt"`
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

// KeybindDTO is one row of the joined action-registry + bound-chord list.
type KeybindDTO struct {
	ActionID string `json:"action_id"`
	Label    string `json:"label"`
	Desc     string `json:"desc"`
	Category string `json:"category"` // "global" | "channel" | "per_radio" | "status"
	Kind     string `json:"kind"`     // "hold" | "press"
	Chord    string `json:"chord"`    // canonical form, "" when unbound
}

// StolenDTO reports which action lost its chord to a new binding.
type StolenDTO struct {
	ActionID string `json:"action_id"`
	Label    string `json:"label"`
	Chord    string `json:"chord"`
}

// SetKeybindResult is the result of SetKeybind.
type SetKeybindResult struct {
	Stolen *StolenDTO `json:"stolen"` // nil when there was no conflict
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
}
