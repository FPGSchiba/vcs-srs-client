package loadgen

import (
	"context"
	"errors"
	"fmt"
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
	Dial   func(ctx context.Context) (*grpc.ClientConn, error)
	Hashes map[string]string // coalition name -> bcrypt hash
	Unit   string
	// AckTimeout bounds the wait for the server's subscription ack. Unlike the
	// real client, which logs and carries on, a bot treats a missing ack as a
	// failed join. Zero means DefaultAckTimeout.
	AckTimeout        time.Duration
	Phase             *atomic.Int32
	Resyncs           *PhaseCounter
	ResourceExhausted *PhaseCounter
}

// ControlBot is one joined control-plane participant.
type ControlBot struct {
	GUID, Token, Coalition, Secret, CoalitionAddr string
	Settings                                      *srspb.ServerSettings
	Counts                                        *UpdateCounts
	Store                                         *state.Store // full bots only

	conn    *grpc.ClientConn
	cc      *control.Client
	cancel  context.CancelFunc
	done    chan struct{}
	closing atomic.Bool

	mu  sync.Mutex
	err error
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
	conn, err := j.Dial(ctx)
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
		conn:          conn,
		cc:            cc,
		cancel:        cancel,
		done:          make(chan struct{}),
	}
	if full {
		bot.Store = st
	}
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
			err = bot.cc.Consume(stream, st, noopEmitter{})
		} else {
			err = Drain(stream.Recv, bot.Counts)
		}
		if ctx.Err() != nil || bot.closing.Load() {
			return
		}
		if status.Code(err) != codes.ResourceExhausted {
			if err == nil {
				err = errors.New("update stream ended by the server")
			}
			bot.setErr(err)
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
				bot.setErr(fmt.Errorf("resync: %w", rerr))
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
	<-b.done
	cerr := b.conn.Close()
	return errors.Join(derr, cerr)
}
