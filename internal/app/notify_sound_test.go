package app

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// appWithSoundSetting builds an App whose settings backend has
// general.play_notification_sounds set to enabled, mirroring the idiom
// TestPlayConnectionSFX_RespectsTheSetting uses in connhealth_test.go for its
// PlayConnectionSounds equivalent.
func appWithSoundSetting(t *testing.T, enabled bool) *App {
	t.Helper()
	a := NewForTest(state.New(), nil, nil)
	a.settings = &settingsBackend{cfg: &config.Config{}}
	a.settings.cfg.General.PlayNotificationSounds = enabled
	return a
}

func TestNotificationSFXIDIsErrorSeverityOnly(t *testing.T) {
	cases := []struct {
		severity string
		want     string
	}{
		{"error", audio.NotifyAlert},
		{"warn", ""},
		{"info", ""},
		{"", ""},
		{"ERROR", ""}, // exact match only; the store emits lowercase
	}
	for _, tc := range cases {
		a := appWithSoundSetting(t, true)
		if got := a.notificationSFXID(tc.severity); got != tc.want {
			t.Errorf("notificationSFXID(%q) = %q, want %q -- sound follows the toast, error only", tc.severity, got, tc.want)
		}
	}
}

func TestNotificationSFXIDRespectsTheSetting(t *testing.T) {
	a := appWithSoundSetting(t, false)
	if got := a.notificationSFXID("error"); got != "" {
		t.Fatalf("notificationSFXID(\"error\") = %q with sounds disabled, want \"\"", got)
	}
}

func TestNotificationSFXIDWithNoSettingsBackendIsSilent(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	if got := a.notificationSFXID("error"); got != "" {
		t.Fatalf("notificationSFXID = %q with no settings backend, want \"\"", got)
	}
}

func TestPlayNotificationSFXWithNoAudioManagerDoesNotPanic(t *testing.T) {
	a := appWithSoundSetting(t, true)
	a.playNotificationSFX("error")
}

// TestNotificationSFXIDIsRaceFreeAgainstASettingsWrite pins that the gate
// reads sb.cfg ONLY under sb.mu.
//
// Both halves are production paths: SetSettings repoints sb.cfg from the
// Wails binding goroutine while the audio poll or joystick poll goroutine
// fires a notification through OnSound -> playNotificationSFX. The bug this
// caught was the nil check -- `if sb == nil || sb.cfg == nil` OUTSIDE the
// lock, which -race reports as a write/read race on sb.cfg. Its sibling
// configuredAudioDevices does the same nil check INSIDE the lock, which is
// the shape both gates now share (see also connectionSFXID).
//
// Benign on amd64 and arm64, where a pointer load cannot tear -- but CI runs
// -race, and a race detector finding is a build failure whatever the memory
// model does in practice.
func TestNotificationSFXIDIsRaceFreeAgainstASettingsWrite(t *testing.T) {
	a := appWithSoundSetting(t, true)
	sb := a.settings

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			// Exactly what SetSettings does: build a new Config and repoint
			// sb.cfg under sb.mu.
			next := config.Config{}
			next.General.PlayNotificationSounds = i%2 == 0
			sb.mu.Lock()
			sb.cfg = &next
			sb.mu.Unlock()
		}
	}()
	for i := 0; i < 2000; i++ {
		a.notificationSFXID("error")
	}
	<-done
}

// TestConnectionSFXIDIsRaceFreeAgainstASettingsWrite is connectionSFXID's
// copy of the test above. The two gates are the same shape by design and
// carried the same out-of-lock nil check; leaving one fixed and its twin
// racy is worse than the one-line diff.
func TestConnectionSFXIDIsRaceFreeAgainstASettingsWrite(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	a.settings = &settingsBackend{cfg: &config.Config{}}
	sb := a.settings

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			next := config.Config{}
			next.General.PlayConnectionSounds = i%2 == 0
			sb.mu.Lock()
			sb.cfg = &next
			sb.mu.Unlock()
		}
	}()
	for i := 0; i < 2000; i++ {
		a.connectionSFXID("connected")
	}
	<-done
}
