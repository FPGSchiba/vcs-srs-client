package loadgen

import (
	"sync"
	"testing"
	"time"
)

func TestHistBoundaries(t *testing.T) {
	in := func(t *testing.T, got, lo, hi time.Duration) {
		t.Helper()
		if got < lo || got > hi {
			t.Fatalf("got %v, want in [%v, %v]", got, lo, hi)
		}
	}
	var h Hist
	h.Record(time.Microsecond)
	in(t, h.Quantile(1), time.Microsecond, 1100*time.Nanosecond)

	h = Hist{}
	h.Record(1234 * time.Microsecond)
	in(t, h.Quantile(1), 1200*time.Microsecond, 1300*time.Microsecond)

	h = Hist{}
	h.Record(500 * time.Nanosecond)
	if got := h.Quantile(1); got != time.Microsecond {
		t.Fatalf("underflow: got %v", got)
	}

	h = Hist{}
	h.Record(11 * time.Second)
	if got := h.Quantile(1); got != 10*time.Second {
		t.Fatalf("overflow: got %v", got)
	}

	h = Hist{}
	for i := 1; i <= 100; i++ {
		h.Record(time.Duration(i) * time.Millisecond)
	}
	in(t, h.Quantile(0.99), 99*time.Millisecond, 100*time.Millisecond)
	in(t, h.Quantile(0.5), 50*time.Millisecond, 51*time.Millisecond)

	var a, b Hist
	a.Record(time.Millisecond)
	a.Record(2 * time.Millisecond)
	b.Record(3 * time.Millisecond)
	a.Merge(&b)
	if a.Count() != 3 {
		t.Fatalf("merge count %d", a.Count())
	}
	s := a.Snapshot()
	a.Reset()
	if a.Count() != 0 || s.Count() != 3 {
		t.Fatalf("reset/snapshot: %d %d", a.Count(), s.Count())
	}
	if (&Hist{}).Quantile(0.5) != 0 {
		t.Fatal("empty quantile should be 0")
	}
}

func TestHistConcurrentRecord(t *testing.T) {
	var h Hist
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10000; i++ {
				h.Record(time.Duration(i+1) * time.Microsecond)
			}
		}()
	}
	wg.Wait()
	if h.Count() != 80000 {
		t.Fatalf("count %d", h.Count())
	}
}
