package app

import (
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
)

// newTestAudioManager builds a real Manager against the in-memory
// FakeBackend, with polling/VU intervals long enough that no background
// goroutine can interfere with the deterministic assertions below.
func newTestAudioManager(t *testing.T) *audio.Manager {
	t.Helper()
	m := audio.NewManager(audio.NewFakeBackend(), audio.ManagerOptions{
		PollInterval: time.Hour, VUInterval: time.Hour,
	})
	t.Cleanup(m.Stop)
	return m
}

// TestMuteToggleActionFlipsManagerMute proves global.mute_toggle -- a
// KindPress action -- flips Muted() on every press, since it never
// receives a matching Released.
func TestMuteToggleActionFlipsManagerMute(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	a.Pressed("global.mute_toggle")
	if !m.Muted() {
		t.Fatal("mute_toggle did not mute")
	}
	a.Pressed("global.mute_toggle")
	if m.Muted() {
		t.Fatal("mute_toggle did not unmute on the second press")
	}
}

// TestPushToMuteHoldsMuteOnlyWhileHeld proves global.push_to_mute -- a
// KindHold action -- mutes on press and unmutes on release.
func TestPushToMuteHoldsMuteOnlyWhileHeld(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	a.Pressed("global.push_to_mute")
	if !m.Muted() {
		t.Fatal("push_to_mute did not mute on press")
	}
	a.Released("global.push_to_mute")
	if m.Muted() {
		t.Fatal("push_to_mute stayed muted after release")
	}
}

// TestMuteToggleActionEmitsMicMutedEvent is Fix 1's guard for
// global.mute_toggle: every press must emit audio:mic_muted with the
// resulting state, since this is the frontend's only way to learn the mic
// was muted (a later task builds the UI that subscribes to it).
func TestMuteToggleActionEmitsMicMutedEvent(t *testing.T) {
	a, em, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	a.Pressed("global.mute_toggle")
	payloads := em.payloadsFor(events.EventAudioMicMuted)
	if len(payloads) != 1 || payloads[0] != true {
		t.Fatalf("after first mute_toggle press, audio:mic_muted payloads = %v, want [true]", payloads)
	}

	a.Pressed("global.mute_toggle")
	payloads = em.payloadsFor(events.EventAudioMicMuted)
	if len(payloads) != 2 || payloads[1] != false {
		t.Fatalf("after second mute_toggle press, audio:mic_muted payloads = %v, want [true false]", payloads)
	}
}

// TestPushToMuteEmitsMicMutedEventOnPressAndRelease is Fix 1's guard for
// global.push_to_mute: press must emit true, release must emit false, and
// nothing else in between.
func TestPushToMuteEmitsMicMutedEventOnPressAndRelease(t *testing.T) {
	a, em, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	a.Pressed("global.push_to_mute")
	a.Released("global.push_to_mute")

	payloads := em.payloadsFor(events.EventAudioMicMuted)
	if len(payloads) != 2 || payloads[0] != true || payloads[1] != false {
		t.Fatalf("audio:mic_muted payloads = %v, want [true false]", payloads)
	}
}

// TestPTTActionDoesNotEmitMicMutedEvent proves global.ptt -- which does not
// touch the mute state at all -- never emits audio:mic_muted, so the event
// stays a reliable signal of an actual mute-state change.
func TestPTTActionDoesNotEmitMicMutedEvent(t *testing.T) {
	a, em, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	a.Pressed("global.ptt")
	a.Released("global.ptt")

	if n := em.count(events.EventAudioMicMuted); n != 0 {
		t.Fatalf("audio:mic_muted emitted %d times for global.ptt, want 0", n)
	}
}

// TestPTTActionReachesTheManager proves global.ptt reaches Manager.SetPTT.
func TestPTTActionReachesTheManager(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	a.Pressed("global.ptt")
	if !m.PTT() {
		t.Fatal("global.ptt did not reach the manager")
	}
	a.Released("global.ptt")
	if m.PTT() {
		t.Fatal("global.ptt release did not reach the manager")
	}
}

// TestRadioPTTActionReachesTheManager proves a per-radio PTT action
// ("radio.<n>.ptt") reaches the manager exactly like global.ptt.
func TestRadioPTTActionReachesTheManager(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	a.Pressed("radio.1.ptt")
	if !m.PTT() {
		t.Fatal("radio.1.ptt did not reach the manager")
	}
	a.Released("radio.1.ptt")
	if m.PTT() {
		t.Fatal("radio.1.ptt release did not reach the manager")
	}
}

