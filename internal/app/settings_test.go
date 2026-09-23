package app

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/joystick"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// fakeJoySource is a joystick.Source with one device and nothing held. Used
// by tests that need a real joystick.Manager without a real OS backend.
type fakeJoySource struct{}

func newFakeJoySource() *fakeJoySource { return &fakeJoySource{} }

func (f *fakeJoySource) Devices() ([]joystick.Device, error) {
	return []joystick.Device{{ID: "stick-c3", Name: "Fake Stick", Buttons: 16, Hats: 1}}, nil
}

func (f *fakeJoySource) Poll() (joystick.State, error) {
	return joystick.State{Held: map[trigger.JoyButton]struct{}{}}, nil
}

func (f *fakeJoySource) Close() {}

// controllableJoySource is a joystick.Source whose held-button state the test
// goroutine can flip at will, so a REAL joystick.Manager's own poll loop
// drives Pressed/Released edges deterministically instead of the test faking
// them directly.
type controllableJoySource struct {
	device joystick.Device

	mu   sync.Mutex
	held map[trigger.JoyButton]struct{}
}

func newControllableJoySource(deviceID trigger.DeviceID) *controllableJoySource {
	return &controllableJoySource{
		device: joystick.Device{ID: deviceID, Name: "Fake Stick", Buttons: 16, Hats: 1},
		held:   map[trigger.JoyButton]struct{}{},
	}
}

func (s *controllableJoySource) Devices() ([]joystick.Device, error) {
	return []joystick.Device{s.device}, nil
}

func (s *controllableJoySource) Poll() (joystick.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := make(map[trigger.JoyButton]struct{}, len(s.held))
	for b := range s.held {
		held[b] = struct{}{}
	}
	return joystick.State{Held: held}, nil
}

func (s *controllableJoySource) Close() {}

// setHeld sets whether b is held, as the NEXT Poll will report it.
func (s *controllableJoySource) setHeld(b trigger.JoyButton, held bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if held {
		s.held[b] = struct{}{}
	} else {
		delete(s.held, b)
	}
}

// waitUntil polls cond, sleeping briefly between attempts, until it returns
// true or timeout elapses. Used to synchronise with a real
// joystick.Manager's own poll-loop goroutine without a fixed sleep, so the
// assertion that follows is not a race against that goroutine's timing.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(time.Millisecond)
	}
}

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
	kb.Load(map[string][]string{})
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
	kb.Load(map[string][]string{})
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
	res, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})
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
		if k.ActionID == "global.ptt" && firstChord(k) != "F1" {
			t.Errorf("chord = %q, want F1", firstChord(k))
		}
	}
}

func TestSetKeybindReportsSteal(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.AddTrigger("global.mute_toggle", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatal(err)
	}
	res, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})
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
	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "ControlLeft", Ctrl: true}); err == nil {
		t.Error("a bare modifier must be rejected")
	}
	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "Nonsense"}); err == nil {
		t.Error("an unknown code must be rejected")
	}
}

