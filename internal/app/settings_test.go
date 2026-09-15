package app

import (
	"errors"
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
	em := &recordingEmitter{}
	reg := &countingRegistrar{}
	a := NewForTest(state.New(), nil, nil)
	cfg := config.Default()
	kb := keybinds.New()
	kb.Load(map[string]string{})
	hk := hotkeys.New(reg, a)
	a.SetSettingsBackend(cfg, "", kb, hk, em)
	return a, em, reg
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
