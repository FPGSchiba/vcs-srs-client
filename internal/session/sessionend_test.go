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

var errUnknownSession = status.Error(codes.Unauthenticated, "session not found for /srs.SRSService/SyncClient: log in again")

func ended(reason string) events.SessionEndedPayload {
	return events.SessionEndedPayload{Reason: reason}
}

// hookRecorder records every OnSessionEnded call.
type hookRecorder struct {
	mu      sync.Mutex
	reasons []string
}

func (r *hookRecorder) fn(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reasons = append(r.reasons, reason)
}

func (r *hookRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reasons...)
}

// connected returns a harness that has connected and whose update stream is
// open on the server.
func connected(t *testing.T, f *grpctest.Fake) (*boundHarness, *hookRecorder) {
	t.Helper()
	if f.SyncVoiceSecret == "" {
		f.SyncVoiceSecret = "secret"
	}
	h := newBound(t, f, nil, 0)
	hooks := &hookRecorder{}
	h.s.SetSessionEndedHook(hooks.fn)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !waitFor(t, func() bool { return f.ActiveStreams() == 1 }) {
		t.Fatal("update stream never opened")
	}
	return h, hooks
}

// assertEndedTail checks the last two events are session_ended{reason} then
// session_changed=logged_out.
func assertEndedTail(t *testing.T, h *boundHarness, reason string) {
	t.Helper()
	names, payloads := h.em.seq()
	n := len(names)
	if n < 2 || names[n-2] != events.EventAuthSessionEnded || payloads[n-2] != ended(reason) ||
		names[n-1] != events.EventAuthSession || payloads[n-1] != "logged_out" {
		t.Fatalf("event tail = %v %v, want auth:session_ended{%s} then logged_out", names, payloads, reason)
	}
}

func TestReconnect_UnauthenticatedEndsSession(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.Fail("SyncClient", errUnknownSession)

	err := h.s.Reconnect(context.Background())
	if !errors.Is(err, session.ErrSessionEnded) {
		t.Fatalf("Reconnect = %v, want ErrSessionEnded", err)
	}
	assertEndedTail(t, h, "expired")

	f.Fail("SyncClient", nil)
	if err := h.s.Reconnect(context.Background()); err == nil || !strings.Contains(err.Error(), "never connected") {
		t.Fatalf("Reconnect after session end = %v, want never connected", err)
	}
}

func TestReconnect_InternalIsOrdinary(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.Fail("SyncClient", status.Error(codes.Internal, "token verification unavailable"))

	err := h.s.Reconnect(context.Background())
	if err == nil || errors.Is(err, session.ErrSessionEnded) {
		t.Fatalf("Reconnect = %v, want an ordinary failure", err)
	}
	if h.em.count(events.EventAuthSession, "logged_out") != 0 {
		t.Fatal("an Internal SyncClient error logged the user out")
	}
	f.Fail("SyncClient", nil)
	if err := h.s.Reconnect(context.Background()); err != nil {
		t.Fatalf("a later Reconnect must dial again with the kept token: %v", err)
	}
}

func TestReconnect_EmptySecretEndsSession(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.SetSync(map[string]*srspb.ClientInfo{"a": {Name: "A"}}, "")

	err := h.s.Reconnect(context.Background())
	if !errors.Is(err, session.ErrSessionEnded) {
		t.Fatalf("Reconnect = %v, want ErrSessionEnded", err)
	}
	assertEndedTail(t, h, "expired")
}

func TestConnect_EmptySecretKeepsStored(t *testing.T) {
	f := grpctest.NewFake()
	h := newBound(t, f, nil, 0)
	h.st.SetVoiceCredentials("s0", "", "")
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if secret, _, _ := h.st.VoiceCredentials(); secret != "s0" {
		t.Fatalf("secret = %q, want the stored s0 kept", secret)
	}
}

