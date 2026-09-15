package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
)

// defaultCaptureTimeout is how long BeginCapture waits before auto-resuming
// OS hotkey registration if EndCapture never arrives (frontend crash, lost
// IPC, etc). A spurious re-arm is harmless; a stuck-suspended state is
// invisible to the user and maddening to diagnose.
const defaultCaptureTimeout = 10 * time.Second

// settingsBackend groups the dependencies behind the settings/keybind
// bindings. It is created by SetSettingsBackend once the real config,
// keybind store and hotkey manager exist.
type settingsBackend struct {
	mu sync.Mutex

	cfg     *config.Config
	cfgPath string
	kb      *keybinds.Store
	hk      *hotkeys.Manager
	em      *events.Tagged

	captureTimeout time.Duration
	captureTimer   *time.Timer
}

// GetSettings returns the current General settings.
func (a *App) GetSettings() SettingsDTO {
	sb := a.settings
	sb.mu.Lock()
	defer sb.mu.Unlock()
	g := sb.cfg.General
	return SettingsDTO{
		StartMinimized:       g.StartMinimized,
		MinimizeToTray:       g.MinimizeToTray,
		ShowTransmitterName:  g.ShowTransmitterName,
		PlayConnectionSounds: g.PlayConnectionSounds,
		RadioSwitchAsPTT:     g.RadioSwitchAsPTT,
	}
}

// SetSettings replaces the General settings, persists them, and emits
// settings:changed so every window re-renders from the new state. Persist
// happens on a COPY of the config; sb.cfg is only repointed at it once the
// save succeeds, so a failed write never leaves the in-memory settings
// disagreeing with disk.
func (a *App) SetSettings(s SettingsDTO) error {
	sb := a.settings
	sb.mu.Lock()
	// Shallow copy: Keybinds is a map and would be shared with the original,
	// but this path never touches Keybinds, so that sharing is harmless.
	next := *sb.cfg
	next.General = config.General{
		StartMinimized:       s.StartMinimized,
		MinimizeToTray:       s.MinimizeToTray,
		ShowTransmitterName:  s.ShowTransmitterName,
		PlayConnectionSounds: s.PlayConnectionSounds,
		RadioSwitchAsPTT:     s.RadioSwitchAsPTT,
	}
	var saveErr error
	if sb.cfgPath != "" {
		saveErr = config.Save(sb.cfgPath, &next)
	}
	if saveErr == nil {
		sb.cfg = &next
	}
	sb.mu.Unlock()
	if saveErr != nil {
		return fmt.Errorf("save settings: %w", saveErr)
	}
	sb.em.SettingsChanged(s)
	return nil
}

// GetKeybinds joins the static + per-radio action registry with the store's
// live chords into the frontend-facing row list.
func (a *App) GetKeybinds() []KeybindDTO {
	sb := a.settings
	actions := a.keybindActions()
	out := make([]KeybindDTO, 0, len(actions))
	for _, act := range actions {
		c, _ := sb.kb.Get(act.ID)
		out = append(out, KeybindDTO{
			ActionID: string(act.ID),
			Label:    act.Label,
			Desc:     act.Desc,
			Category: categoryString(act.Category),
			Kind:     kindString(act.Kind),
			Chord:    c.String(),
		})
	}
	return out
}

// SetKeybind validates the raw capture, binds it, persists, re-applies OS
// hotkeys, and emits keybinds:changed. It reports which other action (if
// any) lost the chord. If persistence fails, the store is rolled back to its
// pre-Set contents (undoing both the new binding and any steal) so the live
// store never disagrees with disk.
func (a *App) SetKeybind(actionID string, cap CaptureDTO) (SetKeybindResult, error) {
	c, err := chord.FromCode(cap.Code, cap.Ctrl, cap.Alt, cap.Shift, cap.Super)
	if err != nil {
		return SetKeybindResult{}, fmt.Errorf("parse capture: %w", err)
	}

	sb := a.settings
	before := sb.kb.Snapshot()
	stolen := sb.kb.Set(keybinds.ActionID(actionID), c)
	if err := a.persistKeybinds(); err != nil {
		sb.kb.Load(before) // undo the Set (and any steal) so the store matches disk
		return SetKeybindResult{}, err
	}
	a.applyHotkeys()

	result := SetKeybindResult{}
	if stolen != nil {
		result.Stolen = &StolenDTO{
			ActionID: string(stolen.ActionID),
			Label:    a.labelFor(stolen.ActionID),
			Chord:    stolen.Chord.String(),
		}
	}
	sb.em.KeybindsChanged(a.GetKeybinds())
	return result, nil
}

// ClearKeybind removes any binding for actionID, persists, re-applies OS
// hotkeys, and emits keybinds:changed. If persistence fails, the store is
// rolled back to its pre-Clear contents.
func (a *App) ClearKeybind(actionID string) error {
	sb := a.settings
	before := sb.kb.Snapshot()
	sb.kb.Clear(keybinds.ActionID(actionID))
	if err := a.persistKeybinds(); err != nil {
		sb.kb.Load(before) // undo the Clear so the store matches disk
		return err
	}
	a.applyHotkeys()
	sb.em.KeybindsChanged(a.GetKeybinds())
	return nil
}

