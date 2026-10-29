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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

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
	// PingEvery is each bot's control Ping period; zero disables pings.
	PingEvery time.Duration
	// JoinRateMax and JoinRateScale bound a set_n ramp's join rate (see
	// RateLimiter). Both zero means unbounded; the CLI supplies the I-9
	// defaults (12 and 5000).
	JoinRateMax, JoinRateScale float64
	// RampTalker is "silent" (the default when empty) or "live": whether the
	// talker keeps sending during a set_n ramp.
	RampTalker string
}

// I-9 defaults for the `run` flags. The CLI and the features line use them.
const (
	DefaultJoinRateMax   = 12.0
	DefaultJoinRateScale = 5000.0
	DefaultMaxInFlight   = 8
	MaxMaxInFlight       = 16
	RampTalkerSilent     = "silent"
	RampTalkerLive       = "live"
)

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
	talker     *talkerLoop
	limiter    *RateLimiter
	// peakInFlight is the highest observed in-flight join count of the last
	// ramp, the gauge behind the in-flight cap.
	peakInFlight atomic.Int32

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

	// Death log (I-10, I-11). dmu guards everything below it.
	dmu           sync.Mutex
	deadTotal     int
	deadSinceStep int
	deadInHold    int
	deadFirst     []DeathRecord
	deathLines    int

	started time.Time
	lastCmd string
	errCtx  string
}

// maxDeathLines caps control_lost lines per process; later deaths are only
// counted (dead_total).
const maxDeathLines = 64

// maxCmdContext caps the last command echoed in the fatal error context.
const maxCmdContext = 200

// maxDeathFirst is how many deaths a step line lists.
const maxDeathFirst = 8

// onDead records one bot's death, stamping the phase and index, and writes
// the control_lost line while the cap allows.
func (r *Runner) onDead(_ *ControlBot, d DeathRecord) {
	d.Phase = Phase(r.phase.Load()).String()
	if i := strings.LastIndexByte(d.Name, '-'); i >= 0 {
		if n, err := strconv.Atoi(d.Name[i+1:]); err == nil {
			d.Bot = n
		}
	}
	r.dmu.Lock()
	defer r.dmu.Unlock()
	r.deadTotal++
	r.deadSinceStep++
	if Phase(r.phase.Load()) == PhaseHold {
		r.deadInHold++
	}
	if len(r.deadFirst) < maxDeathFirst {
		r.deadFirst = append(r.deadFirst, d)
	}
	if r.deathLines < maxDeathLines && r.out != nil {
		r.deathLines++
		_ = writeLine(r.out, DeathLine{Type: "control_lost", DeathRecord: d, DeadTotal: r.deadTotal})
	}
}

// ErrContext describes the run at the moment Serve ended, for the fatal
// error line: last command, bots, dead bots, uptime.
func (r *Runner) ErrContext() string { return r.errCtx }

func (r *Runner) snapshotContext() string {
	r.dmu.Lock()
	dead := r.deadTotal
	r.dmu.Unlock()
	up := 0.0
	if !r.started.IsZero() {
		up = math.Round(time.Since(r.started).Seconds()*10) / 10
	}
	cmd := r.lastCmd
	if cmd == "" {
		cmd = "none"
	} else if len(cmd) > maxCmdContext {
		cmd = cmd[:maxCmdContext] + "..."
	}
	return fmt.Sprintf("last command %s; bots %d, dead %d, uptime %gs", cmd, len(r.bots), dead, up)
}

