package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// testPostDial is the bound for tests that expect a timeout. A session that
// must also complete a Connect uses testPostDialOK instead: the bcrypt hash
// is inside the post-dial bound and takes ~0.5 s under -race on its own.
const (
	testPostDial   = 200 * time.Millisecond
	testPostDialOK = 5 * time.Second
)

// boundHarness is a session against a fake, with a recorder on every
// control transition.
type boundHarness struct {
	f   *grpctest.Fake
	s   *session.Session
	st  *state.Store
	em  *capEmitter
	rec *stateRecorder
}

// newBound builds a session against f. A nil dial uses the fake's own
// bufconn dialer. postDial 0 uses testPostDialOK.
func newBound(t *testing.T, f *grpctest.Fake, dial session.Dialer, postDial time.Duration) *boundHarness {
	t.Helper()
	if dial == nil {
		d, cleanup := grpctest.Start(t, f)
		t.Cleanup(cleanup)
		dial = d
	}
	if postDial == 0 {
		postDial = testPostDialOK
	}
	h := &boundHarness{f: f, st: state.New(), em: &capEmitter{}, rec: &stateRecorder{}}
	h.s = session.New(h.st, h.em, session.Deps{
		Dialer:          dial,
		Version:         "test",
		OnControlState:  h.rec.add,
		PostDialTimeout: postDial,
	})
	return h
}

func (h *boundHarness) connect() error {
	return h.s.Connect(context.Background(), "localhost:0", "n", "p", "u")
}

func (h *boundHarness) last() events.ConnectionState {
	all := h.rec.all()
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func countCalls(xs []string, want string) int {
	n := 0
	for _, x := range xs {
		if x == want {
			n++
		}
	}
	return n
}

// cancelWhenCalled calls CancelConnect shortly after method has been entered
// on the fake.
func cancelWhenCalled(h *boundHarness, method string) {
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if contains(h.f.Calls(), method) {
				time.Sleep(20 * time.Millisecond)
				h.s.CancelConnect()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
}

func TestConnect_BoundedWhenInitAuthStalls(t *testing.T) {
	f := grpctest.NewFake()
	f.Block("InitAuth", make(chan struct{}))
	h := newBound(t, f, nil, testPostDial)

	start := time.Now()
	err := h.connect()
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("Connect took %v against a stalled InitAuth", el)
	}
	if !errors.Is(err, session.ErrServerNotResponding) {
		t.Fatalf("err = %v, want ErrServerNotResponding", err)
	}
	if h.last() != events.ConnDisconnected {
		t.Fatalf("final state = %q, want disconnected; saw %v", h.last(), h.rec.all())
	}
	if h.s.Control() != nil {
		t.Fatal("Control() != nil after a timed-out connect")
	}
}

func TestReconnect_BoundedWhenSyncStalls(t *testing.T) {
	// The bound must also let the initial Connect (bcrypt included) finish,
	// so it is 2 s here rather than testPostDial.
	f := grpctest.NewFake()
	h := newBound(t, f, nil, 2*time.Second)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	f.Block("SyncClient", make(chan struct{}))

	start := time.Now()
	err := h.s.Reconnect(context.Background())
	if el := time.Since(start); el > 4*time.Second {
		t.Fatalf("Reconnect took %v against a stalled SyncClient", el)
	}
	if !errors.Is(err, session.ErrServerNotResponding) {
		t.Fatalf("err = %v, want ErrServerNotResponding", err)
	}
	if h.last() != events.ConnDisconnected {
		t.Fatalf("final state = %q, want disconnected", h.last())
	}
}

func TestCancelConnect_DuringGuestLogin(t *testing.T) {
	f := grpctest.NewFake()
	f.Block("GuestLogin", make(chan struct{}))
	h := newBound(t, f, nil, 10*time.Second)

	cancelWhenCalled(h, "GuestLogin")
	start := time.Now()
	err := h.connect()
	if el := time.Since(start); el > time.Second+500*time.Millisecond {
		t.Fatalf("cancelled Connect took %v", el)
	}
	if !errors.Is(err, session.ErrConnectCancelled) {
		t.Fatalf("err = %v, want ErrConnectCancelled", err)
	}
	if h.last() != events.ConnDisconnected {
		t.Fatalf("final state = %q, want disconnected", h.last())
	}
	if contains(f.Calls(), "Disconnect") {
		t.Fatalf("a Disconnect was sent with no token yet; calls %v", f.Calls())
	}
}

func TestCancelConnect_AfterGuestLoginSendsDisconnect(t *testing.T) {
	f := grpctest.NewFake()
	f.Block("SyncClient", make(chan struct{}))
	h := newBound(t, f, nil, 10*time.Second)

	cancelWhenCalled(h, "SyncClient")
	err := h.connect()
	if !errors.Is(err, session.ErrConnectCancelled) {
		t.Fatalf("err = %v, want ErrConnectCancelled", err)
	}
	calls := f.Calls()
	if len(calls) == 0 || calls[len(calls)-1] != "Disconnect" {
		t.Fatalf("calls = %v, want a best-effort Disconnect last", calls)
	}
}

func TestCancelConnect_DuringDial(t *testing.T) {
	blockingDial := func(ctx context.Context) (*grpc.ClientConn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	h := newBound(t, grpctest.NewFake(), blockingDial, 0)
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.s.CancelConnect()
	}()
	err := h.connect()
	if !errors.Is(err, session.ErrConnectCancelled) {
		t.Fatalf("err = %v, want ErrConnectCancelled", err)
	}
	if h.last() != events.ConnDisconnected {
		t.Fatalf("final state = %q, want disconnected", h.last())
	}
}

// Review Focus #1: a CANCEL that arrives the instant Connect succeeded is a
// no-op -- the user lands on the console, not on a half-torn session.
func TestCancelConnect_AfterSuccessIsNoOp(t *testing.T) {
	h := newBound(t, grpctest.NewFake(), nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	before := h.rec.countOf(events.ConnDisconnected)
	h.s.CancelConnect()
	time.Sleep(100 * time.Millisecond)
	if h.s.Control() == nil {
		t.Fatal("CancelConnect after success tore the session down")
	}
	if got := h.rec.countOf(events.ConnDisconnected); got != before {
		t.Fatalf("disconnected count %d -> %d after a no-op cancel", before, got)
	}
}

// GO-01: every failure path, cancel and timeout included, ends disconnected.
func TestConnect_EveryFailureEndsDisconnected(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(f *grpctest.Fake)
		dial     session.Dialer
		postDial time.Duration
		cancelOn string
	}{
		{name: "dial error", dial: func(context.Context) (*grpc.ClientConn, error) { return nil, errors.New("refused") }},
		{name: "no guest", setup: func(f *grpctest.Fake) { f.HasGuest = false }},
		{name: "login rejected", setup: func(f *grpctest.Fake) { f.RejectLogin = true }},
		{name: "sync error", setup: func(f *grpctest.Fake) { f.Fail("SyncClient", status.Error(codes.Internal, "x")) }},
		{name: "timeout", setup: func(f *grpctest.Fake) { f.Block("InitAuth", make(chan struct{})) }, postDial: testPostDial},
		{name: "cancel", setup: func(f *grpctest.Fake) { f.Block("GuestLogin", make(chan struct{})) },
			postDial: 10 * time.Second, cancelOn: "GuestLogin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := grpctest.NewFake()
			if tc.setup != nil {
				tc.setup(f)
			}
			h := newBound(t, f, tc.dial, tc.postDial)
			if tc.cancelOn != "" {
				cancelWhenCalled(h, tc.cancelOn)
			}
			if err := h.connect(); err == nil {
				t.Fatal("expected Connect to fail")
			}
			if h.last() != events.ConnDisconnected {
				t.Fatalf("final state = %q, want disconnected; saw %v", h.last(), h.rec.all())
			}
		})
	}
}

