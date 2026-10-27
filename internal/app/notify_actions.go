package app

import "github.com/FPGSchiba/vcs-srs-client/internal/notify"

// actionFailure describes the error notification one user-initiated binding
// raises when it fails (Phase 6.9 CL3, FE09).
//
// The key is STABLE and the fields are only what the user can see, per
// CLAUDE.md's notification-channel rules: notify's fingerprint then
// collapses a repeated identical failure -- a knob drag against a read-only
// config directory, for example -- into one item instead of a toast per
// throttled commit.
type actionFailure struct{ key, category, icon, title string }

var (
	actSettingsSave = actionFailure{"action:settings.save", "system", "settings", "Could not save settings"}
	actRadioSelect  = actionFailure{"action:radio.select", "comms", "radio", "Radio selected, but the selection was not saved"}
	actRadioSave    = actionFailure{"action:radio.save", "comms", "radio", "Could not save the radio change"}
	actRadioSend    = actionFailure{"action:radio.send", "comms", "radio", "Radio change saved, but not sent to the server"}
	actLayoutReset  = actionFailure{"action:layout.reset", "profile", "layout", "Could not reset the layout"}
	actAudioMicTest = actionFailure{"action:audio.mictest", "system", "mic", "Could not start the mic test"}
	actAudioPreview = actionFailure{"action:audio.preview", "system", "volume", "Could not preview the effect"}
)

// reportAction raises f on its stable key when err is non-nil and RESOLVES
// the key when err is nil (design D6, the same rule notifyHistoryFlush
// follows). Without the resolve, the same failure recurring after an
// intervening success would match the still-live item's fingerprint and be
// dropped silently -- no toast, no sound.
//
// A no-op when no notifier is wired. Callers invoke it from a deferred
// closure registered BEFORE any settings lock, so it always runs after the
// lock is released (design D7): a notifier OnChange that calls back into the
// App must not deadlock against writeMu.
func (a *App) reportAction(f actionFailure, err error) {
	if a.notif == nil {
		return
	}
	if err == nil {
		a.notif.Resolve(f.key)
		return
	}
	a.notif.Raise(f.key, notify.Item{
		Category: f.category,
		Severity: notify.SeverityError,
		Icon:     f.icon,
		Title:    f.title,
		Body:     err.Error(),
	})
}