// TestAudioActionsAreNoOpsWithoutABackend proves every audio action wiring
// path is inert when SetAudioBackend was never called -- the state every
// other App test in this package exercises, so none of it may panic or
// otherwise misbehave with a nil manager.
func TestAudioActionsAreNoOpsWithoutABackend(t *testing.T) {
	a, _, _ := newTestApp(t)

	a.Pressed("global.ptt")
	a.Released("global.ptt")
	a.Pressed("global.push_to_mute")
	a.Released("global.push_to_mute")
	a.Pressed("global.mute_toggle")
	a.Pressed("radio.1.ptt")
	a.Released("radio.1.ptt")
	// Reaching here without a panic is the assertion; there is no manager
	// to observe state on.

	if err := a.StartMicTest(); err != ErrAudioUnavailable {
		t.Errorf("StartMicTest() = %v, want ErrAudioUnavailable", err)
	}
	if err := a.StopMicTest(); err != ErrAudioUnavailable {
		t.Errorf("StopMicTest() = %v, want ErrAudioUnavailable", err)
	}
	if err := a.PreviewEffect("tx_start"); err != ErrAudioUnavailable {
		t.Errorf("PreviewEffect() = %v, want ErrAudioUnavailable", err)
	}
	if got := a.GetAudioState(); got != (AudioStateDTO{}) {
		t.Errorf("GetAudioState() = %+v, want the zero value", got)
	}
	devs := a.GetAudioDevices()
	if len(devs.Inputs) != 0 || len(devs.Outputs) != 0 {
		t.Errorf("GetAudioDevices() = %+v, want empty (non-nil) lists", devs)
	}
}

// TestGetAudioDevicesAndStateReflectTheManager proves the read bindings
// reach a wired manager once devices have enumerated.
func TestGetAudioDevicesAndStateReflectTheManager(t *testing.T) {
	a, _, _ := newTestApp(t)
	b := audio.NewFakeBackend()
	b.SetDevices(
		[]audio.DeviceInfo{{ID: "mic-1", Name: "Test Mic", IsDefault: true}},
		[]audio.DeviceInfo{{ID: "out-1", Name: "Test Out", IsDefault: true}},
	)
	m := audio.NewManager(b, audio.ManagerOptions{PollInterval: time.Hour, VUInterval: time.Hour})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(m.Stop)
	a.SetAudioBackend(m)

	devs := a.GetAudioDevices()
	if len(devs.Inputs) != 1 || devs.Inputs[0].ID != "mic-1" {
		t.Errorf("GetAudioDevices().Inputs = %+v, want the one enumerated mic", devs.Inputs)
	}
	if len(devs.Outputs) != 1 || devs.Outputs[0].ID != "out-1" {
		t.Errorf("GetAudioDevices().Outputs = %+v, want the one enumerated output", devs.Outputs)
	}

	st := a.GetAudioState()
	if !st.Running {
		t.Error("GetAudioState().Running = false, want true after Start")
	}
}

// TestPreviewEffectReachesTheManager proves PreviewEffect calls through to
// Manager.PlayEffect without erroring for an unknown id (PlayEffect itself
// is documented to silently ignore those).
func TestPreviewEffectReachesTheManager(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	if err := a.PreviewEffect("tx_start"); err != nil {
		t.Fatalf("PreviewEffect: %v", err)
	}
}

// TestGetSettingsSourcesEffectCatalogFromTheManifest is Fix 3's guard: with
// a wired Manager, GetSettings().Audio.Effects must carry every manifest
// slot -- not just ones a user has customised -- with real labels and
// availability read off the SFX manifest, rather than the id-echoing,
// always-false placeholders a frontend used to have to fill in itself with
// a hardcoded second copy of the same data.
func TestGetSettingsSourcesEffectCatalogFromTheManifest(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)

	settingsDTO := a.GetSettings().Audio
	got := settingsDTO.Effects
	wantIDs := m.EffectIDs()
	if len(got) != len(wantIDs) {
		t.Fatalf("Effects has %d entries, want %d (one per manifest slot): %+v", len(got), len(wantIDs), got)
	}
	if gotOrder := settingsDTO.EffectOrder; len(gotOrder) != len(wantIDs) {
		t.Fatalf("EffectOrder = %v, want %v (Manager.EffectIDs' order)", gotOrder, wantIDs)
	} else {
		for i := range wantIDs {
			if gotOrder[i] != wantIDs[i] {
				t.Fatalf("EffectOrder = %v, want %v in that exact order", gotOrder, wantIDs)
			}
		}
	}
	for _, id := range wantIDs {
		e, ok := got[id]
		if !ok {
			t.Fatalf("Effects missing manifest slot %q: %+v", id, got)
		}
		if e.Label == "" || e.Label == id {
			t.Errorf("Effects[%q].Label = %q, want the manifest's real display label, not the id itself", id, e.Label)
		}
		if wantLabel := m.EffectLabel(id); e.Label != wantLabel {
			t.Errorf("Effects[%q].Label = %q, want %q (Manager.EffectLabel)", id, e.Label, wantLabel)
		}
		// No sample pack is vendored in this repo/test environment, so
		// every slot is genuinely unavailable -- this is the same fake
		// backend real production code runs against until the pack lands.
		if e.Available {
			t.Errorf("Effects[%q].Available = true, want false (no SFX sample pack in this test env)", id)
		}
	}
}