func TestBeginCaptureSuspendsAndEndCaptureRestores(t *testing.T) {
	a, _, reg := newTestApp(t)
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})
	before := reg.unregisters

	token := a.BeginCapture("global.ptt")
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
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})

	first := a.BeginCapture("global.ptt")  // chip A starts listening
	second := a.BeginCapture("global.ptt") // user clicks chip B -- dispatched FIRST
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
	a.BeginCapture("global.ptt")
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

	first := a.BeginCapture("global.ptt") // generation 1, times out in 50ms

	a.setCaptureTimeout(10 * time.Second) // generation 2 gets a long timeout
	second := a.BeginCapture("global.ptt")

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
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})
	if err := a.ClearKeybind("global.ptt"); err != nil {
		t.Fatal(err)
	}
	for _, k := range a.GetKeybinds() {
		if k.ActionID == "global.ptt" && firstChord(k) != "" {
			t.Errorf("chord = %q, want empty after clear", firstChord(k))
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

	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err == nil {
		t.Fatal("expected SetKeybind to fail when the config path is unwritable")
	}
	for _, k := range a.GetKeybinds() {
		if k.ActionID == "global.ptt" && firstChord(k) != "" {
			t.Errorf("chord = %q, want empty after a failed SetKeybind", firstChord(k))
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
	if _, err := a.AddTrigger("global.mute_toggle", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("seed SetKeybind: %v", err)
	}

	// Break persistence by removing the directory config.toml lives in.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	// Attempt to steal F1 for global.ptt; the write must fail.
	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err == nil {
		t.Fatal("expected the steal to fail once the config directory is gone")
	}

	for _, k := range a.GetKeybinds() {
		switch k.ActionID {
		case "global.mute_toggle":
			if firstChord(k) != "F1" {
				t.Errorf("victim chord = %q, want F1 restored after the failed steal", firstChord(k))
			}
		case "global.ptt":
			if firstChord(k) != "" {
				t.Errorf("thief chord = %q, want empty after the failed steal", firstChord(k))
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

	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
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
		if k.ActionID == "global.ptt" && firstChord(k) != "F1" {
			t.Errorf("chord = %q, want F1 still present after a failed ClearKeybind", firstChord(k))
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
				_, _ = a.AddTrigger(id, CaptureDTO{Code: codes[i%len(codes)]})
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
		gc, ok := gotCfg[id]
		if !ok || len(gc) != len(c) {
			t.Errorf("cfg.Keybinds[%q] = %v, want %v (from the live store)", id, gc, c)
			continue
		}
		for i := range c {
			if gc[i] != c[i] {
				t.Errorf("cfg.Keybinds[%q][%d] = %q, want %q (from the live store)", id, i, gc[i], c[i])
			}
		}
	}
}

func TestGetHotkeyStateReportsPerActionFailures(t *testing.T) {
	em := &recordingEmitter{}
	reg := &failingRegistrar{failActionID: "global.ptt"}
	a := NewForTest(state.New(), nil, nil)
	cfg := config.Default()
	kb := keybinds.New()
	kb.Load(map[string][]string{})
	hk := hotkeys.New(reg, a)
	a.SetSettingsBackend(cfg, "", kb, hk, em)

	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
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

// firstChord renders the canonical chord of k's single keyboard trigger, or
// "" if k has no triggers. Existing tests were written against the old
// KeybindDTO.Chord field (one action, one chord); this keeps them exercising
// the same "is a keyboard chord bound" assertion against the new triggers
// list without weakening what they check.
func firstChord(k KeybindDTO) string {
	if len(k.Triggers) == 0 {
		return ""
	}
	return k.Triggers[0].Chord
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
	kb.Load(map[string][]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(&failingRegistrar{failActionID: "global.ptt"}, a), em)

	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("SetKeybind(global.ptt): %v", err)
	}
	if _, err := a.AddTrigger("global.mute_toggle", CaptureDTO{Code: "F2"}); err != nil {
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
	kb.Load(map[string][]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(&failingRegistrar{failActionID: "global.ptt"}, a), em)

	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
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
	cfg.Keybinds = map[string]config.KeybindValue{"global.ptt": {"F1"}}
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
	kb.Load(map[string][]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(&failingRegistrar{failActionID: "global.ptt"}, a), em)

	token := a.BeginCapture("global.ptt")
	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
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

// TestHotkeyEdgesAreLogged pins Change A: every hotkey edge is written to the
// app log with its action ID and direction, so a user can confirm hotkeys
// fire from the log file alone with no UI involved.
//
// It also pins the privacy boundary: the record must name the ACTION, never
// the key. The OS layer sees every keystroke on the machine, so a key
// identity in this line would make the log a keylog.
func TestHotkeyEdgesAreLogged(t *testing.T) {
	a, _, _ := newTestApp(t)

	var buf bytes.Buffer
	a.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	a.Pressed("global.ptt")
	a.Released("global.ptt")

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], `action=global.ptt`) || !strings.Contains(lines[0], `edge=down`) {
		t.Errorf("press line missing action/edge: %q", lines[0])
	}
	if !strings.Contains(lines[1], `action=global.ptt`) || !strings.Contains(lines[1], `edge=up`) {
		t.Errorf("release line missing action/edge: %q", lines[1])
	}
	for _, l := range lines {
		if !strings.Contains(l, "level=INFO") {
			t.Errorf("hotkey edge should log at Info, got: %q", l)
		}
	}
}

// TestHotkeyEdgesWithoutBackendDoNotPanic: Pressed/Released can arrive before
// SetSettingsBackend has run (the OS stream is global and outlives a rebind),
// and must be a silent no-op rather than a nil dereference.
func TestHotkeyEdgesWithoutBackendDoNotPanic(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	var buf bytes.Buffer
	a.logger = slog.New(slog.NewTextHandler(&buf, nil))

	a.Pressed("global.ptt")
	a.Released("global.ptt")

	if buf.Len() != 0 {
		t.Errorf("no backend wired, so nothing should be logged; got %q", buf.String())
	}
}

// TestForceReleasedLogsAtWarn pins the stale-latch watchdog's annotation.
//
// Warn, not Info: an ordinary hotkey edge is routine, but this one says a
// release was LOST upstream and the user's microphone was open for the whole
// timeout. Same privacy rule as everywhere else -- action ID, never a key.
func TestForceReleasedLogsAtWarn(t *testing.T) {
	a, _, _ := newTestApp(t)

	var buf bytes.Buffer
	a.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	a.ForceReleased("global.ptt")

	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("a forced release must log at Warn, got: %q", out)
	}
	if !strings.Contains(out, "action=global.ptt") {
		t.Errorf("log line missing the action ID: %q", out)
	}
	if strings.Count(out, "\n") != 0 {
		t.Errorf("expected exactly one log line, got: %q", out)
	}
}

// TestForceReleasedIsAStaleReleaser: the OS layer only calls this through the
// optional hotkeys.StaleReleaser interface, so App failing to satisfy it would
// silently disable the Warn with nothing failing to compile.
func TestForceReleasedIsAStaleReleaser(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, ok := any(a).(hotkeys.StaleReleaser); !ok {
		t.Fatal("*App must implement hotkeys.StaleReleaser, or forced releases " +
			"are never annotated in the log")
	}
}

// TestForceReleasedWithoutBackendDoesNotPanic: the watchdog fires from a timer
// goroutine and can outlive teardown.
func TestForceReleasedWithoutBackendDoesNotPanic(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	var buf bytes.Buffer
	a.logger = slog.New(slog.NewTextHandler(&buf, nil))

	a.ForceReleased("global.ptt")

	if buf.Len() != 0 {
		t.Errorf("no backend wired, so nothing should be logged; got %q", buf.String())
	}
}

func TestGetKeybindsReturnsAllTriggers(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("AddTrigger: %v", err)
	}
	rows := a.GetKeybinds()
	var got *KeybindDTO
	for i := range rows {
		if rows[i].ActionID == "global.ptt" {
			got = &rows[i]
		}
	}
	if got == nil {
		t.Fatal("global.ptt missing from GetKeybinds")
	}
	if len(got.Triggers) != 1 {
		t.Fatalf("Triggers = %+v, want one", got.Triggers)
	}
	if got.Triggers[0].Kind != "key" || got.Triggers[0].Chord != "F1" {
		t.Errorf("trigger = %+v, want key/F1", got.Triggers[0])
	}
}

func TestRemoveTriggerDropsOnlyThatOne(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})
	a.settings.kb.Add("global.ptt", trigger.Joy(trigger.JoyBinding{Device: "stick-c3", Button: 11}))

	if err := a.RemoveTrigger("global.ptt", 0); err != nil {
		t.Fatalf("RemoveTrigger: %v", err)
	}
	list, _ := a.settings.kb.Get("global.ptt")
	if len(list) != 1 || list[0].Kind != trigger.KindJoy {
		t.Errorf("after RemoveTrigger = %+v, want the joystick trigger only", list)
	}
}

func TestRemoveTriggerOutOfRangeErrors(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})
	if err := a.RemoveTrigger("global.ptt", 5); err == nil {
		t.Error("RemoveTrigger(5) = nil error, want out-of-range failure")
	}
}

