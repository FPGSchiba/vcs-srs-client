// Package app: audio bindings and the PTT/mute action wiring.
//
// Audio settings deliberately extend the EXISTING SettingsDTO / GetSettings /
// SetSettings path rather than getting a parallel GetAudioSettings /
// SetAudioSettings pair (see SettingsScreen.tsx's doc comment for why a
// second settings path is exactly the bug that once kept keybind and
// settings changes from reaching the Comms popout). audio.go's own
// bindings -- SetAudioBackend, GetAudioDevices, GetAudioState, StartMicTest/
// StopMicTest, PreviewEffect -- are the parts that genuinely have no home in
// settings.go: device enumeration, health, and one-shot actions, none of
// which are persisted settings.
package app

import (
	"errors"
	"sort"
	"strings"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
)

// ErrAudioUnavailable is returned by the audio bindings below when no
// backend has been wired -- main.go's malgo construction can fail (Task 14)
// and every test that builds an App never calls SetAudioBackend at all.
// Callers get an honest error rather than a silent success that changed
// nothing.
var ErrAudioUnavailable = errors.New("audio: no backend wired")

// SetAudioBackend wires the audio manager, mirroring SetJoystickBackend's
// optional-dependency pattern: every audio binding below, and the
// global.ptt / push_to_mute / mute_toggle action wiring in Pressed/Released,
// degrades to a documented no-op (or ErrAudioUnavailable) until this is
// called. Also pushes the currently persisted audio settings into the
// manager immediately, so a manager constructed with NewManager's built-in
// defaults picks up whatever the user already configured before this app
// session started, rather than waiting for the next SetSettings call.
func (a *App) SetAudioBackend(m *audio.Manager) {
	sb := a.settings
	sb.mu.Lock()
	sb.audio = m
	ac := sb.cfg.Audio
	sb.mu.Unlock()
	if m != nil {
		m.SetConfig(audioManagerConfig(ac))
	}
}

// SystemDefaultDeviceName is the display name of the empty-id "follow the
// system default" entry -- see AudioDeviceDTOs.
const SystemDefaultDeviceName = "System Default"

// GetAudioDevices reports the most recently enumerated input/output
// devices, each list led by the System Default sentinel. Empty (never nil)
// lists when no backend is wired -- the honest "there is no audio at all"
// answer, which is not the same thing as "you may follow the OS default".
func (a *App) GetAudioDevices() AudioDevicesDTO {
	m := a.audioManager()
	if m == nil {
		return AudioDevicesDTO{Inputs: []AudioDeviceDTO{}, Outputs: []AudioDeviceDTO{}}
	}
	inputs, outputs := m.Devices()
	return AudioDevicesDTO{Inputs: AudioDeviceDTOs(inputs), Outputs: AudioDeviceDTOs(outputs)}
}

// AudioDeviceDTOs converts an engine device list into the wire-facing shape,
// PREPENDING the {ID: "", Name: "System Default"} entry.
//
// That entry is synthesised here, in the one converter every consumer goes
// through (GetAudioDevices and main.go's audio:devices_changed emission),
// rather than in any single screen. Nothing used to synthesise it ANYWHERE:
// internal/audio enumerates only real endpoints, so the empty id that
// backend.go documents as a first-class value, config.Default() persists,
// and resolveDevice honours was simply unreachable from the UI. Worse, the
// persisted default of "" then matched no rendered <option>, so both device
// selects mounted on a value no option carried and a user who once picked a
// real device could never get back to following the OS.
//
// IsDefault stays false on the sentinel on purpose: it means "this endpoint
// is currently the OS default", which is a property of a real device in the
// list below it. The sentinel's meaning is "whichever that turns out to be,
// now and later".
func AudioDeviceDTOs(devs []audio.DeviceInfo) []AudioDeviceDTO {
	out := make([]AudioDeviceDTO, 0, len(devs)+1)
	out = append(out, AudioDeviceDTO{ID: "", Name: SystemDefaultDeviceName})
	for _, d := range devs {
		if d.ID == "" {
			// A backend that somehow enumerated an empty id would
			// otherwise produce a duplicate sentinel.
			continue
		}
		out = append(out, AudioDeviceDTO{ID: d.ID, Name: d.Name, IsDefault: d.IsDefault})
	}
	return out
}

