package app

import "github.com/FPGSchiba/vcs-srs-client/internal/notify"

// Audio notification keys. Four independent keys, not one, so a failed
// input and a substituted output are separate items that resolve
// independently.
const (
	keyAudioInput             = "audio.input"
	keyAudioOutput            = "audio.output"
	keyAudioInputSubstituted  = "audio.input.substituted"
	keyAudioOutputSubstituted = "audio.output.substituted"
)

// NotifyAudioState translates one AudioStateDTO into notifications.
//
// This function is a PROJECTION, and the projection is the whole reason
// audio is safe to route here at all. AudioStateDTO has nine fields;
// exactly four are faults:
//
//	InputError, OutputError                 -- the fault itself
//	InputSubstituted, OutputSubstituted     -- works, but not on the chosen device
//
// The other five are deliberately dropped:
//
//	Overruns, Underruns  -- MONOTONIC COUNTERS. audio.Manager's
//	                        emitStateIfChanged compares the whole State
//	                        struct and runs at the end of every poll tick
//	                        (PollInterval defaults to 2s), so a glitching
//	                        engine re-emits roughly 1800 times an hour with
//	                        nothing the user can see having changed. Folding
//	                        these into the notification would produce 1800
//	                        notifications; dropping them produces one.
//	Running, Starting    -- lifecycle, not a fault.
//	InputDevice,
//	OutputDevice         -- which device is in use is already carried by the
//	                        substitution flags, and putting the id in the
//	                        fingerprint would re-notify on every hot-plug
//	                        reshuffle.
//
// Exported because main.go calls it from BOTH audio emit sites: the
// Manager's OnState callback, and the hand-pushed DTO for the case where
// NewMalgoBackend failed and no Manager was ever constructed. An adapter
// hung only off OnState would miss the most severe audio failure there is.
func (a *App) NotifyAudioState(dto AudioStateDTO) {
	n := a.notif
	if n == nil {
		return
	}

	window := a.audioWindow()
	raiseOrResolve := func(key string, cond bool, item notify.Item) {
		if cond {
			n.RaiseWindowed(key, item, window)
			return
		}
		n.ResolveWindowed(key, window)
	}

	raiseOrResolve(keyAudioInput, dto.InputError != "", notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityError,
		Icon:     "mic",
		Title:    "Microphone unavailable",
		Body:     dto.InputError,
		Actions:  []notify.Action{audioSettingsAction()},
	})

	raiseOrResolve(keyAudioOutput, dto.OutputError != "", notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityError,
		Icon:     "volume",
		Title:    "Audio output unavailable",
		Body:     dto.OutputError,
		Actions:  []notify.Action{audioSettingsAction()},
	})

	raiseOrResolve(keyAudioInputSubstituted, dto.InputSubstituted, notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityWarn,
		Icon:     "mic",
		Title:    "Using the system default microphone",
		Body:     "The microphone selected in Settings could not be opened, so the system default is in use.",
		Context:  []notify.KV{{Key: "IN USE", Value: dto.InputDevice}},
		Actions:  []notify.Action{audioSettingsAction()},
	})

	raiseOrResolve(keyAudioOutputSubstituted, dto.OutputSubstituted, notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityWarn,
		Icon:     "volume",
		Title:    "Using the system default audio output",
		Body:     "The output device selected in Settings could not be opened, so the system default is in use.",
		Context:  []notify.KV{{Key: "IN USE", Value: dto.OutputDevice}},
		Actions:  []notify.Action{audioSettingsAction()},
	})
}

func audioSettingsAction() notify.Action {
	return notify.Action{
		Label:  "OPEN AUDIO SETTINGS",
		Icon:   "settings",
		Kind:   "navigate",
		Target: "settings",
	}
}
