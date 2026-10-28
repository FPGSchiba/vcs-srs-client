package loadgen

import "time"

// Bounds are the generator self-health limits (D14). A step whose Health
// breaks any of them is not a valid measurement of the server.
type Bounds struct {
	MaxSocketDrops                              uint64
	RxLagP99, WheelLagP99, SchedP99, GCPauseP99 time.Duration
	MaxGenCPU                                   float64
}

// DefaultBounds: rx lag and wheel lag are the design's 2 ms (§4.5h, §6.3),
// CPU below 85 % (§6.3). The scheduler-latency and GC-pause p99 bounds are
// the plan's values for the design's "in bounds"; they are recorded in every
// report and overridable with --bounds.
var DefaultBounds = Bounds{
	MaxSocketDrops: 0,
	RxLagP99:       2 * time.Millisecond,
	WheelLagP99:    2 * time.Millisecond,
	SchedP99:       2 * time.Millisecond,
	GCPauseP99:     2 * time.Millisecond,
	MaxGenCPU:      0.85,
}

// Health is the generator's own measured state over one hold window.
type Health struct {
	SocketDrops uint64
	// SendErrors counts frames the generator failed to send during the hold;
	// any is a generator fault, not server loss.
	SendErrors  uint64
	RxLagP99    *time.Duration // nil = not available (off Linux)
	WheelLagP99 time.Duration
	SchedP99    time.Duration
	GCPauseP99  time.Duration
	GenCPU      float64 // < 0 = measured by the kit, not by the bot
}

// Violations names every bound h exceeds, in a fixed order. It never returns
// nil. A nil RxLagP99 or a negative GenCPU is not a violation.
func (b Bounds) Violations(h Health) []string {
	v := []string{}
	if h.SocketDrops > b.MaxSocketDrops {
		v = append(v, "socket_drops")
	}
	if h.SendErrors > 0 {
		v = append(v, "send_errors")
	}
	if h.RxLagP99 != nil && *h.RxLagP99 > b.RxLagP99 {
		v = append(v, "rx_lag_p99")
	}
	if h.WheelLagP99 > b.WheelLagP99 {
		v = append(v, "wheel_lag_p99")
	}
	if h.SchedP99 > b.SchedP99 {
		v = append(v, "sched_p99")
	}
	if h.GCPauseP99 > b.GCPauseP99 {
		v = append(v, "gc_pause_p99")
	}
	if h.GenCPU >= 0 && h.GenCPU > b.MaxGenCPU {
		v = append(v, "gen_cpu")
	}
	return v
}
