package app

import (
	"context"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
	"github.com/FPGSchiba/vcs-srs-client/internal/version"
)

// GetBuildInfo returns the client/protocol versions and build identifier.
func (a *App) GetBuildInfo() BuildInfoDTO {
	return BuildInfoDTO{
		ClientVersion:   version.Client,
		ProtocolVersion: version.Protocol,
		Build:           version.Build(),
	}
}

// Connect runs the guest connect sequence, then re-pushes the locally
// persisted radio set (the server creates every client with zero radios)
// and dials the voice session now that SyncClient has populated the
// store's voice_secret. A voice dial failure never fails Connect: the
// control plane is fully usable with no voice, exactly like a missing
// audio backend elsewhere in this app.
func (a *App) Connect(serverURL, name, password, unitID string) error {
	ctx := context.Background()
	if err := a.sess.Connect(ctx, serverURL, name, password, unitID); err != nil {
		return err
	}
	a.rememberServerAddress(serverURL)
	// F1 fix (Phase 6 whole-branch review): cfg.ServerURL is never written --
	// there is no Settings field for it and the Welcome form's address never
	// reaches config -- so main.go's startup monitor.SetServer(cfg.ServerURL)
	// is always SetServer(""), and the status bar's `.val` rendered
	// "standalone" on every default install even while connected. This is
	// the ACTUAL address just dialed, which is what the surface needs. Guard
	// for nil the same as every other a.health use site (voice.go, bindings.go
	// GetConnectionState): tests and any build where wiring failed leave it
	// unset.
	if a.health != nil {
		a.health.SetServer(serverURL)
	}
	a.pushPersistedRadios(ctx)
	a.startVoiceSession()
	return nil
}

// rememberServerAddress persists addr as config's last_server_address after
// a successful connect (FE18, D4). Only the address is written -- never the
// password, never server_url. A failure is logged and swallowed: the
// connection is up, and failing it over a config write would be absurd.
func (a *App) rememberServerAddress(addr string) {
	sb := a.settings
	if sb == nil {
		return
	}
	if _, err := sb.persistCfg(cfgTxn{mutate: func(c *config.Config) { c.LastServerAddress = addr }}); err != nil {
		a.logger.Warn("config: could not remember the server address", "err", err)
	}
}

