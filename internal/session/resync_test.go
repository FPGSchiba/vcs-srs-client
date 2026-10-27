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
//
// "First" means the server has REGISTERED the new subscription before it
// serves SyncClient, not merely that the request left the client: grpc-go
// returns from a server-streaming call once the request is sent. The fake
// delays registration to make that window wide; the client closes it by
// waiting for the SV2 C17 ack (response headers) before SyncClient.
func TestResync_FellBehindResubscribesThenSyncs(t *testing.T) {
	f := grpctest.NewFake()
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}, "b": {Name: "B"}}, "secret")
	h, _ := connected(t, f)
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}}, "secret")
	f.SetSubscribeDelay(150 * time.Millisecond)

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
	if !waitFor(t, func() bool {
		return countCalls(f.Calls(), "SubscribeToUpdates") == 3 && countCalls(f.Calls(), "SyncClient") == 3
	}) {
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

// waitLong is waitFor with a caller-chosen deadline, for paths that sit out
// a post-dial bound.
func waitLong(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// A server from before SV2 C17 (dcce3ad) never acknowledges the
// subscription. The resync must not wedge on it: after the post-dial bound
// it logs a Warn and proceeds to SyncClient, the pre-C17 behaviour.
func TestResync_NoSubscriptionAckFallsBackAfterPostDialBound(t *testing.T) {
	const postDial = 2 * time.Second
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	h := newBound(t, f, nil, postDial)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !waitFor(t, func() bool { return f.ActiveStreams() == 1 }) {
		t.Fatal("update stream never opened")
	}
	f.SetSubscribeAck(false)

	start := time.Now()
	f.EndStream(errFellBehind)
	if !waitLong(postDial+3*time.Second, func() bool { return countCalls(f.Calls(), "SyncClient") == 2 }) {
		t.Fatalf("resync never reached SyncClient without an ack; calls %v", f.Calls())
	}
	if el := time.Since(start); el < postDial-200*time.Millisecond {
		t.Fatalf("SyncClient after %v, want the resync to wait out the %v bound for the ack", el, postDial)
	}
	if !waitFor(t, func() bool { return h.rec.countOf(events.ConnConnected) == 2 }) {
		t.Fatalf("states = %v, want connected re-published after the fallback", h.rec.all())
	}
	if got := h.rec.countOf(events.ConnDisconnected); got != 0 {
		t.Fatalf("the fallback reported disconnected %d times; saw %v", got, h.rec.all())
	}
}

// Without the ack, grpc-go still delivers the response headers with the
// first update, which releases the wait early. This is what a pre-C17
// server does whenever any event is published.
func TestResync_NoSubscriptionAckReleasedByFirstUpdate(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f) // post-dial bound testPostDialOK (5 s)
	f.SetSubscribeAck(false)

	start := time.Now()
	f.EndStream(errFellBehind)
	if !waitFor(t, func() bool {
		return countCalls(f.Calls(), "SubscribeToUpdates") == 2 && f.ActiveStreams() == 1
	}) {
		t.Fatalf("no resubscribe; calls %v", f.Calls())
	}
	nobody := "nobody"
	f.PushUpdate(&srspb.ServerUpdate{
		Type:   srspb.ServerUpdate_CLIENT_LEFT,
		Update: &srspb.ServerUpdate_ClientUpdate{ClientUpdate: &srspb.ClientUpdate{ClientGuid: &nobody}},
	})
	if !waitFor(t, func() bool { return countCalls(f.Calls(), "SyncClient") == 2 }) {
		t.Fatalf("the first update did not release the resync; calls %v", f.Calls())
	}
	if el := time.Since(start); el >= 2*time.Second {
		t.Fatalf("SyncClient after %v, want well inside the post-dial bound", el)
	}
	if !waitFor(t, func() bool { return h.rec.countOf(events.ConnConnected) == 2 }) {
		t.Fatalf("states = %v, want connected re-published", h.rec.all())
	}
}

// A resubscribe the server refuses (the stream ends before any header) is a
// failed subscribe: no SyncClient, and an ordinary control loss.
func TestResync_SubscribeRefusedIsControlLoss(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.Fail("SubscribeToUpdates", status.Error(codes.Internal, "x"))

	f.EndStream(errFellBehind)
	if !waitFor(t, func() bool { return h.rec.countOf(events.ConnDisconnected) == 1 }) {
		t.Fatalf("no disconnected after a refused resubscribe; saw %v, calls %v", h.rec.all(), f.Calls())
	}
	time.Sleep(50 * time.Millisecond)
	if got := countCalls(f.Calls(), "SyncClient"); got != 1 {
		t.Fatalf("SyncClient called %d times, want only the Connect one; calls %v", got, f.Calls())
	}
	if h.em.count(events.EventAuthSession, "logged_out") != 0 {
		t.Fatal("a refused resubscribe logged the user out")
	}
}
