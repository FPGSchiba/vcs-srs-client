package app

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

func TestConnectionStateDTOFrom_MapsEveryField(t *testing.T) {
	snap := connhealth.Snapshot{
		Server: "127.0.0.1:5002",
		Control: connhealth.Link{
			State: connhealth.StateConnected, RTTMs: 8, Healthy: true, Available: true,
		},
		Voice: connhealth.Link{
			State: "retrying", RTTMs: connhealth.RTTUnknown, Available: true,
			Error: "voice: no HELLO_ACK after 5 attempts",
		},
	}

	got := ConnectionStateDTOFrom(snap)

	if got.Server != "127.0.0.1:5002" {
		t.Errorf("Server = %q, want %q", got.Server, "127.0.0.1:5002")
	}
	if got.Control.State != "connected" || got.Control.RTTMs != 8 || !got.Control.Healthy {
		t.Errorf("Control = %+v, want connected/8/healthy", got.Control)
	}
	if got.Voice.State != "retrying" || got.Voice.RTTMs != -1 {
		t.Errorf("Voice = %+v, want retrying/-1", got.Voice)
	}
	if got.Voice.Error != "voice: no HELLO_ACK after 5 attempts" {
		t.Errorf("Voice.Error = %q, want the transition error", got.Voice.Error)
	}
}

func TestGetConnectionState_ReturnsTheMonitorsSnapshot(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	m := connhealth.New(connhealth.Options{})
	m.SetServer("127.0.0.1:5002")
	m.SetControlState(connhealth.StateConnected)
	a.SetConnHealth(m)

	got := a.GetConnectionState()

	if got.Server != "127.0.0.1:5002" {
		t.Errorf("Server = %q, want %q", got.Server, "127.0.0.1:5002")
	}
	if got.Control.State != "connected" {
		t.Errorf("Control.State = %q, want connected", got.Control.State)
	}
}

func TestGetConnectionState_HonestWhenNoMonitorIsWired(t *testing.T) {
	// Tests and any build where wiring failed leave the monitor nil. The
	// binding must answer honestly rather than panic -- the same
	// optional-dependency discipline GetAudioState follows for a machine
	// with no sound card.
	a := NewForTest(state.New(), nil, nil)

	got := a.GetConnectionState()

	if got.Control.State != connhealth.StateDisconnected {
		t.Errorf("Control.State = %q with no monitor, want %q",
			got.Control.State, connhealth.StateDisconnected)
	}
	if got.Voice.State != connhealth.StateUnavailable {
		t.Errorf("Voice.State = %q with no monitor, want %q",
			got.Voice.State, connhealth.StateUnavailable)
	}
	if got.Control.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Control.RTTMs = %d with no monitor, want %d",
			got.Control.RTTMs, connhealth.RTTUnknown)
	}
}

func TestVoiceStateReachesTheMonitor(t *testing.T) {
	// Phase 5's I2 fix wired voice.Session's OnState to an event. Nothing in
	// the shipped binary ever subscribed, and the health model needs the
	// same transitions, so OnState feeds both.
	a := NewForTest(state.New(), nil, nil)
	m := connhealth.New(connhealth.Options{})
	a.SetConnHealth(m)

	a.reportVoiceState("connected", "")

	if got := m.Snapshot().Voice.State; got != "connected" {
		t.Errorf("Voice.State = %q, want connected", got)
	}
	if !m.Snapshot().Voice.Available {
		t.Error("Voice.Available = false after a connected transition, want true")
	}
}

func TestVoiceClosedReportsUnavailable(t *testing.T) {
	// A closed session is not a session in a failed state: it is no session
	// at all, which is exactly what Available is for. Without this, tearing
	// voice down on a clean logout would leave the pill showing a red VOICE
	// alert for a plane nobody asked to be running.
	a := NewForTest(state.New(), nil, nil)
	m := connhealth.New(connhealth.Options{})
	a.SetConnHealth(m)

	a.reportVoiceState("connected", "")
	a.reportVoiceUnavailable()

	snap := m.Snapshot()
	if snap.Voice.State != connhealth.StateUnavailable {
		t.Errorf("Voice.State = %q, want %q", snap.Voice.State, connhealth.StateUnavailable)
	}
	if snap.Voice.Available {
		t.Error("Voice.Available = true after teardown, want false")
	}
}
