package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// recordingEmitter records every emitted event name and payload. Guarded by
// a mutex because emits now also originate off the caller's goroutine (the
// capture auto-resume timer, and the state-store radio observer).
type recordingEmitter struct {
	mu       sync.Mutex
	events   []string
	payloads []any
}

func (r *recordingEmitter) Emit(name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, name)
	r.payloads = append(r.payloads, payload)
}

// names returns a copy of the recorded event names.
func (r *recordingEmitter) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// count returns how many times name was emitted.
func (r *recordingEmitter) count(name string) int {
	n := 0
	for _, e := range r.names() {
		if e == name {
			n++
		}
	}
	return n
}

// lastHotkeyState returns the payload of the most recent hotkeys:state event.
func (r *recordingEmitter) lastHotkeyState(t *testing.T) events.HotkeyStatePayload {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i] == events.EventHotkeysState {
			p, ok := r.payloads[i].(events.HotkeyStatePayload)
			if !ok {
				t.Fatalf("hotkeys:state payload has type %T, want events.HotkeyStatePayload", r.payloads[i])
			}
			return p
		}
	}
	t.Fatalf("no %s event was emitted; got %v", events.EventHotkeysState, r.events)
	return events.HotkeyStatePayload{}
}

type countingRegistrar struct {
	registers   int
	unregisters int
}

func (c *countingRegistrar) Register(string, chord.Chord, bool, hotkeys.Handler) error {
	c.registers++
	return nil
}
func (c *countingRegistrar) UnregisterAll() { c.unregisters++ }

// failingRegistrar fails to register one specific action ID and succeeds for
// everything else, so tests can exercise hotkeys.Manager.Failed() wiring.
type failingRegistrar struct {
	failActionID string
}

func (f *failingRegistrar) Register(actionID string, _ chord.Chord, _ bool, _ hotkeys.Handler) error {
	if actionID == f.failActionID {
		return errors.New("registrar: cannot register this key")
	}
	return nil
}

func (f *failingRegistrar) UnregisterAll() {}

// deadRegistrar fails EVERY registration -- what a missing OS backend
// (registrar_nocgo.go) or a denied macOS Accessibility permission looks
// like. This, and only this, is what the "global hotkeys unavailable" banner
// is meant to describe.
type deadRegistrar struct{}

func (deadRegistrar) Register(string, chord.Chord, bool, hotkeys.Handler) error {
	return errors.New("hotkeys: OS backend unavailable in this build")
}
func (deadRegistrar) UnregisterAll() {}

// newTestApp wires an App with in-memory settings deps and no Wails.
func newTestApp(t *testing.T) (*App, *recordingEmitter, *countingRegistrar) {
	t.Helper()
	return newTestAppWithPath(t, "")
}

// newTestAppWithPath is newTestApp but with an explicit cfgPath, so tests can
// exercise the persistence-failure path with a real (broken) filesystem path
// instead of the "" no-op skip every other test uses.
func newTestAppWithPath(t *testing.T, cfgPath string) (*App, *recordingEmitter, *countingRegistrar) {
	t.Helper()
	em := &recordingEmitter{}
	reg := &countingRegistrar{}
	a := NewForTest(state.New(), nil, nil)
	cfg := config.Default()
	kb := keybinds.New()
	kb.Load(map[string]string{})
	hk := hotkeys.New(reg, a)
	a.SetSettingsBackend(cfg, cfgPath, kb, hk, em)
	return a, em, reg
}

// newTestAppOn is newTestAppWithPath over a caller-supplied state store, so a
// test can push radios into the store after the backend is wired.
func newTestAppOn(t *testing.T, st *state.Store, reg hotkeys.Registrar) (*App, *recordingEmitter) {
	t.Helper()
	em := &recordingEmitter{}
	a := NewForTest(st, nil, nil)
	kb := keybinds.New()
	kb.Load(map[string]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(reg, a), em)
	return a, em
}

// unwritablePath returns a config path under a directory that does not
// exist, so config.Save genuinely fails (ENOENT on the temp-file create).
func unwritablePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "no-such-dir", "config.toml")
}

func TestGetSettingsReturnsDefaults(t *testing.T) {
	a, _, _ := newTestApp(t)
	s := a.GetSettings()
	if !s.MinimizeToTray {
		t.Error("MinimizeToTray should default true")
	}
	if s.StartMinimized {
		t.Error("StartMinimized should default false")
	}
}

