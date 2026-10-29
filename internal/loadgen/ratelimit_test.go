package loadgen

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time { return f.t }
func (f *fakeClock) sleep(_ context.Context, d time.Duration) error {
	f.t = f.t.Add(d)
	return nil
}

func newTestLimiter(max, scale float64) (*RateLimiter, *fakeClock) {
	c := &fakeClock{t: time.Unix(1000, 0)}
	l := &RateLimiter{Max: max, Scale: scale, now: c.now, sleep: c.sleep}
	return l, c
}

func TestRateLimiterRule(t *testing.T) {
	l, _ := newTestLimiter(12, 5000)
	for _, c := range []struct {
		alive int
		want  float64
	}{{100, 12}, {1000, 5}, {2000, 2.5}, {0, 12}} {
		if got := l.Rate(c.alive); got != c.want {
			t.Fatalf("Rate(%d) = %v, want %v", c.alive, got, c.want)
		}
	}
	if got := (&RateLimiter{Max: 12}).Rate(5000); got != 12 {
		t.Fatalf("scale 0 leaves out the term: %v", got)
	}
	if got := (&RateLimiter{Scale: 5000}).Rate(1000); got != 5 {
		t.Fatalf("max 0 leaves out the term: %v", got)
	}
	if got := (&RateLimiter{}).Rate(1000); got != 0 {
		t.Fatalf("both 0 = unbounded (0): %v", got)
	}

	// Spacing follows the rate at the moment of each admission.
	for _, c := range []struct {
		alive int
		gap   time.Duration
	}{{1000, 200 * time.Millisecond}, {100, time.Second / 12}} {
		l, clk := newTestLimiter(12, 5000)
		if err := l.Wait(context.Background(), c.alive); err != nil {
			t.Fatal(err)
		}
		t0 := clk.t
		if err := l.Wait(context.Background(), c.alive); err != nil {
			t.Fatal(err)
		}
		if got := clk.t.Sub(t0); got < c.gap-time.Microsecond || got > c.gap+time.Microsecond {
			t.Fatalf("alive %d: gap %v, want %v", c.alive, got, c.gap)
		}
	}
}

func TestRateLimiterUnboundedReturnsAtOnce(t *testing.T) {
	l, clk := newTestLimiter(0, 0)
	start := clk.t
	for i := 0; i < 100; i++ {
		if err := l.Wait(context.Background(), 500); err != nil {
			t.Fatal(err)
		}
	}
	if !clk.t.Equal(start) {
		t.Fatalf("unbounded limiter slept %v", clk.t.Sub(start))
	}
}

func TestRateLimiterNoBurstCredit(t *testing.T) {
	l, clk := newTestLimiter(10, 0)
	if err := l.Wait(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Hour) // long idle
	t0 := clk.t
	for i := 0; i < 3; i++ {
		if err := l.Wait(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
	}
	// First is free (one token); the next two wait 100 ms each.
	if got := clk.t.Sub(t0); got < 199*time.Millisecond || got > 201*time.Millisecond {
		t.Fatalf("3 joins after idle took %v, want ~200ms (1 token, no burst credit)", got)
	}
}

func TestRateLimiterCancel(t *testing.T) {
	l := &RateLimiter{Max: 1}
	if err := l.Wait(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	done := make(chan error, 1)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	go func() { done <- l.Wait(ctx2, 1) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait ignored ctx")
	}
}
