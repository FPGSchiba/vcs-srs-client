package control

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

type capEmitter struct {
	mu    sync.Mutex
	names []string
}

func (c *capEmitter) Emit(name string, _ any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names = append(c.names, name)
}
func (c *capEmitter) saw(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.names {
		if n == name {
			return true
		}
	}
	return false
}

func TestStream_RoutesClientJoined(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	em := &capEmitter{}
	c := New(conn, "tok")

	guid := "g9"
	f.PushUpdate(&srspb.ServerUpdate{
		Type: srspb.ServerUpdate_CLIENT_JOINED,
		Update: &srspb.ServerUpdate_ClientUpdate{ClientUpdate: &srspb.ClientUpdate{
			ClientGuid: &guid,
			ClientInfo: &srspb.ClientInfo{Name: "Zoe"},
		}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.ConsumeUpdates(ctx, st, em)

	deadline := time.After(2 * time.Second)
	for {
		if _, ok := st.Client(guid); ok && em.saw("state:client_update") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("client_update not observed in time")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestRouteVoiceAddressUpdate pins that VOICE_ADDRESS_UPDATE reaches the
// store. stream.go's default branch silently discarded it, which was correct
// while there was no voice path and is a dropped redirect now.
func TestRouteVoiceAddressUpdate(t *testing.T) {
	st := state.New()
	em := &capEmitter{}
	route(&srspb.ServerUpdate{
		Type: srspb.ServerUpdate_VOICE_ADDRESS_UPDATE,
		Update: &srspb.ServerUpdate_VoiceAddressUpdate{
			VoiceAddressUpdate: &srspb.VoiceAddressUpdate{
				CoalitionVoiceAddr: "10.0.0.9:5002",
				GlobalVoiceAddr:    "10.0.0.1:5002",
				VoiceSecret:        strings.Repeat("A", 43),
			},
		},
	}, st, events.New(em))

	secret, coal, global := st.VoiceCredentials()
	if coal != "10.0.0.9:5002" {
		t.Errorf("coalition addr = %q", coal)
	}
	if global != "10.0.0.1:5002" {
		t.Errorf("global addr = %q", global)
	}
	if len(secret) != 43 {
		t.Errorf("secret length = %d, want 43", len(secret))
	}
}

// TestRouteVoiceAddressUpdate_EmitsTypedEvent pins that the route case emits
// a typed event (as every other case does) rather than only mutating the
// store silently.
func TestRouteVoiceAddressUpdate_EmitsTypedEvent(t *testing.T) {
	st := state.New()
	em := &capEmitter{}
	route(&srspb.ServerUpdate{
		Type: srspb.ServerUpdate_VOICE_ADDRESS_UPDATE,
		Update: &srspb.ServerUpdate_VoiceAddressUpdate{
			VoiceAddressUpdate: &srspb.VoiceAddressUpdate{
				CoalitionVoiceAddr: "10.0.0.9:5002",
				GlobalVoiceAddr:    "10.0.0.1:5002",
				VoiceSecret:        strings.Repeat("A", 43),
			},
		},
	}, st, events.New(em))

	if !em.saw(events.EventVoiceAddressUpdate) {
		t.Fatalf("expected %q to be emitted, saw %v", events.EventVoiceAddressUpdate, em.names)
	}
}

// TestRouteVoiceAddressUpdate_NilPayloadIsIgnored guards the same nil-safety
// pattern every other route case follows (e.g. CLIENT_JOINED with a nil
// ClientUpdate): a malformed update must not panic the stream goroutine.
func TestRouteVoiceAddressUpdate_NilPayloadIsIgnored(t *testing.T) {
	st := state.New()
	em := &capEmitter{}
	route(&srspb.ServerUpdate{
		Type: srspb.ServerUpdate_VOICE_ADDRESS_UPDATE,
	}, st, events.New(em))

	secret, coal, global := st.VoiceCredentials()
	if secret != "" || coal != "" || global != "" {
		t.Fatalf("expected store untouched, got secret=%q coal=%q global=%q", secret, coal, global)
	}
	if em.saw(events.EventVoiceAddressUpdate) {
		t.Fatalf("expected no event for a nil payload")
	}
}
