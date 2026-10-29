package loadgen

import (
	"context"
	"fmt"
	"math"
	"sync"
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

	Bots          int     // bots alive during the hold (= N)
	ExpectedPPS   float64 // 50 * (N-1): the frames a lossless reflector would deliver per second
	HelloTriesMax int     // most HELLOs any bot added for this step needed
	HelloRetried  int     // bots added for this step that needed more than one HELLO
}

// CalStoppedError says why calibration ended before the last requested step:
// a bot could not join, the talker stalled, or a ledger was lost. The steps
// completed before it, and their capacity, are still returned by Calibrate.
type CalStoppedError struct {
	N     int    // the step that could not complete
	Bots  int    // bots alive when it stopped
	Stage string // hello, voice_dial, talker_stalled, ledger or delivery
	Bot   int    // the failing bot's index (0 when not applicable)
	Tries int    // HELLOs the failing bot sent (0 when not applicable)
	Err   error
}

func (e *CalStoppedError) Error() string {
	return fmt.Sprintf("calibrate: stopped at N=%d (%s): %v", e.N, e.Stage, e.Err)
}

func (e *CalStoppedError) Unwrap() error { return e.Err }

// Reason is the calibration_stopped line's reason (interface I-4). A join-stage
// stop (hello, voice_dial) names the bot, as v1's stderr text did; the other
// stages carry their own text.
func (e *CalStoppedError) Reason() string {
	if e.Stage == "hello" || e.Stage == "voice_dial" {
		return fmt.Sprintf("bot %d: %v", e.Bot, e.Err)
	}
	return e.Err.Error()
}

// ReceiveCapacity is the generator's receive capacity: the largest valid
// step's pps. LowerBound means the true ceiling was not reached (the
// reflector reported send errors, or every step stayed valid). PPS == 0
// with LowerBound == false means no step was valid. The reflector process's
// own health is NOT sampled here: a combined verdict must OR the reflect
// command's reflector_stats send_errors into lower_bound (kit, PR-D).
type ReceiveCapacity struct {
	PPS        float64
	LowerBound bool
}

// CalOption tunes Calibrate.
type CalOption func(*calOpts)

type calOpts struct {
	reflErrs   func() uint64
	bounds     Bounds
	minDeliver float64
	hook       func(CalStep)
	progEvry   time.Duration
	progress   func(ProgressLine)
}

// WithStepHook calls f once per completed step, in order, as soon as the step
// is scored, so a caller can stream results while calibration is running.
func WithStepHook(f func(CalStep)) CalOption { return func(o *calOpts) { o.hook = f } }

// WithProgress calls f every `every` while bots are being added for a step,
// with a calibrate_ramp progress line. f runs on its own goroutine, never
// concurrently with itself, and not after Calibrate returns.
func WithProgress(every time.Duration, f func(ProgressLine)) CalOption {
	return func(o *calOpts) { o.progEvry, o.progress = every, f }
}

// WithReflectorErrors supplies the reflector's send-error count, readable
// only when the reflector runs in this process. Across processes the reflect
// subcommand reports it on stdout when it stops.
func WithReflectorErrors(f func() uint64) CalOption { return func(o *calOpts) { o.reflErrs = f } }

// DefaultMinDelivery is the fraction of a step's expected frames that must
// reach the listeners for the step to count: below it the step is invalid and
// calibration ends at the last good step.
const DefaultMinDelivery = 0.99

// WithMinDelivery overrides the minimum delivered fraction of ExpectedPPS.
func WithMinDelivery(f float64) CalOption { return func(o *calOpts) { o.minDeliver = f } }

// WithBounds overrides the self-health bounds.
func WithBounds(b Bounds) CalOption { return func(o *calOpts) { o.bounds = b } }