// GetAudioState reports the audio subsystem's health, mirroring
// GetHotkeyState / GetJoystickState. Zero value when no backend is wired --
// the honest "nothing is running" answer, not an optimistic default.
func (a *App) GetAudioState() AudioStateDTO {
	m := a.audioManager()
	if m == nil {
		return AudioStateDTO{}
	}
	st := m.State()
	return AudioStateDTOFrom(st)
}

// AudioStateDTOFrom is the one audio.State -> AudioStateDTO conversion,
// shared by GetAudioState and main.go's OnState emitter so the two can never
// drift into reporting different subsets of the same snapshot -- which is
// exactly how the substitution and device fields came to be dropped on the
// event path while State carried them.
func AudioStateDTOFrom(st audio.State) AudioStateDTO {
	return AudioStateDTO{
		Running:           st.Running,
		Starting:          st.Starting,
		InputError:        st.InputError,
		OutputError:       st.OutputError,
		Overruns:          st.Overruns,
		Underruns:         st.Underruns,
		InputDevice:       st.InputDevice,
		OutputDevice:      st.OutputDevice,
		InputSubstituted:  st.InputSubstituted,
		OutputSubstituted: st.OutputSubstituted,
	}
}

// StartMicTest turns on mic passthrough so the user can hear their own
// input, without touching the persisted MicPassthrough setting.
func (a *App) StartMicTest() error {
	sb := a.settings
	sb.mu.Lock()
	m := sb.audio
	ac := sb.cfg.Audio
	sb.mu.Unlock()
	if m == nil {
		return ErrAudioUnavailable
	}
	cfg := audioManagerConfig(ac)
	cfg.MicPassthrough = true
	m.SetConfig(cfg)
	return nil
}

// StopMicTest reverts mic passthrough to whatever the persisted setting
// says, undoing StartMicTest's override.
func (a *App) StopMicTest() error {
	sb := a.settings
	sb.mu.Lock()
	m := sb.audio
	ac := sb.cfg.Audio
	sb.mu.Unlock()
	if m == nil {
		return ErrAudioUnavailable
	}
	m.SetConfig(audioManagerConfig(ac))
	return nil
}

// PreviewEffect plays one Radio Effects slot once, for the settings screen's
// preview buttons.
func (a *App) PreviewEffect(id string) error {
	m := a.audioManager()
	if m == nil {
		return ErrAudioUnavailable
	}
	m.PlayEffect(id)
	return nil
}

// GetAudioEffectPresets returns the built-in DSP preset lists (voice
// bandpass + clipping), for the Radio Effects screen's two preset
// dropdowns. Static data straight off internal/audio's own tables --
// available even when no audio backend is wired, unlike GetAudioDevices/
// GetAudioState.
func (a *App) GetAudioEffectPresets() AudioEffectPresetsDTO {
	return AudioEffectPresetsDTO{
		Voice:    audioEffectPresetDTOs(audio.VoicePresetOptions()),
		Clipping: audioEffectPresetDTOs(audio.ClippingPresetOptions()),
	}
}

func audioEffectPresetDTOs(presets []audio.EffectPreset) []AudioEffectPresetDTO {
	out := make([]AudioEffectPresetDTO, 0, len(presets))
	for _, p := range presets {
		out = append(out, AudioEffectPresetDTO{Value: p.ID, Label: p.Label})
	}
	return out
}

// audioManager reads the wired manager (nil if none), under sb.mu, mirroring
// how GetJoystickState reads sb.joy. Never call a Manager method while
// holding sb.mu -- Manager has its own independent locking.
func (a *App) audioManager() *audio.Manager {
	sb := a.settings
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.audio
}

