package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// stateRecorder collects every control transition the session reports
// through Deps.OnControlState.
type stateRecorder struct {
	mu     sync.Mutex
	states []events.ConnectionState
}

func (r *stateRecorder) add(s events.ConnectionState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *stateRecorder) all() []events.ConnectionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.ConnectionState(nil), r.states...)
}

func (r *stateRecorder) countOf(want events.ConnectionState) int {
	n := 0
	for _, s := range r.all() {
		if s == want {
			n++
		}
	}
	return n
}

// waitFor polls cond until it holds or the deadline passes. The stream
// goroutine reports asynchronously, so there is nothing to synchronise on
// from the test side.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestStreamDrop_EmitsDisconnected(t *testing.T) {
	// The core Phase 6 gap (G1): before this, ConsumeUpdates' terminating
	// error went into `_ =` and the UI reported connected indefinitely.
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	f.CloseStream() // the server drops the subscription

	if !waitFor(t, func() bool { return rec.countOf(events.ConnDisconnected) == 1 }) {
		t.Fatalf("no disconnected after the stream dropped; saw %v", rec.all())
	}
}

func TestDisconnect_DoesNotDoubleReportLoss(t *testing.T) {
	// Disconnect publishes its own disconnected. If the stream goroutine
	// also reported one, every clean logout would show the user a
	// DISCONNECTED banner on the way out.
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	// Give any stray goroutine every chance to misbehave.
	time.Sleep(100 * time.Millisecond)

	if got := rec.countOf(events.ConnDisconnected); got != 1 {
		t.Errorf("disconnected reported %d times on a clean logout, want 1; saw %v",
			got, rec.all())
	}
}

func TestReconnect_StaleStreamCannotReportOverTheNewOne(t *testing.T) {
	// The generation guard. Reconnect cancels the old stream and opens a
	// new one; without the guard the old goroutine's termination lands on
	// the fresh, healthy connection and reports it dead.
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.Reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	states := rec.all()
	if len(states) == 0 || states[len(states)-1] != events.ConnConnected {
		t.Errorf("final state = %v, want connected after a successful reconnect", states)
	}
}

func TestPingOnce_MeasuresWhileConnected(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	s := session.New(state.New(), &capEmitter{}, session.Deps{Dialer: dial, Version: "test"})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	rtt, err := s.PingOnce(context.Background(), -1)
	if err != nil {
		t.Fatalf("PingOnce: %v", err)
	}
	if rtt < 0 {
		t.Errorf("PingOnce returned %d ms, want a non-negative measurement", rtt)
	}
}

func TestPingOnce_ErrorsWhileDisconnected(t *testing.T) {
	// The Monitor skips probing while disconnected, but a probe racing a
	// teardown must still fail cleanly rather than nil-deref the control
	// client.
	s := session.New(state.New(), &capEmitter{}, session.Deps{Version: "test"})

	if _, err := s.PingOnce(context.Background(), -1); err == nil {
		t.Fatal("PingOnce() = nil error while never connected, want an error")
	}
}

func TestKeepaliveRespectsTheServersEnforcementFloor(t *testing.T) {
	// The server sets KeepaliveEnforcementPolicy{MinTime: 60s}. A client
	// configured below that floor is not merely suboptimal: gRPC answers
	// with GOAWAY too_many_pings and kills the connection. Pinned as a
	// constant test because the value is a cross-repo contract that nothing
	// else in this repo would catch drifting.
	if session.ClientKeepaliveTime() < 60*time.Second {
		t.Errorf("client keepalive Time = %v, want >= 60s (the server's MinTime)",
			session.ClientKeepaliveTime())
	}
}

// Review Focus #2. On a real cable pull both detectors fire: the probe
// threshold and the stream's own death. The user must see one disconnected,
// and the second path must not re-close an already-closed connection.
func TestBothDetectors_ReportLossExactlyOnce(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Both detectors, as close to simultaneously as the test can arrange.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.MarkControlLost() }()
	go func() { defer wg.Done(); f.CloseStream() }()
	wg.Wait()

	time.Sleep(150 * time.Millisecond)

	if got := rec.countOf(events.ConnDisconnected); got != 1 {
		t.Errorf("disconnected reported %d times, want exactly 1; saw %v", got, rec.all())
	}
}

func TestMarkControlLost_IsIdempotent(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	s.MarkControlLost()
	s.MarkControlLost()
	s.MarkControlLost()

	time.Sleep(100 * time.Millisecond)

	if got := rec.countOf(events.ConnDisconnected); got != 1 {
		t.Errorf("disconnected reported %d times for three calls, want 1", got)
	}
}
