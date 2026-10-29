package loadgen

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// JoinStage names where in a bot's join a failure happened.
type JoinStage string

const (
	// StageControl is anything inside Joiner.Join: dial, InitAuth, GuestLogin,
	// subscribe, ack, SyncClient.
	StageControl JoinStage = "control"
	// StageRadio is the voice-address check, GUID parse and UpdateRadio.
	StageRadio JoinStage = "radio"
	// StageVoiceDial is DialVoice.
	StageVoiceDial JoinStage = "voice_dial"
	// StageHello is Hello exhausting its ladder or the socket failing.
	StageHello JoinStage = "hello"
)

// JoinError is a failed join tagged with the stage it failed at. Error()
// reproduces the v1 error text exactly, so log readers that match it keep
// working: a control failure is the Joiner's own text, every later stage reads
// "bot N: <what>: <err>".
type JoinError struct {
	Bot   int
	GUID  string
	Stage JoinStage
	Tries int
	Err   error
	// what overrides the stage's default text prefix (for example the GUID
	// parse failure, which belongs to the radio stage but reads differently).
	what string
	// bare drops the prefix altogether: "bot N: <err>".
	bare bool
}

func (e *JoinError) Error() string {
	if e.Stage == StageControl {
		return e.Err.Error()
	}
	if e.bare {
		return fmt.Sprintf("bot %d: %v", e.Bot, e.Err)
	}
	what := e.what
	if what == "" {
		switch e.Stage {
		case StageRadio:
			what = "update radio"
		case StageVoiceDial:
			what = "dial voice"
		default:
			what = "hello"
		}
	}
	return fmt.Sprintf("bot %d: %s: %v", e.Bot, what, e.Err)
}

func (e *JoinError) Unwrap() error { return e.Err }

// failure converts the error to its JoinsReport entry.
func (e *JoinError) failure() JoinFailure {
	msg := e.Err.Error()
	if e.what != "" && !e.bare {
		// A custom context (the GUID parse failure) is kept; the default
		// stage prefixes are dropped because the stage field carries them.
		msg = e.what + ": " + msg
	}
	return JoinFailure{Bot: e.Bot, GUID: e.GUID, Stage: e.Stage, Tries: e.Tries, Error: msg}
}

// JoinsSchema is the value of every joins line's schema key.
const JoinsSchema = "loadbot-joins/1"

// maxFailedListed caps JoinReport.Failed (interface I-1).
const maxFailedListed = 32

// JoinFailure is one failed bot of a ramp.
type JoinFailure struct {
	Bot   int       `json:"bot"`
	GUID  string    `json:"guid"`
	Stage JoinStage `json:"stage"`
	Tries int       `json:"tries"`
	Error string    `json:"error"`
}

// StageCounts always carries all four stage keys.
type StageCounts struct {
	Control   int `json:"control"`
	Radio     int `json:"radio"`
	VoiceDial int `json:"voice_dial"`
	Hello     int `json:"hello"`
}

// RampHealth is the scheduler and GC p99 over a ramp. Logged, never classified.
type RampHealth struct {
	SchedP99US   int64 `json:"sched_p99_us"`
	GCPauseP99US int64 `json:"gc_pause_p99_us"`
}

// JoinReport is the `joins` NDJSON line written once per accepted set_n
// (interface I-1).
type JoinReport struct {
	Type            string        `json:"type"`
	Schema          string        `json:"schema"`
	Requested       int           `json:"requested"`
	Had             int           `json:"had"`
	N               int           `json:"n"`
	Joined          int           `json:"joined"`
	JoinFailures    int           `json:"join_failures"`
	FailuresByStage StageCounts   `json:"failures_by_stage"`
	HelloTriesHist  []int         `json:"hello_tries_hist"`
	HelloTriesMax   int           `json:"hello_tries_max"`
	Failed          []JoinFailure `json:"failed"`
	FailedTruncated bool          `json:"failed_truncated"`
	RampS           float64       `json:"ramp_s"`
	RampHealth      RampHealth    `json:"ramp_health"`
	Label           string        `json:"label"`

	// v3 additions (I-12).
	Mode         string     `json:"mode"`
	Talker       string     `json:"talker"`
	JoinsPerS    float64    `json:"joins_per_s"`
	JoinP99Us    int64      `json:"join_p99_us"`
	HelloRetried int        `json:"hello_retried"`
	InFlightMax  int        `json:"in_flight_max"`
	RateLimit    *RateLimit `json:"rate_limit"`
}

