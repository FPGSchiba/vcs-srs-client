package loadgen

import (
	"reflect"
	"testing"
	"time"
)

func TestBoundsViolations(t *testing.T) {
	b := DefaultBounds
	ms := time.Millisecond
	over := 2*ms + time.Nanosecond
	rxOK := 2 * ms
	rxOver := over
	clean := func() Health {
		return Health{RxLagP99: &rxOK, WheelLagP99: 2 * ms, SchedP99: 2 * ms, GCPauseP99: 2 * ms, GenCPU: 0.85}
	}
	cases := []struct {
		name string
		mut  func(h *Health)
		want []string
	}{
		{"clean", func(h *Health) {}, []string{}},
		{"socket_drops", func(h *Health) { h.SocketDrops = 1 }, []string{"socket_drops"}},
		{"rx_lag_p99", func(h *Health) { h.RxLagP99 = &rxOver }, []string{"rx_lag_p99"}},
		{"wheel_lag_p99", func(h *Health) { h.WheelLagP99 = over }, []string{"wheel_lag_p99"}},
		{"sched_p99", func(h *Health) { h.SchedP99 = over }, []string{"sched_p99"}},
		{"gc_pause_p99", func(h *Health) { h.GCPauseP99 = over }, []string{"gc_pause_p99"}},
		{"send_errors", func(h *Health) { h.SendErrors = 1 }, []string{"send_errors"}},
		{"gen_cpu", func(h *Health) { h.GenCPU = 0.86 }, []string{"gen_cpu"}},
		{"nil rx lag", func(h *Health) { h.RxLagP99 = nil }, []string{}},
		{"gen cpu measured by kit", func(h *Health) { h.GenCPU = -1 }, []string{}},
	}
	for _, c := range cases {
		h := clean()
		c.mut(&h)
		got := b.Violations(h)
		if got == nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v want %#v", c.name, got, c.want)
		}
	}
}
