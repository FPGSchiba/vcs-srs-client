package loadgen

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
)

func TestDialInsecureUsesClientKeepalive(t *testing.T) {
	want := keepalive.ClientParameters{Time: session.ClientKeepaliveTime(), Timeout: 10 * time.Second, PermitWithoutStream: true}
	if ControlKeepalive != want {
		t.Fatalf("ControlKeepalive = %+v, want %+v", ControlKeepalive, want)
	}
	if want.Time < 60*time.Second {
		t.Fatalf("keepalive %v is below the server floor", want.Time)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()
	go func() {
		for {
			c, err := lis.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	var rec ConnRecorder
	if rec.LocalAddr() != "" {
		t.Fatal("recorder must start empty")
	}
	conn, err := DialInsecure(lis.Addr().String(), &rec)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Connect()
	if !waitFor(func() bool { return rec.LocalAddr() != "" }) {
		t.Fatal("no local address recorded")
	}
	if _, _, err := net.SplitHostPort(rec.LocalAddr()); err != nil {
		t.Fatalf("local addr %q: %v", rec.LocalAddr(), err)
	}
}

type fakeTick struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func newFakeTick() *fakeTick { return &fakeTick{ch: make(chan time.Time)} }
func (f *fakeTick) tick(time.Duration) (<-chan time.Time, func()) {
	return f.ch, func() { f.stopped.Store(true) }
}

type deathCapture struct {
	mu       sync.Mutex
	n        int
	failures int
	err      error
	ch       chan struct{}
}

func newDeathCapture() *deathCapture { return &deathCapture{ch: make(chan struct{}, 4)} }
func (d *deathCapture) fn(failures int, err error) {
	d.mu.Lock()
	d.n++
	d.failures, d.err = failures, err
	d.mu.Unlock()
	d.ch <- struct{}{}
}

// fire sends one tick and waits for the ping it triggers to return.
func fire(t *testing.T, ft *fakeTick, handled <-chan struct{}) {
	t.Helper()
	select {
	case ft.ch <- time.Now():
	case <-time.After(2 * time.Second):
		t.Fatal("pinger did not take the tick")
	}
	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("ping did not complete")
	}
}

// settle lets the pinger goroutine record the result of the ping that just
// returned (the handled signal fires inside the ping func).
func settle(cond func() bool) bool { return waitFor(cond) }

func TestPingerThreeFailuresIsDeath(t *testing.T) {
	ft := newFakeTick()
	dc := newDeathCapture()
	var stats PingStats
	var fail atomic.Bool
	handled := make(chan struct{}, 1)
	ping := func(context.Context) (time.Duration, error) {
		defer func() { handled <- struct{}{} }()
		if fail.Load() {
			return 0, status.Error(codes.DeadlineExceeded, "slow")
		}
		return 3 * time.Millisecond, nil
	}
	p := startPinger(context.Background(), pingerCfg{Every: time.Second, Ping: ping, Tick: ft.tick, OnDead: dc.fn, Stats: &stats})
	defer p.stop()

	if p.SinceLastOK() != -1 {
		t.Fatalf("since_last_ok before any success = %v, want -1", p.SinceLastOK())
	}
	// 2 failures then a success: alive, the counter resets.
	fail.Store(true)
	fire(t, ft, handled)
	fire(t, ft, handled)
	if !settle(func() bool { return p.Failures() == 2 }) {
		t.Fatalf("failures = %d, want 2", p.Failures())
	}
	fail.Store(false)
	fire(t, ft, handled)
	if !settle(func() bool { return p.Failures() == 0 && p.SinceLastOK() >= 0 }) {
		t.Fatalf("failures = %d since_ok = %v after a success", p.Failures(), p.SinceLastOK())
	}
	if s := p.SinceLastOK(); s > 1 {
		t.Fatalf("since_last_ok = %v, want a small value", s)
	}
	dc.mu.Lock()
	if dc.n != 0 {
		t.Fatal("died on 2 failures")
	}
	dc.mu.Unlock()

	// 3 consecutive failures: death.
	fail.Store(true)
	fire(t, ft, handled)
	fire(t, ft, handled)
	fire(t, ft, handled)
	select {
	case <-dc.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no OnDead after 3 failures")
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.n != 1 || dc.failures != 3 || status.Code(dc.err) != codes.DeadlineExceeded {
		t.Fatalf("death: n=%d failures=%d err=%v", dc.n, dc.failures, dc.err)
	}
	if stats.Sent.Load() != 6 || stats.Failed.Load() != 5 {
		t.Fatalf("sent=%d failed=%d, want 6/5", stats.Sent.Load(), stats.Failed.Load())
	}
	if stats.RTT.Count() != 1 {
		t.Fatalf("rtt samples = %d, want 1", stats.RTT.Count())
	}
}

func TestPingerDisabledStartsNothing(t *testing.T) {
	called := false
	p := startPinger(context.Background(), pingerCfg{Every: 0, Ping: func(context.Context) (time.Duration, error) { called = true; return 0, nil }})
	if p.running() {
		t.Fatal("every <= 0 must not start a goroutine")
	}
	p.stop() // must not block or panic
	if called {
		t.Fatal("ping called")
	}
}

func TestPingerStopsOnClose(t *testing.T) {
	ft := newFakeTick()
	var n atomic.Int32
	handled := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	p := startPinger(ctx, pingerCfg{Every: time.Second, Tick: ft.tick, Ping: func(context.Context) (time.Duration, error) {
		n.Add(1)
		handled <- struct{}{}
		return time.Millisecond, nil
	}})
	fire(t, ft, handled)
	cancel()
	p.stop()
	select {
	case ft.ch <- time.Now():
		t.Fatal("pinger still reading ticks after stop")
	case <-time.After(50 * time.Millisecond):
	}
	if n.Load() != 1 {
		t.Fatalf("pings = %d, want 1", n.Load())
	}
	if !ft.stopped.Load() {
		t.Fatal("ticker not stopped")
	}
}

func TestPingerIgnoresFailuresAfterCancel(t *testing.T) {
	ft := newFakeTick()
	dc := newDeathCapture()
	ctx, cancel := context.WithCancel(context.Background())
	handled := make(chan struct{}, 1)
	p := startPinger(ctx, pingerCfg{Every: time.Second, Tick: ft.tick, OnDead: dc.fn, Ping: func(context.Context) (time.Duration, error) {
		defer func() { handled <- struct{}{} }()
		cancel()
		return 0, errors.New("cancelled")
	}})
	fire(t, ft, handled)
	p.stop()
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.n != 0 || p.Failures() != 0 {
		t.Fatal("a cancelled ping must not count toward death")
	}
}

type deadFixture struct {
	fake   *grpctest.Fake
	joiner *Joiner
	mu     sync.Mutex
	recs   []DeathRecord
	ch     chan struct{}
}

func newDeadFixture(t *testing.T) *deadFixture {
	t.Helper()
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	t.Cleanup(cleanup)
	d := &deadFixture{fake: f, ch: make(chan struct{}, 8)}
	d.joiner = &Joiner{
		Dial: func(ctx context.Context) (*grpc.ClientConn, *ConnRecorder, error) {
			c, err := dial(ctx)
			return c, &ConnRecorder{}, err
		},
		Hashes: map[string]string{"Blue": "hash"}, Unit: "LDBT", AckTimeout: 2 * time.Second,
		Phase: &atomic.Int32{}, Resyncs: &PhaseCounter{}, ResourceExhausted: &PhaseCounter{},
		OnDead: func(_ *ControlBot, rec DeathRecord) {
			d.mu.Lock()
			d.recs = append(d.recs, rec)
			d.mu.Unlock()
			d.ch <- struct{}{}
		},
	}
	return d
}

func TestControlBotOnDeadFiresOnce(t *testing.T) {
	f := newDeadFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := f.joiner.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatal(err)
	}
	defer bot.Close(context.Background())
	f.fake.EndStream(status.Error(codes.Unavailable, "error reading from server: connection reset by peer"))
	select {
	case <-f.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("OnDead not called")
	}
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.recs) != 1 {
		t.Fatalf("OnDead called %d times, want 1", len(f.recs))
	}
	d := f.recs[0]
	if d.Cause != CauseStream || d.Code != "Unavailable" || d.Name != "bot-1" || d.GUID == "" || d.PingFailures != 0 {
		t.Fatalf("record = %+v", d)
	}
	if d.AtUnixMs == 0 || d.AgeS < 0 {
		t.Fatalf("timing not stamped: %+v", d)
	}
}

func TestControlBotCloseFiresNothing(t *testing.T) {
	f := newDeadFixture(t)
	f.joiner.PingEvery = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := f.joiner.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = bot.Close(context.Background())
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.recs) != 0 {
		t.Fatalf("Close must not report a death, got %d", len(f.recs))
	}
}

func TestControlBotPingDeathThroughJoiner(t *testing.T) {
	f := newDeadFixture(t)
	f.joiner.PingEvery = 20 * time.Millisecond
	f.fake.Fail("Ping", status.Error(codes.Unavailable, "gone"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := f.joiner.Join(ctx, "bot-1", "Blue", false)
	if err != nil {
		t.Fatal(err)
	}
	defer bot.Close(context.Background())
	select {
	case <-f.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no ping death")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.recs[0]
	if d.Cause != CausePing || d.PingFailures != 3 || d.Code != "Unavailable" {
		t.Fatalf("record = %+v", d)
	}
	if bot.Err() == nil {
		t.Fatal("bot.Err must be set")
	}
	if bot.Pings.Failed.Load() < 3 {
		t.Fatalf("failed pings = %d", bot.Pings.Failed.Load())
	}
}