// RampMode and BurstMode are the joins line's mode values.
const (
	RampMode  = "ramp"
	BurstMode = "burst"
)

// RateLimit is the ramp's configured limiter; a joins line carries null for
// an unbounded ramp or a burst.
type RateLimit struct {
	MaxPerS float64 `json:"max_per_s"`
	Scale   float64 `json:"scale"`
}

// ProgressLine is the `progress` NDJSON line (interface I-2). The ramp phase
// writes the requested..in_flight keys, calibrate_ramp writes target and
// bots; MarshalJSON emits only the keys of the line's phase.
type ProgressLine struct {
	Type         string  `json:"type"`
	Phase        string  `json:"phase"`
	Requested    int     `json:"requested,omitempty"`
	Had          int     `json:"had,omitempty"`
	Done         int     `json:"done,omitempty"`
	Joined       int     `json:"joined,omitempty"`
	JoinFailures int     `json:"join_failures,omitempty"`
	InFlight     int     `json:"in_flight,omitempty"`
	Target       int     `json:"target,omitempty"`
	Bots         int     `json:"bots,omitempty"`
	HelloRetried int     `json:"hello_retried"`
	ElapsedS     float64 `json:"elapsed_s"`
	// RatePerS is the join limit in force (0 = unbounded) and Talker the
	// talker's state during the ramp (v3, I-14). Ramp lines only.
	RatePerS float64 `json:"rate_per_s,omitempty"`
	Talker   string  `json:"talker,omitempty"`
}

// MarshalJSON keeps zero-valued ramp counters (a ramp that has finished
// nothing yet still reports done 0) while omitting the other phase's keys.
func (p ProgressLine) MarshalJSON() ([]byte, error) {
	if p.Phase == "ramp" || p.Phase == "burst" {
		return json.Marshal(struct {
			Type         string  `json:"type"`
			Phase        string  `json:"phase"`
			Requested    int     `json:"requested"`
			Had          int     `json:"had"`
			Done         int     `json:"done"`
			Joined       int     `json:"joined"`
			JoinFailures int     `json:"join_failures"`
			InFlight     int     `json:"in_flight"`
			HelloRetried int     `json:"hello_retried"`
			ElapsedS     float64 `json:"elapsed_s"`
			RatePerS     float64 `json:"rate_per_s"`
			Talker       string  `json:"talker"`
		}{p.Type, p.Phase, p.Requested, p.Had, p.Done, p.Joined, p.JoinFailures, p.InFlight, p.HelloRetried, p.ElapsedS, p.RatePerS, p.Talker})
	}
	if p.Phase == "calibrate_ramp" {
		return json.Marshal(struct {
			Type         string  `json:"type"`
			Phase        string  `json:"phase"`
			Target       int     `json:"target"`
			Bots         int     `json:"bots"`
			HelloRetried int     `json:"hello_retried"`
			ElapsedS     float64 `json:"elapsed_s"`
		}{p.Type, p.Phase, p.Target, p.Bots, p.HelloRetried, p.ElapsedS})
	}
	type plain ProgressLine
	return json.Marshal(plain(p))
}

// joinTally aggregates one ramp's outcomes. Safe for concurrent use.
type joinTally struct {
	mu        sync.Mutex
	start     time.Time
	requested int
	had       int
	inFlight  int
	joined    int
	retried   int
	triesMax  int
	hist      []int
	byStage   StageCounts
	failures  []JoinFailure

	mode, talker string
	inFlightCap  int
	limiter      *RateLimiter
	peak         int
	dur          Hist // join start to HELLO_ACK, joined bots only
}

// configure records the ramp's mode, talker state, in-flight cap and
// limiter for the progress and joins lines.
func (t *joinTally) configure(mode, talker string, inFlightCap int, l *RateLimiter) {
	t.mu.Lock()
	t.mode, t.talker, t.inFlightCap, t.limiter = mode, talker, inFlightCap, l
	t.mu.Unlock()
}

// alive is the bot count the rate limiter sees: had plus joined so far.
func (t *joinTally) alive() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.had + t.joined
}

// peakInFlight is the highest in-flight count seen.
func (t *joinTally) peakInFlight() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.peak
}

