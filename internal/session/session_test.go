package session_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/auth"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// capEmitter records every emitted event name and payload, in order.
type capEmitter struct {
	mu       sync.Mutex
	names    []string
	payloads []any
}

func (c *capEmitter) Emit(n string, p any) {
	c.mu.Lock()
	c.names = append(c.names, n)
	c.payloads = append(c.payloads, p)
	c.mu.Unlock()
}

// count returns how many times name was emitted with a payload equal to p
// (any p matches when p is nil).
func (c *capEmitter) count(name string, p any) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for i, x := range c.names {
		if x == name && (p == nil || c.payloads[i] == p) {
			n++
		}
	}
	return n
}

// seq returns the recorded (name, payload) pairs.
func (c *capEmitter) seq() ([]string, []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.names...), append([]any(nil), c.payloads...)
}

func (c *capEmitter) saw(n string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, x := range c.names {
		if x == n {
			return true
		}
	}
	return false
}

func TestConnect_HappyPath(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	st := state.New()
	em := &capEmitter{}
	s := session.New(st, em, session.Deps{Dialer: dial, Version: "test"})

	if err := s.Connect(context.Background(), "localhost:0", "Name", "pw", "unit"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !em.saw("control:connection") || !em.saw("auth:session_changed") {
		t.Fatalf("expected connect events, got %v", em.names)
	}
}

func TestConnect_GuestUnavailable(t *testing.T) {
	f := grpctest.NewFake()
	f.HasGuest = false
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	s := session.New(state.New(), &capEmitter{}, session.Deps{Dialer: dial, Version: "test"})
	err := s.Connect(context.Background(), "localhost:0", "n", "p", "u")
	if !errors.Is(err, auth.ErrGuestUnavailable) {
		t.Fatalf("expected ErrGuestUnavailable, got %v", err)
	}
}
