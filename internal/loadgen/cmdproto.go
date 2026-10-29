package loadgen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ReportLabel is stamped on every report: nothing the bot measures is a
// capacity figure (design D9).
const ReportLabel = "Shared GitHub runner (4 vCPU), loopback, relative only. Not a capacity figure."

// JoinLabel qualifies JoinP99Us: joins are closed-loop, so it is not an
// open-loop latency (design 4.5g).
// Both plan labels appear verbatim, joined by "; ".
const JoinLabel = "closed-loop, <= 16 in flight; server-side cost only; client hash excluded"

// Command is one NDJSON line on the bot's stdin. Exactly one field is set.
type Command struct {
	SetN   *int      `json:"set_n"`
	Hold   *float64  `json:"hold"`
	Report *string   `json:"report"`
	Quit   bool      `json:"quit"`
	Burst  *BurstCmd `json:"burst"`
}

// BurstCmd is the burst command's body (I-13). All three fields are required.
type BurstCmd struct {
	Add      int    `json:"add"`
	InFlight int    `json:"in_flight"`
	Talker   string `json:"talker"`
}

// ParseCommand decodes one command line: unknown fields, trailing data and
// anything but exactly one set field are errors.
func ParseCommand(line []byte) (Command, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var c Command
	if err := dec.Decode(&c); err != nil {
		return Command{}, fmt.Errorf("command: %w", err)
	}
	if dec.More() {
		return Command{}, errors.New("command: trailing data after JSON object")
	}
	set := 0
	if c.SetN != nil {
		set++
		if *c.SetN < 1 {
			return Command{}, fmt.Errorf("command: set_n %d must be >= 1", *c.SetN)
		}
	}
	if c.Hold != nil {
		set++
		if !(*c.Hold > 0) {
			return Command{}, fmt.Errorf("command: hold %v must be > 0", *c.Hold)
		}
	}
	if c.Report != nil {
		set++
	}
	if c.Quit {
		set++
	}
	if c.Burst != nil {
		set++
		b := c.Burst
		switch {
		case b.Add < 1:
			return Command{}, fmt.Errorf("command: burst add %d must be >= 1", b.Add)
		case b.InFlight < 1 || b.InFlight > MaxMaxInFlight:
			return Command{}, fmt.Errorf("command: burst in_flight %d must be 1..%d", b.InFlight, MaxMaxInFlight)
		case b.Talker != RampTalkerSilent && b.Talker != RampTalkerLive:
			return Command{}, fmt.Errorf("command: burst talker %q: want silent or live", b.Talker)
		}
	}
	if set != 1 {
		return Command{}, fmt.Errorf("command: exactly one field must be set, got %d", set)
	}
	return c, nil
}

// PhaseCounts is a per-phase counter in report form.
type PhaseCounts struct {
	Ramp   uint64 `json:"ramp"`
	Settle uint64 `json:"settle"`
	Hold   uint64 `json:"hold"`
}

// holdDelta turns cumulative counts into a per-hold figure: Hold is the
// increase since base; Ramp and Settle stay cumulative, because they accrue
// between holds (set_n and idle time), not inside one.
func holdDelta(now, base PhaseCounts) PhaseCounts {
	now.Hold -= base.Hold
	return now
}

func phaseCounts(c *PhaseCounter) PhaseCounts {
	return PhaseCounts{Ramp: c[PhaseRamp].Load(), Settle: c[PhaseSettle].Load(), Hold: c[PhaseHold].Load()}
}

// HealthReport is Health in wire form: microseconds, null for not available.
type HealthReport struct {
	SocketDrops   uint64   `json:"socket_drops"`
	SendErrors    uint64   `json:"send_errors"`
	RxLagP99Us    *int64   `json:"rx_lag_p99_us"`
	WheelLagP99Us int64    `json:"wheel_lag_p99_us"`
	SchedP99Us    int64    `json:"sched_p99_us"`
	GCPauseP99Us  int64    `json:"gc_pause_p99_us"`
	GCCycles      uint64   `json:"gc_cycles"`
	GenCPU        *float64 `json:"gen_cpu"`
}

// NewHealthReport converts h; a nil RxLagP99 and a negative GenCPU become null.
func NewHealthReport(h Health) HealthReport {
	r := HealthReport{
		SocketDrops:   h.SocketDrops,
		SendErrors:    h.SendErrors,
		WheelLagP99Us: h.WheelLagP99.Microseconds(),
		SchedP99Us:    h.SchedP99.Microseconds(),
		GCPauseP99Us:  h.GCPauseP99.Microseconds(),
		GCCycles:      h.GCCycles,
	}
	if h.RxLagP99 != nil {
		us := h.RxLagP99.Microseconds()
		r.RxLagP99Us = &us
	}
	if h.GenCPU >= 0 {
		c := h.GenCPU
		r.GenCPU = &c
	}
	return r
}

// BoundsReport is Bounds in wire form, recorded in every report.
type BoundsReport struct {
	MaxSocketDrops   uint64  `json:"max_socket_drops"`
	RxLagP99Us       int64   `json:"rx_lag_p99_us"`
	WheelLagP99Us    int64   `json:"wheel_lag_p99_us"`
	SchedP99Us       int64   `json:"sched_p99_us"`
	GCPauseP99Us     int64   `json:"gc_pause_p99_us"`
	GCPauseWarnP99Us int64   `json:"gc_pause_warn_p99_us"`
	MaxGenCPU        float64 `json:"max_gen_cpu"`
}