// dispatchAudioPressed and dispatchAudioReleased are called from Pressed and
// Released (settings.go) -- the existing hotkey/joystick dispatch entry
// points, already deduplicating a hold action's press/release across
// keyboard and joystick sources via isHold/pressCount before either of
// these is ever reached. They add NO parallel dispatch path: they just give
// three already-existing action IDs their first real consumer.
//
// radio.<n>.select is handled BEFORE the m == nil guard below: selecting a
// radio only changes which radio global.ptt will target on its NEXT press
// (state.Store.SetSelectedRadio), which has nothing to do with whether an
// audio backend is wired. Gating it on m would silently strand selection at
// whatever it last was on any machine with no sound card -- the same class
// of "audio-only" bug the rest of this function's m == nil guard exists to
// AVOID for the PTT/mute actions, which genuinely do need the manager.
//
// Every other case is a no-op when no audio backend is wired (m == nil), so
// every pre-existing test that builds an App without SetAudioBackend keeps
// passing unchanged.
func (a *App) dispatchAudioPressed(actionID string) {
	if id, ok := radioSelectID(actionID); ok {
		a.st.SetSelectedRadio(id)
		// Best-effort persist (M4 fix), matching the SelectRadio binding,
		// queued OFF this goroutine (M6 fix) -- this runs on gohook's event-
		// reader goroutine, and a synchronous config.Save here is exactly
		// the kind of stall internal/hotkeys/dispatch.go's own doc warns
		// can strand a later key event (and therefore a held PTT) behind
		// it. See queueSelectedRadioPersist's doc for how ordering is still
		// preserved despite the async write.
		a.queueSelectedRadioPersist(id)
		return
	}

	sb := a.settings
	sb.mu.Lock()
	m := sb.audio
	em := sb.em
	sb.mu.Unlock()
	if m == nil {
		return
	}
	switch {
	case actionID == "global.ptt" || isRadioPTTAction(actionID):
		// txPress resolves actionID (global.ptt against the SELECTED radio,
		// radio.<n>.ptt against radio n), ADDS it to the refcounted
		// active-TX set, and installs the resulting targets on the live
		// session ATOMICALLY with that mutation -- see voice.go's txPress
		// doc both for why a press can never be the transition that fails
		// to open the gate, and for why the session call now lives inside
		// txPress rather than out here (the blocker fix: it can no longer
		// interleave with clearTXTargetsIfStillIdle's own store).
		a.txPress(actionID)
		m.SetPTT(true)
	case actionID == "global.push_to_mute":
		// Emit only on an actual transition -- see push_to_mute's Released
		// half below for why this is the mirror image of that guard.
		if !m.Muted() {
			m.SetMuted(true)
			em.AudioMicMuted(true)
		}
	case actionID == "global.mute_toggle":
		// KindPress: fires once on down, never gets a matching Released (see
		// isHold's doc), so this is the only edge this action ever sees.
		// ToggleMuted's CAS loop (see its doc in internal/audio/manager.go)
		// is what makes this safe when two independent sources -- e.g. a
		// keyboard chord and a joystick button, both valid bind targets for
		// the same action -- race each other here; SetMuted(!Muted()) would
		// let both read the same starting value and drop one toggle.
		em.AudioMicMuted(m.ToggleMuted())
	}
}

