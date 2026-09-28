package app

import (
	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
)

// notificationSFXID maps a notification's severity to the sound slot that
// marks it, or "" for one that gets no sound.
//
// SOUND FOLLOWS THE TOAST: error severity only. A sound with no visible
// cause is the worst outcome available here, so rather than a per-severity
// palette, exactly what toasts also sounds. Warn reaches the badge and bell
// silently; info is raised already-read by the store and reaches nothing.
//
// Gated on general.play_notification_sounds. Mirrors connectionSFXID, which
// does the same job for control-link transitions.
func (a *App) notificationSFXID(severity string) string {
	sb := a.settings
	if sb == nil || sb.cfg == nil {
		return ""
	}
	sb.mu.Lock()
	enabled := sb.cfg.General.PlayNotificationSounds
	sb.mu.Unlock()
	if !enabled {
		return ""
	}
	if severity != string(notify.SeverityError) {
		return ""
	}
	return audio.NotifyAlert
}

// PlayNotificationSFX plays the sound marking a notification, if its
// severity earns one.
//
// It ships SILENT: notify_alert.wav does not exist, bringing the outstanding
// sample pack to ten files. The engine is asset-agnostic by design -- a
// missing sample plays silence and is logged once, never a crash -- so this
// wiring is correct and testable today and goes audible the moment the
// sample lands.
//
// Exported for main.go's notify.Options.OnSound hook. It cannot route
// straight to the audio Manager there: the Manager does not exist yet when
// the notifier is constructed, and the gate has to consult the settings
// backend anyway.
func (a *App) PlayNotificationSFX(severity string) {
	id := a.notificationSFXID(severity)
	if id == "" {
		return
	}
	m := a.audioManager()
	if m == nil {
		return
	}
	m.PlayNotification(id)
}