// TestGetSettingsFallsBackToPlaceholderEffectsWithoutABackend proves the
// nil-Manager path (audio backend unavailable, or a test that never wires
// one) still returns a well-formed Effects map -- id as label, Available
// false -- rather than panicking on a nil Manager.
func TestGetSettingsFallsBackToPlaceholderEffectsWithoutABackend(t *testing.T) {
	a, _, _ := newTestApp(t)

	got := a.GetSettings().Audio.Effects
	if len(got) != 0 {
		t.Fatalf("Effects = %+v, want empty: config.Audio.Effects starts nil and there is no Manager to enumerate the manifest", got)
	}
}

// TestConfigAudioFromDTODropsUntouchedDefaultEffects proves SetSettings
// does not turn config.Audio.Effects permanently non-nil just because
// GetSettings now always returns the full manifest catalog (the fix
// above): a round-tripped settings save where every slot is still at its
// untouched default (Enabled:false, File:"") must persist NO effects at
// all, preserving the "nil until customised" invariant config.Audio.
// Effects' own doc requires (avoids TOML table noise on every user's
// config.toml).
func TestConfigAudioFromDTODropsUntouchedDefaultEffects(t *testing.T) {
	dto := AudioSettingsDTO{
		Effects: map[string]AudioEffectDTO{
			"tx_start": {Enabled: false, File: "", Label: "TX Start", Available: false},
			"tx_end":   {Enabled: false, File: "", Label: "TX End", Available: false},
			"rx_start": {Enabled: true, File: "custom.wav", Label: "RX Start", Available: false},
		},
	}
	got := configAudioFromDTO(dto).Effects
	if len(got) != 1 {
		t.Fatalf("Effects = %+v, want exactly the one customised slot", got)
	}
	if e, ok := got["rx_start"]; !ok || e.Enabled != true || e.File != "custom.wav" {
		t.Fatalf("Effects[\"rx_start\"] = %+v, ok=%v, want {Enabled:true File:custom.wav}", got["rx_start"], ok)
	}
	if _, ok := got["tx_start"]; ok {
		t.Errorf("Effects retained untouched-default slot %q -- config.Audio.Effects must stay nil/sparse until a user customises a slot", "tx_start")
	}
}

// TestGetAudioEffectPresetsReturnsBuiltInDSPPresets proves the preset
// bindings are available even with no audio backend wired (they are
// static data, unlike GetAudioDevices/GetAudioState) and match
// internal/audio's own labeled preset tables -- the backend source of
// truth a frontend used to duplicate as a hardcoded second copy.
func TestGetAudioEffectPresetsReturnsBuiltInDSPPresets(t *testing.T) {
	a, _, _ := newTestApp(t) // no SetAudioBackend call

	got := a.GetAudioEffectPresets()
	wantVoice := audio.VoicePresetOptions()
	if len(got.Voice) != len(wantVoice) {
		t.Fatalf("Voice presets = %+v, want %d entries matching audio.VoicePresetOptions()", got.Voice, len(wantVoice))
	}
	for i, p := range wantVoice {
		if got.Voice[i].Value != p.ID || got.Voice[i].Label != p.Label {
			t.Errorf("Voice[%d] = %+v, want {%q %q}", i, got.Voice[i], p.ID, p.Label)
		}
	}
	wantClipping := audio.ClippingPresetOptions()
	if len(got.Clipping) != len(wantClipping) {
		t.Fatalf("Clipping presets = %+v, want %d entries matching audio.ClippingPresetOptions()", got.Clipping, len(wantClipping))
	}
	for i, p := range wantClipping {
		if got.Clipping[i].Value != p.ID || got.Clipping[i].Label != p.Label {
			t.Errorf("Clipping[%d] = %+v, want {%q %q}", i, got.Clipping[i], p.ID, p.Label)
		}
	}
}