func (a *App) dispatchAudioReleased(actionID string) {
	sb := a.settings
	sb.mu.Lock()
	m := sb.audio
	em := sb.em
	sb.mu.Unlock()
	if m == nil {
		return
	}
	switch {
	case actionID == "global.ptt" || isRadioPTTAction(actionID):
		// txRelease removes actionID from the active-TX set, reports
		// whether ANY OTHER action is still held -- the exact Manager.SetPTT
		// gate condition, independent of whether either action ever
		// resolved to a real frequency (TestGateStaysOpenWhileAnyTargetIsHeld)
		// -- and, when held, installs the shrunk targets on the live
		// session ATOMICALLY with the removal (same reasoning as txPress;
		// see its doc). Only a fully-emptied set ends the transmission
		// outright; a set that merely shrank keeps transmitting on what
		// remains (TestReleasingOneKeepsTheOther).
		held := a.txRelease(actionID)
		if !held {
			// The generation no longer bumps here (M1 fix -- see the press
			// edge above); the gate stays open for ptt_release_delay_ms
			// after this and WriteFrame keeps accumulating on the CURRENT
			// targets for that whole tail, which a reset here would
			// truncate. scheduleTXTargetClear instead arms a clear for once
			// the tail has genuinely finished (I3 fix), so a later VOX
			// trigger cannot key a stale frequency with no PTT held.
			a.scheduleTXTargetClear()
		}
		m.SetPTT(held)
	case actionID == "global.push_to_mute":
		// Guarded the same way as the press half: emit the resulting state
		// only when it actually changes, rather than on every release edge
		// regardless of whether push_to_mute was even the reason the mic
		// was muted (mute_toggle could have muted it independently).
		if m.Muted() {
			m.SetMuted(false)
			em.AudioMicMuted(false)
		}
	}
}

// isRadioPTTAction reports whether actionID is a per-radio PTT action
// ("radio.<n>.ptt"), keybinds' own shape for CatPerRadio PTT actions.
func isRadioPTTAction(actionID string) bool {
	const prefix, suffix = "radio.", ".ptt"
	return len(actionID) > len(prefix)+len(suffix) &&
		strings.HasPrefix(actionID, prefix) &&
		strings.HasSuffix(actionID, suffix)
}

