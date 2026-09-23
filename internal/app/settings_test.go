package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
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
	return []joystick.Device{{ID: "stick-c3", Name: "Fake Stick"}}, nil
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

	mu    sync.Mutex
	held  map[trigger.JoyButton]struct{}
	polls int
}

func newControllableJoySource(deviceID trigger.DeviceID) *controllableJoySource {
	return &controllableJoySource{
		device: joystick.Device{ID: deviceID, Name: "Fake Stick"},
		held:   map[trigger.JoyButton]struct{}{},
	}
}

func (s *controllableJoySource) Devices() ([]joystick.Device, error) {
	return []joystick.Device{s.device}, nil
}

func (s *controllableJoySource) Poll() (joystick.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	held := make(map[trigger.JoyButton]struct{}, len(s.held))
	for b := range s.held {
		held[b] = struct{}{}
	}
	return joystick.State{Held: held}, nil
}

func (s *controllableJoySource) Close() {}

// pollCount reports how many times the manager has sampled this source. It is
// how a test waits for the poll LOOP itself rather than for one of its
// downstream effects -- which matters when the bug under test is that the
// loop stops sampling at all.
func (s *controllableJoySource) pollCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.polls
}

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

// payloadsFor returns the payloads of every event emitted under name, in
// order.
func (r *recordingEmitter) payloadsFor(name string) []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []any
	for i, e := range r.events {
		if e == name {
			out = append(out, r.payloads[i])
		}
	}
	return out
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

// latchingRegistrar is the only fake in this file that reproduces the ONE
// behaviour of the real OS registrar that matters to the app layer's locking:
// UnregisterAll() releases every latched HOLD action SYNCHRONOUSLY, on the
// caller's goroutine, before it returns. That is exactly what
// registrar_gohook.go does -- UnregisterAll -> dispatcher.clear() ->
// drainLatchedLocked() -> h.Released(id) -- and it is the reason
// hotkeys.Manager.Suspend()/Apply() can re-enter App.Released from inside the
// call.
//
// countingRegistrar, failingRegistrar and deadRegistrar all implement
// UnregisterAll as a no-op, so every test built on them is structurally blind
// to that re-entrancy: App.BeginCapture could (and did) call hk.Suspend()
// while holding sb.mu, self-deadlock against isHold()'s sb.mu.Lock(), and
// still pass the entire suite. Keep this fake, and keep using it for any test
// that exercises a suspend/apply path with an action HELD.
type latchingRegistrar struct {
	mu      sync.Mutex
	h       hotkeys.Handler
	hold    map[string]bool
	latched map[string]bool
}

func newLatchingRegistrar() *latchingRegistrar {
	return &latchingRegistrar{hold: map[string]bool{}, latched: map[string]bool{}}
}

func (r *latchingRegistrar) Register(actionID string, _ chord.Chord, hold bool, h hotkeys.Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.h = h
	r.hold[actionID] = hold
	return nil
}

// UnregisterAll mirrors dispatcher.clear(): drop the registrations AND
// release whatever is latched, outside the fake's own lock, on this
// goroutine.
func (r *latchingRegistrar) UnregisterAll() {
	r.mu.Lock()
	h := r.h
	release := make([]string, 0, len(r.latched))
	for id := range r.latched {
		release = append(release, id)
	}
	r.latched = map[string]bool{}
	r.hold = map[string]bool{}
	r.mu.Unlock()

	for _, id := range release {
		if h != nil {
			h.Released(id)
		}
	}
}

// pressForTest latches actionID and drives Pressed, the way a real key-down
// through the dispatcher does. Only HOLD actions latch, matching
// dispatcher.keyDown.
func (r *latchingRegistrar) pressForTest(actionID string) {
	r.mu.Lock()
	h := r.h
	if r.hold[actionID] {
		r.latched[actionID] = true
	}
	r.mu.Unlock()
	if h != nil {
		h.Pressed(actionID)
	}
}

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

// TestGetSettingsReflectsAudioDefaults proves SettingsDTO.Audio carries
// config.Default()'s Audio table, not a zero value.
func TestGetSettingsReflectsAudioDefaults(t *testing.T) {
	a, _, _ := newTestApp(t)
	s := a.GetSettings()
	if !s.Audio.AGC || s.Audio.Levels.Master != 0.75 {
		t.Fatalf("audio defaults missing from SettingsDTO: %+v", s.Audio)
	}
}