func TestHoldActionHeldByTwoSourcesEmitsOneEdgePair(t *testing.T) {
	// The PTT-cut defect, end to end through App.Pressed/Released.
	a, em, _ := newTestApp(t)
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}) // global.ptt is KindHold

	a.Pressed("global.ptt")  // keyboard
	a.Pressed("global.ptt")  // joystick
	a.Released("global.ptt") // keyboard lets go; joystick still held

	if n := em.count(events.EventHotkeyPressed); n != 1 {
		t.Errorf("HotkeyPressed emitted %d times, want 1", n)
	}
	if n := em.count(events.EventHotkeyReleased); n != 0 {
		t.Errorf("HotkeyReleased emitted while the joystick still held it (%d times)", n)
	}

	a.Released("global.ptt") // joystick lets go
	if n := em.count(events.EventHotkeyReleased); n != 1 {
		t.Errorf("HotkeyReleased emitted %d times, want 1", n)
	}
}

func TestPressKindActionIsNotRefcounted(t *testing.T) {
	// global.mute_toggle is KindPress and never receives a Released, so
	// refcounting it would silence every press after the first.
	a, em, _ := newTestApp(t)
	a.Pressed("global.mute_toggle")
	a.Pressed("global.mute_toggle")
	if n := em.count(events.EventHotkeyPressed); n != 2 {
		t.Errorf("press-kind action emitted %d times, want 2", n)
	}
}

