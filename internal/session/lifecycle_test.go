package session_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// GO-02: a second Connect while live must cancel and close the first
// connection, so its update stream does not outlive it.
func TestConnect_SecondConnectReplacesFirst(t *testing.T) {
	f := grpctest.NewFake()
	h := newBound(t, f, nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	first := h.s.Control()
	// The stream opens asynchronously; wait until the server holds it, or
	// the second Connect can cancel it before it was ever opened.
	if !waitFor(t, func() bool { return f.ActiveStreams() == 1 }) {
		t.Fatal("first update stream never opened")
	}
	if err := h.connect(); err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if !waitFor(t, func() bool { return countCalls(f.Calls(), "SubscribeToUpdates") == 2 }) {
		t.Fatalf("calls = %v, want two SubscribeToUpdates", f.Calls())
	}
	if !waitFor(t, func() bool { return f.ActiveStreams() == 1 }) {
		t.Fatalf("server sees %d live update streams after the second Connect, want 1 (the first leaked)", f.ActiveStreams())
	}
	if c := h.s.Control(); c == nil || c == first {
		t.Fatal("Control() is not the second connection's client")
	}
	time.Sleep(100 * time.Millisecond)
	if got := h.rec.countOf(events.ConnDisconnected); got != 0 {
		t.Fatalf("replacing the first connection reported disconnected %d times; saw %v", got, h.rec.all())
	}
}

func TestConnectRacesReconnect_OneLiveConn(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	h := newBound(t, f, nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	for i := 0; i < 20; i++ {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = h.connect() }()
		go func() { defer wg.Done(); _ = h.s.Reconnect(context.Background()) }()
		wg.Wait()
	}
	if h.s.Control() == nil {
		t.Fatal("no live control client after racing Connect and Reconnect")
	}
	if !waitFor(t, func() bool { return f.ActiveStreams() == 1 }) {
		t.Fatalf("server sees %d live update streams, want exactly 1", f.ActiveStreams())
	}
}

// GO-10: logout forgets the token, so a stale RECONNECT cannot resurrect it.
func TestReconnect_AfterDisconnectNeverConnected(t *testing.T) {
	f := grpctest.NewFake()
	h := newBound(t, f, nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := h.s.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	syncs := countCalls(f.Calls(), "SyncClient")
	err := h.s.Reconnect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "never connected") {
		t.Fatalf("Reconnect after Disconnect = %v, want a never-connected error", err)
	}
	if got := countCalls(f.Calls(), "SyncClient"); got != syncs {
		t.Fatalf("Reconnect after Disconnect sent a SyncClient (%d -> %d)", syncs, got)
	}
}

// CON-07: a Reconnect's snapshot replaces the roster.
func TestReconnect_DropsGhosts(t *testing.T) {
	f := grpctest.NewFake()
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}, "b": {Name: "B"}}, "secret")
	h := newBound(t, f, nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}}, "secret")
	if err := h.s.Reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	snap := h.st.Snapshot()
	if len(snap.Clients) != 1 || snap.Clients["a"] == nil {
		t.Fatalf("clients after reconnect = %v, want only a", snap.Clients)
	}
}

func TestDisconnect_ClearsRosterAndSecret(t *testing.T) {
	f := grpctest.NewFake()
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}}, "secret")
	f.SyncRadios = map[string]*srspb.RadioInfo{"a": {}}
	h := newBound(t, f, nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := h.s.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	snap := h.st.Snapshot()
	if len(snap.Clients) != 0 || len(snap.Radios) != 0 {
		t.Fatalf("after Disconnect clients=%v radios=%v, want both empty", snap.Clients, snap.Radios)
	}
	if secret, _, _ := h.st.VoiceCredentials(); secret != "" {
		t.Fatalf("voice secret %q survived Disconnect", secret)
	}
}

func TestDisconnect_BoundedByCallerCtx(t *testing.T) {
	f := grpctest.NewFake()
	h := newBound(t, f, nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	f.Block("Disconnect", make(chan struct{}))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = h.s.Disconnect(ctx)
	if el := time.Since(start); el > time.Second {
		t.Fatalf("Disconnect took %v against a stalled server", el)
	}
	if h.em.count(events.EventAuthSession, "logged_out") != 1 {
		t.Fatal("Disconnect did not emit logged_out")
	}
}

// Review Focus #2: Logout while a connect is still negotiating aborts it.
func TestDisconnect_DuringConnectAbortsIt(t *testing.T) {
	f := grpctest.NewFake()
	f.Block("GuestLogin", make(chan struct{}))
	h := newBound(t, f, nil, 10*time.Second)

	connErr := make(chan error, 1)
	go func() { connErr <- h.connect() }()
	if !waitFor(t, func() bool { return contains(f.Calls(), "GuestLogin") }) {
		t.Fatal("GuestLogin never reached")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_ = h.s.Disconnect(ctx)
	if el := time.Since(start); el > time.Second {
		t.Fatalf("Disconnect took %v while a connect was negotiating", el)
	}
	select {
	case err := <-connErr:
		if !errors.Is(err, session.ErrConnectCancelled) {
			t.Fatalf("Connect = %v, want ErrConnectCancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Connect did not return after Disconnect")
	}
	if got := h.em.count(events.EventAuthSession, nil); got != 1 {
		t.Fatalf("auth:session_changed emitted %d times, want exactly 1", got)
	}
	if h.em.count(events.EventAuthSession, "logged_out") != 1 {
		t.Fatal("the one auth:session_changed is not logged_out")
	}
}

// Review finding (CL1): a second Connect that tears the live session down and
// then fails must not leave the first session's token behind for RECONNECT
// to resume.
func TestConnect_FailedReplacementForgetsTheOldSession(t *testing.T) {
	f := grpctest.NewFake()
	h := newBound(t, f, nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	f.Fail("InitAuth", status.Error(codes.Internal, "boom"))
	if err := h.connect(); err == nil {
		t.Fatal("second connect should have failed")
	}
	err := h.s.Reconnect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "never connected") {
		t.Fatalf("Reconnect after a failed replacement = %v, want the never-connected refusal", err)
	}
}
