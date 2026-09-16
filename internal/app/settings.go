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

// Bounds for the permission re-check that RequestHotkeyPermission arms.
//
// This is NOT a background loop. macOS offers no notification for a TCC
// permission change, so the only ways to learn about a grant are to ask
// AXIsProcessTrusted again, or to be told by the user. The poll
// exists solely in the window between an explicit user request and its
// answer, and cancels itself on the first grant or at the timeout, whichever
// comes first. The primary trigger is the cheaper one: the window-focus
// re-check (RecheckHotkeyPermission), since the user must leave the app to
// grant and come back afterwards.
const (
	defaultPermissionPollInterval = 500 * time.Millisecond
	defaultPermissionPollTimeout  = 30 * time.Second
)

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

	// permInterval / permTimeout bound the re-check armed by
	// RequestHotkeyPermission. Overridable through
	// SetHotkeyPermissionPoll for tests, mirroring SetCaptureTimeout.
	permInterval time.Duration
	permTimeout  time.Duration

	// permCancel is closed to stop the in-flight re-check, so a second
	// request supersedes the first instead of running two polls.
	// permDone is closed by that poll's goroutine when it returns; tests
	// join on it, and its nil-ness is how "never armed a poll" is asserted.
	permCancel chan struct{}
	permDone   chan struct{}

	// lastPerm is the grant state as of the most recent read. Kept so
	// RecheckHotkeyPermission can tell a genuine flip from a repeat, and so
	// the focus hook can return without touching the OS once access is
	// granted (or was never applicable).
	lastPerm hotkeys.Permission
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

// setCaptureTimeout overrides the auto-resume timeout. For tests only, and
// UNEXPORTED for that reason: an exported method on the service is bound and
// reachable from the webview, and a timing knob is not something the
// frontend has any business setting.
func (a *App) setCaptureTimeout(d time.Duration) {
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
	return hotkeyStateDTO(a.settings.hk.State(), a.permissionStatus())
}

// RequestHotkeyPermission fires the OS permission prompt for global hotkey
// capture and arms a bounded re-check for the answer.
//
// The returned Prompted is what the OS request call said and nothing more;
// the grant itself is only ever concluded from Status(). That split is the
// whole point of the flow: on macOS the prompt is handled asynchronously by
// TCC, so this call returns while the sheet is still on screen, and a UI that
// rendered the request's return value as the answer would claim "granted"
// against a process that still cannot see a keystroke.
//
// Three outcomes:
//   - already granted: apply immediately, no poll (nothing to wait for).
//   - not applicable: return, no poll (Windows/X11 have no grant to wait for,
//     so a 30-second ticker there would burn wakeups for a state that cannot
//     change).
//   - otherwise: arm the poll, which re-applies hotkeys and emits
//     hotkeys:state if and when Status() flips to granted.
func (a *App) RequestHotkeyPermission() HotkeyPermissionResultDTO {
	sb := a.settings
	if sb == nil || a.perm == nil {
		return HotkeyPermissionResultDTO{Permission: hotkeys.PermissionUnknown.String()}
	}

	prompted := a.perm.Request()
	status := a.permissionStatus()

	switch status {
	case hotkeys.PermissionGranted:
		// The grant was already in place (or landed synchronously). Re-apply
		// now rather than making the user wait a poll interval for it.
		//
		// Same two obligations as the focus path, and for the same reasons:
		// cancel any poll a previous GRANT ACCESS click armed (a second click
		// after the grant would otherwise leave the old one ticking to
		// re-apply), and hold writeMu across the apply so this IPC-reachable
		// call cannot land between a failed persist and its rollback in
		// SetKeybind -- which would register the OS against a binding both
		// the store and disk deny. See RecheckHotkeyPermission.
		a.cancelPermissionPoll()
		sb.writeMu.Lock()
		a.applyHotkeys()
		sb.writeMu.Unlock()
	case hotkeys.PermissionNotApplicable:
		// Nothing to wait for.
	default:
		a.armPermissionPoll()
	}

	return HotkeyPermissionResultDTO{Prompted: prompted, Permission: status.String()}
}

