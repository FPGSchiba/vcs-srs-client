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
