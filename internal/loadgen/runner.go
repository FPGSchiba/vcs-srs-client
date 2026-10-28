package loadgen

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

const (
	frameInterval = 20 * time.Millisecond
	framesPerSec  = 50
	// holdLeadFrames is how far ahead of the talker a hold's ledger base is
	// placed, so every listener's ledger is installed before frame `base` is
	// sent.
	holdLeadFrames = 5
	// holdDrain lets the last frames of a hold arrive before the snapshot.
	holdDrain = 200 * time.Millisecond
)

// RunConfig configures a Runner.
type RunConfig struct {
	Arm         string
	Scenario    Scenario
	ControlAddr string
	VoiceAddr   string // fallback when sync carries no coalition_voice_addr
	Allow       AllowList
	Passwords   map[string]string
	Unit        string
	MaxInFlight int // concurrent joins; default 16
	Seed        int64
	FullSample  int // bots that keep a state.Store; default 16
	Bounds      Bounds
}

// DialInsecure returns a Joiner.Dial for a plaintext loopback control port.
// The caller has already passed addr through AllowList.Check.
func DialInsecure(addr string) func(ctx context.Context) (*grpc.ClientConn, error) {
	return func(context.Context) (*grpc.ClientConn, error) {
		return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
}

type runnerBot struct {
	ctl   *ControlBot
	voice *VoiceConn
	id    uuid.UUID
	tries int
}

// Runner holds the persistent bots and executes NDJSON commands.
type Runner struct {
	cfg    RunConfig
	joiner *Joiner
	phase  atomic.Int32
	rex    PhaseCounter
	resync PhaseCounter

	bots      []*runnerBot
	voiceAddr string
	freq      voice.KHz
	fullSet   map[int]bool // chosen once, at the first set_n

	nextSeq    atomic.Uint32 // next sequence the talker will send
	wheel      Hist
	joinHist   Hist
	sendErrors atomic.Uint64
	talkCancel context.CancelFunc
	talkDone   chan struct{}

	// nextIdx is the index (and so the bot name) the next join takes. It is
	// not len(bots): a failed join leaves a hole, and reusing len(bots) would
	// collide with a live bot's name.
	nextIdx       int
	nRequested    int
	joinFailTotal atomic.Uint64
	// progressEvery is the progress-line period; zero means 5 s.
	progressEvery time.Duration
	out           *lineWriter

	last            *StepReport
	lastPerListener []LedgerResult
}

// NewRunner hashes the passwords once and returns an idle Runner.
func NewRunner(cfg RunConfig, dial func(ctx context.Context) (*grpc.ClientConn, error)) (*Runner, error) {
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 16
	}
	if cfg.FullSample <= 0 {
		cfg.FullSample = 16
	}
	if cfg.Bounds == (Bounds{}) {
		cfg.Bounds = DefaultBounds
	}
	hashes, err := HashPasswords(cfg.Passwords)
	if err != nil {
		return nil, err
	}
	r := &Runner{cfg: cfg, freq: cfg.Scenario.FreqKHz()}
	r.nextSeq.Store(1)
	r.joiner = &Joiner{
		Dial: dial, Hashes: hashes, Unit: cfg.Unit,
		Phase: &r.phase, Resyncs: &r.resync, ResourceExhausted: &r.rex,
	}
	return r, nil
}

type errLine struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// lineWriter serialises whole-line writes so a progress goroutine and Serve
// never interleave bytes.
type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func writeLine(out io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = out.Write(append(b, '\n'))
	return err
}

// Serve executes commands from in until quit or EOF, writing NDJSON to out.
// A malformed command line is reported as an error line and skipped. A failed
// join is data, not an error: set_n reports it in a joins line and carries on.
// A failed command is still fatal (the error is written, cleanup runs, and it
// is returned): a talker (bot 0) that cannot join, a decreasing set_n, a
// cancelled ctx.
func (r *Runner) Serve(ctx context.Context, in io.Reader, rawOut io.Writer) error {
	out := &lineWriter{w: rawOut}
	r.out = out
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r.shutdown(cctx)
	}()
	// Read stdin in a goroutine so ctx cancellation is honoured while it is
	// blocked. After a cancel that goroutine stays parked in Read until the
	// caller closes stdin or the process exits.
	type inLine struct {
		b   []byte
		err error
		eof bool
	}
	lines := make(chan inLine)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			select {
			case lines <- inLine{b: append([]byte(nil), sc.Bytes()...)}:
			case <-stop:
				return
			}
		}
		select {
		case lines <- inLine{err: sc.Err(), eof: true}:
		case <-stop:
		}
	}()
	for {
		var line []byte
		select {
		case <-ctx.Done():
			return ctx.Err()
		case l := <-lines:
			if l.eof {
				return l.err
			}
			line = l.b
		}
		if len(line) == 0 {
			continue
		}
		cmd, perr := ParseCommand(line)
		if perr != nil {
			if werr := writeLine(out, errLine{"error", perr.Error()}); werr != nil {
				return werr
			}
			continue
		}
		var cerr error
		switch {
		case cmd.SetN != nil:
			var jr JoinReport
			jr, cerr = r.setN(ctx, *cmd.SetN)
			if cerr == nil {
				if werr := writeLine(out, jr); werr != nil {
					return werr
				}
			}
		case cmd.Hold != nil:
			cerr = r.hold(ctx, time.Duration(*cmd.Hold*float64(time.Second)), *cmd.Hold)
		case cmd.Report != nil:
			cerr = r.report(out, *cmd.Report)
		case cmd.Quit:
			return nil
		}
		if cerr != nil {
			_ = writeLine(out, errLine{"error", cerr.Error()})
			return cerr
		}
	}
}