// OpenHotkeyPermissionSettings deep-links the OS settings pane where the user
// can grant global hotkey access. The escape hatch for when the one-shot
// prompt has already been spent, which is the state a user who once pressed
// "Don't Allow" is permanently stuck in otherwise.
func (a *App) OpenHotkeyPermissionSettings() error {
	if a.perm == nil {
		return hotkeys.ErrNoPermissionSettings
	}
	return a.perm.OpenSettings()
}

// RecheckHotkeyPermission re-reads the grant state and, if it has flipped to
// granted, re-applies hotkeys and emits hotkeys:state.
//
// This is the PRIMARY trigger, wired to the main window's focus event in
// main.go. Granting requires leaving the app and coming back, so focus is
// precisely the moment the answer can have changed -- and unlike the poll it
// costs nothing when it has not.
//
// Exported so main.go stays a thin wiring layer and this logic is testable;
// the focus hook itself must contain no policy.
func (a *App) RecheckHotkeyPermission() {
	sb := a.settings
	if sb == nil || a.perm == nil {
		return // settings backend not wired (tests that only exercise session bindings)
	}

	// Cheap early-out FIRST, deliberately outside writeMu. Window focus fires
	// constantly; the overwhelmingly common case must not queue behind a
	// keybind write.
	sb.mu.Lock()
	last := sb.lastPerm
	sb.mu.Unlock()
	if last == hotkeys.PermissionGranted || last == hotkeys.PermissionNotApplicable {
		return // nothing a re-check could improve
	}

	// writeMu for the rest, matching every other applyHotkeys caller. Without
	// it this races the snapshot -> mutate -> persist -> rollback sequence in
	// SetKeybind: a focus event landing between a failed persist and its
	// rollback would register the OS against a binding that is about to be
	// undone, leaving the OS holding a hotkey both the store and disk deny.
	// Lock order is writeMu -> mu, as established by the existing callers;
	// the read above released mu before this line, so the two are never held
	// in the opposite order.
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

	// Re-read lastPerm now that the lock is held, mirroring the way
	// applyGrantedHotkeys re-checks cancel after winning writeMu. Two focus
	// goroutines can both pass the early-out above before either applies, and
	// the window is the writeMu acquisition itself -- not microscopic when a
	// keybind write is in flight. Reading the same way in both paths also
	// removes the question a reader would otherwise have to answer about why
	// only one of them re-checks.
	sb.mu.Lock()
	last = sb.lastPerm
	sb.mu.Unlock()
	if last == hotkeys.PermissionGranted || last == hotkeys.PermissionNotApplicable {
		return // another focus goroutine got here first
	}

	if a.permissionStatus() != hotkeys.PermissionGranted {
		return
	}

	// Stop the bounded poll before applying. The realistic path -- click
	// GRANT, leave for System Settings, grant, come back -- fires this hook
	// with the poll still armed, and a tick within the next 500 ms would
	// otherwise tear down and recreate the event tap that was just built.
	// Manager.mu makes the end state correct either way, but a duplicate
	// teardown of a freshly granted tap is the worst possible moment for one.
	a.cancelPermissionPoll()

	// Re-apply rather than merely re-emitting: Apply tears down and recreates
	// the OS registrations, which on macOS means a fresh CGEventTap built
	// under the new trust. That is what can pick the grant up without a
	// restart. applyHotkeys emits hotkeys:state itself.
	a.applyHotkeys()
}

