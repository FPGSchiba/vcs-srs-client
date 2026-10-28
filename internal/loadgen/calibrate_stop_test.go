package loadgen

import (
	"context"
	"errors"
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

// A bot that cannot complete its handshake ends calibration at the last good
// step: the completed steps and their capacity come back with a typed error.
func TestCalibrateStopsAtLastGoodStep(t *testing.T) {
	proxy, allow, r := calibrateBehindProxy(t, func(i int) bool { return i >= 6 })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	steps, capy, err := Calibrate(ctx, proxy, allow, 1, []int{4, 8}, time.Second,
		WithReflectorErrors(r.SendErrors))
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
		t.Errorf("capacity PPS = %v, want > 0 over the completed step", capy.PPS)
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
	var mu chan struct{} = make(chan struct{}, 1)
	_, _, _ = Calibrate(ctx, proxy, allow, 1, []int{4}, time.Second,
		WithProgress(50*time.Millisecond, func(p ProgressLine) {
			mu <- struct{}{}
			lines = append(lines, p)
			<-mu
		}))
	mu <- struct{}{}
	defer func() { <-mu }()
	if len(lines) == 0 {
		t.Fatal("no progress lines")
	}
	for _, p := range lines {
		if p.Type != "progress" || p.Phase != "calibrate_ramp" || p.Target != 4 {
			t.Errorf("line = %+v", p)
		}
	}
}