// Calibrate measures the generator's receive side with no control plane: for
// each n it brings the bot count up to n (bots persist across steps), has one
// talker send SyntheticFrame at 50 fps through the reflector, and scores the
// generator's own health during the hold.
func Calibrate(ctx context.Context, reflAddr string, allow AllowList, seed int64, ns []int, hold time.Duration, opts ...CalOption) ([]CalStep, ReceiveCapacity, error) {
	o := calOpts{bounds: DefaultBounds, minDeliver: DefaultMinDelivery}
	for _, f := range opts {
		f(&o)
	}
	if err := allow.Check(reflAddr); err != nil {
		return nil, ReceiveCapacity{}, fmt.Errorf("calibrate: reflector refused: %w", err)
	}
	if hold <= 0 {
		return nil, ReceiveCapacity{}, fmt.Errorf("calibrate: hold %v must be > 0", hold)
	}
	// Bots persist across steps and stay registered at the reflector, so a
	// step smaller than an earlier one would carry more load than its N says.
	if len(ns) == 0 {
		return nil, ReceiveCapacity{}, fmt.Errorf("calibrate: no steps")
	}
	for i, n := range ns {
		if n < 2 {
			return nil, ReceiveCapacity{}, fmt.Errorf("calibrate: step %d needs >= 2 bots", n)
		}
		if i > 0 && n <= ns[i-1] {
			return nil, ReceiveCapacity{}, fmt.Errorf("calibrate: steps %v are not strictly ascending", ns)
		}
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
	// stopped ends calibration at the last good step: the completed steps and
	// their capacity come back with the typed reason.
	stopped := func(e *CalStoppedError) ([]CalStep, ReceiveCapacity, error) {
		return steps, calCapacity(steps, o.reflErrs, reflBase), e
	}
	for _, n := range ns {
		triesMax, retried := 0, 0
		var liveBots, liveRetried atomic.Int64
		liveBots.Store(int64(len(bots)))
		stopRamp := o.startRampProgress(n, &liveBots, &liveRetried)
		for len(bots) < n {
			if err := ctx.Err(); err != nil {
				stopRamp()
				return steps, ReceiveCapacity{}, err
			}
			idx := len(bots)
			v, err := DialVoice(reflAddr, DeterministicUUID(seed, idx))
			if err != nil {
				stopRamp()
				if cerr := ctx.Err(); cerr != nil {
					return steps, ReceiveCapacity{}, cerr
				}
				return stopped(&CalStoppedError{N: n, Bots: idx, Stage: "voice_dial", Bot: idx, Err: err})
			}
			hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			tries, err := v.Hello(hctx, "", DefaultHelloLadder)
			cancel()
			if tries > triesMax {
				triesMax = tries
			}
			if err != nil {
				v.Close()
				stopRamp()
				if cerr := ctx.Err(); cerr != nil {
					return steps, ReceiveCapacity{}, cerr
				}
				return stopped(&CalStoppedError{N: n, Bots: idx, Stage: "hello", Bot: idx, Tries: tries, Err: err})
			}
			if tries > 1 {
				retried++
				liveRetried.Add(1)
			}
			v.Start(context.Background())
			bots = append(bots, v)
			liveBots.Store(int64(len(bots)))
		}
		stopRamp()
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
				return stopped(&CalStoppedError{N: n, Bots: len(bots), Stage: "talker_stalled",
					Err: fmt.Errorf("talker stalled at N=%d", n)})
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
				return stopped(&CalStoppedError{N: n, Bots: len(bots), Stage: "ledger",
					Err: fmt.Errorf("bot lost its ledger at N=%d", n)})
			}
			unique += lr.Unique
		}
		v := o.bounds.Violations(h)
		step := CalStep{
			N: n, EgressPPS: float64(unique) / hold.Seconds(),
			Health: h, Violations: v, Valid: len(v) == 0,
			Bots: n, ExpectedPPS: framesPerSec * float64(n-1),
			HelloTriesMax: triesMax, HelloRetried: retried,
		}
		var short *CalStoppedError
		if exp := step.ExpectedPPS * hold.Seconds(); exp > 0 && float64(unique) < o.minDeliver*exp {
			pct := 100 * float64(unique) / exp
			step.Valid = false
			step.Violations = append(step.Violations,
				fmt.Sprintf("delivered %.1f%% of expected frames, under the %.0f%% minimum", pct, 100*o.minDeliver))
			short = &CalStoppedError{N: n, Bots: len(bots), Stage: "delivery",
				Err: fmt.Errorf("delivered %.1f%% of expected frames at N=%d, under the %.0f%% minimum",
					pct, n, 100*o.minDeliver)}
		}
		steps = append(steps, step)
		if o.hook != nil {
			o.hook(step)
		}
		if short != nil {
			return stopped(short)
		}
	}
	return steps, calCapacity(steps, o.reflErrs, reflBase), nil
}

// calCapacity is the receive capacity over the completed steps: the largest
// valid pps; LowerBound when the last completed step was valid (the sweep
// ended before the ceiling) or the reflector reported errors since reflBase.
func calCapacity(steps []CalStep, reflErrs func() uint64, reflBase uint64) ReceiveCapacity {
	var capy ReceiveCapacity
	for _, s := range steps {
		if s.Valid && s.EgressPPS > capy.PPS {
			capy.PPS = s.EgressPPS
		}
	}
	if len(steps) > 0 && steps[len(steps)-1].Valid {
		capy.LowerBound = true
	}
	if reflErrs != nil && reflErrs() > reflBase {
		capy.LowerBound = true
	}
	return capy
}

// startRampProgress writes a calibrate_ramp line every progEvry while the
// bots for one step are being added. The returned stop waits for the
// goroutine, so no line is written after it returns.
func (o *calOpts) startRampProgress(target int, bots, retried *atomic.Int64) (stop func()) {
	if o.progress == nil || o.progEvry <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(finished)
		tk := time.NewTicker(o.progEvry)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				o.progress(ProgressLine{
					Type: "progress", Phase: "calibrate_ramp", Target: target,
					Bots: int(bots.Load()), HelloRetried: int(retried.Load()),
					ElapsedS: time.Since(start).Seconds(),
				})
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done); <-finished }) }
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
