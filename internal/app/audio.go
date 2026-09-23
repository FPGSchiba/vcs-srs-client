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

// GetAudioDevices reports the most recently enumerated input/output
// devices. Empty (never nil) lists when no backend is wired.
func (a *App) GetAudioDevices() AudioDevicesDTO {
	m := a.audioManager()
	if m == nil {
		return AudioDevicesDTO{Inputs: []AudioDeviceDTO{}, Outputs: []AudioDeviceDTO{}}
	}
	inputs, outputs := m.Devices()
	return AudioDevicesDTO{Inputs: audioDeviceDTOs(inputs), Outputs: audioDeviceDTOs(outputs)}
}

func audioDeviceDTOs(devs []audio.DeviceInfo) []AudioDeviceDTO {
	out := make([]AudioDeviceDTO, 0, len(devs))
	for _, d := range devs {
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
	return AudioStateDTO{
		Running:     st.Running,
		InputError:  st.InputError,
		OutputError: st.OutputError,
		Overruns:    st.Overruns,
		Underruns:   st.Underruns,
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
// Every case is a no-op when no audio backend is wired (m == nil), so every
// pre-existing test that builds an App without SetAudioBackend keeps
// passing unchanged.
func (a *App) dispatchAudioPressed(actionID string) {
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
		m.SetPTT(false)
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
// binding-facing DTO. See AudioEffectDTO's doc for why Label/Available are
// placeholders today.
func audioSettingsDTO(ac config.Audio) AudioSettingsDTO {
	effects := make(map[string]AudioEffectDTO, len(ac.Effects))
	for id, e := range ac.Effects {
		effects[id] = AudioEffectDTO{
			Enabled:   e.Enabled,
			File:      e.File,
			Label:     id,
			Available: false,
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
		Effects: effects,
	}
}

// configAudioFromDTO is audioSettingsDTO's inverse, used by SetSettings.
func configAudioFromDTO(dto AudioSettingsDTO) config.Audio {
	var effects map[string]config.AudioEffect
	if len(dto.Effects) > 0 {
		effects = make(map[string]config.AudioEffect, len(dto.Effects))
		for id, e := range dto.Effects {
			effects[id] = config.AudioEffect{Enabled: e.Enabled, File: e.File}
		}
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