// cancelPermissionPoll stops the in-flight bounded re-check, if any.
//
// Safe to call concurrently and repeatedly: the channel is taken and nilled
// under sb.mu in one step, so exactly one caller can ever observe a non-nil
// value and therefore exactly one close happens. That matters because Wails
// dispatches window hooks on their own goroutine (`go a.handleWindowEvent`),
// so two rapid focus events genuinely can run RecheckHotkeyPermission at the
// same time.
//
// Never waits for the goroutine to finish. Callers on the focus path hold
// writeMu, which the poll goroutine may itself be blocked acquiring --
// waiting here would deadlock. Shutdown, which holds nothing, uses
// stopPermissionPoll instead.
func (a *App) cancelPermissionPoll() {
	sb := a.settings
	if sb == nil {
		return
	}
	sb.mu.Lock()
	ch := sb.permCancel
	sb.permCancel = nil
	sb.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// stopPermissionPoll cancels the re-check and waits, bounded, for its
// goroutine to return. Called from ServiceShutdown so a poll armed moments
// before quit cannot call into the hotkey library or emit a Wails event
// after teardown.
//
// The wait is bounded because the goroutine may be blocked acquiring writeMu
// behind an in-flight keybind write; a quit must not hang on that. Safe to
// call with no poll armed.
func (a *App) stopPermissionPoll(wait time.Duration) {
	sb := a.settings
	if sb == nil {
		return
	}
	a.cancelPermissionPoll()

	sb.mu.Lock()
	done := sb.permDone
	sb.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(wait):
	}
}

// setHotkeyPermissionPoll overrides the bounded re-check's interval and
// timeout. For tests only, mirroring setCaptureTimeout, and unexported for
// the same reason -- with more force here: this one sets a POLL INTERVAL, so
// an exported binding would let the webview call
// setHotkeyPermissionPoll(1, 1e12) and turn the re-check into a spin loop.
// Non-positive values leave the corresponding default in place.
func (a *App) setHotkeyPermissionPoll(interval, timeout time.Duration) {
	sb := a.settings
	sb.mu.Lock()
	if interval > 0 {
		sb.permInterval = interval
	}
	if timeout > 0 {
		sb.permTimeout = timeout
	}
	sb.mu.Unlock()
}

// permissionStatus reads the OS grant state and caches it for
// RecheckHotkeyPermission's early-out. Returns PermissionUnknown when no
// checker is wired, which is the honest answer and keeps the DTO from
// claiming a state it never observed.
func (a *App) permissionStatus() hotkeys.Permission {
	sb := a.settings
	if sb == nil || a.perm == nil {
		return hotkeys.PermissionUnknown
	}
	p := a.perm.Status()
	sb.mu.Lock()
	sb.lastPerm = p
	sb.mu.Unlock()
	return p
}

// armPermissionPoll starts the bounded re-check described on
// RequestHotkeyPermission. A second request supersedes the first rather than
// stacking a second ticker on the same checker.
func (a *App) armPermissionPoll() {
	sb := a.settings

	sb.mu.Lock()
	if sb.permCancel != nil {
		close(sb.permCancel) // supersede the poll already running
	}
	cancel := make(chan struct{})
	done := make(chan struct{})
	sb.permCancel, sb.permDone = cancel, done
	interval, timeout := sb.permInterval, sb.permTimeout
	sb.mu.Unlock()

	if interval <= 0 {
		interval = defaultPermissionPollInterval
	}
	if timeout <= 0 {
		timeout = defaultPermissionPollTimeout
	}

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		deadline := time.NewTimer(timeout)
		defer deadline.Stop()
		for {
			select {
			case <-cancel:
				return
			case <-deadline.C:
				// Give up silently. The user may simply not have granted;
				// the focus re-check still covers a later grant, so there is
				// nothing to report and nothing left running.
				return
			case <-ticker.C:
				if a.permissionStatus() == hotkeys.PermissionGranted {
					a.applyGrantedHotkeys(cancel)
					return
				}
			}
		}
	}()
}

