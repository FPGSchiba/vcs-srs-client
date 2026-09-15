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
)

type recordingEmitter struct {
	events []string
}

func (r *recordingEmitter) Emit(name string, _ any) { r.events = append(r.events, name) }

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
	if !contains(em.events, events.EventSettingsChanged) {
		t.Errorf("expected %s, got %v", events.EventSettingsChanged, em.events)
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
	if !contains(em.events, events.EventKeybindsChanged) {
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

	if err := a.BeginCapture(); err != nil {
		t.Fatalf("BeginCapture: %v", err)
	}
	if reg.unregisters <= before {
		t.Error("BeginCapture must unregister OS hotkeys")
	}
	if err := a.EndCapture(); err != nil {
		t.Fatalf("EndCapture: %v", err)
	}
}

func TestCaptureAutoResumesAfterTimeout(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.SetCaptureTimeout(50 * time.Millisecond)
	if err := a.BeginCapture(); err != nil {
		t.Fatal(err)
	}
	// Never call EndCapture — simulates the frontend dying mid-capture.
	time.Sleep(150 * time.Millisecond)
	if !a.hotkeysResumed() {
		t.Error("hotkeys must auto-resume if EndCapture never arrives")
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
	if contains(em.events, events.EventSettingsChanged) {
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
	if contains(em.events, events.EventKeybindsChanged) {
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
	count := 0
	for _, e := range em.events {
		if e == events.EventKeybindsChanged {
			count++
		}
	}
	if count != 1 {
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
	seedEvents := len(em.events)

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
	if len(em.events) != seedEvents {
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
	if got.Registered {
		t.Error("Registered should be false while a binding is failing")
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
