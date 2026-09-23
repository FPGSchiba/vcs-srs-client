package events_test

import (
	"sync"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
)

type fakeEmitter struct {
	mu     sync.Mutex
	events []emitted
}

type emitted struct {
	name    string
	payload any
}

func (f *fakeEmitter) Emit(name string, payload any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, emitted{name, payload})
}

func (f *fakeEmitter) last() emitted {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) == 0 {
		return emitted{}
	}
	return f.events[len(f.events)-1]
}

func TestEmitter_ClientUpdate(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.ClientUpdate("guid-1", events.ClientUpdatePayload{Name: "Alice"})

	got := f.last()
	if got.name != events.EventClientUpdate {
		t.Fatalf("unexpected event name: %q", got.name)
	}
	p, ok := got.payload.(events.ClientUpdateEnvelope)
	if !ok {
		t.Fatalf("unexpected payload type: %T", got.payload)
	}
	if p.Guid != "guid-1" || p.Info.Name != "Alice" {
		t.Fatalf("unexpected payload: %+v", p)
	}
}

func TestEmitter_ConnectionState(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.ConnectionState(events.ConnReconnecting)

	got := f.last()
	if got.name != events.EventControlConnection {
		t.Fatalf("unexpected event name: %q", got.name)
	}
	if got.payload != events.ConnReconnecting {
		t.Fatalf("expected reconnecting, got %v", got.payload)
	}
}

func TestEmitter_ClientLeft(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.ClientLeft("guid-9")

	got := f.last()
	if got.name != events.EventClientLeft {
		t.Fatalf("unexpected event name: %q", got.name)
	}
}

func TestEmitter_RadioUpdate(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.RadioUpdate("g", events.RadioInfoPayload{Muted: true})
	if f.last().name != events.EventRadioUpdate {
		t.Fatalf("unexpected: %q", f.last().name)
	}
}

func TestEmitter_SessionChanged(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.SessionChanged("logged_in")
	if f.last().name != events.EventAuthSession {
		t.Fatalf("unexpected: %q", f.last().name)
	}
}

func TestEmitter_SettingsChanged(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.SettingsChanged(map[string]bool{"start_minimized": true})

	got := f.last()
	if got.name != events.EventSettingsChanged {
		t.Fatalf("unexpected event name: %q", got.name)
	}
}

func TestEmitter_KeybindsChanged(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.KeybindsChanged([]string{"global.ptt"})

	got := f.last()
	if got.name != events.EventKeybindsChanged {
		t.Fatalf("unexpected event name: %q", got.name)
	}
}

func TestEmitter_HotkeyPressed(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.HotkeyPressed("global.ptt")

	got := f.last()
	if got.name != events.EventHotkeyPressed {
		t.Fatalf("unexpected event name: %q", got.name)
	}
	p, ok := got.payload.(struct {
		ActionID string `json:"action_id"`
	})
	if !ok {
		t.Fatalf("unexpected payload type: %T", got.payload)
	}
	if p.ActionID != "global.ptt" {
		t.Fatalf("unexpected action id: %q", p.ActionID)
	}
}

func TestEmitter_HotkeyReleased(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.HotkeyReleased("global.ptt")

	if f.last().name != events.EventHotkeyReleased {
		t.Fatalf("unexpected: %q", f.last().name)
	}
}

func TestEmitter_HotkeysState(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	failed := map[string]string{"global.ptt": "register global.ptt (F1): already bound"}
	e.HotkeysState(false, "register F1: already bound", failed, "denied")

	got := f.last()
	if got.name != events.EventHotkeysState {
		t.Fatalf("unexpected event name: %q", got.name)
	}
	p, ok := got.payload.(events.HotkeyStatePayload)
	if !ok {
		t.Fatalf("unexpected payload type: %T", got.payload)
	}
	if p.Registered || p.Error == "" {
		t.Fatalf("unexpected payload: %+v", p)
	}
	if reason, ok := p.Failed["global.ptt"]; !ok || reason == "" {
		t.Fatalf("expected a failure reason for global.ptt, got %+v", p.Failed)
	}
	// The permission state must survive onto the wire: the UI branches on it
	// to decide whether the failure is one the user can act on, and a payload
	// that dropped it would silently degrade to the old error-string parsing.
	if p.Permission != "denied" {
		t.Errorf("Permission = %q, want %q", p.Permission, "denied")
	}
}

func TestAudioEventNames(t *testing.T) {
	cases := map[string]string{
		events.EventAudioDevicesChanged: "audio:devices_changed",
		events.EventAudioVU:             "audio:vu",
		events.EventAudioState:          "audio:state",
		events.EventAudioMicMuted:       "audio:mic_muted",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("event name %q, want %q", got, want)
		}
	}
}

func TestAudioEmittersForwardPayloads(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.AudioMicMuted(true)
	if f.last().name != events.EventAudioMicMuted {
		t.Fatalf("emitted %q", f.last().name)
	}
}
