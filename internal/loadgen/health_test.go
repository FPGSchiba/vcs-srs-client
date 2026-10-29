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
		{"gc_pause_p99", func(h *Health) { h.GCPauseP99 = 5*ms + time.Nanosecond }, []string{"gc_pause_p99"}},
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

func TestBoundsGCWarnVsHard(t *testing.T) {
	b := DefaultBounds
	us := time.Microsecond
	for _, c := range []struct {
		name string
		gc   time.Duration
		viol []string
		warn []string
	}{
		{"2.6ms warns", 2600 * us, []string{}, []string{"gc_pause_p99"}},
		{"5.1ms violates", 5100 * us, []string{"gc_pause_p99"}, []string{}},
		{"1.9ms neither", 1900 * us, []string{}, []string{}},
		{"5ms exactly warns", 5000 * us, []string{}, []string{"gc_pause_p99"}},
	} {
		h := Health{GCPauseP99: c.gc, GenCPU: -1}
		if got := b.Violations(h); !reflect.DeepEqual(got, c.viol) {
			t.Errorf("%s: violations %#v want %#v", c.name, got, c.viol)
		}
		if got := b.Warnings(h); got == nil || !reflect.DeepEqual(got, c.warn) {
			t.Errorf("%s: warnings %#v want %#v", c.name, got, c.warn)
		}
	}
}

func TestDefaultBoundsGC(t *testing.T) {
	d := DefaultBounds
	if d.GCPauseP99 != 5*time.Millisecond || d.GCPauseWarnP99 != 2*time.Millisecond {
		t.Fatalf("gc bounds = %v / %v", d.GCPauseP99, d.GCPauseWarnP99)
	}
	if d.RxLagP99 != 2*time.Millisecond || d.WheelLagP99 != 2*time.Millisecond ||
		d.SchedP99 != 2*time.Millisecond || d.MaxGenCPU != 0.85 || d.MaxSocketDrops != 0 {
		t.Fatalf("other defaults changed: %+v", d)
	}
}

func TestParseBoundsGCWarn(t *testing.T) {
	b, err := ParseBounds(`{"gc_pause_warn_p99":"3ms"}`)
	if err != nil || b.GCPauseWarnP99 != 3*time.Millisecond || b.GCPauseP99 != 5*time.Millisecond {
		t.Fatalf("got %+v, %v", b, err)
	}
	if _, err := ParseBounds(`{"gc_pause_warn_p99":"6ms"}`); err == nil {
		t.Fatal("warn above hard must be an error")
	}
	if _, err := ParseBounds(`{"gc_pause_p99":"1ms"}`); err == nil {
		t.Fatal("hard below the default warn must be an error")
	}
}

func TestStepReportGCCycles(t *testing.T) {
	r := NewHealthReport(Health{GCCycles: 7, GenCPU: -1})
	if r.GCCycles != 7 {
		t.Fatalf("GCCycles = %d", r.GCCycles)
	}
	if br := NewBoundsReport(DefaultBounds); br.GCPauseWarnP99Us != 2000 || br.GCPauseP99Us != 5000 {
		t.Fatalf("bounds report = %+v", br)
	}
}
