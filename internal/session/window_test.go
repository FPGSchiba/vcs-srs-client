package session_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// joined is a CLIENT_JOINED update for guid.
func joined(guid, name string) *srspb.ServerUpdate {
	g := guid
	return &srspb.ServerUpdate{
		Type: srspb.ServerUpdate_CLIENT_JOINED,
		Update: &srspb.ServerUpdate_ClientUpdate{ClientUpdate: &srspb.ClientUpdate{
			ClientGuid: &g,
			ClientInfo: &srspb.ClientInfo{Name: name},
		}},
	}
}

// publishAfterSnapshot arranges for the next SyncClient to publish "late"
// joining AFTER it has taken its snapshot (so the snapshot does not contain
// it) and before it answers -- the window the client must not lose an
// update in.
//
// When waitRouted is set the hook then gives the client up to 300 ms to
// route the update into st before answering. That is what makes the
// snapshot-wipes-update failure deterministic: a client that applies stream
// updates before the snapshot has applied it by then, and ResetRoster then
// drops it. A client that holds updates until the snapshot is applied never
// routes it in that time, and pays the 300 ms.
func publishAfterSnapshot(f *grpctest.Fake, st *state.Store, waitRouted bool) {
	f.OnNextSync(func() {
		f.Publish(joined("late", "Late"))
		if !waitRouted {
			return
		}
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			if _, ok := st.Client("late"); ok {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	})
}

// letClientRead waits, inside a SyncClient hook, until the fake has Sent n
// updates and then a little longer, so they reach the client before the
// snapshot does (the snapshot answers on its own HTTP/2 stream and would
// otherwise race them).
func letClientRead(f *grpctest.Fake, n int) {
	deadline := time.Now().Add(3 * time.Second)
	for f.Sent() < n && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
}

func hasLate(st *state.Store) bool {
	_, ok := st.Client("late")
	return ok
}

// Gap 1, Connect: the update stream is registered BEFORE SyncClient, so an
// update the server publishes after taking the snapshot is delivered.
func TestConnect_UpdatePublishedAfterSnapshotIsNotLost(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	h := newBound(t, f, nil, 0)
	publishAfterSnapshot(f, h.st, false)

	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !waitFor(t, func() bool { return hasLate(h.st) }) {
		t.Fatalf("an update published after the snapshot was lost; roster %v, calls %v",
			h.st.Snapshot().Clients, f.Calls())
	}
	calls := f.Calls()
	if sub, sync := indexOfNth(calls, "SubscribeToUpdates", 1), indexOfNth(calls, "SyncClient", 1); sub < 0 || sync < sub {
		t.Fatalf("calls = %v, want SubscribeToUpdates registered before SyncClient", calls)
	}
}

// Gap 1, Reconnect: same ordering on the RECONNECT path.
func TestReconnect_UpdatePublishedAfterSnapshotIsNotLost(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.EndStream(status.Error(codes.Unavailable, "drop"))
	if !waitFor(t, func() bool { return h.rec.countOf(events.ConnDisconnected) == 1 }) {
		t.Fatalf("no control loss; saw %v", h.rec.all())
	}
	publishAfterSnapshot(f, h.st, false)

	if err := h.s.Reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if !waitFor(t, func() bool { return hasLate(h.st) }) {
		t.Fatalf("an update published after the reconnect snapshot was lost; roster %v, calls %v",
			h.st.Snapshot().Clients, f.Calls())
	}
}

// Gap 2, Connect: an update delivered on the stream before the snapshot is
// applied must survive the snapshot (ResetRoster), not be wiped by it.
func TestConnect_UpdateDeliveredBeforeSnapshotSurvivesIt(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	h := newBound(t, f, nil, 0)
	publishAfterSnapshot(f, h.st, true)

	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !waitFor(t, func() bool { return hasLate(h.st) }) {
		t.Fatalf("the update was wiped by the snapshot; roster %v", h.st.Snapshot().Clients)
	}
}

// Gap 2, resync: the same on the SV2 C13 resync path, which already
// subscribed first but started routing before SyncClient answered.
func TestResync_UpdateDeliveredBeforeSnapshotSurvivesIt(t *testing.T) {
	f := grpctest.NewFake()
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}}, "secret")
	h, _ := connected(t, f)
	publishAfterSnapshot(f, h.st, true)

	f.EndStream(errFellBehind)
	if !waitFor(t, func() bool {
		return countCalls(f.Calls(), "SyncClient") == 2 && h.rec.countOf(events.ConnConnected) == 2
	}) {
		t.Fatalf("resync did not complete; calls %v, states %v", f.Calls(), h.rec.all())
	}
	if !waitFor(t, func() bool { return hasLate(h.st) }) {
		t.Fatalf("the resync snapshot wiped an update delivered before it; roster %v", h.st.Snapshot().Clients)
	}
	if h.st.Snapshot().Clients["a"] == nil {
		t.Fatalf("roster %v lost the snapshot's own client", h.st.Snapshot().Clients)
	}
}

