package voice

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Two distinct secrets of exactly VoiceSecretLen bytes. They are asymmetric
// with respect to each other and to byte reversal, so a HELLO that carried
// the wrong slice of the payload could not accidentally match.
const (
	goodSecret = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	badSecret  = "ZYXWVUTSRQPONMLKJIHGFEDCBAzyxwvutsrqponmlkj"
)

// ladderRungs is the HELLO retry schedule from the design doc, section 7.1,
// written out by hand on purpose. Deriving it from helloLadder would make
// these tests pass for whatever schedule the implementation happens to use.
var ladderRungs = []time.Duration{
	1500 * time.Millisecond,
	3000 * time.Millisecond,
	4500 * time.Millisecond,
	6000 * time.Millisecond,
	7500 * time.Millisecond,
}

// specKeepalive and specRetry likewise restate the spec rather than reading
// the implementation's constants.
const (
	specKeepalive = 5 * time.Second
	specRetry     = 15 * time.Second
)

// fakeClock is a manually driven clock. step, when non-zero, also advances
// it by that much on every read, which is how the RTT test guarantees that
// two distinct clock reads are separated by a measurable interval without
// any real sleeping.
type fakeClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(c.step)
	return c.now
}

// peek reads the clock without applying step.
func (c *fakeClock) peek() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// stateRecorder captures everything delivered to Options.OnState.
type stateRecorder struct {
	mu      sync.Mutex
	seen    map[State]int
	errs    []error
	lastErr error
}

func newStateRecorder() *stateRecorder {
	return &stateRecorder{seen: map[State]int{}}
}

func (r *stateRecorder) on(st State, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[st]++
	if err != nil {
		r.errs = append(r.errs, err)
		r.lastErr = err
	}
}

func (r *stateRecorder) count(st State) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[st]
}

func (r *stateRecorder) saw(st State) bool { return r.count(st) > 0 }

func (r *stateRecorder) errCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.errs)
}

// waitFor polls cond until it holds or the real-time budget runs out. Every
// test's forward progress comes from the fake clock; this only bounds how
// long we are willing to wait for goroutines to notice.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Microsecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// settle gives the session's goroutines a few poll cycles of real time in
// which to do something, so that a "this must NOT have happened" assertion
// is not just observing that nothing has happened yet.
func settle() { time.Sleep(30 * time.Millisecond) }

// parkedAfter blocks until the session's lifecycle goroutine has parked on a
// wait newer than prev, and returns that wait's sequence number and deadline.
//
// Every clock advance in these tests is gated on this. Moving an injected
// clock while the session is between two waits would let the jump be counted
// against the wrong interval, which is a flaky test, not a real defect.
func parkedAfter(t *testing.T, s *Session, prev uint64) (uint64, time.Time) {
	t.Helper()
	var seq uint64
	var deadline time.Time
	waitFor(t, "the session to park on a new deadline", func() bool {
		seq, deadline = s.waitingOn()
		return seq > prev
	})
	return seq, deadline
}

func testOptions(clk *fakeClock, rec *stateRecorder) Options {
	return Options{
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnState: rec.on,
		Clock:   clk.Now,
		poll:    time.Millisecond,
	}
}