func (r *Runner) botName(i int) string { return fmt.Sprintf("%s-%04d", r.cfg.Unit, i) }

func (r *Runner) setN(ctx context.Context, n int) (JoinReport, error) {
	have := len(r.bots)
	had0 := have
	switch {
	case n < have:
		return JoinReport{}, fmt.Errorf("set_n %d: decreasing N is not supported (have %d)", n, have)
	case n == have:
		r.nRequested = n
		return newJoinTally(n, have).report(n, have, 0, RampHealth{}), nil
	}
	if r.fullSet == nil {
		// Drawn once from the seed over the first N; bots added later by a
		// larger set_n drain only.
		r.fullSet = map[int]bool{}
		k := r.cfg.FullSample
		if k > n {
			k = n
		}
		for _, p := range rand.New(rand.NewSource(r.cfg.Seed)).Perm(n)[:k] {
			r.fullSet[p] = true
		}
	}
	r.nRequested = n
	r.phase.Store(int32(PhaseRamp))
	tally := newJoinTally(n, have)
	rs0 := SampleRuntime()
	if have == 0 {
		// Bot 0 joins alone so the voice endpoint and server settings are
		// known before the rest, and it becomes the talker. Without a talker
		// there is nothing to measure, so its failure stays fatal.
		b, err := r.joinOne(ctx, 0, r.fullSet[0])
		if err != nil {
			return JoinReport{}, err
		}
		r.bots = append(r.bots, b)
		r.nextIdx = 1
		r.startTalker(b)
		tally.begin()
		tally.ok(b.tries)
		have = 1
	}
	stopProgress := r.startProgress(tally)
	defer stopProgress()

	want := n - have
	slots := make([]*runnerBot, want)
	first := r.nextIdx
	sem := make(chan struct{}, r.cfg.MaxInFlight)
	var wg sync.WaitGroup
	for k := range slots {
		sem <- struct{}{}
		if ctx.Err() != nil {
			<-sem
			break
		}
		tally.begin()
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			defer func() { <-sem }()
			idx := first + k
			b, err := r.joinOne(ctx, idx, r.fullSet[idx])
			if err != nil {
				var je *JoinError
				if ctx.Err() == nil && errors.As(err, &je) {
					tally.fail(je.failure())
					return
				}
				tally.fail(JoinFailure{Bot: idx, Stage: StageControl, Error: err.Error()})
				return
			}
			slots[k] = b
			tally.ok(b.tries)
		}(k)
	}
	wg.Wait()
	// Keep what joined so shutdown releases it. Index order is preserved.
	for _, b := range slots {
		if b != nil {
			r.bots = append(r.bots, b)
		}
	}
	r.nextIdx = first + want
	if err := ctx.Err(); err != nil {
		return JoinReport{}, err
	}
	rs1 := SampleRuntime()
	rep := tally.report(n, had0, time.Since(tally.start), RampHealth{
		SchedP99US:   DeltaQuantile(rs0.Sched, rs1.Sched, 0.99).Microseconds(),
		GCPauseP99US: DeltaQuantile(rs0.GC, rs1.GC, 0.99).Microseconds(),
	})
	r.joinFailTotal.Add(uint64(rep.JoinFailures))
	r.phase.Store(int32(PhaseSettle))
	return rep, nil
}

// startProgress writes a ramp progress line every progressEvery (5 s by
// default) until the returned stop function is called. A ramp shorter than
// the period writes none.
func (r *Runner) startProgress(t *joinTally) (stop func()) {
	every := r.progressEvery
	if every <= 0 {
		every = 5 * time.Second
	}
	if r.out == nil {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		tk := time.NewTicker(every)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				_ = writeLine(r.out, t.progress())
			}
		}
	}()
	return func() { close(done); <-stopped }
}

