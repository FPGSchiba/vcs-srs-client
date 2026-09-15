package app

import (
	"fmt"
	"strings"
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

	// writeMu serialises the snapshot -> mutate -> persist -> restore -> emit
	// sequence in SetSettings, SetKeybind and ClearKeybind. keybinds.Store and
	// hotkeys.Manager are each thread-safe per call, but the SEQUENCE is not
	// atomic: without this, a failing call could roll back to a snapshot taken
	// before a concurrent call's successful persist, erasing a committed
	// write (or, across SetSettings vs. the keybind mutators, one call's
	// stale copy of *cfg could overwrite the other's just-persisted field).
	// Deliberately a SEPARATE mutex from mu: mu guards this struct's own
	// fields and is taken inside helpers on this path (persistKeybinds,
	// SetSettings itself), so reusing it here would self-deadlock.
	writeMu sync.Mutex

	cfg     *config.Config
	cfgPath string
	kb      *keybinds.Store
	hk      *hotkeys.Manager
	em      *events.Tagged

	captureTimeout time.Duration
	captureTimer   *time.Timer

	// captureGen is the capture-token generation counter. BeginCapture
	// increments it and hands the new value to the frontend; EndCapture and
	// the auto-resume timer only re-arm OS hotkeys if the token they carry is
	// still the current generation. See BeginCapture for why the invariant
	// lives here rather than in the frontend.
	captureGen int64

	// radioSig is the signature of the per-radio action set the last
	// RefreshKeybinds saw, so a radio update that does not change the local
	// client's radios does not re-emit keybinds:changed.
	radioSig string
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
// disagreeing with disk. writeMu is held for the whole call (through the
// emit) so a concurrent SetKeybind/ClearKeybind cannot interleave with this
// copy-persist-swap and clobber it, or vice versa.
func (a *App) SetSettings(s SettingsDTO) error {
	sb := a.settings
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

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
// store never disagrees with disk. writeMu is held for the whole call
// (through the emit) so a concurrent mutator's snapshot can never be older
// than this call's own commit, which would otherwise let a failing rollback
// here erase a different call's successful, already-persisted write.
func (a *App) SetKeybind(actionID string, cap CaptureDTO) (SetKeybindResult, error) {
	c, err := chord.FromCode(cap.Code, cap.Ctrl, cap.Alt, cap.Shift, cap.Super)
	if err != nil {
		return SetKeybindResult{}, fmt.Errorf("parse capture: %w", err)
	}

	sb := a.settings
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

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
// rolled back to its pre-Clear contents. writeMu is held for the whole call
// for the same reason as SetKeybind: it keeps this call's rollback from
// racing a concurrent mutator's successful commit.
func (a *App) ClearKeybind(actionID string) error {
	sb := a.settings
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

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
// does not swallow the very key the UI is about to listen for, and returns
// the CAPTURE TOKEN identifying this capture. The caller must hand that token
// back to EndCapture.
//
// The token exists because the frontend cannot order two fire-and-forget IPC
// calls. Clicking chip B while chip A is listening dispatches B's
// BeginCapture first and A's EndCapture second (A only unmounts once React
// commits), so a token-less EndCapture would resume every OS hotkey in the
// middle of B's capture -- the OS would then swallow the very keypress meant
// to rebind B, which is exactly the hazard spec section 7 opens with. The
// same inversion happens when the user clicks a new chip while the previous
// row's SetKeybind is still in flight.
//
// Making the token the authority moves that invariant into the only layer
// that can enforce it: BeginCapture bumps the generation, and EndCapture and
// the auto-resume timer are no-ops unless the token they carry is still
// current. Every interleaving -- row switch, post-capture race, timeout -- is
// then safe without the frontend sequencing anything.
func (a *App) BeginCapture() int64 {
	sb := a.settings
	sb.mu.Lock()
	sb.captureGen++
	gen := sb.captureGen
	sb.hk.Suspend()
	if sb.captureTimer != nil {
		sb.captureTimer.Stop()
	}
	timeout := sb.captureTimeout
	if timeout <= 0 {
		timeout = defaultCaptureTimeout
	}
	// The timer captures its OWN generation, so a timeout belonging to a
	// superseded capture cannot re-arm hotkeys under a live one.
	sb.captureTimer = time.AfterFunc(timeout, func() { a.resumeCapture(gen) })
	sb.mu.Unlock()
	return gen
}

// EndCapture re-arms OS hotkey registration, but only if token is still the
// current capture generation. A stale token -- from a capture the user has
// already moved on from -- is a deliberate no-op, which is what keeps a row
// switch from resuming hotkeys during the capture that superseded it.
//
// Returns nothing: Resume's error is a PER-BINDING registration failure (one
// unregisterable chord among many working ones), which is reported through
// hotkeys:state / HotkeyStateDTO.Failed. Rejecting the frontend's
// endCapture() promise for it would repeat the same category error I4 fixed
// in the banner -- treating one bad binding as a failure of the whole
// operation.
func (a *App) EndCapture(token int64) {
	a.resumeCapture(token)
}

// resumeCapture is the single gate both EndCapture and the auto-resume timer
// go through. A stale token is a no-op.
func (a *App) resumeCapture(token int64) {
	sb := a.settings
	sb.mu.Lock()
	if token != sb.captureGen {
		sb.mu.Unlock()
		return // superseded by a newer capture -- leave hotkeys suspended
	}
	if sb.captureTimer != nil {
		sb.captureTimer.Stop()
		sb.captureTimer = nil
	}
	sb.mu.Unlock()

	_ = sb.hk.Resume() // per-binding failures surface via the event below
	// Resume is where bindings saved DURING the capture actually hit the OS
	// (Apply only records the desired set while suspended), so it is the
	// first moment a newly bound unregisterable key can be known to have
	// failed. Without this emit that failure would never reach the UI.
	a.emitHotkeyState()
}

// SetCaptureTimeout overrides the auto-resume timeout. For tests only.
func (a *App) SetCaptureTimeout(d time.Duration) {
	sb := a.settings
	sb.mu.Lock()
	sb.captureTimeout = d
	sb.mu.Unlock()
}

// hotkeysResumed is an unexported test helper. It asks whether the manager is
// out of its suspended state -- NOT Registered(), which by design stays true
// across a suspension (see hotkeys.Manager.Registered) and would make every
// resume assertion vacuous.
func (a *App) hotkeysResumed() bool {
	return !a.settings.hk.Suspended()
}

// GetHotkeyState reports whether OS hotkey registration is currently
// healthy, and why not if it is not.
func (a *App) GetHotkeyState() HotkeyStateDTO {
	return hotkeyStateDTO(a.settings.hk.State())
}

// hotkeyStateDTO renders a hotkeys.State for the binding surface. Error is
// populated ONLY when Registered is false, so the two fields cannot
// contradict each other: Registered==false plus Error is "global hotkeys are
// dead, here is why", while a partial failure leaves Registered true and
// says which bindings failed through Failed alone. The UI banner keys off
// Registered, so letting Error survive a partial failure would resurrect the
// bug where one bad binding blanked the banner for nineteen working ones.
func hotkeyStateDTO(s hotkeys.State) HotkeyStateDTO {
	msg := ""
	if !s.Registered {
		msg = errString(s.LastError)
	}
	return HotkeyStateDTO{Registered: s.Registered, Error: msg, Failed: s.Failed}
}

// emitHotkeyState publishes the current registration health on
// hotkeys:state. Called after every apply and after every resume, because
// those are the only two moments the OS layer's view can change.
func (a *App) emitHotkeyState() {
	sb := a.settings
	dto := hotkeyStateDTO(sb.hk.State())
	sb.em.HotkeysState(dto.Registered, dto.Error, dto.Failed)
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
	_ = sb.hk.Apply(binds) // failures surface via the event below
	// Without this the hotkeys:state event had no production emitter at all:
	// a binding the OS refuses (Numpad7 and friends) saved cleanly, was
	// recorded in Manager.Failed(), and never reached the UI, so the row
	// rendered the chord as if it were live.
	a.emitHotkeyState()
}

// RefreshKeybinds recomputes the joined action list against the CURRENT radio
// set, re-applies OS hotkeys and emits keybinds:changed.
//
// Per-radio actions are derived from the local client's radios, which do not
// exist at startup -- they arrive with SyncClient at connect time and change
// again on every server-side radio update. Without this the per-radio panel
// stayed absent and per-radio hotkeys stayed unregistered until the user
// happened to change some unrelated keybind. Wired to state.Store's radio
// observer in SetSettingsBackend.
//
// Radio updates for OTHER clients flow through the same observer, so this
// short-circuits unless the LOCAL radio set actually changed; otherwise every
// remote radio tweak would re-emit the whole keybind list.
func (a *App) RefreshKeybinds() {
	sb := a.settings
	if sb == nil {
		return // settings backend not wired (tests that only exercise session bindings)
	}
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

	sig := radioSignature(a.radioRefs())
	sb.mu.Lock()
	unchanged := sb.radioSig == sig
	sb.radioSig = sig
	sb.mu.Unlock()
	if unchanged {
		return
	}

	a.applyHotkeys()
	sb.em.KeybindsChanged(a.GetKeybinds())
}

// radioSignature renders the local radio set as a comparable string. Both the
// id and the name matter: the name is part of the per-radio action label, so
// a rename must still refresh the rows.
func radioSignature(refs []keybinds.RadioRef) string {
	var b strings.Builder
	for _, r := range refs {
		fmt.Fprintf(&b, "%d:%s|", r.ID, r.Name)
	}
	return b.String()
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
