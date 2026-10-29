package loadgen

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/auth"
	"github.com/FPGSchiba/vcs-srs-client/internal/control"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/version"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// Phase tags what a run is doing so per-phase counters can separate the O(N^2)
// join fan-out (ramp) from a hold breach (design 4.2, H3).
type Phase int32

const (
	PhaseRamp Phase = iota
	PhaseSettle
	PhaseHold
)

// PhaseCounter is one counter per Phase.
type PhaseCounter [3]atomic.Uint64

// DefaultAckTimeout mirrors internal/session's subscribeAckTimeout.
const DefaultAckTimeout = 2 * time.Second

// Joiner composes auth + control directly (design D6) instead of calling
// session.Connect per bot, so each join reuses a precomputed bcrypt hash.
type Joiner struct {
	Dial   DialFunc
	Hashes map[string]string // coalition name -> bcrypt hash
	Unit   string
	// AckTimeout bounds the wait for the server's subscription ack. Unlike the
	// real client, which logs and carries on, a bot treats a missing ack as a
	// failed join. Zero means DefaultAckTimeout.
	AckTimeout        time.Duration
	Phase             *atomic.Int32
	Resyncs           *PhaseCounter
	ResourceExhausted *PhaseCounter
	// PingEvery is each bot's Ping period; zero disables pings. Every bot
	// starts at a random offset in [0, PingEvery) derived from PingSeed and
	// its name, so the server never sees N pings in one millisecond.
	PingEvery time.Duration
	PingSeed  int64
	// OnDead is called at most once per bot, the moment its control plane is
	// declared dead (never for a Close). It runs on the bot's receive or ping
	// goroutine and must not block. The record carries identity, cause, code,
	// timing and ping state; the caller stamps what only it knows (index,
	// phase).
	OnDead func(b *ControlBot, d DeathRecord)
}

// ControlBot is one joined control-plane participant.
type ControlBot struct {
	GUID, Token, Coalition, Secret, CoalitionAddr string
	Settings                                      *srspb.ServerSettings
	Counts                                        *UpdateCounts
	Store                                         *state.Store // full bots only
	Name                                          string
	// LocalAddr is the control connection's local ip:port at join time.
	LocalAddr string
	// Pings counts this bot's Pings.
	Pings PingStats

	conn    *grpc.ClientConn
	cc      *control.Client
	cancel  context.CancelFunc
	done    chan struct{}
	closing atomic.Bool

	joinedAt time.Time
	rec      *ConnRecorder
	pinger   *pinger
	dead     atomic.Bool
	onDead   func(DeathRecord)

	mu  sync.Mutex
	err error
}

// countingStream counts every update a full bot receives, so Counts is
// complete whichever consumer path the bot takes.
type countingStream struct {
	srspb.SRSService_SubscribeToUpdatesClient
	c *UpdateCounts
}

func (s countingStream) Recv() (*srspb.ServerUpdate, error) {
	u, err := s.SRSService_SubscribeToUpdatesClient.Recv()
	if err == nil {
		s.c.add(u.GetType())
	}
	return u, err
}

type noopEmitter struct{}

func (noopEmitter) Emit(string, any) {}

func (j *Joiner) ackTimeout() time.Duration {
	if j.AckTimeout > 0 {
		return j.AckTimeout
	}
	return DefaultAckTimeout
}

// count increments the current phase's slot of c; a nil counter set is a no-op
// (like Phase, the counters are optional).
func (j *Joiner) count(c *PhaseCounter) {
	if c != nil {
		c[j.phase()].Add(1)
	}
}

func (j *Joiner) phase() Phase {
	if j.Phase == nil {
		return PhaseRamp
	}
	p := Phase(j.Phase.Load())
	if p < PhaseRamp || p > PhaseHold {
		return PhaseRamp
	}
	return p
}

