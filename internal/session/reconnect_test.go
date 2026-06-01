package session_test

import (
	"context"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

func TestReconnect_ReestablishesAfterConnect(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	em := &capEmitter{}
	s := session.New(state.New(), em, session.Deps{Dialer: dial, Version: "test"})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.Reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if s.Control() == nil {
		t.Fatal("expected live control client after reconnect")
	}
}
