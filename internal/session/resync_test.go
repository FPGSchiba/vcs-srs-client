package session_test

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

var errFellBehind = status.Error(codes.ResourceExhausted, "update stream fell behind: resubscribe and call SyncClient")

// indexOfNth returns the index of the nth (1-based) occurrence of want, or -1.
func indexOfNth(xs []string, want string, nth int) int {
	seen := 0
	for i, x := range xs {
		if x == want {
			seen++
			if seen == nth {
				return i
			}
		}
	}
	return -1
}

// SV2 C13 (D6): a stream that fell behind is not a session loss. The client
// resubscribes on the same connection FIRST, then SyncClient replaces the
// roster, and connected is published again.
func TestResync_FellBehindResubscribesThenSyncs(t *testing.T) {
	f := grpctest.NewFake()
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}, "b": {Name: "B"}}, "secret")
	h, _ := connected(t, f)
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}}, "secret")

	f.EndStream(errFellBehind)

	if !waitFor(t, func() bool {
		calls := f.Calls()
		sub := indexOfNth(calls, "SubscribeToUpdates", 2)
		sync := indexOfNth(calls, "SyncClient", 2)
		return sub >= 0 && sync > sub
	}) {
		t.Fatalf("calls = %v, want a second SubscribeToUpdates followed by a second SyncClient", f.Calls())
	}
	if !waitFor(t, func() bool {
		snap := h.st.Snapshot()
		return len(snap.Clients) == 1 && snap.Clients["a"] != nil
	}) {
		t.Fatalf("roster after resync = %v, want only a", h.st.Snapshot().Clients)
	}
	if !waitFor(t, func() bool { return h.last() == events.ConnConnected && h.rec.countOf(events.ConnConnected) == 2 }) {
		t.Fatalf("states = %v, want connected re-published", h.rec.all())
	}
	if got := h.rec.countOf(events.ConnDisconnected); got != 0 {
		t.Fatalf("a resync reported disconnected %d times; saw %v", got, h.rec.all())
	}
	if h.em.count(events.EventAuthSession, "logged_out") != 0 {
		t.Fatal("a resync logged the user out")
	}
}

// Review Focus #5: the new stream falls behind again at once.
func TestResync_RepeatedFellBehindResyncsAgain(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)

	f.EndStream(errFellBehind)
	if !waitFor(t, func() bool { return countCalls(f.Calls(), "SyncClient") == 2 && f.ActiveStreams() == 1 }) {
		t.Fatalf("first resync did not complete; calls %v", f.Calls())
	}
	f.EndStream(errFellBehind)
	if !waitFor(t, func() bool { return countCalls(f.Calls(), "SubscribeToUpdates") == 3 && countCalls(f.Calls(), "SyncClient") == 3 }) {
		t.Fatalf("calls = %v, want three SubscribeToUpdates and three SyncClient", f.Calls())
	}
	time.Sleep(50 * time.Millisecond)
	if got := h.rec.countOf(events.ConnDisconnected); got != 0 {
		t.Fatalf("repeated resync reported disconnected %d times; saw %v", got, h.rec.all())
	}
}

func TestResync_SyncUnauthenticatedEndsSession(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.Fail("SyncClient", errUnknownSession)

	f.EndStream(errFellBehind)
	if !waitFor(t, func() bool { return h.em.count(events.EventAuthSessionEnded, nil) == 1 }) {
		t.Fatal("a resync whose SyncClient is Unauthenticated did not end the session")
	}
	assertEndedTail(t, h, "expired")
}

func TestResync_SyncInternalIsControlLoss(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.Fail("SyncClient", status.Error(codes.Internal, "x"))

	f.EndStream(errFellBehind)
	if !waitFor(t, func() bool { return h.rec.countOf(events.ConnDisconnected) == 1 }) {
		t.Fatalf("no disconnected after a failed resync; saw %v", h.rec.all())
	}
	time.Sleep(50 * time.Millisecond)
	if h.em.count(events.EventAuthSession, "logged_out") != 0 {
		t.Fatal("an Internal resync failure logged the user out")
	}
}
