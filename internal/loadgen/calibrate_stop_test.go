package loadgen

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen/lgtest"
)

// calibrateBehindProxy starts an in-process reflector behind a HELLO-dropping
// proxy and returns the proxy address plus an allow list covering it.
func calibrateBehindProxy(t *testing.T, drop func(int) bool) (string, AllowList, *Reflector) {
	t.Helper()
	r := startReflector(t, 2)
	proxy := lgtest.HelloDropProxy(t, r.Addr(), drop)
	allow, err := ParseAllow(proxy)
	if err != nil {
		t.Fatal(err)
	}
	return proxy, allow, r
}

// calCapacity counts only valid steps: this is why the stop test must take
// host health and delivery out of play, or a jittery runner zeroes capacity.
func TestCalCapacityIgnoresInvalidSteps(t *testing.T) {
	got := calCapacity([]CalStep{{N: 4, EgressPPS: 150, Valid: false}}, nil, 0)
	if got.PPS != 0 {
		t.Errorf("PPS = %v, want 0 from an invalid step", got.PPS)
	}
	got = calCapacity([]CalStep{{N: 4, EgressPPS: 150, Valid: true}}, nil, 0)
	if got.PPS != 150 {
		t.Errorf("PPS = %v, want 150", got.PPS)
	}
}

// relaxedBounds takes host timing jitter out of a test that is about the stop
// path, not about generator health: a shared macOS runner can exceed the 2 ms
// p99 bounds, which scores the step invalid and zeroes the capacity.
var relaxedBounds = Bounds{
	MaxSocketDrops: ^uint64(0),
	RxLagP99:       time.Hour, WheelLagP99: time.Hour, SchedP99: time.Hour, GCPauseP99: time.Hour,
	MaxGenCPU: 1e9,
}

