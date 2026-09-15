package app

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// setMinimizeToTray flips just the MinimizeToTray setting via the real
// SetSettings path (cfgPath is "" in newTestApp, so persistence is a no-op).
func setMinimizeToTray(t *testing.T, a *App, on bool) {
	t.Helper()
	s := a.GetSettings()
	s.MinimizeToTray = on
	if err := a.SetSettings(s); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
}

// TestOnMainWindowClose_TruthTable exercises every combination of tray
// presence and MinimizeToTray. The tray-absent/MinimizeToTray-on case is the
// entire point of spec R13: a failed tray must never leave the user with a
// hidden window and no way to bring it back, so OnMainWindowClose must
// return false (let the close through) in that case even though the user's
// setting asked for close-to-tray. Do not "simplify" this away.
func TestOnMainWindowClose_TruthTable(t *testing.T) {
	tests := []struct {
		name           string
		trayPresent    bool
		minimizeToTray bool
		wantPrevent    bool
	}{
		{
			name:           "tray present, minimize on -> hide",
			trayPresent:    true,
			minimizeToTray: true,
			wantPrevent:    true,
		},
		{
			name:           "tray present, minimize off -> quit",
			trayPresent:    true,
			minimizeToTray: false,
			wantPrevent:    false,
		},
		{
			name:           "R13: tray absent, minimize on -> quit anyway, never strand the user",
			trayPresent:    false,
			minimizeToTray: true,
			wantPrevent:    false,
		},
		{
			name:           "tray absent, minimize off -> quit",
			trayPresent:    false,
			minimizeToTray: false,
			wantPrevent:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _, _ := newTestApp(t)
			if tt.trayPresent {
				a.tray = &application.SystemTray{}
			} else {
				a.tray = nil
			}
			setMinimizeToTray(t, a, tt.minimizeToTray)

			if got := a.OnMainWindowClose(); got != tt.wantPrevent {
				t.Errorf("OnMainWindowClose() = %v, want %v (trayPresent=%v, minimizeToTray=%v)",
					got, tt.wantPrevent, tt.trayPresent, tt.minimizeToTray)
			}
		})
	}
}

// TestTrayAvailable_TrayPresent asserts TrayAvailable reports true once a
// tray has been assigned.
func TestTrayAvailable_TrayPresent(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.tray = &application.SystemTray{}

	if !a.TrayAvailable() {
		t.Error("TrayAvailable() = false, want true when a.tray is set")
	}
}

// TestTrayAvailable_TrayAbsent asserts TrayAvailable reports false when no
// tray was ever assigned (the zero value of *App.tray).
func TestTrayAvailable_TrayAbsent(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.tray = nil

	if a.TrayAvailable() {
		t.Error("TrayAvailable() = true, want false when a.tray is nil")
	}
}