func TestStreamEnd_KickedEndsSessionOnce(t *testing.T) {
	f := grpctest.NewFake()
	h, hooks := connected(t, f)
	f.EndStream(status.Error(codes.Unauthenticated, "session ended: kicked"))

	if !waitFor(t, func() bool { return h.em.count(events.EventAuthSession, "logged_out") == 1 }) {
		t.Fatal("no logged_out after the server kicked us")
	}
	time.Sleep(100 * time.Millisecond)
	if got := h.em.count(events.EventAuthSessionEnded, nil); got != 1 {
		t.Fatalf("session_ended emitted %d times, want 1", got)
	}
	assertEndedTail(t, h, "kicked")
	if got := hooks.all(); len(got) != 1 || got[0] != "kicked" {
		t.Fatalf("hook calls = %v, want [kicked]", got)
	}
	if h.s.Control() != nil {
		t.Fatal("Control() != nil after the session ended")
	}
}

func TestStreamEnd_UnknownSessionEnds(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.EndStream(status.Error(codes.Unauthenticated, "session not found: log in again"))

	if !waitFor(t, func() bool { return h.em.count(events.EventAuthSessionEnded, nil) == 1 }) {
		t.Fatal("no session_ended for an unknown session")
	}
	assertEndedTail(t, h, "expired")
}

func TestStreamEnd_AbortedIsControlLoss(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.EndStream(status.Error(codes.Aborted, "superseded by a newer subscription"))

	if !waitFor(t, func() bool { return h.rec.countOf(events.ConnDisconnected) == 1 }) {
		t.Fatalf("no disconnected after Aborted; saw %v", h.rec.all())
	}
	time.Sleep(50 * time.Millisecond)
	if h.em.count(events.EventAuthSession, "logged_out") != 0 {
		t.Fatal("Aborted logged the user out")
	}
	if err := h.s.Reconnect(context.Background()); err != nil {
		t.Fatalf("token must be kept after Aborted; Reconnect: %v", err)
	}
}

// SV1 C12: the server ends our stream because WE disconnected.
func TestStreamEnd_AfterUserDisconnectIgnored(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	if err := h.s.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	f.EndStream(status.Error(codes.Unauthenticated, "session ended: disconnected"))
	time.Sleep(150 * time.Millisecond)

	if got := h.em.count(events.EventAuthSession, "logged_out"); got != 1 {
		t.Fatalf("logged_out emitted %d times, want 1", got)
	}
	if got := h.em.count(events.EventAuthSessionEnded, nil); got != 0 {
		t.Fatalf("session_ended emitted %d times after a user Disconnect, want 0", got)
	}
}

func TestPingOnce_UnauthenticatedEndsSession(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.Fail("Ping", status.Error(codes.Unauthenticated, "session not found for /srs.SRSService/Ping: log in again"))

	_, err := h.s.PingOnce(context.Background(), -1)
	if !errors.Is(err, session.ErrSessionEnded) {
		t.Fatalf("PingOnce = %v, want ErrSessionEnded", err)
	}
	assertEndedTail(t, h, "expired")
}

func TestUpdateRadioInfo_UnauthenticatedEndsSession(t *testing.T) {
	f := grpctest.NewFake()
	h, _ := connected(t, f)
	f.Fail("UpdateRadioInfo", status.Error(codes.Unauthenticated, "session not found for /srs.SRSService/UpdateRadioInfo: log in again"))

	err := h.s.UpdateRadioInfo(context.Background(), &srspb.RadioInfo{})
	if !errors.Is(err, session.ErrSessionEnded) {
		t.Fatalf("UpdateRadioInfo = %v, want ErrSessionEnded", err)
	}
	assertEndedTail(t, h, "expired")
}

// Review Focus #3: the stream-end and a Reconnect's SyncClient both see the
// session gone. The user sees one session_ended and one logged_out.
func TestSessionEnded_StreamAndReconnectRaceFiresOnce(t *testing.T) {
	f := grpctest.NewFake()
	h, hooks := connected(t, f)
	f.Fail("SyncClient", errUnknownSession)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); f.EndStream(status.Error(codes.Unauthenticated, "session ended: kicked")) }()
	go func() { defer wg.Done(); _ = h.s.Reconnect(context.Background()) }()
	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	if got := h.em.count(events.EventAuthSessionEnded, nil); got != 1 {
		t.Fatalf("session_ended emitted %d times, want 1", got)
	}
	if got := h.em.count(events.EventAuthSession, "logged_out"); got != 1 {
		t.Fatalf("logged_out emitted %d times, want 1", got)
	}
	if got := hooks.all(); len(got) != 1 {
		t.Fatalf("hook called %d times, want 1", len(got))
	}
}