// BeginCapture suspends every OS hotkey registration so a registered hotkey
// does not swallow the very key the UI is about to listen for. It arms a
// timeout that auto-resumes if EndCapture never arrives. Safe to call
// repeatedly: each call re-arms the timer.
func (a *App) BeginCapture() error {
	sb := a.settings
	sb.mu.Lock()
	sb.hk.Suspend()
	if sb.captureTimer != nil {
		sb.captureTimer.Stop()
	}
	timeout := sb.captureTimeout
	if timeout <= 0 {
		timeout = defaultCaptureTimeout
	}
	hk := sb.hk
	sb.captureTimer = time.AfterFunc(timeout, func() {
		_ = hk.Resume()
	})
	sb.mu.Unlock()
	return nil
}

// EndCapture stops the pending auto-resume timer and re-arms OS hotkey
// registration. Safe to call repeatedly.
func (a *App) EndCapture() error {
	sb := a.settings
	sb.mu.Lock()
	if sb.captureTimer != nil {
		sb.captureTimer.Stop()
		sb.captureTimer = nil
	}
	sb.mu.Unlock()
	return sb.hk.Resume()
}

// SetCaptureTimeout overrides the auto-resume timeout. For tests only.
func (a *App) SetCaptureTimeout(d time.Duration) {
	sb := a.settings
	sb.mu.Lock()
	sb.captureTimeout = d
	sb.mu.Unlock()
}

// hotkeysResumed is an unexported test helper.
func (a *App) hotkeysResumed() bool {
	return a.settings.hk.Registered()
}

// GetHotkeyState reports whether OS hotkey registration is currently
// healthy, and why not if it is not.
func (a *App) GetHotkeyState() HotkeyStateDTO {
	sb := a.settings
	return HotkeyStateDTO{
		Registered: sb.hk.Registered(),
		Error:      errString(sb.hk.LastError()),
		Failed:     sb.hk.Failed(),
	}
}

// Pressed implements hotkeys.Handler.
func (a *App) Pressed(actionID string) {
	if a.settings == nil {
		return
	}
	a.settings.em.HotkeyPressed(actionID)
}

// Released implements hotkeys.Handler.
func (a *App) Released(actionID string) {
	if a.settings == nil {
		return
	}
	a.settings.em.HotkeyReleased(actionID)
}

// keybindActions returns the full bindable action registry: static actions
// plus per-radio actions derived from the LOCAL client's own radios. When not
// connected (no self, no radios) only the static actions are returned.
func (a *App) keybindActions() []keybinds.Action {
	actions := keybinds.StaticActions()
	return append(actions, keybinds.PerRadioActions(a.radioRefs())...)
}

// radioRefs reads the local client's own radios from state for
// keybinds.PerRadioActions. Returns nil when not connected.
func (a *App) radioRefs() []keybinds.RadioRef {
	snap := a.st.Snapshot()
	radios, ok := snap.Radios[snap.SelfGUID]
	if !ok || radios == nil {
		return nil
	}
	out := make([]keybinds.RadioRef, 0, len(radios.GetRadios()))
	for _, r := range radios.GetRadios() {
		out = append(out, keybinds.RadioRef{ID: r.GetId(), Name: r.GetName()})
	}
	return out
}

// labelFor resolves an action's display label from the registry.
func (a *App) labelFor(id keybinds.ActionID) string {
	for _, act := range a.keybindActions() {
		if act.ID == id {
			return act.Label
		}
	}
	return string(id)
}

// persistKeybinds snapshots the store into cfg.Keybinds and saves, skipped
// when cfgPath == "" (as in tests). As with SetSettings, the snapshot is
// written into a COPY of the config; sb.cfg only picks up the new Keybinds
// map once the save succeeds, so a failed write can never leave cfg.Keybinds
// out of sync with what's actually on disk (or with the store, which the
// caller rolls back on error).
func (a *App) persistKeybinds() error {
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Keybinds = sb.kb.Snapshot()
	var saveErr error
	if sb.cfgPath != "" {
		saveErr = config.Save(sb.cfgPath, &next)
	}
	if saveErr == nil {
		sb.cfg = &next
	}
	sb.mu.Unlock()
	if saveErr != nil {
		return fmt.Errorf("save keybinds: %w", saveErr)
	}
	return nil
}

// applyHotkeys re-applies the full desired binding set to the OS layer from
// the current keybind store contents.
func (a *App) applyHotkeys() {
	sb := a.settings
	binds := map[string]hotkeys.Binding{}
	for _, act := range a.keybindActions() {
		c, ok := sb.kb.Get(act.ID)
		if !ok || c.IsZero() {
			continue
		}
		binds[string(act.ID)] = hotkeys.Binding{Chord: c, Hold: act.Kind == keybinds.KindHold}
	}
	_ = sb.hk.Apply(binds) // failures surface via GetHotkeyState
}

// categoryString renders a Category as its lowercase wire form.
func categoryString(c keybinds.Category) string {
	switch c {
	case keybinds.CatGlobal:
		return "global"
	case keybinds.CatChannel:
		return "channel"
	case keybinds.CatPerRadio:
		return "per_radio"
	case keybinds.CatStatus:
		return "status"
	default:
		return "global"
	}
}

// kindString renders a Kind as its lowercase wire form.
func kindString(k keybinds.Kind) string {
	if k == keybinds.KindHold {
		return "hold"
	}
	return "press"
}

// errString renders an error as a string, "" when nil.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