func TestSetSettingsEmitsChange(t *testing.T) {
	a, em, _ := newTestApp(t)
	s := a.GetSettings()
	s.StartMinimized = true
	if err := a.SetSettings(s); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if !a.GetSettings().StartMinimized {
		t.Error("setting did not stick")
	}
	if !contains(em.names(), events.EventSettingsChanged) {
		t.Errorf("expected %s, got %v", events.EventSettingsChanged, em.names())
	}
}

func TestGetKeybindsJoinsRegistryWithChords(t *testing.T) {
	a, _, _ := newTestApp(t)
	list := a.GetKeybinds()
	if len(list) == 0 {
		t.Fatal("expected keybind rows")
	}
	byID := map[string]KeybindDTO{}
	for _, k := range list {
		byID[k.ActionID] = k
	}
	ptt, ok := byID["global.ptt"]
	if !ok {
		t.Fatal("global.ptt missing from the joined list")
	}
	if ptt.Label != "Global PTT" {
		t.Errorf("label = %q, want %q", ptt.Label, "Global PTT")
	}
	if ptt.Kind != "hold" {
		t.Errorf("kind = %q, want hold", ptt.Kind)
	}
}

func TestSetKeybindStoresAndEmits(t *testing.T) {
	a, em, _ := newTestApp(t)
	res, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})
	if err != nil {
		t.Fatalf("SetKeybind: %v", err)
	}
	if res.Stolen != nil {
		t.Errorf("unexpected steal: %+v", res.Stolen)
	}
	if !contains(em.names(), events.EventKeybindsChanged) {
		t.Error("expected keybinds:changed event")
	}
	for _, k := range a.GetKeybinds() {
		if k.ActionID == "global.ptt" && k.Chord != "F1" {
			t.Errorf("chord = %q, want F1", k.Chord)
		}
	}
}

func TestSetKeybindReportsSteal(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.SetKeybind("global.mute_toggle", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatal(err)
	}
	res, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stolen == nil {
		t.Fatal("expected a steal report")
	}
	if res.Stolen.ActionID != "global.mute_toggle" {
		t.Errorf("stolen from %q, want global.mute_toggle", res.Stolen.ActionID)
	}
	if res.Stolen.Label != "Mute toggle" {
		t.Errorf("stolen label = %q, want %q", res.Stolen.Label, "Mute toggle")
	}
}

func TestSetKeybindRejectsBadCapture(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "ControlLeft", Ctrl: true}); err == nil {
		t.Error("a bare modifier must be rejected")
	}
	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "Nonsense"}); err == nil {
		t.Error("an unknown code must be rejected")
	}
}

func TestBeginCaptureSuspendsAndEndCaptureRestores(t *testing.T) {
	a, _, reg := newTestApp(t)
	a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})
	before := reg.unregisters

	token := a.BeginCapture()
	if token == 0 {
		t.Fatal("BeginCapture must return a non-zero capture token")
	}
	if reg.unregisters <= before {
		t.Error("BeginCapture must unregister OS hotkeys")
	}
	if a.hotkeysResumed() {
		t.Error("hotkeys must be suspended while a capture is in flight")
	}
	a.EndCapture(token)
	if !a.hotkeysResumed() {
		t.Error("EndCapture with the current token must re-arm hotkeys")
	}
}

// TestEndCaptureWithStaleTokenDoesNotResume is the C1 regression guard.
// Switching capture between rows dispatches the NEW row's BeginCapture before
// the old row's EndCapture (the old KeyChip only unmounts once React commits).
// If that late EndCapture resumed, every OS hotkey would be live during the
// new capture and the OS would swallow the keypress meant to rebind it.
func TestEndCaptureWithStaleTokenDoesNotResume(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})

	first := a.BeginCapture()  // chip A starts listening
	second := a.BeginCapture() // user clicks chip B -- dispatched FIRST
	if second == first {
		t.Fatalf("BeginCapture returned the same token twice: %d", second)
	}

	// Chip A's cancel lands second, carrying the superseded token.
	a.EndCapture(first)
	if a.hotkeysResumed() {
		t.Fatal("a stale EndCapture must NOT re-arm hotkeys during the capture that superseded it")
	}

	// The newest capture is still resumable.
	a.EndCapture(second)
	if !a.hotkeysResumed() {
		t.Error("the newest capture's token must still re-arm hotkeys")
	}
}