// awaitHeader waits for the server's subscription ack (response headers) up
// to AckTimeout. Header never errors on an ended stream (it yields nil, nil),
// so a nil metadata is a failure too.
func (j *Joiner) awaitHeader(ctx context.Context, stream srspb.SRSService_SubscribeToUpdatesClient) error {
	acked := make(chan bool, 1)
	go func() {
		md, err := stream.Header()
		acked <- err == nil && md != nil
	}()
	timer := time.NewTimer(j.ackTimeout())
	defer timer.Stop()
	select {
	case ok := <-acked:
		if ok {
			return nil
		}
		return errors.New("update stream ended before the server acknowledged the subscription")
	case <-timer.C:
		return fmt.Errorf("no subscription ack within %v", j.ackTimeout())
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Join logs one bot in: Dial, InitAuth, GuestLogin, Subscribe (ack awaited),
// SyncClient, then the receive goroutine. A full bot keeps a state.Store and
// routes updates through control.Consume; the rest drain.
func (j *Joiner) Join(ctx context.Context, name, coalition string, full bool) (*ControlBot, error) {
	hash, ok := j.Hashes[coalition]
	if !ok {
		return nil, fmt.Errorf("join %s: no password hash for coalition %q", name, coalition)
	}
	conn, rec, err := j.Dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("join %s: dial: %w", name, err)
	}
	fail := func(err error) (*ControlBot, error) {
		_ = conn.Close()
		return nil, fmt.Errorf("join %s: %w", name, err)
	}
	ac := auth.New(conn)
	init, err := ac.InitAuth(ctx, version.Client)
	if err != nil {
		return fail(err)
	}
	guest, err := ac.GuestLogin(ctx, name, hash, j.Unit, init.ClientGUID)
	if err != nil {
		return fail(err)
	}
	cc := control.New(conn, guest.Token)

	// The stream outlives Join's ctx; it is ended by Close or by the server.
	sctx, cancel := context.WithCancel(context.Background())
	stream, err := cc.Subscribe(sctx)
	if err != nil {
		cancel()
		return fail(err)
	}
	if err := j.awaitHeader(ctx, stream); err != nil {
		cancel()
		return fail(err)
	}
	st := state.New()
	if _, err := cc.SyncClient(ctx, st); err != nil {
		cancel()
		return fail(err)
	}

	secret, coalitionAddr, _ := st.VoiceCredentials()
	bot := &ControlBot{
		GUID:          init.ClientGUID,
		Token:         guest.Token,
		Coalition:     coalition,
		Secret:        secret,
		CoalitionAddr: coalitionAddr,
		Settings:      st.Settings(),
		Counts:        &UpdateCounts{},
		Name:          name,
		conn:          conn,
		rec:           rec,
		joinedAt:      time.Now(),
		cc:            cc,
		cancel:        cancel,
		done:          make(chan struct{}),
	}
	if full {
		bot.Store = st
	}
	if rec != nil {
		bot.LocalAddr = rec.LocalAddr()
	}
	if j.OnDead != nil {
		bot.onDead = func(d DeathRecord) { j.OnDead(bot, d) }
	}
	bot.pinger = newPinger(pingerCfg{
		Every:  j.PingEvery,
		Offset: pingOffset(j.PingSeed, name, j.PingEvery),
		Ping:   bot.pingOnce(j.PingEvery),
		OnDead: func(failures int, last error) { bot.fireDead(CausePing, last) },
		Stats:  &bot.Pings,
	})
	bot.pinger.start(sctx)
	go j.receive(sctx, bot, st, stream, full)
	return bot, nil
}

// receive runs the stream and, on ResourceExhausted, resyncs: Subscribe, ack,
// SyncClient, resume (mirrors internal/session resync). Any other end kills
// the bot.
func (j *Joiner) receive(ctx context.Context, bot *ControlBot, st *state.Store, stream srspb.SRSService_SubscribeToUpdatesClient, full bool) {
	defer close(bot.done)
	// Cancel for the current resync stream (nil for the initial one, which
	// Close ends through ctx).
	var streamCancel context.CancelFunc
	defer func() {
		if streamCancel != nil {
			streamCancel()
		}
	}()
	for {
		var err error
		if full {
			err = bot.cc.Consume(countingStream{stream, bot.Counts}, st, noopEmitter{})
		} else {
			err = Drain(stream.Recv, bot.Counts)
		}
		if ctx.Err() != nil || bot.closing.Load() {
			return
		}
		if status.Code(err) != codes.ResourceExhausted {
			bot.fireDead(CauseStream, err)
			return
		}
		j.count(j.ResourceExhausted)

		// A per-stream context so a failed resync releases the new stream
		// instead of holding a server-side subscriber until Close.
		nctx, ncancel := context.WithCancel(ctx)
		next, rerr := bot.cc.Subscribe(nctx)
		if rerr == nil {
			rerr = j.awaitHeader(ctx, next)
		}
		if rerr == nil {
			sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, rerr = bot.cc.SyncClient(sctx, st)
			cancel()
		}
		if rerr != nil {
			ncancel()
			if ctx.Err() == nil && !bot.closing.Load() {
				bot.fireDead(CauseResync, fmt.Errorf("resync: %w", rerr))
			}
			return
		}
		j.count(j.Resyncs)
		if streamCancel != nil {
			streamCancel()
		}
		streamCancel = ncancel
		stream = next
	}
}

func (b *ControlBot) setErr(err error) {
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	b.mu.Unlock()
}

// pingOnce returns the pinger's call: one Ping bounded by every, carrying the
// previous round trip like the real client does.
func (b *ControlBot) pingOnce(every time.Duration) func(ctx context.Context) (time.Duration, error) {
	var lastMs int64
	return func(ctx context.Context) (time.Duration, error) {
		cctx, cancel := context.WithTimeout(ctx, every)
		defer cancel()
		start := time.Now()
		if _, err := b.cc.PingOnce(cctx, lastMs); err != nil {
			return 0, err
		}
		rtt := time.Since(start)
		lastMs = rtt.Milliseconds()
		return rtt, nil
	}
}

// fireDead records why the bot died and reports it exactly once, at once. It
// is a no-op for a bot that is closing or whose context is gone. A nil err is
// a clean server-side stream end.
func (b *ControlBot) fireDead(cause string, err error) {
	if b.closing.Load() {
		return
	}
	if !b.dead.CompareAndSwap(false, true) {
		return
	}
	d := NewDeathRecord(cause, err)
	if err == nil {
		err = errors.New(StreamEndedByServer)
	}
	b.setErr(err)
	d.Name, d.GUID = b.Name, b.GUID
	d.AtUnixMs = time.Now().UnixMilli()
	d.AgeS = time.Since(b.joinedAt).Seconds()
	d.LocalAddr = b.LocalAddr
	if b.rec != nil {
		if a := b.rec.LocalAddr(); a != "" {
			d.LocalAddr = a
		}
	}
	if b.pinger != nil {
		d.SinceLastOKPingS = b.pinger.SinceLastOK()
		d.PingFailures = b.pinger.Failures()
		b.pinger.halt()
	}
	if b.onDead != nil {
		b.onDead(d)
	}
}

// pingOffset is a deterministic offset in [0, every) for one bot.
func pingOffset(seed int64, name string, every time.Duration) time.Duration {
	if every <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	r := rand.New(rand.NewSource(seed ^ int64(h.Sum64())))
	return time.Duration(r.Int63n(int64(every)))
}

// Err returns why the bot died, or nil while it is alive.
func (b *ControlBot) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// UpdateRadio pushes one enabled radio per frequency (MHz as float32, the
// wire form) to the server.
func (b *ControlBot) UpdateRadio(ctx context.Context, freqsMHz ...float32) error {
	info := &srspb.RadioInfo{}
	for i, f := range freqsMHz {
		info.Radios = append(info.Radios, &srspb.Radio{
			Id:        uint32(i + 1),
			Name:      fmt.Sprintf("Radio %d", i+1),
			Frequency: f,
			Enabled:   true,
		})
	}
	return b.cc.UpdateRadioInfo(ctx, info)
}

// Close disconnects, stops the receive goroutine and closes the connection.
func (b *ControlBot) Close(ctx context.Context) error {
	b.closing.Store(true)
	derr := b.cc.Disconnect(ctx)
	b.cancel()
	if b.pinger != nil {
		b.pinger.stop()
	}
	<-b.done
	cerr := b.conn.Close()
	return errors.Join(derr, cerr)
}