// dialTestSession dials the fixture server and registers cleanup.
func dialTestSession(t *testing.T, ts *testServer, secret string, opt Options) *Session {
	t.Helper()
	s, err := Dial(Sources{Update: ts.addr()}, uuid.New(), secret, opt)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// walkLadder steps the injected clock through every rung of the HELLO
// ladder, checking at each rung that exactly one more HELLO went out and
// that the rung's length is the one the spec names. It returns the sequence
// number and clock position reached, so a caller can carry on from there.
func walkLadder(t *testing.T, s *Session, ts *testServer, clk *fakeClock) (uint64, time.Time) {
	t.Helper()
	seq, now := uint64(0), clk.peek()
	for i, rung := range ladderRungs {
		nextSeq, deadline := parkedAfter(t, s, seq)
		if got := deadline.Sub(now); got != rung {
			t.Fatalf("ladder rung %d = %v, want %v", i+1, got, rung)
		}
		// The session parks as soon as it has written the HELLO, which can be
		// before the fixture has read it off the socket.
		want := i + 1
		waitFor(t, "the HELLO for this ladder attempt", func() bool { return ts.helloCount() >= want })
		if got := ts.helloCount(); got != want {
			t.Fatalf("helloCount = %d on ladder attempt %d, want %d", got, want, want)
		}
		clk.advance(rung)
		seq, now = nextSeq, deadline
	}
	return seq, now
}

func TestSessionSecretFixtureLengths(t *testing.T) {
	if len(goodSecret) != VoiceSecretLen || len(badSecret) != VoiceSecretLen {
		t.Fatalf("test secrets must be exactly %d bytes, got %d and %d",
			VoiceSecretLen, len(goodSecret), len(badSecret))
	}
	if goodSecret == badSecret {
		t.Fatal("test secrets must differ")
	}
}

func TestSessionStateString(t *testing.T) {
	want := map[State]string{
		StateIdle:        "idle",
		StateResolving:   "resolving",
		StateHandshaking: "handshaking",
		StateConnected:   "connected",
		StateRebinding:   "rebinding",
		StateRetrying:    "retrying",
		StateClosed:      "closed",
	}
	for st, name := range want {
		if got := st.String(); got != name {
			t.Errorf("State(%d).String() = %q, want %q", int(st), got, name)
		}
	}
	if got := State(99).String(); got != "unknown" {
		t.Errorf("State(99).String() = %q, want %q", got, "unknown")
	}
}

func TestSessionHandshakeSucceeds(t *testing.T) {
	ts := newTestServer(t, goodSecret)
	clk := newFakeClock()
	rec := newStateRecorder()

	s := dialTestSession(t, ts, goodSecret, testOptions(clk, rec))

	// Wait on the callback, not on State(): setState publishes the new state
	// before it delivers the callback, so waiting on State() and then reading
	// the recorder would race.
	waitFor(t, "StateConnected", func() bool { return rec.saw(StateConnected) })
	if s.State() != StateConnected {
		t.Fatalf("State = %v after connecting, want connected", s.State())
	}
	if !rec.saw(StateResolving) || !rec.saw(StateHandshaking) {
		t.Fatalf("missing state transitions: resolving=%d handshaking=%d",
			rec.count(StateResolving), rec.count(StateHandshaking))
	}
	settle()
	if got := ts.helloCount(); got != 1 {
		t.Fatalf("helloCount = %d, want 1 (no retry should fire on a clean ACK)", got)
	}
	if rec.errCount() != 0 {
		t.Fatal("unexpected error delivered on a clean handshake")
	}
}

func TestSessionWrongSecretNeverConnects(t *testing.T) {
	ts := newTestServer(t, goodSecret)
	clk := newFakeClock()
	rec := newStateRecorder()

	s := dialTestSession(t, ts, badSecret, testOptions(clk, rec))

	// The fixture never ACKs a secret it does not accept, so every rung of
	// the ladder must expire.
	walkLadder(t, s, ts, clk)

	waitFor(t, "StateRetrying carrying a reason", func() bool {
		return rec.saw(StateRetrying) && rec.errCount() > 0
	})
	if s.State() != StateRetrying {
		t.Fatalf("State = %v after the ladder was exhausted, want retrying", s.State())
	}
	if rec.saw(StateConnected) {
		t.Fatal("reached StateConnected with a secret the server rejects")
	}
}

func TestSessionRetryLadderResendsHello(t *testing.T) {
	ts := newTestServer(t, goodSecret)
	ts.setDropHello(true)
	clk := newFakeClock()
	rec := newStateRecorder()

	s := dialTestSession(t, ts, goodSecret, testOptions(clk, rec))
	start := clk.peek()

	seq1, dl1 := parkedAfter(t, s, 0)
	waitFor(t, "the first HELLO", func() bool { return ts.helloCount() >= 1 })
	if got := ts.helloCount(); got != 1 {
		t.Fatalf("helloCount = %d on the first attempt, want 1", got)
	}
	if got := dl1.Sub(start); got != ladderRungs[0] {
		t.Fatalf("first rung = %v, want %v", got, ladderRungs[0])
	}

	// Just short of the first rung: no resend yet.
	clk.advance(ladderRungs[0] - 100*time.Millisecond)
	settle()
	if got := ts.helloCount(); got != 1 {
		t.Fatalf("helloCount = %d before the first rung elapsed, want 1", got)
	}

	// Crossing the first rung resends.
	clk.advance(100 * time.Millisecond)
	seq2, dl2 := parkedAfter(t, s, seq1)
	waitFor(t, "the second HELLO", func() bool { return ts.helloCount() >= 2 })
	if got := ts.helloCount(); got != 2 {
		t.Fatalf("helloCount = %d after the first rung, want 2", got)
	}
	if got := dl2.Sub(dl1); got != ladderRungs[1] {
		t.Fatalf("second rung = %v, want %v", got, ladderRungs[1])
	}

	// Crossing the second rung resends again: three HELLOs in total.
	clk.advance(ladderRungs[1])
	seq3, dl3 := parkedAfter(t, s, seq2)
	waitFor(t, "the third HELLO", func() bool { return ts.helloCount() >= 3 })
	if got := ts.helloCount(); got != 3 {
		t.Fatalf("helloCount = %d after the second rung, want 3", got)
	}
	if got := dl3.Sub(dl2); got != ladderRungs[2] {
		t.Fatalf("third rung = %v, want %v", got, ladderRungs[2])
	}
	_ = seq3

	settle()
	if got := ts.helloCount(); got != 3 {
		t.Fatalf("helloCount = %d with the third rung still running, want 3", got)
	}
	if rec.saw(StateConnected) {
		t.Fatal("reached StateConnected although every HELLO was dropped")
	}
	// No handshake means no keepalive, so nothing can have been measured.
	if got := s.RTT(); got != 0 {
		t.Fatalf("RTT = %v with no keepalive ever answered, want 0", got)
	}
}

func TestSessionExhaustionRetriesRatherThanSurrendering(t *testing.T) {
	ts := newTestServer(t, goodSecret)
	ts.setDropHello(true)
	clk := newFakeClock()
	rec := newStateRecorder()

	s := dialTestSession(t, ts, goodSecret, testOptions(clk, rec))
	seq, now := walkLadder(t, s, ts, clk)

	waitFor(t, "StateRetrying carrying a reason", func() bool {
		return rec.saw(StateRetrying) && rec.errCount() > 0
	})
	if got := ts.helloCount(); got != len(ladderRungs) {
		t.Fatalf("helloCount = %d at exhaustion, want %d", got, len(ladderRungs))
	}

	// The exhausted ladder must be waiting out the retry interval, not
	// falling through to a keepalive path and not giving up.
	_, deadline := parkedAfter(t, s, seq)
	if got := deadline.Sub(now); got != specRetry {
		t.Fatalf("retry interval = %v, want %v", got, specRetry)
	}
	settle()
	if got := ts.helloCount(); got != len(ladderRungs) {
		t.Fatalf("helloCount = %d before the retry interval elapsed, want %d", got, len(ladderRungs))
	}
	if got := ts.keepaliveCount(); got != 0 {
		t.Fatalf("keepaliveCount = %d on an unbound socket, want 0: keepalives from an "+
			"unbound address are dropped, so that fallback cannot work here", got)
	}

	// Then the whole ladder runs again.
	clk.advance(specRetry)
	waitFor(t, "the ladder to re-run", func() bool { return ts.helloCount() >= len(ladderRungs)+1 })

	if rec.saw(StateClosed) {
		t.Fatal("session closed itself on exhaustion; it must keep retrying")
	}
	if s.State() == StateClosed {
		t.Fatal("State() == StateClosed on exhaustion; it must keep retrying")
	}
}

func TestSessionBindingLossTriggersReHello(t *testing.T) {
	ts := newTestServer(t, goodSecret)
	clk := newFakeClock()
	rec := newStateRecorder()

	s := dialTestSession(t, ts, goodSecret, testOptions(clk, rec))
	start := clk.peek()
	waitFor(t, "StateConnected", func() bool { return rec.saw(StateConnected) })

	// The handshake consumed wait #1 (the first ladder rung, satisfied by the
	// ACK); wait #2 onwards are keepalive intervals.
	seq, now := uint64(1), start

	// While keepalives are answered, six intervals -- twice the binding-loss
	// threshold -- must not provoke a re-HELLO. This is what proves an
	// answered keepalive actually resets the unanswered counter.
	for i := 0; i < 6; i++ {
		nextSeq, deadline := parkedAfter(t, s, seq)
		if got := deadline.Sub(now); got != specKeepalive {
			t.Fatalf("keepalive interval %d = %v, want %v", i+1, got, specKeepalive)
		}
		wantKA := i + 1
		waitFor(t, "the keepalive for this interval", func() bool { return ts.keepaliveCount() >= wantKA })
		if got := ts.keepaliveCount(); got != wantKA {
			t.Fatalf("keepaliveCount = %d at interval %d, want %d", got, wantKA, wantKA)
		}
		clk.advance(specKeepalive)
		seq, now = nextSeq, deadline
	}
	if got := ts.helloCount(); got != 1 {
		t.Fatalf("helloCount = %d while keepalives were answered, want 1", got)
	}
	if s.State() != StateConnected {
		t.Fatalf("State = %v while keepalives were answered, want connected", s.State())
	}

	// Now the binding goes silently dead: the server stops answering, but
	// every write still succeeds and nothing anywhere reports an error.
	ts.setDropKeepalive(true)

	// Two unanswered intervals are not enough to call it lost.
	for i := 0; i < 2; i++ {
		nextSeq, deadline := parkedAfter(t, s, seq)
		clk.advance(deadline.Sub(now))
		seq, now = nextSeq, deadline
	}
	settle()
	if got := ts.helloCount(); got != 1 {
		t.Fatalf("helloCount = %d after only two unanswered keepalives, want 1", got)
	}

	// A third -- and, if the reply to the keepalive that was in flight when
	// the drop was installed made it home, a fourth -- tips it over into a
	// re-HELLO on the same socket.
	for i := 0; i < 4 && ts.helloCount() < 2; i++ {
		nextSeq, deadline := parkedAfter(t, s, seq)
		clk.advance(deadline.Sub(now))
		seq, now = nextSeq, deadline
	}
	waitFor(t, "a re-HELLO after unanswered keepalives", func() bool { return ts.helloCount() >= 2 })
	if !rec.saw(StateRebinding) {
		t.Fatal("no StateRebinding delivered on binding loss")
	}
	waitFor(t, "recovery to StateConnected", func() bool { return s.State() == StateConnected })
}

func TestSessionRTTMeasured(t *testing.T) {
	ts := newTestServer(t, goodSecret)
	clk := newFakeClock()
	clk.step = time.Millisecond // every clock read advances, so a round trip is measurable
	rec := newStateRecorder()

	s := dialTestSession(t, ts, goodSecret, testOptions(clk, rec))

	waitFor(t, "StateConnected", func() bool { return rec.saw(StateConnected) })
	waitFor(t, "a keepalive round trip", func() bool { return s.RTT() > 0 })
}

func TestSessionCloseSendsBye(t *testing.T) {
	ts := newTestServer(t, goodSecret)
	clk := newFakeClock()
	rec := newStateRecorder()

	s := dialTestSession(t, ts, goodSecret, testOptions(clk, rec))
	waitFor(t, "StateConnected", func() bool { return rec.saw(StateConnected) })

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitFor(t, "a BYE from the bound address", func() bool {
		total, fromBound := ts.byeCounts()
		return total >= 1 && fromBound >= 1
	})
	if s.State() != StateClosed {
		t.Fatalf("State after Close = %v, want closed", s.State())
	}
	if !rec.saw(StateClosed) {
		t.Fatal("no StateClosed delivered to OnState")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
