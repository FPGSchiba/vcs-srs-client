package session

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

type nopEmitter struct{}

func (nopEmitter) Emit(string, any) {}

// A fell-behind end reported by a REPLACED stream (stale generation) must not
// resubscribe. Driven through handleStreamEnd directly: the fake's EndStream
// can only reach the live stream, never one the client already replaced.
func TestResync_StaleGenerationIgnored(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	dial, cleanup := grpctest.Start(t, f)
	t.Cleanup(cleanup)
	s := New(state.New(), nopEmitter{}, Deps{Dialer: dial, Version: "test", OnControlState: func(events.ConnectionState) {}})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	s.mu.Lock()
	oldGen := s.streamGen
	s.mu.Unlock()
	if err := s.Reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	before := 0
	for _, c := range f.Calls() {
		if c == "SubscribeToUpdates" {
			before++
		}
	}

	// A live (uncancelled) ctx, so only the generation guard can stop it.
	s.handleStreamEnd(context.Background(), oldGen,
		status.Error(codes.ResourceExhausted, "update stream fell behind: resubscribe and call SyncClient"))
	time.Sleep(100 * time.Millisecond)

	after := 0
	for _, c := range f.Calls() {
		if c == "SubscribeToUpdates" {
			after++
		}
	}
	if after != before {
		t.Fatalf("a stale-generation fell-behind resubscribed (%d -> %d SubscribeToUpdates)", before, after)
	}
	if s.Control() == nil {
		t.Fatal("a stale-generation stream end tore down the live connection")
	}
}