// NewBoundsReport converts b.
func NewBoundsReport(b Bounds) BoundsReport {
	return BoundsReport{
		MaxSocketDrops:   b.MaxSocketDrops,
		RxLagP99Us:       b.RxLagP99.Microseconds(),
		WheelLagP99Us:    b.WheelLagP99.Microseconds(),
		SchedP99Us:       b.SchedP99.Microseconds(),
		GCPauseP99Us:     b.GCPauseP99.Microseconds(),
		GCPauseWarnP99Us: b.GCPauseWarnP99.Microseconds(),
		MaxGenCPU:        b.MaxGenCPU,
	}
}

// StepReport is one hold's NDJSON result line.
type StepReport struct {
	Type      string  `json:"type"` // "step"
	Step      string  `json:"step"`
	Label     string  `json:"label"`
	Payload   string  `json:"payload"`
	Arm       string  `json:"arm"`
	Scenario  string  `json:"scenario"`
	N         int     `json:"n"`
	Talkers   int     `json:"talkers"`
	Listeners int     `json:"listeners"`
	HoldS     float64 `json:"hold_s"`

	// NRequested is the last set_n's requested N; JoinFailuresTotal counts
	// failed join attempts since the process started (interface I-3).
	NRequested        int    `json:"n_requested"`
	JoinFailuresTotal uint64 `json:"join_failures_total"`

	Expected  uint64  `json:"expected"`
	Unique    uint64  `json:"unique"`
	Lost      uint64  `json:"lost"`
	LossRatio float64 `json:"loss_ratio"`
	Dups      uint64  `json:"dups"`
	Reordered uint64  `json:"reordered"`
	BurstMax  int     `json:"burst_max"`

	Resyncs           PhaseCounts       `json:"resyncs"`
	ResourceExhausted PhaseCounts       `json:"resource_exhausted"`
	Updates           map[string]uint64 `json:"updates"`

	JoinP99Us     int64  `json:"join_p99_us"`
	JoinLabel     string `json:"join_label"`
	HelloTriesMax int    `json:"hello_tries_max"`

	Health     HealthReport `json:"health"`
	Violations []string     `json:"violations"`
	Valid      bool         `json:"valid"`
	Bounds     BoundsReport `json:"bounds"`

	ControlLost ControlLostReport `json:"control_lost"`
	Ping        PingReport        `json:"ping"`
	// Warnings is never null.
	Warnings []string `json:"warnings"`
}

// ControlLostReport is a step's view of dead bots (I-11).
type ControlLostReport struct {
	DeadTotal         int            `json:"dead_total"`
	DeadInHold        int            `json:"dead_in_hold"`
	DeadSinceLastStep int            `json:"dead_since_last_step"`
	Alive             int            `json:"alive"`
	First             []DeathSummary `json:"first"`
}

// DeathSummary is an I-10 object without type and dead_total.
type DeathSummary = DeathRecord

// PingReport is the control-plane Ping activity of one hold (I-11).
type PingReport struct {
	EveryS   float64 `json:"every_s"`
	Sent     uint64  `json:"sent"`
	Failed   uint64  `json:"failed"`
	RTTP99Us int64   `json:"rtt_p99_us"`
}

// ParseBounds overlays a JSON object on DefaultBounds. Keys: max_socket_drops,
// rx_lag_p99, wheel_lag_p99, sched_p99, gc_pause_p99, gc_pause_warn_p99 (Go
// duration strings; the warn bound must not exceed the hard one) and
// max_gen_cpu. Empty input is the defaults; unknown keys are an error.
func ParseBounds(s string) (Bounds, error) {
	b := DefaultBounds
	if s == "" {
		return b, nil
	}
	var raw struct {
		MaxSocketDrops *uint64  `json:"max_socket_drops"`
		RxLagP99       *string  `json:"rx_lag_p99"`
		WheelLagP99    *string  `json:"wheel_lag_p99"`
		SchedP99       *string  `json:"sched_p99"`
		GCPauseP99     *string  `json:"gc_pause_p99"`
		GCPauseWarnP99 *string  `json:"gc_pause_warn_p99"`
		MaxGenCPU      *float64 `json:"max_gen_cpu"`
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Bounds{}, fmt.Errorf("bounds: %w", err)
	}
	if raw.MaxSocketDrops != nil {
		b.MaxSocketDrops = *raw.MaxSocketDrops
	}
	if raw.MaxGenCPU != nil {
		b.MaxGenCPU = *raw.MaxGenCPU
	}
	for _, f := range []struct {
		name string
		in   *string
		out  *time.Duration
	}{
		{"rx_lag_p99", raw.RxLagP99, &b.RxLagP99},
		{"wheel_lag_p99", raw.WheelLagP99, &b.WheelLagP99},
		{"sched_p99", raw.SchedP99, &b.SchedP99},
		{"gc_pause_p99", raw.GCPauseP99, &b.GCPauseP99},
		{"gc_pause_warn_p99", raw.GCPauseWarnP99, &b.GCPauseWarnP99},
	} {
		if f.in == nil {
			continue
		}
		d, err := time.ParseDuration(*f.in)
		if err != nil {
			return Bounds{}, fmt.Errorf("bounds: %s: %w", f.name, err)
		}
		*f.out = d
	}
	if b.GCPauseWarnP99 > b.GCPauseP99 {
		return Bounds{}, fmt.Errorf("bounds: gc_pause_warn_p99 %v exceeds gc_pause_p99 %v", b.GCPauseWarnP99, b.GCPauseP99)
	}
	return b, nil
}