// A step that delivers under the minimum fraction of its expected frames is
// invalid and ends calibration at the last good step (stage "delivery").
func TestCalibrateStopsOnUnderDelivery(t *testing.T) {
	r := startReflector(t, 2)
	allow, err := ParseAllow(r.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// A fraction above 1 can never be met, so the first step must be rejected.
	steps, capy, err := Calibrate(ctx, r.Addr(), allow, 1, []int{4, 8}, time.Second,
		WithBounds(relaxedBounds), WithMinDelivery(1.5))
	var stop *CalStoppedError
	if !errors.As(err, &stop) {
		t.Fatalf("err = %v, want *CalStoppedError", err)
	}
	if stop.Stage != "delivery" || stop.N != 4 || stop.Bots != 4 {
		t.Errorf("stop = %+v, want Stage delivery N 4 Bots 4", stop)
	}
	if len(steps) != 1 || steps[0].Valid || len(steps[0].Violations) == 0 {
		t.Fatalf("steps = %+v, want the N=4 step recorded as invalid", steps)
	}
	if capy.PPS != 0 {
		t.Errorf("capacity PPS = %v, want 0: an undelivering step adds none", capy.PPS)
	}
	if got := stop.Reason(); !strings.Contains(got, "delivered") || !strings.Contains(got, "150%") {
		t.Errorf("Reason() = %q, want delivered %% against the 150%% minimum", got)
	}
}

// A bot that cannot complete its handshake ends calibration at the last good
// step: the completed steps and their capacity come back with a typed error.
func TestCalibrateStopsAtLastGoodStep(t *testing.T) {
	proxy, allow, r := calibrateBehindProxy(t, func(i int) bool { return i >= 6 })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	steps, capy, err := Calibrate(ctx, proxy, allow, 1, []int{4, 8}, time.Second,
		WithReflectorErrors(r.SendErrors), WithBounds(relaxedBounds), WithMinDelivery(0))
	var stop *CalStoppedError
	if !errors.As(err, &stop) {
		t.Fatalf("err = %v, want *CalStoppedError", err)
	}
	if len(steps) != 1 || steps[0].N != 4 {
		t.Fatalf("steps = %+v, want exactly N=4", steps)
	}
	if stop.N != 8 || stop.Bots != 6 || stop.Stage != "hello" || stop.Bot != 6 || stop.Tries != 5 {
		t.Errorf("stop = %+v, want N 8 Bots 6 Stage hello Bot 6 Tries 5", stop)
	}
	if capy.PPS <= 0 {
		t.Errorf("capacity PPS = %v, want > 0 over the completed step; step = %+v", capy.PPS, steps[0])
	}
}

// WithStepHook sees each completed step, in order, before Calibrate returns,
// and the new step fields are filled in.
func TestCalibrateStreamsEachStep(t *testing.T) {
	r := startReflector(t, 2)
	allow, err := ParseAllow(r.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var hooked []CalStep
	returned := false
	steps, _, err := Calibrate(ctx, r.Addr(), allow, 1, []int{4, 8}, time.Second,
		WithBounds(relaxedBounds), WithMinDelivery(0),
		WithStepHook(func(s CalStep) {
			if returned {
				t.Error("hook called after Calibrate returned")
			}
			hooked = append(hooked, s)
		}))
	returned = true
	if err != nil {
		t.Fatal(err)
	}
	if len(hooked) != 2 || len(steps) != 2 {
		t.Fatalf("hooked %d steps, returned %d, want 2 and 2", len(hooked), len(steps))
	}
	for i, s := range hooked {
		if s.N != steps[i].N || s.N != []int{4, 8}[i] {
			t.Errorf("hook %d: N = %d", i, s.N)
		}
		if s.Warnings == nil {
			t.Errorf("N=%d Warnings is nil, want a non-nil slice", s.N)
		}
		if s.Bots != s.N {
			t.Errorf("N=%d Bots = %d", s.N, s.Bots)
		}
		if s.ExpectedPPS != float64(50*(s.N-1)) {
			t.Errorf("N=%d ExpectedPPS = %v", s.N, s.ExpectedPPS)
		}
		if s.HelloTriesMax < 1 {
			t.Errorf("N=%d HelloTriesMax = %d, want >= 1", s.N, s.HelloTriesMax)
		}
	}
}

// With no completed step there is nothing to report: a typed error and no steps.
func TestCalibrateNoCompletedStepIsError(t *testing.T) {
	proxy, allow, _ := calibrateBehindProxy(t, func(i int) bool { return i >= 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	steps, _, err := Calibrate(ctx, proxy, allow, 1, []int{4}, time.Second)
	var stop *CalStoppedError
	if !errors.As(err, &stop) {
		t.Fatalf("err = %v, want *CalStoppedError", err)
	}
	if len(steps) != 0 {
		t.Errorf("steps = %+v, want none", steps)
	}
	if stop.N != 4 || stop.Bots != 1 || stop.Stage != "hello" {
		t.Errorf("stop = %+v", stop)
	}
}

// WithProgress receives calibrate_ramp lines while bots are being added.
func TestCalibrateProgress(t *testing.T) {
	proxy, allow, _ := calibrateBehindProxy(t, func(i int) bool { return i == 2 })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var lines []ProgressLine
	var mu sync.Mutex
	_, _, _ = Calibrate(ctx, proxy, allow, 1, []int{4}, time.Second,
		WithProgress(50*time.Millisecond, func(p ProgressLine) {
			mu.Lock()
			lines = append(lines, p)
			mu.Unlock()
		}))
	mu.Lock()
	defer mu.Unlock()
	if len(lines) == 0 {
		t.Fatal("no progress lines")
	}
	for _, p := range lines {
		if p.Type != "progress" || p.Phase != "calibrate_ramp" || p.Target != 4 {
			t.Errorf("line = %+v", p)
		}
	}
	prev := 0
	for _, p := range lines {
		if p.Bots < prev {
			t.Errorf("Bots went backwards: %d after %d", p.Bots, prev)
		}
		prev = p.Bots
	}
	if last := lines[len(lines)-1]; last.ElapsedS <= 0 {
		t.Errorf("ElapsedS = %v, want > 0", last.ElapsedS)
	}
}

// The calibration_stopped reason reads like the I-4 example (and v1's stderr
// text): a join-stage stop names the bot, other stages carry their own text.
func TestCalStoppedReasonMatchesInterface(t *testing.T) {
	cases := []struct {
		e    CalStoppedError
		want string
	}{
		{CalStoppedError{N: 4096, Bots: 3702, Stage: "hello", Bot: 3702, Tries: 5,
			Err: errors.New("no HELLO_ACK after 5 tries")}, "bot 3702: no HELLO_ACK after 5 tries"},
		{CalStoppedError{N: 8, Bots: 6, Stage: "voice_dial", Bot: 6,
			Err: errors.New("dial udp: refused")}, "bot 6: dial udp: refused"},
		{CalStoppedError{N: 8, Bots: 8, Stage: "talker_stalled",
			Err: errors.New("talker stalled at N=8")}, "talker stalled at N=8"},
		{CalStoppedError{N: 8, Bots: 8, Stage: "ledger",
			Err: errors.New("bot lost its ledger at N=8")}, "bot lost its ledger at N=8"},
		{CalStoppedError{N: 8, Bots: 8, Stage: "delivery",
			Err: errors.New("delivered 12.0% of expected frames at N=8, under the 99% minimum")},
			"delivered 12.0% of expected frames at N=8, under the 99% minimum"},
	}
	for _, c := range cases {
		if got := c.e.Reason(); got != c.want {
			t.Errorf("%s: Reason() = %q, want %q", c.e.Stage, got, c.want)
		}
	}
}

// The 99% rule on its own: a step that delivered nothing, or 98%, is short;
// 99% and above is not. DefaultMinDelivery is the rule's one constant.
func TestDeliveryShortfall(t *testing.T) {
	if DefaultMinDelivery != 0.99 {
		t.Fatalf("DefaultMinDelivery = %v, want 0.99", DefaultMinDelivery)
	}
	cases := []struct {
		name   string
		unique uint64
		want   float64
		short  bool
	}{
		{"0%", 0, 0, true},
		{"98%", 980, 98, true},
		{"99%", 990, 99, false},
		{"100%", 1000, 100, false},
	}
	for _, c := range cases {
		pct, short := deliveryShortfall(c.unique, 1000, DefaultMinDelivery)
		if short != c.short || pct != c.want {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.name, pct, short, c.want, c.short)
		}
	}
	if _, short := deliveryShortfall(0, 0, DefaultMinDelivery); short {
		t.Error("expected 0 frames must not count as a shortfall")
	}
}

// The delivery rule compares against the frames the listeners were told to
// expect (hold rounded to whole 20 ms frames), not the unrounded hold: a 29 ms
// hold sends one frame per listener, and delivering it is 100%, not 69%.
func TestCalibrateDeliveryCountsWholeFrames(t *testing.T) {
	r := startReflector(t, 2)
	allow, err := ParseAllow(r.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	steps, _, err := Calibrate(ctx, r.Addr(), allow, 1, []int{4}, 29*time.Millisecond,
		WithBounds(relaxedBounds))
	if err != nil {
		t.Fatalf("err = %v, want nil: one whole frame per listener delivered", err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %+v, want one", steps)
	}
}