// TestSetSettingsPersistsAudio proves the audio fields ride the SAME
// copy-persist-swap as General -- not a second, parallel persistence path.
func TestSetSettingsPersistsAudio(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	a, _, _ := newTestAppWithPath(t, cfgPath)

	s := a.GetSettings()
	s.Audio.VOX = true
	s.Audio.Levels.SFX = 0.25
	s.Audio.InputDevice = "mic-9"
	if err := a.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Audio.VOX || cfg.Audio.Levels.SFX != 0.25 || cfg.Audio.InputDevice != "mic-9" {
		t.Fatalf("audio settings not persisted: %+v", cfg.Audio)
	}
}

// TestSetSettingsWithAWiredManagerDoesNotDeadlock proves SetSettings's
// mgr.SetConfig push happens after sb.mu.Unlock() as documented, not while
// holding it: Manager.SetConfig only takes Manager's own lock-free atomic
// store, so nesting the two would still not self-deadlock on its own, but
// this pins the ordering down directly rather than relying on that being a
// coincidence, and gives a fast, deterministic failure (a hung goroutine)
// instead of trusting go test's multi-minute default timeout to eventually
// notice.
func TestSetSettingsWithAWiredManagerDoesNotDeadlock(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := audio.NewManager(audio.NewFakeBackend(), audio.ManagerOptions{
		PollInterval: time.Hour, VUInterval: time.Hour,
	})
	t.Cleanup(m.Stop)
	a.SetAudioBackend(m)

	s := a.GetSettings()
	s.Audio.Levels.Master = 0.42

	done := make(chan error, 1)
	go func() { done <- a.SetSettings(s) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetSettings: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SetSettings did not return within 2s with an audio backend wired -- suspect a lock ordering deadlock against Manager.SetConfig")
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

// TestCaptureTimeoutTellsTheFrontendItsCaptureDied is the JOYSTICK half of
// the auto-resume timeout, which TestCaptureAutoResumesAfterTimeout above
// says nothing about.
//
// The timeout exists as a crashed-frontend safety net, but a LIVE frontend
// has to be told too, and it used to be told nothing: capturingId stayed set,
// the KeyChip stayed mounted, and the row went on saying "Press a key or
// joystick button -- hold a second button first for a modifier" over a
// capture that no longer existed with BOTH managers resumed.
//
// The keyboard half was self-recovering -- a keypress after the timeout still
// reached AddTrigger and still bound -- which is why this only became
// reachable once jm.BeginCapture was armed under the same inherited capture
// budget (defaultCaptureTimeout): a joystick capture completes INSIDE the manager and is gone once
// cancelled. So the scenario this drives is the destructive one: global.ptt
// already holds the button, the user spends longer than the timeout hunting
// for it on a 30-button throttle (which the modifier prompt actively
// encourages), and the press that follows binds NOTHING and instead keys the
// radio.
func TestCaptureTimeoutTellsTheFrontendItsCaptureDied(t *testing.T) {
	a, em, _ := newTestApp(t)

	src := newControllableJoySource("stick-c3")
	jm := joystick.New(src, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	jm.PollInterval = 2 * time.Millisecond
	a.SetJoystickBackend(jm)
	defer jm.Close()

	btn := trigger.JoyButton{Device: "stick-c3", Button: 5}
	a.settings.kb.Add(keybinds.ActionID("global.ptt"),
		trigger.Joy(trigger.JoyBinding{Device: btn.Device, Button: btn.Button}))
	a.applyHotkeys()

	a.setCaptureTimeout(30 * time.Millisecond)
	a.BeginCapture("global.push_to_mute")
	// Never call EndCapture: the user is still hunting for the button.
	waitUntil(t, time.Second, func() bool { return a.hotkeysResumed() })

	// The row must have been told, and told WHICH row.
	expired := em.payloadsFor(events.EventCaptureExpired)
	if len(expired) != 1 {
		t.Fatalf("got %d %s events, want exactly 1 -- a live frontend that is not told "+
			"keeps rendering a listening chip over a capture the backend already tore down",
			len(expired), events.EventCaptureExpired)
	}
	p, ok := expired[0].(events.CaptureExpiredPayload)
	if !ok {
		t.Fatalf("%s payload has type %T, want events.CaptureExpiredPayload",
			events.EventCaptureExpired, expired[0])
	}
	if p.ActionID != "global.push_to_mute" {
		t.Errorf("capture-expired action_id = %q, want %q", p.ActionID, "global.push_to_mute")
	}

	// And the capture really is gone on the joystick side: the press that
	// follows binds nothing and fires the action that already holds the
	// button.
	src.setHeld(btn, true)
	waitUntil(t, time.Second, func() bool { return em.count(events.EventHotkeyPressed) >= 1 })
	src.setHeld(btn, false)
	waitUntil(t, time.Second, func() bool { return em.count(events.EventHotkeyReleased) >= 1 })

	got, _ := a.settings.kb.Get(keybinds.ActionID("global.push_to_mute"))
	for _, tr := range got {
		if tr.Kind == trigger.KindJoy {
			t.Errorf("global.push_to_mute gained joystick trigger %s after the capture expired; "+
				"an expired capture must bind nothing", tr.String())
		}
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
	a.resumeCapture(first, true)
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
	// SettingsDTO now carries Audio.Effects (a map), so it is no longer
	// comparable with == -- reflect.DeepEqual is the correct replacement,
	// not a weaker check: it still walks every field, map included.
	if got := a.GetSettings(); !reflect.DeepEqual(got, before) {
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

// TestBeginCaptureDoesNotDeadlockWhileAHotkeyIsHeld is the C1 guard.
//
// BeginCapture used to call sb.hk.Suspend() while holding sb.mu. Suspend
// reaches registrar.UnregisterAll(), which -- in the REAL registrar, and in
// latchingRegistrar above -- synchronously calls Handler.Released for every
// latched hold action. App.Released asks isHold(), which takes sb.mu. sb.mu
// is not reentrant, so the whole app hung on the caller's goroutine with the
// microphone still open. Go's deadlock detector never fires for it because
// the other goroutines stay runnable.
//
// The timeout is what makes this a FAILING test rather than a hanging suite:
// BeginCapture runs on its own goroutine and the test fails if it has not
// returned. jm.Suspend() was already hoisted out of sb.mu for precisely this
// hazard; the keyboard manager one line above it was not.
func TestBeginCaptureDoesNotDeadlockWhileAHotkeyIsHeld(t *testing.T) {
	em := &recordingEmitter{}
	a := NewForTest(state.New(), nil, nil)
	reg := newLatchingRegistrar()
	kb := keybinds.New()
	kb.Load(map[string][]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(reg, a), em)

	c, err := chord.FromCode("F8", false, false, false, false)
	if err != nil {
		t.Fatalf("chord.FromCode: %v", err)
	}
	a.settings.kb.Add(keybinds.ActionID("global.ptt"), trigger.Key(c))
	a.applyHotkeys()

	// global.ptt is a HOLD action, so this leaves it latched in the
	// registrar: the next UnregisterAll owes it a Released.
	reg.pressForTest("global.ptt")

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.BeginCapture("global.ptt")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("BeginCapture did not return within 5s: sb.hk.Suspend() is being " +
			"called while sb.mu is held, and the synchronous Released it triggers " +
			"self-deadlocks on isHold()")
	}

	if em.count(events.EventHotkeyPressed) != 1 || em.count(events.EventHotkeyReleased) != 1 {
		t.Errorf("Pressed=%d Released=%d across the suspend; want 1 and 1 -- "+
			"the held action must be released exactly once when registrations drop",
			em.count(events.EventHotkeyPressed), em.count(events.EventHotkeyReleased))
	}
}

// TestApplyHotkeysBalancesEdgesWhenAPerRadioActionDisappears is the I1 guard.
//
// sb.holds used to be assigned BEFORE both Apply calls, so the synchronous
// Released callbacks those Applies emit were judged against the NEW holds
// map. When the server removes a radio while its PTT is held,
// isHold("radio.3.ptt") is false by the time the release arrives,
// presses.release() is never called, and the refcount leaks at 1 -- which
// deadens the action for the rest of the session. That leak used to be swept
// by a blanket sb.presses.reset() after the Applies, and the sweep itself was
// the residual stuck-PTT race: a 100Hz tick landing between jm.Apply's return
// and reset() re-pressed the still-held button, reset() then zeroed the count
// underneath it, and the physical release was swallowed with the transmission
// stuck open.
//
// With sb.holds assigned AFTER both Applies, each manager balances its own
// edges against the map that was live when the action was pressed, and there
// is nothing left for reset() to sweep. joystick.Manager.Apply already orders
// itself this way (takeActiveLocked before m.hold = hold).
//
// The action is re-bound and pressed a SECOND time at the end because that is
// the only externally visible symptom of a leaked count: a leak of 1 makes the
// next genuine press a no-op.
func TestApplyHotkeysBalancesEdgesWhenAPerRadioActionDisappears(t *testing.T) {
	st := state.New()
	a, em := newTestAppOn(t, st, &countingRegistrar{})

	radios := func(ids ...uint32) *srspb.RadioInfo {
		out := &srspb.RadioInfo{}
		for _, id := range ids {
			out.Radios = append(out.Radios, &srspb.Radio{Id: id, Name: "RADIO"})
		}
		return out
	}
	st.SetRadios("me", radios(1, 3))
	st.SetSelf("me", &srspb.ClientInfo{Name: "me"})

	src := newControllableJoySource("stick-c3")
	jm := joystick.New(src, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	jm.PollInterval = 2 * time.Millisecond
	a.SetJoystickBackend(jm)
	defer jm.Close()

	btn := trigger.JoyButton{Device: "stick-c3", Button: 0}
	a.settings.kb.Add(keybinds.ActionID("radio.3.ptt"),
		trigger.Joy(trigger.JoyBinding{Device: btn.Device, Button: btn.Button}))
	a.applyHotkeys()

	src.setHeld(btn, true)
	waitUntil(t, time.Second, func() bool { return em.count(events.EventHotkeyPressed) >= 1 })

	// The server drops radio 3 while its PTT is still physically held. The
	// action vanishes from the registry, so the new holds map does not
	// contain it at all.
	st.SetRadios("me", radios(1))

	waitUntil(t, time.Second, func() bool {
		return em.count(events.EventHotkeyReleased) == em.count(events.EventHotkeyPressed)
	})
	time.Sleep(20 * time.Millisecond) // settle: the action is gone, nothing more may fire
	if p, r := em.count(events.EventHotkeyPressed), em.count(events.EventHotkeyReleased); p != r {
		t.Fatalf("Pressed=%d Released=%d after the bound radio disappeared mid-hold; want them equal", p, r)
	}

	// Let go, give the radio back, and press again. A leaked refcount is
	// invisible until exactly here: press() would see 1 -> 2 and swallow the
	// edge, leaving the user's PTT permanently dead.
	src.setHeld(btn, false)
	time.Sleep(20 * time.Millisecond)
	st.SetRadios("me", radios(1, 3))
	src.setHeld(btn, true)

	waitUntil(t, time.Second, func() bool { return em.count(events.EventHotkeyPressed) >= 2 })
}

// deniedJoySource is a Source whose enumeration fails the way the Linux
// backend's does for a user who is not in the 'input' group: a real,
// actionable error that is NOT ErrUnsupported.
type deniedJoySource struct{}

func (deniedJoySource) Devices() ([]joystick.Device, error) {
	return nil, errors.New("joystick: cannot read /dev/input (add your user to the 'input' group, then log out and back in)")
}
func (deniedJoySource) Poll() (joystick.State, error) { return joystick.State{}, errors.New("denied") }
func (deniedJoySource) Close()                        {}

// unsupportedJoySource is the macOS/other-platform stub: no backend exists,
// and there is nothing the user can do about it.
type unsupportedJoySource struct{}

func (unsupportedJoySource) Devices() ([]joystick.Device, error) { return nil, joystick.ErrUnsupported }
func (unsupportedJoySource) Poll() (joystick.State, error) {
	return joystick.State{}, joystick.ErrUnsupported
}
func (unsupportedJoySource) Close() {}

// TestJoystickStateDistinguishesDeniedFromUnsupported is the I3 guard.
//
// A permission denial and "this platform has no backend" used to arrive at
// the UI as the same {Supported:false, Error:""}: NewOSSource returned the
// permission error, main.go logged it and never built the manager, so sb.joy
// stayed nil. Keybinds.tsx gates its banner on supported && error, so the
// carefully-worded "add your user to the 'input' group" message could not
// reach the user by any path -- spec section 8 unmet end to end, and section
// 11's "unsupported is informational, not an error" collapsed into it.
//
// Denied must be Supported:true with a non-empty Error (the banner fires);
// unsupported must be Supported:false with no Error (the affordance is
// hidden and nothing is reported as broken).
func TestJoystickStateDistinguishesDeniedFromUnsupported(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("denied", func(t *testing.T) {
		a, _, _ := newTestApp(t)
		jm := joystick.New(deniedJoySource{}, a, discard)
		a.SetJoystickBackend(jm)
		defer jm.Close()

		got := a.GetJoystickState()
		if !got.Supported {
			t.Error("Supported must stay true for a permission denial: the user CAN fix it, " +
				"and the UI must offer the banner rather than hiding the whole feature")
		}
		if got.Error == "" {
			t.Fatal("Error must carry the actionable message for a permission denial")
		}
		if !strings.Contains(got.Error, "input") {
			t.Errorf("Error = %q, want it to name the 'input' group remedy", got.Error)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		a, _, _ := newTestApp(t)
		jm := joystick.New(unsupportedJoySource{}, a, discard)
		a.SetJoystickBackend(jm)
		defer jm.Close()

		got := a.GetJoystickState()
		if got.Supported {
			t.Error("Supported must be false when the platform has no backend")
		}
		if got.Error != "" {
			t.Errorf("Error = %q, want empty: unsupported is informational, not a failure "+
				"the user can act on", got.Error)
		}
	})
}

// joyTriggerCount reports how many KindJoy triggers actionID currently has in
// the keybind store.
func joyTriggerCount(a *App, actionID string) int {
	list, ok := a.settings.kb.Get(keybinds.ActionID(actionID))
	if !ok {
		return 0
	}
	n := 0
	for _, tr := range list {
		if tr.Kind == trigger.KindJoy {
			n++
		}
	}
	return n
}

// TestJoystickCaptureBindsEndToEnd is the C1 guard, and it is deliberately an
// APP-level test rather than a joystick-package one: both halves were
// individually correct and the feature was still unreachable, because nothing
// drove the real sequence App.BeginCapture -> Manager.tick -> feedCapture ->
// onJoystickCaptured across the seam between them.
//
// App.BeginCapture calls jm.Suspend() and then jm.BeginCapture(). tick() used
// to return on m.suspended BEFORE it ever called Source.Poll(), so with a
// capture armed the poll loop went silent: feedCapture never ran, the
// completion callback never fired, and pressing a HOTAS button in
// Settings -> Keybinds bound nothing and logged nothing. Only config.toml
// editing could produce a joystick binding at all.
//
// The first wait is what pins the actual defect: it asserts the source is
// still being SAMPLED while a capture is armed. The binding assertion after it
// is the user-visible consequence.
func TestJoystickCaptureBindsEndToEnd(t *testing.T) {
	a, _, _ := newTestApp(t)

	src := newControllableJoySource("stick-c3")
	jm := joystick.New(src, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	jm.PollInterval = 2 * time.Millisecond
	a.SetJoystickBackend(jm)
	defer jm.Close()

	if token := a.BeginCapture("global.ptt"); token == 0 {
		t.Fatal("BeginCapture returned no token")
	}

	// One full poll after arming establishes the capture baseline (capture
	// never touches Source itself -- see joystick.BeginCapture), so the press
	// below cannot be mistaken for a button that was already down.
	base := src.pollCount()
	waitFor(t, 2*time.Second,
		"the poll loop stopped sampling the source while a capture was armed: "+
			"tick() returns on m.suspended before Source.Poll(), so feedCapture can never run "+
			"and a joystick binding can never be captured from the UI at all",
		func() bool { return src.pollCount() > base+1 })

	btn := trigger.JoyButton{Device: "stick-c3", Button: 4}
	src.setHeld(btn, true)
	held := src.pollCount()
	waitFor(t, 2*time.Second, "the poll loop stopped sampling while the button was held",
		func() bool { return src.pollCount() > held+1 })
	src.setHeld(btn, false)

	waitFor(t, 2*time.Second,
		"global.ptt gained no joystick trigger after a full press/release during capture",
		func() bool { return joyTriggerCount(a, "global.ptt") == 1 })
}

// probingRegistrar runs a probe from inside Register -- that is, from inside
// sb.hk.Apply, which is the window applyHotkeys opens between computing the
// new holds map and installing it. It is the only deterministic seam into
// that window, which is otherwise microseconds wide.
type probingRegistrar struct {
	mu    sync.Mutex
	probe func()
}

func (p *probingRegistrar) setProbe(fn func()) {
	p.mu.Lock()
	p.probe = fn
	p.mu.Unlock()
}

func (p *probingRegistrar) Register(string, chord.Chord, bool, hotkeys.Handler) error {
	p.mu.Lock()
	probe := p.probe
	p.mu.Unlock()
	if probe != nil {
		probe()
	}
	return nil
}

func (p *probingRegistrar) UnregisterAll() {}

// TestApplyHotkeysTreatsANewHoldActionAsHoldInsideTheApplyWindow is the M1
// guard.
//
// sb.holds is assigned AFTER both Apply calls, which is what keeps a
// DISAPPEARING action's release judged against the map that was live when it
// was pressed. The symmetric case is an action APPEARING in this apply -- a
// per-radio action for a radio the server just added, already bound in
// config.toml -- that gets pressed inside the same window: isHold() then
// reads the OLD map, returns false, skips presses.press(), and the eventual
// Released hits presses.release() with n <= 0 and is swallowed. Stuck
// transmission, the exact failure mode the previous wave removed on the
// other side.
//
// The fix is to install the UNION of old and new holds before the Applies and
// the new map after, so both directions are covered. This pins it by probing
// isHold from inside sb.hk.Apply.
func TestApplyHotkeysTreatsANewHoldActionAsHoldInsideTheApplyWindow(t *testing.T) {
	st := state.New()
	reg := &probingRegistrar{}
	a, _ := newTestAppOn(t, st, reg)

	c, err := chord.FromCode("F7", false, false, false, false)
	if err != nil {
		t.Fatalf("chord.FromCode: %v", err)
	}
	// Bound on disk before the radio exists -- exactly what loading
	// config.toml at startup produces.
	a.settings.kb.Add(keybinds.ActionID("radio.3.ptt"), trigger.Key(c))

	var mu sync.Mutex
	var seen []bool
	reg.setProbe(func() {
		mu.Lock()
		seen = append(seen, a.isHold("radio.3.ptt"))
		mu.Unlock()
	})

	st.SetSelf("me", &srspb.ClientInfo{Name: "me"})
	st.SetRadios("me", &srspb.RadioInfo{Radios: []*srspb.Radio{{Id: 3, Name: "RADIO"}}})

	waitFor(t, time.Second, "radio.3.ptt never reached the registrar", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) > 0
	})

	mu.Lock()
	defer mu.Unlock()
	for i, hold := range seen {
		if !hold {
			t.Fatalf("isHold(radio.3.ptt) = false on probe %d, inside the Apply window; "+
				"a press landing here skips presses.press() and its Released is later "+
				"swallowed with n <= 0 -- a stuck transmission", i)
		}
	}
}

// lastJoystickCaptured returns the payload of the most recent
// keybinds:joy_captured event, and reports whether there was one.
func (r *recordingEmitter) lastJoystickCaptured() (events.JoystickCapturedPayload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i] != events.EventJoystickCaptured {
			continue
		}
		p, ok := r.payloads[i].(events.JoystickCapturedPayload)
		return p, ok
	}
	return events.JoystickCapturedPayload{}, false
}

// TestJoystickCaptureReportsTheSteal is the I1 guard.
//
// onJoystickCaptured logged the *keybinds.Stolen and discarded it, and the
// event carried only action_id -- so binding a button that another action
// already owned made that action's chip vanish on the next keybinds:changed
// with no warning at all. The same steal performed with a key showed
// "taken from ...", because AddTrigger hands its StolenDTO back to the
// caller. Spec section 10 requires steal reporting for BOTH kinds.
func TestJoystickCaptureReportsTheSteal(t *testing.T) {
	a, em, _ := newTestApp(t)

	src := newControllableJoySource("stick-c3")
	jm := joystick.New(src, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	jm.PollInterval = 2 * time.Millisecond
	a.SetJoystickBackend(jm)
	defer jm.Close()

	btn := trigger.JoyButton{Device: "stick-c3", Button: 4}
	a.settings.kb.Add(keybinds.ActionID("global.mute_toggle"),
		trigger.Joy(trigger.JoyBinding{Device: btn.Device, Button: btn.Button}))
	a.applyHotkeys()

	if token := a.BeginCapture("global.ptt"); token == 0 {
		t.Fatal("BeginCapture returned no token")
	}
	base := src.pollCount()
	waitFor(t, 2*time.Second, "capture baseline never sampled",
		func() bool { return src.pollCount() > base+1 })

	src.setHeld(btn, true)
	held := src.pollCount()
	waitFor(t, 2*time.Second, "held button never sampled",
		func() bool { return src.pollCount() > held+1 })
	src.setHeld(btn, false)

	waitFor(t, 2*time.Second, "keybinds:joy_captured was never emitted", func() bool {
		_, ok := em.lastJoystickCaptured()
		return ok
	})

	p, _ := em.lastJoystickCaptured()
	if p.ActionID != "global.ptt" {
		t.Fatalf("joy_captured action_id = %q, want global.ptt", p.ActionID)
	}
	stolen, ok := p.Stolen.(*StolenDTO)
	if !ok || stolen == nil {
		t.Fatalf("joy_captured carried no steal (%#v); the losing row gets no warning at all, "+
			"while the same steal by keyboard shows one", p.Stolen)
	}
	if stolen.ActionID != "global.mute_toggle" {
		t.Errorf("stolen from %q, want global.mute_toggle", stolen.ActionID)
	}
	if stolen.Trigger.Label == "" {
		t.Error("stolen trigger has no label; the banner renders it verbatim")
	}
}

// TestJoystickStateIsPushedWhenDevicesChange is the app half of the I3 guard.
//
// GetJoystickState was pull-only: useSettingsSync called it once on mount and
// nothing ever re-polled, so a stick plugged in after the Settings screen
// mounted kept its chips rendered muted. SetJoystickBackend now registers a
// state observer before Start(), so the loop's own enumeration reaches the
// UI.
func TestJoystickStateIsPushedWhenDevicesChange(t *testing.T) {
	a, em, _ := newTestApp(t)

	src := newControllableJoySource("stick-c3")
	jm := joystick.New(src, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	jm.PollInterval = 2 * time.Millisecond
	jm.RediscoverInterval = 5 * time.Millisecond
	a.SetJoystickBackend(jm)
	defer jm.Close()

	waitFor(t, 2*time.Second, "joystick:state was never emitted for the first enumeration",
		func() bool { return em.count(events.EventJoystickState) >= 1 })

	p, ok := em.lastJoystickState()
	if !ok {
		t.Fatal("joystick:state payload has the wrong type")
	}
	if !p.Supported {
		t.Error("joystick:state reports unsupported for a working fake source")
	}
	if len(p.Devices) != 1 || p.Devices[0].ID != "stick-c3" {
		t.Errorf("joystick:state devices = %+v, want the one attached stick", p.Devices)
	}

	// A steady state must not keep emitting: rediscover runs every 5ms here
	// and tick every 2ms, so a per-tick emit would be obvious.
	before := em.count(events.EventJoystickState)
	time.Sleep(60 * time.Millisecond)
	if after := em.count(events.EventJoystickState); after != before {
		t.Errorf("joystick:state emitted %d more times with nothing changing; "+
			"it must fire on change, not on every poll", after-before)
	}
}

// lastJoystickState returns the payload of the most recent joystick:state
// event, and reports whether there was one of the right type.
func (r *recordingEmitter) lastJoystickState() (events.JoystickStatePayload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i] != events.EventJoystickState {
			continue
		}
		p, ok := r.payloads[i].(events.JoystickStatePayload)
		return p, ok
	}
	return events.JoystickStatePayload{}, false
}

// TestJoystickStateDevicesMarshalsAsAnArrayNeverNull pins the shape of the
// pull path against the push path and against the TS type.
//
// GetJoystickState built Devices by append on a nil slice, so with no
// joystick attached -- or no backend at all -- it marshalled to `null`, while
// the push path (JoystickStatePayload, built with make) always sends `[]` and
// the TS type declares a non-nullable JoystickDevice[]. Nothing dereferenced
// it until the trigger chips started deriving connectivity from the live
// device list, at which point `null.some(...)` is a TypeError on first paint.
func TestJoystickStateDevicesMarshalsAsAnArrayNeverNull(t *testing.T) {
	assertArray := func(t *testing.T, got JoystickStateDTO) {
		t.Helper()
		if got.Devices == nil {
			t.Error("Devices is nil; the frontend types it as JoystickDevice[] and iterates it")
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if !strings.Contains(string(b), `"devices":[`) {
			t.Errorf("marshalled as %s, want \"devices\" as a JSON array", b)
		}
	}

	t.Run("no backend at all", func(t *testing.T) {
		a, _, _ := newTestApp(t)
		assertArray(t, a.GetJoystickState())
	})

	t.Run("backend with no attached device", func(t *testing.T) {
		a, _, _ := newTestApp(t)
		jm := joystick.New(emptyJoySource{}, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
		a.SetJoystickBackend(jm)
		defer jm.Close()
		assertArray(t, a.GetJoystickState())
	})
}

// emptyJoySource is a healthy backend with nothing plugged in -- the state
// that produced a nil Devices slice.
type emptyJoySource struct{}

func (emptyJoySource) Devices() ([]joystick.Device, error) { return nil, nil }
func (emptyJoySource) Poll() (joystick.State, error) {
	return joystick.State{Held: map[trigger.JoyButton]struct{}{}}, nil
}
func (emptyJoySource) Close() {}

// hotkeyActions returns the action IDs carried by every recorded event of the
// given name, in order. The hotkey payloads are anonymous structs, so this
// reads the field reflectively rather than re-declaring the shape here.
func (r *recordingEmitter) hotkeyActions(name string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for i, e := range r.events {
		if e != name {
			continue
		}
		f := reflect.ValueOf(r.payloads[i]).FieldByName("ActionID")
		if f.IsValid() && f.Kind() == reflect.String {
			out = append(out, f.String())
		}
	}
	return out
}

// TestJoystickCaptureBoundButtonThenFires is the other half of the C1 guard.
//
// TestJoystickCaptureBindsEndToEnd stops at "the trigger was STORED", and
// "the binding exists but nothing happens" is precisely the class of bug that
// has already shipped on this branch once: both halves individually correct,
// the seam between them dead. Capture suspends the joystick manager and hands
// the button to the capture sink; if the binding is applied while suspended
// and nothing re-arms dispatch on EndCapture, the freshly captured button is
// inert for the rest of the session and every unit test still passes.
//
// So this drives the WHOLE user journey -- BeginCapture, baseline poll, hold,
// release, EndCapture, then press the captured button for real -- and asserts
// hotkey:pressed and hotkey:released actually fire for the action that was
// bound.
func TestJoystickCaptureBoundButtonThenFires(t *testing.T) {
	a, em, _ := newTestApp(t)

	src := newControllableJoySource("stick-c3")
	jm := joystick.New(src, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	jm.PollInterval = 2 * time.Millisecond
	a.SetJoystickBackend(jm)
	defer jm.Close()

	token := a.BeginCapture("global.ptt")
	if token == 0 {
		t.Fatal("BeginCapture returned no token")
	}

	// One full poll after arming establishes the capture baseline, so the
	// press below is seen as a delta rather than as an already-down button.
	base := src.pollCount()
	waitFor(t, 2*time.Second, "the poll loop never sampled the source after arming a capture",
		func() bool { return src.pollCount() > base+1 })

	btn := trigger.JoyButton{Device: "stick-c3", Button: 4}
	src.setHeld(btn, true)
	held := src.pollCount()
	waitFor(t, 2*time.Second, "the poll loop stopped sampling while the button was held",
		func() bool { return src.pollCount() > held+1 })
	src.setHeld(btn, false)

	waitFor(t, 2*time.Second, "global.ptt gained no joystick trigger from the capture",
		func() bool { return joyTriggerCount(a, "global.ptt") == 1 })

	// Capture only ever emits releases, never presses: nothing may have
	// transmitted while the user was binding.
	if got := em.count(events.EventHotkeyPressed); got != 0 {
		t.Fatalf("hotkey:pressed fired %d times DURING capture; binding a button must not transmit", got)
	}

	a.EndCapture(token)

	// The journey's point: press the button that was just captured.
	src.setHeld(btn, true)
	waitFor(t, 2*time.Second,
		"the captured button fired nothing when pressed: the binding was stored but is inert, "+
			"which is exactly the 'it exists and nothing happens' failure capture is meant to end",
		func() bool { return em.count(events.EventHotkeyPressed) >= 1 })

	src.setHeld(btn, false)
	waitFor(t, 2*time.Second,
		"the captured button emitted a press but never a release; global.ptt is a hold action, "+
			"so this is a stuck transmission",
		func() bool { return em.count(events.EventHotkeyReleased) >= 1 })

	if got := em.hotkeyActions(events.EventHotkeyPressed); len(got) != 1 || got[0] != "global.ptt" {
		t.Errorf("hotkey:pressed actions = %v, want exactly [global.ptt]", got)
	}
	if got := em.hotkeyActions(events.EventHotkeyReleased); len(got) != 1 || got[0] != "global.ptt" {
		t.Errorf("hotkey:released actions = %v, want exactly [global.ptt]", got)
	}
}