func TestCaptureAutoResumesAfterTimeout(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.setCaptureTimeout(50 * time.Millisecond)
	a.BeginCapture()
	// Never call EndCapture — simulates the frontend dying mid-capture.
	time.Sleep(150 * time.Millisecond)
	if !a.hotkeysResumed() {
		t.Error("hotkeys must auto-resume if EndCapture never arrives")
	}
}

// TestSupersededCaptureTimeoutDoesNotResume covers the auto-resume timer.
//
// Two mechanisms have to hold. BeginCapture stops the previous generation's
// timer, which the sleep below checks. But Stop() loses the race when the old
// timer has ALREADY fired and its callback is waiting on the mutex -- for
// that case the callback carries its own generation and must no-op. The
// second half of this test drives that callback's exact code path
// (resumeCapture with the superseded generation) directly, because the race
// itself is not reproducible on demand.
func TestSupersededCaptureTimeoutDoesNotResume(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.setCaptureTimeout(50 * time.Millisecond)

	first := a.BeginCapture() // generation 1, times out in 50ms

	a.setCaptureTimeout(10 * time.Second) // generation 2 gets a long timeout
	second := a.BeginCapture()

	time.Sleep(150 * time.Millisecond) // long enough for generation 1's timer
	if a.hotkeysResumed() {
		t.Fatal("a superseded capture's timeout must not re-arm hotkeys")
	}

	// The un-cancellable case: generation 1's callback runs anyway.
	a.resumeCapture(first)
	if a.hotkeysResumed() {
		t.Fatal("a superseded timer callback that beat Stop() must still be a no-op")
	}

	a.EndCapture(second)
	if !a.hotkeysResumed() {
		t.Error("the live capture must still be resumable after a stale timeout fired")
	}
}

func TestClearKeybind(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})
	if err := a.ClearKeybind("global.ptt"); err != nil {
		t.Fatal(err)
	}
	for _, k := range a.GetKeybinds() {
		if k.ActionID == "global.ptt" && k.Chord != "" {
			t.Errorf("chord = %q, want empty after clear", k.Chord)
		}
	}
}

func TestSetSettingsFailsWithoutMutatingOnPersistError(t *testing.T) {
	a, em, _ := newTestAppWithPath(t, unwritablePath(t))

	before := a.GetSettings()
	next := before
	next.StartMinimized = !before.StartMinimized
	if err := a.SetSettings(next); err == nil {
		t.Fatal("expected SetSettings to fail when the config path is unwritable")
	}
	if got := a.GetSettings(); got != before {
		t.Errorf("GetSettings() = %+v after a failed save, want unchanged %+v", got, before)
	}
	if contains(em.names(), events.EventSettingsChanged) {
		t.Error("settings:changed must not fire when persistence fails")
	}
}

func TestSetKeybindFailsWithoutMutatingOnPersistError(t *testing.T) {
	a, em, _ := newTestAppWithPath(t, unwritablePath(t))

	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"}); err == nil {
		t.Fatal("expected SetKeybind to fail when the config path is unwritable")
	}
	for _, k := range a.GetKeybinds() {
		if k.ActionID == "global.ptt" && k.Chord != "" {
			t.Errorf("chord = %q, want empty after a failed SetKeybind", k.Chord)
		}
	}
	if contains(em.names(), events.EventKeybindsChanged) {
		t.Error("keybinds:changed must not fire when persistence fails")
	}
}

// TestSetKeybindStealFailsRestoresVictim is the case that matters most: a
// failed SetKeybind that stole a chord must not leave the victim unbound.
func TestSetKeybindStealFailsRestoresVictim(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	a, em, _ := newTestAppWithPath(t, cfgPath)

	// Seed a real binding while the path is still writable.
	if _, err := a.SetKeybind("global.mute_toggle", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("seed SetKeybind: %v", err)
	}

	// Break persistence by removing the directory config.toml lives in.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	// Attempt to steal F1 for global.ptt; the write must fail.
	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"}); err == nil {
		t.Fatal("expected the steal to fail once the config directory is gone")
	}

	for _, k := range a.GetKeybinds() {
		switch k.ActionID {
		case "global.mute_toggle":
			if k.Chord != "F1" {
				t.Errorf("victim chord = %q, want F1 restored after the failed steal", k.Chord)
			}
		case "global.ptt":
			if k.Chord != "" {
				t.Errorf("thief chord = %q, want empty after the failed steal", k.Chord)
			}
		}
	}
	// The seed call legitimately emitted once; the failed steal must not add
	// a second, incorrect emission.
	if count := em.count(events.EventKeybindsChanged); count != 1 {
		t.Errorf("keybinds:changed fired %d times, want exactly 1 (from the seed call only)", count)
	}
}

