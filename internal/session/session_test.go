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

type capEmitter struct {
	mu    sync.Mutex
	names []string
}

func (c *capEmitter) Emit(n string, _ any) { c.mu.Lock(); c.names = append(c.names, n); c.mu.Unlock() }
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
