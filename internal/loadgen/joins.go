package loadgen

import (
	"encoding/json"
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
}

// MarshalJSON keeps zero-valued ramp counters (a ramp that has finished
// nothing yet still reports done 0) while omitting the other phase's keys.
func (p ProgressLine) MarshalJSON() ([]byte, error) {
	if p.Phase == "ramp" {
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
		}{p.Type, p.Phase, p.Requested, p.Had, p.Done, p.Joined, p.JoinFailures, p.InFlight, p.HelloRetried, p.ElapsedS})
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
	t.mu.Unlock()
}

func (t *joinTally) finish() {
	if t.inFlight > 0 {
		t.inFlight--
	}
}

// ok records a bot whose ACK came on HELLO number tries.
func (t *joinTally) ok(tries int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.finish()
	t.joined++
	if tries >= 2 {
		t.retried++
	}
	if tries > t.triesMax {
		t.triesMax = tries
	}
	k := tries
	if k < 0 {
		k = 0
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
	t.finish()
	switch f.Stage {
	case StageControl:
		t.byStage.Control++
	case StageRadio:
		t.byStage.Radio++
	case StageVoiceDial:
		t.byStage.VoiceDial++
	default:
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
	return ProgressLine{
		Type: "progress", Phase: "ramp", Requested: t.requested, Had: t.had,
		Done: t.joined + len(t.failures), Joined: t.joined, JoinFailures: len(t.failures),
		InFlight: t.inFlight, HelloRetried: t.retried,
		ElapsedS: time.Since(t.start).Seconds(),
	}
}

// report builds the joins line once the ramp has ended.
func (t *joinTally) report(requested, had int, ramp time.Duration, rh RampHealth) JoinReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	failed := append([]JoinFailure{}, t.failures...)
	sort.Slice(failed, func(i, j int) bool { return failed[i].Bot < failed[j].Bot })
	truncated := len(failed) > maxFailedListed
	if truncated {
		failed = failed[:maxFailedListed]
	}
	return JoinReport{
		Type: "joins", Schema: JoinsSchema, Requested: requested, Had: had,
		N: had + t.joined, Joined: t.joined, JoinFailures: len(t.failures),
		FailuresByStage: t.byStage, HelloTriesHist: append([]int(nil), t.hist...),
		HelloTriesMax: t.triesMax, Failed: failed, FailedTruncated: truncated,
		RampS: ramp.Seconds(), RampHealth: rh, Label: ReportLabel,
	}
}
