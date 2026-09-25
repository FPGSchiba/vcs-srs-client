package app

import (
	"context"

	"github.com/FPGSchiba/vcs-srs-client/internal/version"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
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
	a.pushPersistedRadios(ctx)
	a.startVoiceSession()
	return nil
}

// Disconnect tears down the voice session, THEN the control session -- so a
// voice session never outlives the control connection it was dialed
// against.
func (a *App) Disconnect() error {
	a.stopVoiceSession()
	return a.sess.Disconnect(context.Background())
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
func (a *App) UpdateRadioInfo(info RadioInfoDTO) error {
	if err := a.persistRadios(info.Radios); err != nil {
		return err
	}
	return a.sess.UpdateRadioInfo(context.Background(), RadioInfoToProto(info))
}

// OpenWindow opens (or focuses) a window by id.
func (a *App) OpenWindow(id string) { a.windows.Open(id) }

// CloseWindow closes a window by id.
func (a *App) CloseWindow(id string) { a.windows.Close(id) }

// ToggleWindow opens the window if closed, or closes it if open.
func (a *App) ToggleWindow(id string) { a.windows.Toggle(id) }

// GetOpenWindows returns the ids of currently-open windows.
func (a *App) GetOpenWindows() []string { return a.windows.OpenWindows() }

// GetWindowGeometry returns a window's last geometry.
func (a *App) GetWindowGeometry(id string) windowstate.Geometry { return a.windows.Geometry(id) }

// SetWindowGeometry records a window's geometry (from debounced move/resize).
func (a *App) SetWindowGeometry(id string, g windowstate.Geometry) { a.windows.SetGeometry(id, g) }
