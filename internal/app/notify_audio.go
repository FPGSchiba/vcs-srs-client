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
// audio is safe to route here at all. AudioStateDTO has ten fields;
// exactly four are faults:
//
//	InputError, OutputError                 -- the fault itself
//	InputSubstituted, OutputSubstituted     -- works, but not on the chosen device
//
// The other six are deliberately dropped:
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
//	                        substitution flags, and putting the RESOLVED id in
//	                        the fingerprint would re-notify on every hot-plug
//	                        reshuffle.
//
// That last one is not hypothetical. The substitution items used to carry
// "IN USE: <resolved id>" in Context, which notify.fingerprint hashes -- so a
// dismissed "the configured mic is missing" warning came straight back the
// moment the OS default changed underneath it, with nothing about the fault
// having changed. The substitution pair now names the CONFIGURED device
// instead, read from the persisted settings because AudioStateDTO carries
// only the ids actually IN USE (see dto.go). A configured id is stable --
// it changes only when the user changes their own settings -- and it is the
// more useful fact: the user wants to know which of THEIR choices failed,
// not which stranger the OS substituted in this second. See spec 5.4 and
// ruling R20.
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

	configuredIn, configuredOut := a.configuredAudioDevices()

	raiseOrResolve(keyAudioInputSubstituted, dto.InputSubstituted, notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityWarn,
		Icon:     "mic",
		Title:    "Using the system default microphone",
		Body:     substitutedBody("microphone", configuredIn),
		Context:  []notify.KV{{Key: "CONFIGURED", Value: configuredIn}},
		Actions:  []notify.Action{audioSettingsAction()},
	})

	raiseOrResolve(keyAudioOutputSubstituted, dto.OutputSubstituted, notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityWarn,
		Icon:     "volume",
		Title:    "Using the system default audio output",
		Body:     substitutedBody("output device", configuredOut),
		Context:  []notify.KV{{Key: "CONFIGURED", Value: configuredOut}},
		Actions:  []notify.Action{audioSettingsAction()},
	})
}

// configuredAudioDevices returns the input/output device ids the user's
// SETTINGS ask for -- deliberately not the ids in use, which is all
// AudioStateDTO carries.
//
// This is the substitution notifications' identity (see NotifyAudioState):
// a configured id moves only when the user moves it, so the fingerprint
// changes only when the fault genuinely becomes a different fault.
//
// Empty ids are a supported answer, not a failure. A nil settings backend is
// the same optional-dependency discipline every other App use site follows,
// and an empty configured id is internal/audio's first-class "follow the
// system default", which can never BE substituted -- manager.go computes
// substitution as "configured id non-empty AND different from the resolved
// one". So whenever either substitution flag is set in production, this
// returns a real id.
//
// sb.mu is released before returning and nothing here calls into the audio
// Manager, which is audioManager's rule (never hold sb.mu across a Manager
// call) read from the other side: this runs on the Manager's OnState
// callback, which the Manager invokes with its own lock released.
func (a *App) configuredAudioDevices() (input, output string) {
	sb := a.settings
	if sb == nil {
		return "", ""
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.cfg == nil {
		return "", ""
	}
	return sb.cfg.Audio.InputDevice, sb.cfg.Audio.OutputDevice
}

// substitutedBody writes the substitution body, naming the configured device
// that could not be opened -- spec 5.4's own wording, which the generic prose
// this replaces did not honour. The unnamed form covers the no-settings-
// backend case configuredAudioDevices documents; production always has an id.
func substitutedBody(what, configured string) string {
	if configured == "" {
		return "The " + what + " selected in Settings could not be opened, so the system default is in use."
	}
	return "The " + what + " selected in Settings (" + configured + ") could not be opened, so the system default is in use."
}

func audioSettingsAction() notify.Action {
	return notify.Action{
		Label:  "OPEN AUDIO SETTINGS",
		Icon:   "settings",
		Kind:   "navigate",
		Target: "settings",
	}
}