func (r *Runner) resolveVoiceAddr(b *ControlBot) error {
	if err := r.cfg.Scenario.CheckAgainstServer(b.Settings); err != nil {
		return err
	}
	// The real client's precedence: the coalition address from sync when set,
	// else the configured one. Never global_voice_addr.
	addr := b.CoalitionAddr
	if addr == "" {
		addr = r.cfg.VoiceAddr
	}
	if addr == "" {
		return errors.New("no voice endpoint: sync gave no coalition_voice_addr and --voice is unset")
	}
	if err := r.cfg.Allow.Check(addr); err != nil {
		return fmt.Errorf("voice endpoint refused: %w", err)
	}
	r.voiceAddr = addr
	return nil
}

func (r *Runner) joinOne(ctx context.Context, i int, full bool) (*runnerBot, error) {
	start := time.Now()
	ctl, err := r.joiner.Join(ctx, r.botName(i), r.cfg.Scenario.Coalition, full)
	if err != nil {
		return nil, &JoinError{Bot: i, Stage: StageControl, Err: err}
	}
	r.joinHist.Record(time.Since(start))
	fail := func(stage JoinStage, tries int, what string, bare bool, err error) (*runnerBot, error) {
		_ = ctl.Close(context.Background())
		return nil, &JoinError{Bot: i, GUID: ctl.GUID, Stage: stage, Tries: tries, Err: err, what: what, bare: bare}
	}
	if i == 0 {
		if err := r.resolveVoiceAddr(ctl); err != nil {
			return fail(StageRadio, 0, "", true, err)
		}
	}
	id, err := uuid.Parse(ctl.GUID)
	if err != nil {
		return fail(StageRadio, 0, fmt.Sprintf("client guid %q", ctl.GUID), false, err)
	}
	if err := ctl.UpdateRadio(ctx, r.freq.MHz32()); err != nil {
		return fail(StageRadio, 0, "", false, err)
	}
	vc, err := DialVoice(r.voiceAddr, id)
	if err != nil {
		return fail(StageVoiceDial, 0, "", false, err)
	}
	tries, err := vc.Hello(ctx, ctl.Secret, DefaultHelloLadder)
	if err != nil {
		vc.Close()
		return fail(StageHello, tries, "", false, err)
	}
	// The receive loop outlives this call; Close ends it.
	vc.Start(context.Background())
	return &runnerBot{ctl: ctl, voice: vc, id: id, tries: tries}, nil
}

// startTalker sends SyntheticFrame at 50 fps on an absolute schedule: tick k
// is due at t0 + k*20ms, never an accumulating ticker.
// After a stall the loop does not skip or reschedule: it sends the missed
// frames back to back (open loop), and the lag is recorded against each due
// time, so no coordinated omission. The burst can distort loss and burst
// figures; the D14 self-health set exists to flag such a stall.
func (r *Runner) startTalker(b *runnerBot) {
	ctx, cancel := context.WithCancel(context.Background())
	r.talkCancel = cancel
	r.talkDone = make(chan struct{})
	go func() {
		defer close(r.talkDone)
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
			r.wheel.Record(lag)
			seq := r.nextSeq.Load()
			if err := b.voice.Send(seq, r.freq, SyntheticFrame(seq)); err != nil {
				r.sendErrors.Add(1)
			}
			r.nextSeq.Store(seq + 1)
		}
	}()
}

