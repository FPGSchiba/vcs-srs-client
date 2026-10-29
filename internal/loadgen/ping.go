package loadgen

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
)

// ControlKeepalive is the bot's gRPC client keepalive. It is the real
// client's (internal/session): 75 s / 10 s with PermitWithoutStream, above the
// server's 60 s enforcement floor. A bot without it had no keepalive at all,
// an idle path the real client never takes.
var ControlKeepalive = keepalive.ClientParameters{
	Time:                session.ClientKeepaliveTime(),
	Timeout:             10 * time.Second,
	PermitWithoutStream: true,
}

// PingFailureThreshold is the number of consecutive Ping failures that is
// death: the real client's connhealth.DefaultFailureThreshold.
const PingFailureThreshold = connhealth.DefaultFailureThreshold

// ConnRecorder remembers the local address of the most recent control TCP
// connection, so a death record can name the socket the server saw.
type ConnRecorder struct{ addr atomic.Value }

// LocalAddr is the recorded local ip:port, or "" before any connection.
func (r *ConnRecorder) LocalAddr() string {
	s, _ := r.addr.Load().(string)
	return s
}

func (r *ConnRecorder) set(s string) { r.addr.Store(s) }

// DialFunc is Joiner.Dial: it returns the connection and the recorder that
// tracks its local address.
type DialFunc func(ctx context.Context) (*grpc.ClientConn, *ConnRecorder, error)

// DialInsecure opens a plaintext control connection with the real client's
// keepalive, recording each connection's local address in rec (which may be
// nil). The caller has already passed addr through AllowList.Check.
func DialInsecure(addr string, rec *ConnRecorder) (*grpc.ClientConn, error) {
	var d net.Dialer
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(ControlKeepalive),
		grpc.WithContextDialer(func(ctx context.Context, target string) (net.Conn, error) {
			c, err := d.DialContext(ctx, "tcp", target)
			if err == nil && rec != nil {
				rec.set(c.LocalAddr().String())
			}
			return c, err
		}),
	)
}

// InsecureDialer returns a Joiner.Dial that gives every bot its own recorder.
func InsecureDialer(addr string) DialFunc {
	return func(context.Context) (*grpc.ClientConn, *ConnRecorder, error) {
		rec := &ConnRecorder{}
		c, err := DialInsecure(addr, rec)
		return c, rec, err
	}
}

// PingStats counts a bot's Pings.
type PingStats struct {
	Sent, Failed atomic.Uint64
	RTT          Hist
}

// pingerCfg configures a pinger. Ping and Tick are injectable for tests.
type pingerCfg struct {
	Every  time.Duration
	Offset time.Duration // delay before the first tick, so bots do not ping in lockstep
	// Ping makes one call and returns its round trip.
	Ping func(ctx context.Context) (time.Duration, error)
	// Tick returns the tick channel and its stop func; nil means a real ticker.
	Tick func(d time.Duration) (<-chan time.Time, func())
	// OnDead is called once, on its own goroutine, after
	// PingFailureThreshold consecutive failures. Because the loop has already
	// returned, it may call stop() or Close without deadlocking.
	OnDead func(failures int, last error)
	Stats  *PingStats
}

type pinger struct {
	cfg    pingerCfg
	cancel context.CancelFunc
	done   chan struct{}

	failures atomic.Int32
	lastOK   atomic.Int64 // unix nanos; 0 = never
	once     sync.Once
}

func realTick(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// newPinger builds a stopped pinger, so its owner can publish the pointer
// before the loop (whose OnDead may read it) runs.
func newPinger(cfg pingerCfg) *pinger {
	p := &pinger{cfg: cfg}
	if p.cfg.Tick == nil {
		p.cfg.Tick = realTick
	}
	return p
}

// startPinger builds and starts a pinger. Every <= 0 starts nothing.
func startPinger(ctx context.Context, cfg pingerCfg) *pinger {
	p := newPinger(cfg)
	p.start(ctx)
	return p
}

func (p *pinger) start(ctx context.Context) {
	if p.cfg.Every <= 0 || p.cfg.Ping == nil {
		return
	}
	ctx, p.cancel = context.WithCancel(ctx)
	p.done = make(chan struct{})
	go p.loop(ctx)
}

func (p *pinger) running() bool { return p.done != nil }

func (p *pinger) loop(ctx context.Context) {
	defer close(p.done)
	if p.cfg.Offset > 0 {
		t := time.NewTimer(p.cfg.Offset)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return
		}
	}
	ticks, stop := p.cfg.Tick(p.cfg.Every)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		rtt, err := p.cfg.Ping(ctx)
		if ctx.Err() != nil {
			// Cancelled by a Close or a death, not a failed ping.
			return
		}
		if s := p.cfg.Stats; s != nil {
			s.Sent.Add(1)
		}
		if err == nil {
			p.failures.Store(0)
			p.lastOK.Store(time.Now().UnixNano())
			if s := p.cfg.Stats; s != nil {
				s.RTT.Record(rtt)
			}
			continue
		}
		if s := p.cfg.Stats; s != nil {
			s.Failed.Add(1)
		}
		if n := p.failures.Add(1); int(n) >= PingFailureThreshold {
			if p.cfg.OnDead != nil {
				go p.cfg.OnDead(int(n), err)
			}
			return
		}
	}
}

// stop ends the loop and waits for it. Safe to call more than once, and from
// the loop's own OnDead only through a separate goroutine.
func (p *pinger) stop() {
	p.once.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
	})
	if p.done != nil {
		<-p.done
	}
}

// halt cancels the loop without waiting (used from inside OnDead).
func (p *pinger) halt() {
	if p.cancel != nil {
		p.cancel()
	}
}

// Failures is the current run of consecutive failed Pings.
func (p *pinger) Failures() int { return int(p.failures.Load()) }

// SinceLastOK is the seconds since the last successful Ping, -1 if none.
func (p *pinger) SinceLastOK() float64 {
	t := p.lastOK.Load()
	if t == 0 {
		return -1
	}
	return time.Since(time.Unix(0, t)).Seconds()
}
