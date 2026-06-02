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

// Connect runs the guest connect sequence.
func (a *App) Connect(serverURL, name, password, unitID string) error {
	return a.sess.Connect(context.Background(), serverURL, name, password, unitID)
}

// Disconnect tears down the session.
func (a *App) Disconnect() error { return a.sess.Disconnect(context.Background()) }

// Reconnect re-establishes the control session.
func (a *App) Reconnect() error { return a.sess.Reconnect(context.Background()) }

// GetClientState returns the current snapshot for window hydration.
func (a *App) GetClientState() ClientStateSnapshot {
	snap := a.st.Snapshot()
	return SnapshotFromProto(snap.Clients, snap.Radios)
}

// UpdateRadioInfo pushes a radio config change through the live control client.
func (a *App) UpdateRadioInfo(info RadioInfoDTO) error {
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
