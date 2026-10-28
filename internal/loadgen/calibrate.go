package loadgen

import (
	"context"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
)

// CalStep is one calibration step: N bots, one talker, N-1 listeners.
type CalStep struct {
	N          int
	EgressPPS  float64 // unique frames delivered to listeners per second of hold
	Health     Health
	Violations []string
	Valid      bool
}

// ReceiveCapacity is the generator's receive capacity: the largest valid
// step's pps. LowerBound means the true ceiling was not reached (the
// reflector reported send errors, or every step stayed valid).
type ReceiveCapacity struct {
	PPS        float64
	LowerBound bool
}

// CalOption tunes Calibrate.
type CalOption func(*calOpts)

type calOpts struct {
	reflErrs func() uint64
	bounds   Bounds
}

// WithReflectorErrors supplies the reflector's send-error count, readable
// only when the reflector runs in this process. Across processes the reflect
// subcommand reports it on stdout when it stops.
func WithReflectorErrors(f func() uint64) CalOption { return func(o *calOpts) { o.reflErrs = f } }

// WithBounds overrides the self-health bounds.
func WithBounds(b Bounds) CalOption { return func(o *calOpts) { o.bounds = b } }

// Calibrate measures the generator's receive side with no control plane: for
// each n it brings the bot count up to n (bots persist across steps), has one
// talker send SyntheticFrame at 50 fps through the reflector, and scores the
// generator's own health during the hold.
func Calibrate(ctx context.Context, reflAddr string, allow AllowList, seed int64, ns []int, hold time.Duration, opts ...CalOption) ([]CalStep, ReceiveCapacity, error) {
	o := calOpts{bounds: DefaultBounds}
	for _, f := range opts {
		f(&o)
	}
	if err := allow.Check(reflAddr); err != nil {
		return nil, ReceiveCapacity{}, fmt.Errorf("calibrate: reflector refused: %w", err)
	}
	if hold <= 0 {
		return nil, ReceiveCapacity{}, fmt.Errorf("calibrate: hold %v must be > 0", hold)
	}
	const freq = voice.KHz(251000)
	var bots []*VoiceConn
	defer func() {
		for _, b := range bots {
			b.Bye()
			b.Close()
		}
	}()

	var wheel Hist
	var nextSeq atomic.Uint32
	var sendErrs atomic.Uint64
	nextSeq.Store(1)
	var talkCancel context.CancelFunc
	var talkDone chan struct{}
	defer func() {
		if talkCancel != nil {
			talkCancel()
			<-talkDone
		}
	}()

	frames := int(math.Round(hold.Seconds() * framesPerSec))
	if frames < 1 {
		frames = 1
	}
	var reflBase uint64
	if o.reflErrs != nil {
		reflBase = o.reflErrs()
	}
	var steps []CalStep
	for _, n := range ns {
		if n < 2 {
			return steps, ReceiveCapacity{}, fmt.Errorf("calibrate: step %d needs >= 2 bots", n)
		}
		for len(bots) < n {
			if err := ctx.Err(); err != nil {
				return steps, ReceiveCapacity{}, err
			}
			v, err := DialVoice(reflAddr, DeterministicUUID(seed, len(bots)))
			if err != nil {
				return steps, ReceiveCapacity{}, err
			}
			hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err = v.Hello(hctx, "", DefaultHelloLadder)
			cancel()
			if err != nil {
				v.Close()
				return steps, ReceiveCapacity{}, fmt.Errorf("calibrate: bot %d: %w", len(bots), err)
			}
			v.Start(context.Background())
			bots = append(bots, v)
		}
		if talkCancel == nil {
			tctx, cancel := context.WithCancel(context.Background())
			talkCancel, talkDone = cancel, make(chan struct{})
			go calTalk(tctx, talkDone, bots[0], freq, &nextSeq, &wheel, &sendErrs)
		}
		talker, listeners := bots[0], bots[1:n]
		base := uint64(nextSeq.Load()) + holdLeadFrames
		for _, l := range listeners {
			l.ExpectFrom(talker.ID, freq, base, frames)
			l.RxLag.Reset()
		}
		wheel.Reset()
		se0 := sendErrs.Load()
		drops0 := make([]uint32, len(listeners))
		for i, l := range listeners {
			drops0[i] = l.Drops.Load()
		}
		rs0 := SampleRuntime()
		end := base + uint64(frames)
		deadline := time.After(hold + 5*time.Second)
		poll := time.NewTicker(5 * time.Millisecond)
		for uint64(nextSeq.Load()) < end {
			select {
			case <-ctx.Done():
				poll.Stop()
				return steps, ReceiveCapacity{}, ctx.Err()
			case <-deadline:
				poll.Stop()
				return steps, ReceiveCapacity{}, fmt.Errorf("calibrate: talker stalled at N=%d", n)
			case <-poll.C:
			}
		}
		poll.Stop()
		select {
		case <-ctx.Done():
			return steps, ReceiveCapacity{}, ctx.Err()
		case <-time.After(holdDrain):
		}
		rs1 := SampleRuntime()

		h := Health{GenCPU: -1, SendErrors: sendErrs.Load() - se0}
		var rx Hist
		for i, l := range listeners {
			h.SocketDrops += uint64(l.Drops.Load() - drops0[i])
			rx.Merge(l.RxLag)
		}
		if rx.Count() > 0 {
			p := rx.Quantile(0.99)
			h.RxLagP99 = &p
		}
		h.WheelLagP99 = wheel.Quantile(0.99)
		h.SchedP99 = DeltaQuantile(rs0.Sched, rs1.Sched, 0.99)
		h.GCPauseP99 = DeltaQuantile(rs0.GC, rs1.GC, 0.99)

		var unique uint64
		for _, l := range listeners {
			lr, ok := l.LedgerResult(talker.ID, freq)
			if !ok {
				return steps, ReceiveCapacity{}, fmt.Errorf("calibrate: bot lost its ledger at N=%d", n)
			}
			unique += lr.Unique
		}
		v := o.bounds.Violations(h)
		steps = append(steps, CalStep{
			N: n, EgressPPS: float64(unique) / hold.Seconds(),
			Health: h, Violations: v, Valid: len(v) == 0,
		})
	}

	var capy ReceiveCapacity
	for _, s := range steps {
		if s.Valid && s.EgressPPS > capy.PPS {
			capy.PPS = s.EgressPPS
		}
	}
	if len(steps) > 0 && steps[len(steps)-1].Valid {
		capy.LowerBound = true // the sweep ended before the ceiling
	}
	if o.reflErrs != nil && o.reflErrs() > reflBase {
		capy.LowerBound = true
	}
	return steps, capy, nil
}

// calTalk sends SyntheticFrame at 50 fps on an absolute schedule.
func calTalk(ctx context.Context, done chan struct{}, b *VoiceConn, freq voice.KHz, next *atomic.Uint32, wheel *Hist, errs *atomic.Uint64) {
	defer close(done)
	t0 := time.Now()
	for k := 0; ctx.Err() == nil; k++ {
		due := t0.Add(time.Duration(k) * frameInterval)
		if d := time.Until(due); d > 0 {
			timer := time.NewTimer(d)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		lag := time.Since(due)
		if lag < 0 {
			lag = 0
		}
		wheel.Record(lag)
		seq := next.Load()
		if err := b.Send(seq, freq, SyntheticFrame(seq)); err != nil {
			errs.Add(1)
		}
		next.Store(seq + 1)
	}
}