func TestClearKeybindFailsWithoutMutatingOnPersistError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	a, em, _ := newTestAppWithPath(t, cfgPath)

	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("seed SetKeybind: %v", err)
	}
	seedEvents := len(em.names())

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	if err := a.ClearKeybind("global.ptt"); err == nil {
		t.Fatal("expected ClearKeybind to fail once the config directory is gone")
	}
	for _, k := range a.GetKeybinds() {
		if k.ActionID == "global.ptt" && k.Chord != "F1" {
			t.Errorf("chord = %q, want F1 still present after a failed ClearKeybind", k.Chord)
		}
	}
	if len(em.names()) != seedEvents {
		t.Error("keybinds:changed must not fire when persistence fails")
	}
}

// TestConcurrentMutationsKeepCfgAndStoreConsistent guards against the race
// the rollback fix (round 2) introduced: SetSettings, SetKeybind and
// ClearKeybind each snapshot -> mutate -> persist -> (maybe restore) -> emit,
// and without a mutex serialising that whole sequence, a failing call could
// roll back to a snapshot taken before a DIFFERENT call's successful,
// already-persisted commit, silently erasing it. The exact interleaving
// needed to reproduce that loss is not forceable deterministically, so this
// instead asserts the invariant the race breaks: after a storm of concurrent
// mutators, cfg.Keybinds (what actually gets written to disk) must still
// agree with the live keybinds.Store, no matter which interleaving won.
func TestConcurrentMutationsKeepCfgAndStoreConsistent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	a, _, _ := newTestAppWithPath(t, cfgPath)

	actionIDs := []string{
		"global.mute_toggle", "global.push_to_mute",
		"global.emergency_broadcast", "global.compact_overlay",
	}
	codes := []string{"F1", "F2", "F3", "F4", "F5", "F6"}

	const n = 60
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := actionIDs[i%len(actionIDs)]
			switch i % 3 {
			case 0:
				_, _ = a.SetKeybind(id, CaptureDTO{Code: codes[i%len(codes)]})
			case 1:
				_ = a.ClearKeybind(id)
			case 2:
				s := a.GetSettings()
				s.StartMinimized = i%2 == 0
				_ = a.SetSettings(s)
			}
		}(i)
	}
	wg.Wait()

	sb := a.settings
	sb.mu.Lock()
	gotCfg := sb.cfg.Keybinds
	sb.mu.Unlock()
	want := sb.kb.Snapshot()

	if len(gotCfg) != len(want) {
		t.Fatalf("cfg.Keybinds has %d entries, kb.Snapshot() has %d: cfg=%v store=%v", len(gotCfg), len(want), gotCfg, want)
	}
	for id, c := range want {
		if gotCfg[id] != c {
			t.Errorf("cfg.Keybinds[%q] = %q, want %q (from the live store)", id, gotCfg[id], c)
		}
	}
}