// GetLastServerAddress returns the address of the last successful connect,
// for the Welcome screen to prefill; "" when none is remembered or no
// settings backend is wired.
func (a *App) GetLastServerAddress() string {
	sb := a.settings
	if sb == nil {
		return ""
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.cfg.LastServerAddress
}

// Disconnect tears down the voice session, THEN the control session -- so a
// voice session never outlives the control connection it was dialed
// against.
func (a *App) Disconnect() error {
	a.stopVoiceSession()
	ctx, cancel := context.WithTimeout(context.Background(), disconnectTimeout)
	defer cancel()
	return a.sess.Disconnect(ctx)
}

// CancelConnect aborts a Connect or Reconnect still in flight (the Welcome
// screen's CANCEL). The in-flight call then returns the session's
// connect-cancelled error, which the frontend shows no error for. A no-op
// when nothing is in flight.
func (a *App) CancelConnect() {
	a.sess.CancelConnect()
}

// Reconnect re-establishes the control session, then re-pushes the locally
// persisted radio set -- exactly what Connect does after a.sess.Connect
// (see pushPersistedRadios' doc). Without this (I1), the server creates the
// reconnected client with zero radios again and nothing ever re-pushes them:
// the voice half recovers on its own (Session.Reconnect re-dials against the
// fresh secret), but the server has nothing to relay to us, so receive stays
// dead after every reconnect even though the banner reports connected.
func (a *App) Reconnect() error {
	ctx := context.Background()
	if err := a.sess.Reconnect(ctx); err != nil {
		return err
	}
	a.pushPersistedRadios(ctx)
	return nil
}

// GetClientState returns the current snapshot for window hydration.
func (a *App) GetClientState() ClientStateSnapshot {
	snap := a.st.Snapshot()
	out := SnapshotFromProto(snap.Clients, snap.Radios)
	out.SelfGUID = snap.SelfGUID
	if snap.Self != nil {
		self := clientInfoFromProto(snap.Self)
		out.Self = &self
	}
	return out
}

// UpdateRadioInfo pushes a radio config change through the live control
// client, but writes it through to local config FIRST (C1 fix -- see
// persistRadios' doc in voice.go). Without the write-through,
// resolveTXTarget and refreshRXContext -- which both read sb.cfg.Radios
// directly, never the server's echo -- kept transmitting on and accepting
// the OLD frequency after every UI tune, no matter what the server was told.
//
// refreshRXContext runs synchronously right after the write-through, not
// after the server's echo (residual from wave A's review). TX has no
// equivalent window -- resolveTXTarget reads sb.cfg.Radios directly and
// moves the instant persistRadios returns -- but before this, RX kept
// accepting the OLD frequency list for as long as the server's own
// CLIENT_RADIO_UPDATE echo (or a fresh dial) took to arrive and re-trigger
// OnRadiosChanged. Calling it here closes that window so TX and RX move in
// lockstep with the local write, and it is a documented no-op while
// disconnected (see refreshRXContext's own doc).
// Failures are reported through reportAction (Phase 6.9 CL3, FE09).
func (a *App) UpdateRadioInfo(info RadioInfoDTO) error {
	err := a.persistRadios(info.Radios)
	a.reportAction(actRadioSave, err)
	if err != nil {
		return err
	}
	a.refreshRXContext()
	ctx, cancel := context.WithTimeout(context.Background(), radioPushTimeout)
	defer cancel()
	err = a.sess.UpdateRadioInfo(ctx, RadioInfoToProto(info))
	a.reportAction(actRadioSend, err)
	return err
}

// OpenWindow opens (or focuses) a window by id.
func (a *App) OpenWindow(id string) { a.windows.Open(id) }

// CloseWindow closes a window by id.
func (a *App) CloseWindow(id string) { a.windows.Close(id) }

// ToggleWindow opens the window if closed, or closes it if open.
func (a *App) ToggleWindow(id string) { a.windows.Toggle(id) }

// GetOpenWindows returns the ids of currently-open windows.
func (a *App) GetOpenWindows() []string { return a.windows.OpenWindows() }

// GetConnectionState returns the current connection-health snapshot, for a
// window hydrating on mount. The push counterpart is the connection:state
// event; this exists because a window opened AFTER a transition has missed
// it -- exactly the precedent GetHotkeyState, GetJoystickState and
// GetAudioState set.
//
// With no monitor wired it answers honestly -- disconnected, voice
// unavailable, nothing measured -- rather than panicking or claiming health
// nothing has confirmed.
func (a *App) GetConnectionState() ConnectionStateDTO {
	if a.health == nil {
		return ConnectionStateDTOFrom(connhealth.Snapshot{
			Control: connhealth.Link{
				State: connhealth.StateDisconnected, RTTMs: connhealth.RTTUnknown, Available: true,
			},
			Voice: connhealth.Link{
				State: connhealth.StateUnavailable, RTTMs: connhealth.RTTUnknown,
			},
		})
	}
	return ConnectionStateDTOFrom(a.health.Snapshot())
}

// ReconnectVoice tears the live voice session down and dials a fresh one.
//
// The voice plane already recovers on its own -- voice.Session re-HELLOs on
// its ladder and, failing that, re-dials a fresh socket every 15s
// indefinitely -- so this is not the only route back. It exists so the
// banner's RECONNECT VOICE button can force the attempt NOW rather than
// leaving the user to wait out the current rung.
//
// F8 fix (Phase 6 whole-branch review): it now returns an error when no
// session could be started -- no voice secret yet, an unparseable self GUID,
// a build whose codec is the stub, or a dial failure -- because those are
// exactly the cases worth reporting through the banner's failure slot: a
// user who clicks RECONNECT VOICE and gets `RECONNECTING…` followed by
// silence has no way to learn why. This is deliberately NOT the same
// discipline as startVoiceSession's other callers (Connect/Reconnect, via
// the void-returning startVoiceSession wrapper): those must not fail the
// control connection over a missing voice capability, but this call exists
// FOR THE USER to ask "why didn't that work", so the answer must reach them.
func (a *App) ReconnectVoice() error {
	a.stopVoiceSession()
	return a.startVoiceSessionWithGen(a.nextVoiceGeneration())
}