// audioSettingsDTO converts config's raw persisted Audio into the
// binding-facing DTO, merging in each effect slot's live Label/Available
// from m's SFX manifest (see AudioEffectDTO's doc). m may be nil (no audio
// backend wired) -- the row SET then comes from whatever ac.Effects
// already has (nothing, until Manager can enumerate the manifest), and
// every Label falls back to the id with Available false, matching the
// honest "nothing confirmed" answer used elsewhere in this package.
func audioSettingsDTO(ac config.Audio, m *audio.Manager) AudioSettingsDTO {
	var ids []string
	if m != nil {
		// The manifest is the canonical id SET and display order (see
		// SFX.EffectIDs' doc) -- every slot renders, customised or not,
		// which is exactly what the frontend's Radio Effects panel needs
		// and could not get from ac.Effects alone (nil until a user
		// touches a slot, see config.Audio.Effects' doc).
		ids = m.EffectIDs()
	} else {
		ids = make([]string, 0, len(ac.Effects))
		for id := range ac.Effects {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	effects := make(map[string]AudioEffectDTO, len(ids))
	for _, id := range ids {
		e := ac.Effects[id] // zero value (Enabled:false, File:"") if never customised
		label, available := id, false
		if m != nil {
			label = m.EffectLabel(id)
			available = m.EffectAvailable(id)
		}
		effects[id] = AudioEffectDTO{
			Enabled:   e.Enabled,
			File:      e.File,
			Label:     label,
			Available: available,
		}
	}
	return AudioSettingsDTO{
		InputDevice:       ac.InputDevice,
		OutputDevice:      ac.OutputDevice,
		InputDeviceName:   ac.InputDeviceName,
		OutputDeviceName:  ac.OutputDeviceName,
		MicPassthrough:    ac.MicPassthrough,
		AGC:               ac.AGC,
		NoiseSuppression:  ac.NoiseSuppression,
		VOX:               ac.VOX,
		VOXThreshold:      ac.VOXThreshold,
		VOXMinLengthMS:    ac.VOXMinLengthMS,
		VOXHangMS:         ac.VOXHangMS,
		VOXNoiseCancel:    ac.VOXNoiseCancel,
		PTTStartDelayMS:   ac.PTTStartDelayMS,
		PTTReleaseDelayMS: ac.PTTReleaseDelayMS,
		VoiceEffect:       ac.VoiceEffect,
		ClippingEffect:    ac.ClippingEffect,
		Levels: AudioLevelsDTO{
			Master:       ac.Levels.Master,
			Voice:        ac.Levels.Voice,
			SFX:          ac.Levels.SFX,
			Notification: ac.Levels.Notification,
		},
		Effects:     effects,
		EffectOrder: ids,
	}
}

// configAudioFromDTO is audioSettingsDTO's inverse, used by SetSettings.
//
// It only round-trips Enabled/File -- Label/Available have no field on
// config.AudioEffect (audioSettingsDTO reads them fresh off the Manager on
// every GetSettings, they are never persisted) -- and it drops any slot
// that is still at its untouched default (Enabled:false, File:""). Without
// that filter, audioSettingsDTO now populating every manifest slot on
// GetSettings (not just customised ones) would round-trip straight back
// through here on the very next SetSettings -- e.g. the user just toggles
// AGC, but the full settings struct, effects included, saves with it -- and
// turn config.Audio.Effects permanently non-nil for every user, defeating
// the whole reason it starts nil (see that field's doc).
func configAudioFromDTO(dto AudioSettingsDTO) config.Audio {
	var effects map[string]config.AudioEffect
	for id, e := range dto.Effects {
		if !e.Enabled && e.File == "" {
			continue
		}
		if effects == nil {
			effects = make(map[string]config.AudioEffect, len(dto.Effects))
		}
		effects[id] = config.AudioEffect{Enabled: e.Enabled, File: e.File}
	}
	return config.Audio{
		InputDevice:       dto.InputDevice,
		OutputDevice:      dto.OutputDevice,
		InputDeviceName:   dto.InputDeviceName,
		OutputDeviceName:  dto.OutputDeviceName,
		MicPassthrough:    dto.MicPassthrough,
		AGC:               dto.AGC,
		NoiseSuppression:  dto.NoiseSuppression,
		VOX:               dto.VOX,
		VOXThreshold:      dto.VOXThreshold,
		VOXMinLengthMS:    dto.VOXMinLengthMS,
		VOXHangMS:         dto.VOXHangMS,
		VOXNoiseCancel:    dto.VOXNoiseCancel,
		PTTStartDelayMS:   dto.PTTStartDelayMS,
		PTTReleaseDelayMS: dto.PTTReleaseDelayMS,
		VoiceEffect:       dto.VoiceEffect,
		ClippingEffect:    dto.ClippingEffect,
		Levels: config.AudioLevels{
			Master:       dto.Levels.Master,
			Voice:        dto.Levels.Voice,
			SFX:          dto.Levels.SFX,
			Notification: dto.Levels.Notification,
		},
		Effects: effects,
	}
}

// audioManagerConfig maps the persisted config shape into the engine's own
// Config. config.Audio is a raw-values layer (see its own doc: internal/
// config does not import internal/audio); this is the one place that gap
// gets bridged.
func audioManagerConfig(ac config.Audio) audio.Config {
	return audio.Config{
		InputDevice:      ac.InputDevice,
		OutputDevice:     ac.OutputDevice,
		MicPassthrough:   ac.MicPassthrough,
		AGC:              ac.AGC,
		NoiseSuppression: ac.NoiseSuppression,
		Gate: audio.GateConfig{
			VOXEnabled:        ac.VOX,
			VOXThreshold:      ac.VOXThreshold,
			VOXMinLengthMS:    ac.VOXMinLengthMS,
			VOXHangMS:         ac.VOXHangMS,
			PTTStartDelayMS:   ac.PTTStartDelayMS,
			PTTReleaseDelayMS: ac.PTTReleaseDelayMS,
		},
		Levels: audio.Levels{
			Master:       ac.Levels.Master,
			Voice:        ac.Levels.Voice,
			SFX:          ac.Levels.SFX,
			Notification: ac.Levels.Notification,
		},
		VoiceEffect:    ac.VoiceEffect,
		ClippingEffect: ac.ClippingEffect,
		VOXNoiseCancel: ac.VOXNoiseCancel,
	}
}
