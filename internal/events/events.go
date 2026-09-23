// Package events defines typed wrappers over the Wails event emitter so payload
// shapes are declared in one place (and Wails-generated TS picks them up via the
// App bindings that reference these types).
package events

// Emitter is the minimal interface this package needs from the Wails
// Application. Production code adapts wails Application event emission to it;
// tests pass a fake.
type Emitter interface {
	Emit(name string, payload any)
}

// Event names — keep in sync with frontend src/shared/api/events.ts.
const (
	EventClientState       = "state:client_state"
	EventClientUpdate      = "state:client_update"
	EventClientLeft        = "state:client_left"
	EventRadioUpdate       = "state:radio_update"
	EventSettingsUpdate    = "state:settings_update"
	EventServerAction      = "state:server_action"
	EventAuthFlowStep      = "auth:flow_step"
	EventAuthSession       = "auth:session_changed"
	EventControlConnection = "control:connection"
	EventWindowGeometry    = "window:geometry_changed"
	EventWindowState       = "window:state" // payload: []string of open window ids
	EventSettingsChanged   = "settings:changed"
	EventKeybindsChanged   = "keybinds:changed"
	EventHotkeyPressed     = "hotkey:pressed"
	EventHotkeyReleased    = "hotkey:released"
	EventHotkeysState      = "hotkeys:state"
	// EventJoystickCaptured is emitted when a joystick capture completed and bound
	// itself.
	EventJoystickCaptured = "keybinds:joy_captured"
)

// ConnectionState is the payload value used with EventControlConnection.
type ConnectionState string

const (
	ConnConnected    ConnectionState = "connected"
	ConnReconnecting ConnectionState = "reconnecting"
	ConnDisconnected ConnectionState = "disconnected"
)

// ClientUpdatePayload mirrors srspb.ClientInfo fields so the public event shape
// never leaks generated proto types into the binding surface.
type ClientUpdatePayload struct {
	Name      string `json:"name"`
	Coalition string `json:"coalition"`
	UnitId    string `json:"unit_id"`
	RoleId    uint32 `json:"role_id"`
}

// ClientUpdateEnvelope is what arrives on EventClientUpdate: { guid, info }.
type ClientUpdateEnvelope struct {
	Guid string              `json:"guid"`
	Info ClientUpdatePayload `json:"info"`
}

// Tagged is the typed publisher.
type Tagged struct {
	em Emitter
}

// New constructs a Tagged emitter from a low-level Emitter implementation.
func New(em Emitter) *Tagged {
	return &Tagged{em: em}
}

// ClientUpdate emits EventClientUpdate.
func (t *Tagged) ClientUpdate(guid string, info ClientUpdatePayload) {
	t.em.Emit(EventClientUpdate, ClientUpdateEnvelope{Guid: guid, Info: info})
}

// ClientLeft emits EventClientLeft.
func (t *Tagged) ClientLeft(guid string) {
	t.em.Emit(EventClientLeft, struct {
		Guid string `json:"guid"`
	}{Guid: guid})
}

// ConnectionState emits EventControlConnection.
func (t *Tagged) ConnectionState(state ConnectionState) {
	t.em.Emit(EventControlConnection, state)
}

// RadioPayload mirrors srspb.Radio for the binding/event surface.
type RadioPayload struct {
	ID         uint32  `json:"id"`
	Name       string  `json:"name"`
	Frequency  float32 `json:"frequency"`
	Enabled    bool    `json:"enabled"`
	IsIntercom bool    `json:"is_intercom"`
}

// RadioInfoPayload mirrors srspb.RadioInfo.
type RadioInfoPayload struct {
	Radios []RadioPayload `json:"radios"`
	Muted  bool           `json:"muted"`
}

// RadioUpdateEnvelope is the EventRadioUpdate payload.
type RadioUpdateEnvelope struct {
	Guid  string           `json:"guid"`
	Radio RadioInfoPayload `json:"radio"`
}

// RadioUpdate emits EventRadioUpdate.
func (t *Tagged) RadioUpdate(guid string, info RadioInfoPayload) {
	t.em.Emit(EventRadioUpdate, RadioUpdateEnvelope{Guid: guid, Radio: info})
}

// SettingsUpdate emits EventSettingsUpdate with an opaque payload.
func (t *Tagged) SettingsUpdate(payload any) { t.em.Emit(EventSettingsUpdate, payload) }

// ServerAction emits EventServerAction with an opaque payload.
func (t *Tagged) ServerAction(payload any) { t.em.Emit(EventServerAction, payload) }

// SessionChanged emits EventAuthSession ("logged_in" | "logged_out").
func (t *Tagged) SessionChanged(state string) { t.em.Emit(EventAuthSession, state) }

// ClientState emits EventClientState with a full snapshot payload.
func (t *Tagged) ClientState(snapshot any) { t.em.Emit(EventClientState, snapshot) }

// HotkeyStatePayload is the EventHotkeysState payload.
type HotkeyStatePayload struct {
	Registered bool   `json:"registered"`
	Error      string `json:"error"`
	// Failed maps action ID -> reason for every binding that could not be
	// registered, mirroring app.HotkeyStateDTO.Failed. The UI needs to know
	// WHICH binding did not take effect, not just that something failed.
	Failed map[string]string `json:"failed"`
	// Permission is the OS grant state for global hotkey capture, mirroring
	// app.HotkeyStateDTO.Permission: "unknown" | "granted" | "denied" |
	// "not_applicable". Carried here so the UI branches on a STATE rather
	// than pattern-matching Error's text, which is a registrar message and
	// not a stable contract.
	Permission string `json:"permission"`
}

// SettingsChanged emits EventSettingsChanged with the full settings struct.
func (t *Tagged) SettingsChanged(payload any) { t.em.Emit(EventSettingsChanged, payload) }

// KeybindsChanged emits EventKeybindsChanged with the FULL binding list.
// Full replacement rather than a delta: the list is small, and it removes a
// class of frontend/backend divergence bug.
func (t *Tagged) KeybindsChanged(payload any) { t.em.Emit(EventKeybindsChanged, payload) }

// HotkeyPressed emits EventHotkeyPressed.
func (t *Tagged) HotkeyPressed(actionID string) {
	t.em.Emit(EventHotkeyPressed, struct {
		ActionID string `json:"action_id"`
	}{ActionID: actionID})
}

// HotkeyReleased emits EventHotkeyReleased (Hold actions only).
func (t *Tagged) HotkeyReleased(actionID string) {
	t.em.Emit(EventHotkeyReleased, struct {
		ActionID string `json:"action_id"`
	}{ActionID: actionID})
}

// JoystickCaptured tells the UI that a joystick capture completed and bound
// itself, so the listening chip can close. The binding itself arrives via
// EventKeybindsChanged -- this carries only the action id, because the UI
// needs to know WHICH row to close and nothing more.
func (t *Tagged) JoystickCaptured(actionID string) {
	t.em.Emit(EventJoystickCaptured, struct {
		ActionID string `json:"action_id"`
	}{ActionID: actionID})
}

// HotkeysState emits EventHotkeysState so a failed registration is visible in
// the UI rather than silent. failed maps action ID -> failure reason for
// every binding that could not be registered; permission is the OS grant
// state (see HotkeyStatePayload.Permission), which tells the UI whether the
// failure is one the user can actually do something about.
func (t *Tagged) HotkeysState(registered bool, errMsg string, failed map[string]string, permission string) {
	t.em.Emit(EventHotkeysState, HotkeyStatePayload{
		Registered: registered,
		Error:      errMsg,
		Failed:     failed,
		Permission: permission,
	})
}