// applyGrantedHotkeys re-applies the OS registrations under writeMu, which
// every other applyHotkeys caller also holds -- see RecheckHotkeyPermission
// for what goes wrong without it.
//
// It re-checks cancel AFTER taking the lock, not just before: by the time
// this goroutine wins writeMu, the focus re-check may already have detected
// the same grant and applied. Skipping here is what keeps that from being a
// second, redundant teardown-and-recreate of a tap built seconds ago.
func (a *App) applyGrantedHotkeys(cancel <-chan struct{}) {
	sb := a.settings
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()
	select {
	case <-cancel:
		return // superseded by the focus re-check, or we are shutting down
	default:
	}
	a.applyHotkeys() // recreates the taps AND emits hotkeys:state
}

// permissionPollDone is an unexported test helper returning the channel the
// in-flight re-check closes when it returns, or nil if none was ever armed.
func (a *App) permissionPollDone() <-chan struct{} {
	sb := a.settings
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.permDone
}

// hotkeyStateDTO renders a hotkeys.State for the binding surface. Error is
// populated ONLY when Registered is false, so the two fields cannot
// contradict each other: Registered==false plus Error is "global hotkeys are
// dead, here is why", while a partial failure leaves Registered true and
// says which bindings failed through Failed alone. The UI banner keys off
// Registered, so letting Error survive a partial failure would resurrect the
// bug where one bad binding blanked the banner for nineteen working ones.
func hotkeyStateDTO(s hotkeys.State, perm hotkeys.Permission) HotkeyStateDTO {
	msg := ""
	if !s.Registered {
		msg = errString(s.LastError)
	}
	return HotkeyStateDTO{
		Registered: s.Registered,
		Error:      msg,
		Failed:     s.Failed,
		Permission: perm.String(),
	}
}

// emitHotkeyState publishes the current registration health on
// hotkeys:state. Called after every apply and after every resume, because
// those are the only two moments the OS layer's view can change.
func (a *App) emitHotkeyState() {
	sb := a.settings
	dto := hotkeyStateDTO(sb.hk.State(), a.permissionStatus())
	sb.em.HotkeysState(dto.Registered, dto.Error, dto.Failed, dto.Permission)
}

// Pressed implements hotkeys.Handler.
//
// The log line alongside the emit is deliberate and deliberately at Info: a
// hotkey edge is a low-frequency, genuinely diagnostic event, and the log
// file is the only place a user can confirm "my key IS firing, the problem
// is downstream" without any UI. It records the ACTION ID and the edge --
// never the key, the keycode or the character. The OS layer sees every
// keystroke on the machine (see internal/hotkeys/registrar_gohook.go), so
// widening this to key identities would turn the log into a keylog.
func (a *App) Pressed(actionID string) {
	if a.settings == nil {
		return
	}
	a.logger.Info("hotkey fired", "action", actionID, "edge", "down")
	a.settings.em.HotkeyPressed(actionID)
}

// Released implements hotkeys.Handler. See Pressed for why this logs the
// action ID and nothing about the key itself.
func (a *App) Released(actionID string) {
	if a.settings == nil {
		return
	}
	a.logger.Info("hotkey fired", "action", actionID, "edge", "up")
	a.settings.em.HotkeyReleased(actionID)
}

// ForceReleased implements hotkeys.StaleReleaser. The OS layer calls it when
// its stale-latch watchdog had to release a hotkey that never got its key-up.
//
// Warn, not Info: an ordinary edge is routine, but this one means a release
// was LOST somewhere upstream -- a dropped event, a key released while we were
// backgrounded, a listener that died without saying so -- and the user's
// microphone was open for the whole timeout before we noticed. It is rare by
// construction, so it is worth waking someone reading the log.
//
// Action ID only. The OS layer sees every keystroke on the machine, so the
// rule that a log line never carries a key identity holds here exactly as it
// does in Pressed and Released.
//
// The matching Released arrives immediately after this and does the actual
// work; this call only annotates it.
func (a *App) ForceReleased(actionID string) {
	if a.settings == nil {
		return
	}
	a.logger.Warn("hotkey force-released after timeout: no key-up was ever delivered",
		"action", actionID, "after", hotkeys.DefaultStaleLatchTimeout)
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