// NewRunner hashes the passwords once and returns an idle Runner.
func NewRunner(cfg RunConfig, dial DialFunc) (*Runner, error) {
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 16
	}
	if cfg.FullSample <= 0 {
		cfg.FullSample = 16
	}
	if cfg.Bounds == (Bounds{}) {
		cfg.Bounds = DefaultBounds
	}
	switch cfg.RampTalker {
	case "":
		cfg.RampTalker = RampTalkerSilent
	case RampTalkerSilent, RampTalkerLive:
	default:
		return nil, fmt.Errorf("ramp talker %q: want silent or live", cfg.RampTalker)
	}
	if cfg.JoinRateMax < 0 || cfg.JoinRateScale < 0 {
		return nil, errors.New("join rate: max and scale must not be negative")
	}
	if cfg.MaxInFlight > MaxMaxInFlight {
		return nil, fmt.Errorf("max in flight %d: want 1..%d", cfg.MaxInFlight, MaxMaxInFlight)
	}
	hashes, err := HashPasswords(cfg.Passwords)
	if err != nil {
		return nil, err
	}
	r := &Runner{cfg: cfg, freq: cfg.Scenario.FreqKHz(),
		limiter: &RateLimiter{Max: cfg.JoinRateMax, Scale: cfg.JoinRateScale}}
	r.nextSeq.Store(1)
	r.joiner = &Joiner{
		Dial: dial, Hashes: hashes, Unit: cfg.Unit,
		Phase: &r.phase, Resyncs: &r.resync, ResourceExhausted: &r.rex,
		PingEvery: cfg.PingEvery, PingSeed: cfg.Seed, OnDead: r.onDead,
	}
	r.phase.Store(int32(PhaseIdle))
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
//
// A dead bot is data too: a control_lost line when it dies and a
// control_lost block in the next step. A hold fails only for the talker, a
// lost ledger, ctx, or a missing listener.
func (r *Runner) Serve(ctx context.Context, in io.Reader, rawOut io.Writer) error {
	out := &lineWriter{w: rawOut}
	r.out = out
	r.started = time.Now()
	defer func() {
		r.errCtx = r.snapshotContext()
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
		r.lastCmd = string(line)
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

// setN ramps to n bots. Bot 0 (the have == 0 path) joins alone, outside the
// rate limiter and the in-flight cap, so the voice endpoint is known first. Its
// duration is measured from tally.start, so it is counted in the joins line
// (joined, join_p99_us, joins_per_s) of the first ramp.
func (r *Runner) setN(ctx context.Context, n int) (JoinReport, error) {
	have := len(r.bots)
	had0 := have
	switch {
	case n < have:
		return JoinReport{}, fmt.Errorf("set_n %d: decreasing N is not supported (have %d)", n, have)
	case n == have:
		r.nRequested = n
		nt := newJoinTally(n, have)
		nt.configure(RampMode, r.cfg.RampTalker, r.cfg.MaxInFlight, r.limiter)
		return nt.report(n, have, 0, RampHealth{}), nil
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
	silent := r.cfg.RampTalker == RampTalkerSilent
	if silent && r.talker != nil {
		r.talker.pause()
	}
	// Resume on every exit, error paths included. On the have == 0 path the
	// talker does not exist yet; it is created paused below.
	defer func() {
		if silent && r.talker != nil {
			r.talker.resume()
		}
	}()
	r.phase.Store(int32(PhaseRamp))
	tally := newJoinTally(n, have)
	tally.configure(RampMode, r.cfg.RampTalker, r.cfg.MaxInFlight, r.limiter)
	rs0 := SampleRuntime()
	if have == 0 {
		// Bot 0 joins alone so the voice endpoint and server settings are
		// known before the rest, and it becomes the talker. Without a talker
		// there is nothing to measure, so its failure stays fatal.
		b, err := r.joinOne(ctx, 0, r.fullSet[0])
		if err != nil {
			// Fatal for the whole command (Serve exits), and the progress
			// ticker has not started. Reset the phase anyway so making this
			// error non-fatal later cannot leave the runner stuck in ramp.
			r.phase.Store(int32(PhaseSettle))
			return JoinReport{}, err
		}
		r.bots = append(r.bots, b)
		r.nextIdx = 1
		r.startTalker(b, silent)
		tally.begin()
		tally.ok(b.tries, time.Since(tally.start))
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
		// A rate token first, then an in-flight slot, so a join waiting for
		// its token holds no slot.
		if err := r.limiter.Wait(ctx, tally.alive()); err != nil {
			break
		}
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
			jstart := time.Now()
			b, err := r.joinOne(ctx, idx, r.fullSet[idx])
			if err != nil {
				if ctx.Err() != nil {
					// Interrupted by cancellation, not a join failure. The
					// tally is discarded (setN returns ctx.Err()), so do not
					// record a misleading control-stage failure.
					return
				}
				var je *JoinError
				if errors.As(err, &je) {
					tally.fail(je.failure())
					return
				}
				tally.fail(JoinFailure{Bot: idx, Stage: StageControl, Error: err.Error()})
				return
			}
			slots[k] = b
			tally.ok(b.tries, time.Since(jstart))
		}(k)
	}
	wg.Wait()
	r.peakInFlight.Store(int32(tally.peakInFlight()))
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

// defaultProgressEvery is the progress-line period when none is configured.
const defaultProgressEvery = 5 * time.Second

// progressPeriod returns every, or the 5 s default when it is not positive.
func progressPeriod(every time.Duration) time.Duration {
	if every <= 0 {
		return defaultProgressEvery
	}
	return every
}

// startProgress writes a ramp progress line every progressEvery (5 s by
// default) until the returned stop function is called. A ramp shorter than
// the period writes none.
func (r *Runner) startProgress(t *joinTally) (stop func()) {
	every := progressPeriod(r.progressEvery)
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

// startTalker starts the talker on b, paused when paused is true. See
// talkerLoop for the schedule and the pause semantics.
func (r *Runner) startTalker(b *runnerBot, paused bool) {
	r.talker = startTalkerLoop(b.voice, r.freq, &r.nextSeq, &r.wheel, &r.sendErrors, paused)
}

func (r *Runner) hold(ctx context.Context, d time.Duration, secs float64) error {
	if len(r.bots) < 2 {
		return errors.New("hold: need at least one listener (set_n >= 2)")
	}
	talker := r.bots[0]
	listeners := r.bots[1:]
	frames := int(math.Round(secs * framesPerSec))
	if frames < 1 {
		frames = 1
	}
	// Zero the hold's death counter before the phase flips, so a death that
	// lands right after the flip is stamped hold and still counted.
	r.dmu.Lock()
	r.deadInHold = 0
	r.dmu.Unlock()
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
	ping0 := r.pingTotals(true)
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
	rep.Ping = r.pingReport(ping0)
	rep.Warnings = []string{}
	r.dmu.Lock()
	rep.ControlLost = ControlLostReport{
		DeadTotal: r.deadTotal, DeadInHold: r.deadInHold,
		DeadSinceLastStep: r.deadSinceStep, Alive: len(r.bots) - r.deadTotal,
		First: append([]DeathSummary{}, r.deadFirst...),
	}
	r.deadSinceStep, r.deadFirst = 0, nil
	r.dmu.Unlock()
	r.last, r.lastPerListener = rep, per
	return nil
}

type pingTotals struct{ sent, failed uint64 }

// pingTotals sums every bot's Ping counters; reset also clears the RTT
// histograms so the hold's p99 covers the hold only.
func (r *Runner) pingTotals(reset bool) pingTotals {
	var t pingTotals
	for _, b := range r.bots {
		t.sent += b.ctl.Pings.Sent.Load()
		t.failed += b.ctl.Pings.Failed.Load()
		if reset {
			b.ctl.Pings.RTT.Reset()
		}
	}
	return t
}

func (r *Runner) pingReport(base pingTotals) PingReport {
	if r.cfg.PingEvery <= 0 {
		return PingReport{}
	}
	now := r.pingTotals(false)
	var h Hist
	for _, b := range r.bots {
		h.Merge(&b.ctl.Pings.RTT)
	}
	return PingReport{
		EveryS: r.cfg.PingEvery.Seconds(), Sent: now.sent - base.sent,
		Failed: now.failed - base.failed, RTTP99Us: h.Quantile(0.99).Microseconds(),
	}
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
	if r.talker != nil {
		r.talker.stop()
		r.talker = nil
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