// Held updates replay IN ORDER on top of the snapshot: a join followed by a
// leave of the same client ends with the client absent, and a rename lands
// on the last name.
func TestConnect_HeldUpdatesReplayInOrder(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}}, "secret")
	h := newBound(t, f, nil, 0)
	gone := "gone"
	f.OnNextSync(func() {
		f.Publish(joined("gone", "Gone"))
		f.Publish(&srspb.ServerUpdate{
			Type:   srspb.ServerUpdate_CLIENT_LEFT,
			Update: &srspb.ServerUpdate_ClientUpdate{ClientUpdate: &srspb.ClientUpdate{ClientGuid: &gone}},
		})
		f.Publish(joined("a", "A1"))
		f.Publish(joined("a", "A2"))
		letClientRead(f, 4)
	})
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !waitFor(t, func() bool {
		c, ok := h.st.Client("a")
		return ok && c.GetName() == "A2"
	}) {
		t.Fatalf("roster %v, want a renamed to A2", h.st.Snapshot().Clients)
	}
	if _, ok := h.st.Client("gone"); ok {
		t.Fatalf("roster %v, want gone removed (join then leave replayed in order)", h.st.Snapshot().Clients)
	}
}

// More updates arrive while the snapshot loads than the hold keeps: that is
// a fell-behind (C13), not a silent drop. The session resyncs: a second
// subscription, a second SyncClient, connected again, no disconnected.
func TestConnect_HoldOverflowResyncs(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	h := newBound(t, f, nil, 0)
	n := session.PendingUpdateCap + 1
	f.OnNextSync(func() {
		for i := 0; i < n; i++ {
			f.Publish(joined(fmt.Sprintf("c%d", i), "C"))
		}
		letClientRead(f, n)
	})
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !waitFor(t, func() bool {
		return countCalls(f.Calls(), "SubscribeToUpdates") == 2 && countCalls(f.Calls(), "SyncClient") == 2 &&
			h.rec.countOf(events.ConnConnected) == 2
	}) {
		t.Fatalf("an overflowing hold did not resync; calls %v, states %v", f.Calls(), h.rec.all())
	}
	if got := h.rec.countOf(events.ConnDisconnected); got != 0 {
		t.Fatalf("an overflowing hold reported disconnected %d times; saw %v", got, h.rec.all())
	}
	if !waitFor(t, func() bool { return f.ActiveStreams() == 1 }) {
		t.Fatalf("%d update streams open after the resync, want 1 (the overflowed one leaked)", f.ActiveStreams())
	}
}

// A subscription refused with Unauthenticated during Connect fails the
// Connect with that error, exactly as SyncClient's Unauthenticated did when
// SyncClient was the first call after GuestLogin: no SyncClient, no
// logged_in, no session-ended notice, and disconnected published.
func TestConnect_SubscribeRefusedUnauthenticatedFailsConnect(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	f.Fail("SubscribeToUpdates", status.Error(codes.Unauthenticated, "session not found: log in again"))
	h := newBound(t, f, nil, 0)

	err := h.connect()
	if err == nil {
		t.Fatal("connect succeeded with a refused subscription")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("connect err = %v, want the Unauthenticated status", err)
	}
	if got := countCalls(f.Calls(), "SyncClient"); got != 0 {
		t.Fatalf("SyncClient called %d times after a refused subscription", got)
	}
	if h.em.count(events.EventAuthSession, "logged_in") != 0 || h.em.count(events.EventAuthSessionEnded, nil) != 0 {
		t.Fatal("a refused subscription during Connect logged in or emitted session_ended")
	}
	if h.last() != events.ConnDisconnected {
		t.Fatalf("states = %v, want disconnected last", h.rec.all())
	}
}

// Connect waits for the SV2 C17 ack before SyncClient, like the resync.
func TestConnect_WaitsForSubscriptionAckBeforeSync(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = "secret"
	f.SetSubscribeDelay(150 * time.Millisecond)
	h := newBound(t, f, nil, 0)
	publishAfterSnapshot(f, h.st, false)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !waitFor(t, func() bool { return hasLate(h.st) }) {
		t.Fatalf("with a slow registration the update was lost; calls %v", f.Calls())
	}
}