func newJoinTally(requested, had int) *joinTally {
	return &joinTally{
		start: time.Now(), requested: requested, had: had,
		hist: make([]int, len(DefaultHelloLadder)+1),
	}
}

// begin marks one join as running.
func (t *joinTally) begin() {
	t.mu.Lock()
	t.inFlight++
	if t.inFlight > t.peak {
		t.peak = t.inFlight
	}
	t.mu.Unlock()
}

// finishLocked retires one in-flight join. The caller must hold t.mu.
func (t *joinTally) finishLocked() {
	if t.inFlight > 0 {
		t.inFlight--
	}
}

// ok records a bot whose ACK came on HELLO number tries, d after its join
// started.
func (t *joinTally) ok(tries int, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.finishLocked()
	t.joined++
	t.dur.Record(d)
	if tries >= 2 {
		t.retried++
	}
	if tries > t.triesMax {
		t.triesMax = tries
	}
	// Index 0 is always 0 (I-1): a join that reports no HELLO is counted as 1.
	k := tries
	if k < 1 {
		k = 1
	}
	if k >= len(t.hist) {
		k = len(t.hist) - 1
	}
	t.hist[k]++
}

// fail records a failed bot. Its Tries count toward hello_tries_max only.
func (t *joinTally) fail(f JoinFailure) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.finishLocked()
	switch f.Stage {
	case StageControl:
		t.byStage.Control++
	case StageRadio:
		t.byStage.Radio++
	case StageVoiceDial:
		t.byStage.VoiceDial++
	case StageHello:
		t.byStage.Hello++
	default:
		// An unknown stage is still a failed join; surface it under hello
		// rather than losing it from failures_by_stage.
		t.byStage.Hello++
	}
	if f.Tries > t.triesMax {
		t.triesMax = f.Tries
	}
	t.failures = append(t.failures, f)
}

// progress snapshots the ramp phase's progress line.
func (t *joinTally) progress() ProgressLine {
	t.mu.Lock()
	defer t.mu.Unlock()
	var rate float64
	if t.limiter != nil {
		rate = t.limiter.Rate(t.had + t.joined)
	}
	phase := "ramp"
	if t.mode == BurstMode {
		phase = "burst"
	}
	return ProgressLine{
		Type: "progress", Phase: phase, Requested: t.requested, Had: t.had,
		Done: t.joined + len(t.failures), Joined: t.joined, JoinFailures: len(t.failures),
		InFlight: t.inFlight, HelloRetried: t.retried,
		ElapsedS: time.Since(t.start).Seconds(),
		RatePerS: rate, Talker: t.talker,
	}
}

// report builds the joins line once the ramp has ended. requested and had must
// equal the values passed to newJoinTally.
func (t *joinTally) report(requested, had int, ramp time.Duration, rh RampHealth) JoinReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	failed := append([]JoinFailure{}, t.failures...)
	sort.Slice(failed, func(i, j int) bool { return failed[i].Bot < failed[j].Bot })
	truncated := len(failed) > maxFailedListed
	if truncated {
		failed = failed[:maxFailedListed]
	}
	mode := t.mode
	if mode == "" {
		mode = RampMode
	}
	rep := JoinReport{
		Type: "joins", Schema: JoinsSchema, Requested: requested, Had: had,
		N: had + t.joined, Joined: t.joined, JoinFailures: len(t.failures),
		FailuresByStage: t.byStage, HelloTriesHist: append([]int(nil), t.hist...),
		HelloTriesMax: t.triesMax, Failed: failed, FailedTruncated: truncated,
		RampS: ramp.Seconds(), RampHealth: rh, Label: ReportLabel,
		Mode: mode, Talker: t.talker, HelloRetried: t.retried, InFlightMax: t.inFlightCap,
	}
	if ramp > 0 {
		rep.JoinsPerS = float64(t.joined) / ramp.Seconds()
	}
	if t.joined > 0 {
		rep.JoinP99Us = t.dur.Quantile(0.99).Microseconds()
	}
	if t.limiter != nil && mode == RampMode && (t.limiter.Max > 0 || t.limiter.Scale > 0) {
		rep.RateLimit = &RateLimit{MaxPerS: t.limiter.Max, Scale: t.limiter.Scale}
	}
	return rep
}
