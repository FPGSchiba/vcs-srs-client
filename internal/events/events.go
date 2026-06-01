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