// Review finding (CL1): a CANCEL that arrives while Connect is still waiting
// for lifeMu -- here behind a Disconnect whose RPC is stalled -- used to be a
// no-op, because no attempt cancel was published yet. The connect then went
// on to log the user in after they pressed CANCEL.
func TestCancelConnect_WhileConnectWaitsForLifecycleLock(t *testing.T) {
	f := grpctest.NewFake()
	h := newBound(t, f, nil, 0)
	if err := h.connect(); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	release := make(chan struct{})
	f.Block("Disconnect", release)

	discDone := make(chan struct{})
	go func() {
		defer close(discDone)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.s.Disconnect(ctx)
	}()
	if !waitFor(t, func() bool { return contains(f.Calls(), "Disconnect") }) {
		t.Fatal("Disconnect RPC never reached")
	}

	connErr := make(chan error, 1)
	go func() { connErr <- h.connect() }()
	time.Sleep(100 * time.Millisecond) // Connect is now parked on lifeMu
	h.s.CancelConnect()
	close(release)
	<-discDone

	select {
	case err := <-connErr:
		if !errors.Is(err, session.ErrConnectCancelled) {
			t.Fatalf("Connect = %v, want ErrConnectCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return")
	}
	if h.s.Control() != nil {
		t.Fatal("a cancelled connect installed a session")
	}
}

// A CancelConnect with no attempt in flight or waiting must not carry over
// and cancel a later Connect.
func TestCancelConnect_IdleDoesNotCancelTheNextConnect(t *testing.T) {
	h := newBound(t, grpctest.NewFake(), nil, 0)
	h.s.CancelConnect()
	if err := h.connect(); err != nil {
		t.Fatalf("connect after an idle CancelConnect: %v", err)
	}
}
