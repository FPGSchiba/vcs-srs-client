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
	a.PlayNotificationSFX("error")
}