func TestGetHotkeyStateReportsPerActionFailures(t *testing.T) {
	em := &recordingEmitter{}
	reg := &failingRegistrar{failActionID: "global.ptt"}
	a := NewForTest(state.New(), nil, nil)
	cfg := config.Default()
	kb := keybinds.New()
	kb.Load(map[string]string{})
	hk := hotkeys.New(reg, a)
	a.SetSettingsBackend(cfg, "", kb, hk, em)

	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("SetKeybind: %v", err)
	}

	got := a.GetHotkeyState()
	if reason, ok := got.Failed["global.ptt"]; !ok || reason == "" {
		t.Fatalf("expected a failure reason for global.ptt, got %+v", got.Failed)
	}
	// The shipped defaults are also loaded and register fine, so this is a
	// PARTIAL failure: Registered stays true (see I4 /
	// hotkeys.Manager.Registered) and the one bad binding is named in Failed.
	if !got.Registered {
		t.Error("one failing binding among working ones must not report global hotkeys as unavailable")
	}

	// Clearing the failing binding and re-applying should leave Failed empty.
	if err := a.ClearKeybind("global.ptt"); err != nil {
		t.Fatalf("ClearKeybind: %v", err)
	}
	got = a.GetHotkeyState()
	if len(got.Failed) != 0 {
		t.Errorf("Failed should be empty after a clean apply, got %+v", got.Failed)
	}
	if !got.Registered {
		t.Error("Registered should be true after a clean apply")
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// TestPartialRegistrationFailureKeepsHotkeysRegistered is the I4 guard.
// Registered() drives the "Global hotkeys unavailable" banner, so it must
// mean "nothing is registered at all", not "at least one binding failed".
// One unregisterable key (Numpad7 and friends are accepted by internal/chord
// but have no OS mapping) used to blank the banner for every other working
// binding and name one arbitrary victim.
func TestPartialRegistrationFailureKeepsHotkeysRegistered(t *testing.T) {
	em := &recordingEmitter{}
	a := NewForTest(state.New(), nil, nil)
	kb := keybinds.New()
	kb.Load(map[string]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(&failingRegistrar{failActionID: "global.ptt"}, a), em)

	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("SetKeybind(global.ptt): %v", err)
	}
	if _, err := a.SetKeybind("global.mute_toggle", CaptureDTO{Code: "F2"}); err != nil {
		t.Fatalf("SetKeybind(global.mute_toggle): %v", err)
	}

	got := a.GetHotkeyState()
	if !got.Registered {
		t.Error("one failing binding among working ones must NOT report global hotkeys as unavailable")
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want empty while Registered is true (per-binding reasons belong in Failed)", got.Error)
	}
	if _, ok := got.Failed["global.ptt"]; !ok {
		t.Errorf("the failing binding must still be named in Failed, got %+v", got.Failed)
	}
	if _, ok := got.Failed["global.mute_toggle"]; ok {
		t.Errorf("the working binding must not appear in Failed, got %+v", got.Failed)
	}
}

// TestApplyHotkeysEmitsHotkeyState is the I1 guard: hotkeys:state had no
// production emitter, so a registration failure was recorded in
// Manager.Failed() and never reached the UI -- the row rendered the chord as
// if it were live (DoD 9 / manual checklist 13).
func TestApplyHotkeysEmitsHotkeyState(t *testing.T) {
	em := &recordingEmitter{}
	a := NewForTest(state.New(), nil, nil)
	kb := keybinds.New()
	kb.Load(map[string]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(&failingRegistrar{failActionID: "global.ptt"}, a), em)

	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("SetKeybind: %v", err)
	}

	got := em.lastHotkeyState(t)
	if reason, ok := got.Failed["global.ptt"]; !ok || reason == "" {
		t.Fatalf("hotkeys:state must name the failing action, got %+v", got.Failed)
	}
}

// TestSetSettingsBackendEmitsInitialHotkeyState covers the startup emit: a
// backend that cannot register anything (no cgo backend, denied permission)
// must surface before the user touches a single keybind.
func TestSetSettingsBackendEmitsInitialHotkeyState(t *testing.T) {
	em := &recordingEmitter{}
	a := NewForTest(state.New(), nil, nil)
	// SetSettingsBackend seeds the store from cfg.Keybinds (falling back to
	// the shipped defaults when empty), so the binding has to go through cfg.
	cfg := config.Default()
	cfg.Keybinds = map[string]string{"global.ptt": "F1"}
	a.SetSettingsBackend(cfg, "", keybinds.New(), hotkeys.New(deadRegistrar{}, a), em)

	got := em.lastHotkeyState(t)
	if got.Registered {
		t.Error("a backend that registered nothing must report Registered=false at startup")
	}
	if _, ok := got.Failed["global.ptt"]; !ok {
		t.Errorf("startup hotkeys:state must name the failing action, got %+v", got.Failed)
	}
}

