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
//
// The nil check on sb.cfg is INSIDE sb.mu, which is not a style choice: cfg
// is a POINTER that SetSettings and the keybind mutators repoint under
// sb.mu, and this gate runs on whichever goroutine raised the notification
// (the audio poll, the joystick poll, the hotkey callback). Reading it
// outside the lock is a genuine data race -- -race reported it -- and the
// nil case is the one place it is tempting to skip the lock for. Its
// sibling configuredAudioDevices (notify_audio.go) has the correct shape;
// so does connectionSFXID, fixed alongside this.
func (a *App) notificationSFXID(severity string) string {
	sb := a.settings
	if sb == nil {
		return ""
	}
	sb.mu.Lock()
	if sb.cfg == nil {
		sb.mu.Unlock()
		return ""
	}
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

// playNotificationSFX plays the sound marking a notification, if its
// severity earns one.
//
// It ships SILENT: notify_alert.wav does not exist, bringing the outstanding
// sample pack to ten files. The engine is asset-agnostic by design -- a
// missing sample plays silence and is logged once, never a crash -- so this
// wiring is correct and testable today and goes audible the moment the
// sample lands.
//
// UNEXPORTED, and reached from main.go's notify.Options.OnSound hook through
// the package-level PlayNotificationSFX below. Every exported METHOD on App
// is bound into the webview by application.NewService (main.go), and a
// renderer able to fire the alert sound on demand is not something this
// wiring needs -- the same rule setCaptureTimeout and setNotifyWindows
// follow (ruling R13). OnSound cannot route straight to the audio Manager
// either: the Manager does not exist yet when the notifier is constructed,
// and the gate has to consult the settings backend anyway.
func (a *App) playNotificationSFX(severity string) {
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

// PlayNotificationSFX plays the sound marking a notification. main.go hands
// it to notify.Options.OnSound.
//
// Package-level rather than a method on *App for the reason NotifyAudioState
// documents: application.NewService(gui) binds exported METHODS into the
// webview, and package-level functions are invisible to it.
func PlayNotificationSFX(a *App, severity string) { a.playNotificationSFX(severity) }
