package app

import (
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
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
