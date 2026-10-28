package loadgen

import (
	"testing"
	"time"
)

func TestParseCommand(t *testing.T) {
	for _, line := range []string{
		`{"set_n":8}`, `{"hold":2}`, `{"report":"s1"}`, `{"quit":true}`,
	} {
		c, err := ParseCommand([]byte(line))
		if err != nil {
			t.Fatalf("ParseCommand(%s): %v", line, err)
		}
		set := 0
		if c.SetN != nil {
			set++
		}
		if c.Hold != nil {
			set++
		}
		if c.Report != nil {
			set++
		}
		if c.Quit {
			set++
		}
		if set != 1 {
			t.Fatalf("%s: %d fields set", line, set)
		}
	}
	for _, line := range []string{
		`{"set_n":1,"hold":2}`, `{"bogus":1}`, ``, `{}`, `{"quit":false}`,
		`{"set_n":1} {"quit":true}`, `not json`, `{"hold":0}`, `{"set_n":0}`,
	} {
		if _, err := ParseCommand([]byte(line)); err == nil {
			t.Fatalf("ParseCommand(%q): want error", line)
		}
	}
}

func TestParseBounds(t *testing.T) {
	b, err := ParseBounds("")
	if err != nil || b != DefaultBounds {
		t.Fatalf("empty: %+v %v", b, err)
	}
	b, err = ParseBounds(`{"wheel_lag_p99":"5ms","max_gen_cpu":0.5}`)
	if err != nil {
		t.Fatal(err)
	}
	if b.WheelLagP99 != 5*time.Millisecond || b.MaxGenCPU != 0.5 || b.SchedP99 != DefaultBounds.SchedP99 {
		t.Fatalf("got %+v", b)
	}
	for _, bad := range []string{`{"nope":1}`, `{"sched_p99":"x"}`, `{`} {
		if _, err := ParseBounds(bad); err == nil {
			t.Fatalf("ParseBounds(%q): want error", bad)
		}
	}
}

func TestHealthReportNullForNA(t *testing.T) {
	r := NewHealthReport(Health{WheelLagP99: 3 * time.Millisecond, GenCPU: -1})
	if r.RxLagP99Us != nil || r.GenCPU != nil {
		t.Fatalf("n/a must be nil: %+v", r)
	}
	if r.WheelLagP99Us != 3000 {
		t.Fatalf("wheel = %d", r.WheelLagP99Us)
	}
}