// TestResumeAfterCaptureEmitsHotkeyState: while suspended, Apply only records
// the desired set -- nothing hits the OS until Resume. Resume is therefore the
// first moment a chord bound DURING a capture can be known to be
// unregisterable, and the normal UI flow (BeginCapture -> SetKeybind ->
// EndCapture) goes through exactly that path.
func TestResumeAfterCaptureEmitsHotkeyState(t *testing.T) {
	em := &recordingEmitter{}
	a := NewForTest(state.New(), nil, nil)
	kb := keybinds.New()
	kb.Load(map[string]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(&failingRegistrar{failActionID: "global.ptt"}, a), em)

	token := a.BeginCapture()
	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("SetKeybind: %v", err)
	}
	a.EndCapture(token)

	got := em.lastHotkeyState(t)
	if reason, ok := got.Failed["global.ptt"]; !ok || reason == "" {
		t.Fatalf("the hotkeys:state emitted after resume must name the failing action, got %+v", got.Failed)
	}
}

// TestRadiosArrivingAfterStartupProducePerRadioRows is the I2 guard. At
// startup there is no session, so radioRefs() is nil and the PER-RADIO panel
// has no rows. When the user connects and radios arrive, the rows and their
// OS registrations must follow without the user touching an unrelated
// keybind first (DoD 5).
func TestRadiosArrivingAfterStartupProducePerRadioRows(t *testing.T) {
	st := state.New()
	a, em := newTestAppOn(t, st, &countingRegistrar{})

	if perRadio := perRadioRows(a.GetKeybinds()); len(perRadio) != 0 {
		t.Fatalf("expected no per-radio rows before connect, got %v", perRadio)
	}
	before := em.count(events.EventKeybindsChanged)

	// Mirrors Session.Connect: SyncClient fills radios, THEN SetSelf records
	// which GUID is ours.
	st.SetRadios("me", &srspb.RadioInfo{Radios: []*srspb.Radio{
		{Id: 1, Name: "GUARD"},
		{Id: 2, Name: "FLEET"},
	}})
	st.SetSelf("me", &srspb.ClientInfo{Name: "me"})

	rows := perRadioRows(a.GetKeybinds())
	if len(rows) != 4 { // one PTT + one Select per radio
		t.Fatalf("expected 4 per-radio rows for 2 radios, got %d: %v", len(rows), rows)
	}
	if em.count(events.EventKeybindsChanged) <= before {
		t.Error("keybinds:changed must fire when the radio set becomes known")
	}
}

// TestUnrelatedRadioUpdateDoesNotReEmit: radio updates for OTHER clients flow
// through the same observer, and must not re-broadcast the whole keybind list.
func TestUnrelatedRadioUpdateDoesNotReEmit(t *testing.T) {
	st := state.New()
	a, em := newTestAppOn(t, st, &countingRegistrar{})
	st.SetSelf("me", &srspb.ClientInfo{Name: "me"})
	st.SetRadios("me", &srspb.RadioInfo{Radios: []*srspb.Radio{{Id: 1, Name: "GUARD"}}})

	before := em.count(events.EventKeybindsChanged)
	st.SetRadios("someone-else", &srspb.RadioInfo{Radios: []*srspb.Radio{{Id: 9, Name: "THEIRS"}}})

	if got := em.count(events.EventKeybindsChanged); got != before {
		t.Errorf("keybinds:changed fired %d times for another client's radios, want %d", got-before, 0)
	}
	_ = a
}

// perRadioRows filters a keybind list down to the per-radio category.
func perRadioRows(rows []KeybindDTO) []string {
	var out []string
	for _, r := range rows {
		if r.Category == "per_radio" {
			out = append(out, r.ActionID)
		}
	}
	return out
}

// TestBackendUnavailableReportsHotkeysUnavailable is the other half of I4:
// when NOTHING registers -- no OS backend, denied permission -- the banner
// must still fire, with a reason.
func TestBackendUnavailableReportsHotkeysUnavailable(t *testing.T) {
	em := &recordingEmitter{}
	a := NewForTest(state.New(), nil, nil)
	a.SetSettingsBackend(config.Default(), "", keybinds.New(), hotkeys.New(deadRegistrar{}, a), em)

	got := a.GetHotkeyState()
	if got.Registered {
		t.Error("Registered must be false when every binding failed to register")
	}
	if got.Error == "" {
		t.Error("Error must explain why hotkeys are unavailable when Registered is false")
	}
	if len(got.Failed) == 0 {
		t.Error("Failed must still name the individual bindings")
	}
}