func (r *Runner) hold(ctx context.Context, d time.Duration, secs float64) error {
	if len(r.bots) < 2 {
		return errors.New("hold: need at least one listener (set_n >= 2)")
	}
	for i, b := range r.bots {
		if err := b.ctl.Err(); err != nil {
			return fmt.Errorf("hold: bot %d died: %w", i, err)
		}
	}
	talker := r.bots[0]
	listeners := r.bots[1:]
	frames := int(math.Round(secs * framesPerSec))
	if frames < 1 {
		frames = 1
	}
	r.phase.Store(int32(PhaseHold))
	defer r.phase.Store(int32(PhaseSettle))

	base := uint64(r.nextSeq.Load()) + holdLeadFrames
	for _, l := range listeners {
		l.voice.ExpectFrom(talker.id, r.freq, base, frames)
		l.voice.RxLag.Reset()
	}
	r.wheel.Reset()
	resync0, rex0 := phaseCounts(&r.resync), phaseCounts(&r.rex)
	sendErr0 := r.sendErrors.Load()
	drops0 := make([]uint32, len(listeners))
	for i, l := range listeners {
		drops0[i] = l.voice.Drops.Load()
	}
	rs0 := SampleRuntime()

	// The window ends when the talker has sent the last expected frame (it
	// paces itself at 50 fps), bounded so a stalled talker cannot hang.
	end := base + uint64(frames)
	deadline := time.After(d + 5*time.Second)
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for uint64(r.nextSeq.Load()) < end {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return errors.New("hold: talker did not send the expected frames in time")
		case <-poll.C:
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(holdDrain):
	}
	rs1 := SampleRuntime()

	h := Health{GenCPU: -1}
	var rx Hist
	var drops uint64
	for i, l := range listeners {
		drops += uint64(l.voice.Drops.Load() - drops0[i])
		rx.Merge(l.voice.RxLag)
	}
	h.SocketDrops = drops
	h.SendErrors = r.sendErrors.Load() - sendErr0
	if rx.Count() > 0 {
		p := rx.Quantile(0.99)
		h.RxLagP99 = &p
	}
	h.WheelLagP99 = r.wheel.Quantile(0.99)
	h.SchedP99 = DeltaQuantile(rs0.Sched, rs1.Sched, 0.99)
	h.GCPauseP99 = DeltaQuantile(rs0.GC, rs1.GC, 0.99)

	rep := &StepReport{
		Type: "step", Label: ReportLabel, Payload: PayloadLabel,
		Arm: r.cfg.Arm, Scenario: r.cfg.Scenario.Name,
		NRequested: r.nRequested, JoinFailuresTotal: r.joinFailTotal.Load(),
		N: len(r.bots), Talkers: 1, Listeners: len(listeners), HoldS: secs,
		Resyncs:           holdDelta(phaseCounts(&r.resync), resync0),
		ResourceExhausted: holdDelta(phaseCounts(&r.rex), rex0),
		Updates:           r.updateTotals(),
		JoinP99Us:         r.joinHist.Quantile(0.99).Microseconds(), JoinLabel: JoinLabel,
		Health: NewHealthReport(h), Bounds: NewBoundsReport(r.cfg.Bounds),
	}
	rep.Violations = r.cfg.Bounds.Violations(h)
	rep.Valid = len(rep.Violations) == 0
	per := make([]LedgerResult, 0, len(listeners))
	for _, l := range listeners {
		if rep.HelloTriesMax < l.tries {
			rep.HelloTriesMax = l.tries
		}
		lr, ok := l.voice.LedgerResult(talker.id, r.freq)
		if !ok {
			return errors.New("hold: a listener lost its ledger")
		}
		per = append(per, lr)
		rep.Expected += lr.Expected
		rep.Unique += lr.Unique
		rep.Lost += lr.Lost
		rep.Dups += lr.Dups
		rep.Reordered += lr.Reordered
		for _, b := range lr.Bursts {
			if b > rep.BurstMax {
				rep.BurstMax = b
			}
		}
	}
	if talker.tries > rep.HelloTriesMax {
		rep.HelloTriesMax = talker.tries
	}
	if rep.Expected > 0 {
		rep.LossRatio = float64(rep.Lost) / float64(rep.Expected)
	}
	r.last, r.lastPerListener = rep, per
	return nil
}

// updateTotals sums every bot's drain counters by update type name.
func (r *Runner) updateTotals() map[string]uint64 {
	m := map[string]uint64{}
	var types []int
	for t := range srspb.ServerUpdate_UpdateType_name {
		types = append(types, int(t))
	}
	sort.Ints(types)
	for _, b := range r.bots {
		for _, t := range types {
			ut := srspb.ServerUpdate_UpdateType(t)
			if c := b.ctl.Counts.Get(ut); c > 0 {
				m[ut.String()] += c
			}
		}
		if o := b.ctl.Counts.Other.Load(); o > 0 {
			m["OTHER"] += o
		}
	}
	return m
}

func (r *Runner) report(out io.Writer, step string) error {
	if r.last == nil {
		return errors.New("report: no hold has run yet")
	}
	rep := *r.last
	rep.Step = step
	return writeLine(out, rep)
}

// shutdown stops the talker, then BYEs and closes every bot.
func (r *Runner) shutdown(ctx context.Context) {
	if r.talkCancel != nil {
		r.talkCancel()
		<-r.talkDone
		r.talkCancel = nil
	}
	var wg sync.WaitGroup
	for _, b := range r.bots {
		wg.Add(1)
		go func(b *runnerBot) {
			defer wg.Done()
			b.voice.Bye()
			b.voice.Close()
			_ = b.ctl.Close(ctx)
		}(b)
	}
	wg.Wait()
	r.bots = nil
}