func TestBeginCaptureSuspendsBothManagers(t *testing.T) {
	a, _, _ := newTestApp(t)
	joySrc := newFakeJoySource()
	jm := joystick.New(joySrc, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	a.SetJoystickBackend(jm)

	token := a.BeginCapture("global.ptt")
	if token == 0 {
		t.Fatal("BeginCapture returned no token")
	}
	if !a.settings.hk.Suspended() {
		t.Error("keyboard manager not suspended during capture")
	}
	if !jm.IsSuspendedForTest() {
		t.Error("joystick manager not suspended during capture")
	}

	a.EndCapture(token)
	if jm.IsSuspendedForTest() {
		t.Error("joystick manager still suspended after EndCapture")
	}
}

// TestApplyHotkeysDuringRebindDoesNotSwallowARelease is the regression guard
// for the applyHotkeys reset-ordering bug: sb.presses.reset() used to run
// BEFORE sb.hk.Apply/jm.Apply, so the synchronous Released those calls emit
// for EVERY currently-held action -- not just the one whose binding actually
// changed, see joystick.Manager.Apply's takeActiveLocked and
// hotkeys/dispatch.go's clear() -- landed on an already-zeroed refcount and
// was swallowed by presses.release(). The next poll tick then saw the button
// still physically down and fired a second, UNPAIRED HotkeyPressed:
// "doubled Pressed, dropped Released".
//
// global.ptt is bound to a joystick button and held continuously. An
// UNRELATED action (global.mute_toggle) is then bound, which reapplies both
// managers and, as a side effect, releases and re-presses global.ptt even
// though nothing about ITS OWN binding changed. Correct behaviour after the
// fix is noisier than ideal but self-consistent: Pressed, Released (from the
// reapply), Pressed again (the next tick re-observes the still-held button)
// -- exactly one more Pressed than Released, never two Presseds in a row
// with no Released between them.
func TestApplyHotkeysDuringRebindDoesNotSwallowARelease(t *testing.T) {
	a, em, _ := newTestApp(t)

	src := newControllableJoySource("stick-c3")
	jm := joystick.New(src, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	jm.PollInterval = 2 * time.Millisecond
	a.SetJoystickBackend(jm)
	defer jm.Close()

	btn := trigger.JoyButton{Device: "stick-c3", Button: 0}
	a.settings.kb.Add(keybinds.ActionID("global.ptt"), trigger.Joy(trigger.JoyBinding{Device: btn.Device, Button: btn.Button}))
	a.applyHotkeys()

	src.setHeld(btn, true)
	waitUntil(t, time.Second, func() bool { return em.count(events.EventHotkeyPressed) >= 1 })

	if _, err := a.AddTrigger("global.mute_toggle", CaptureDTO{Code: "F2"}); err != nil {
		t.Fatalf("AddTrigger: %v", err)
	}

	// The reapply's Released is synchronous with AddTrigger; this waits for
	// the FOLLOWING re-press, which only the poll loop can produce, so the
	// assertion below is not racing that goroutine.
	waitUntil(t, time.Second, func() bool { return em.count(events.EventHotkeyPressed) >= 2 })
	time.Sleep(20 * time.Millisecond) // settle: nothing further should fire while still held

	pressed := em.count(events.EventHotkeyPressed)
	released := em.count(events.EventHotkeyReleased)
	if pressed != released+1 {
		t.Errorf("Pressed=%d Released=%d after an unrelated rebind with the button still held; "+
			"want Pressed == Released+1 (down, up from the reapply, down again) -- "+
			"got an unpaired press or a swallowed release", pressed, released)
	}
}

// TestApplyHotkeysDuringRebindDoesNotStrandAHeldAction is the "permanently
// stuck pressed" half of the same regression. If the physical release lands
// in the SAME window as an unrelated rebind -- after Apply has already
// cleared the manager's own active-tracking for the action, before the next
// poll tick can sample the now-released button -- the old reset-before-Apply
// ordering left NOTHING able to ever emit the matching Released:
// HotkeyPressed=1, HotkeyReleased=0, forever, with the user's PTT/mic
// indicator believing the action is still held.
func TestApplyHotkeysDuringRebindDoesNotStrandAHeldAction(t *testing.T) {
	a, em, _ := newTestApp(t)

	src := newControllableJoySource("stick-c3")
	jm := joystick.New(src, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	jm.PollInterval = 2 * time.Millisecond
	a.SetJoystickBackend(jm)
	defer jm.Close()

	btn := trigger.JoyButton{Device: "stick-c3", Button: 0}
	a.settings.kb.Add(keybinds.ActionID("global.ptt"), trigger.Joy(trigger.JoyBinding{Device: btn.Device, Button: btn.Button}))
	a.applyHotkeys()

	src.setHeld(btn, true)
	waitUntil(t, time.Second, func() bool { return em.count(events.EventHotkeyPressed) >= 1 })

	if _, err := a.AddTrigger("global.mute_toggle", CaptureDTO{Code: "F2"}); err != nil {
		t.Fatalf("AddTrigger: %v", err)
	}
	// The physical release lands immediately after the rebind's synchronous
	// Apply, before the next poll tick can observe it -- the exact window
	// the bug depended on: Apply's own active-tracking is already cleared by
	// the time this runs, so a tick that sampled the release itself would
	// find nothing to release either.
	src.setHeld(btn, false)

	waitUntil(t, 500*time.Millisecond, func() bool {
		return em.count(events.EventHotkeyPressed) == em.count(events.EventHotkeyReleased)
	})
	time.Sleep(20 * time.Millisecond) // settle: no further edges should follow

	pressed := em.count(events.EventHotkeyPressed)
	released := em.count(events.EventHotkeyReleased)
	if pressed != released {
		t.Errorf("Pressed=%d Released=%d after releasing during the rebind window; "+
			"want them equal -- the action must not be left permanently \"held\"", pressed, released)
	}
}
