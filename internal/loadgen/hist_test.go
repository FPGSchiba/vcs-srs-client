package loadgen

import (
	"math"
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

func TestHistEdgeIndices(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		idx  int
	}{
		{"negative", -5 * time.Second, 0},
		{"zero", 0, 0},
		{"999ns", 999 * time.Nanosecond, 0},
		{"1000ns", 1000 * time.Nanosecond, 1},
		{"1099ns", 1099 * time.Nanosecond, 1},
		{"1100ns", 1100 * time.Nanosecond, 2},
		{"9999ns", 9999 * time.Nanosecond, 90},
		{"10us", 10 * time.Microsecond, 91},
		{"9.999s", 9999 * time.Millisecond, histOverflow - 1},
		{"10s", 10 * time.Second, histOverflow},
		{"1h", time.Hour, histOverflow},
	}
	for _, c := range cases {
		if got := histIndex(c.d); got != c.idx {
			t.Errorf("%s: histIndex=%d want %d", c.name, got, c.idx)
		}
	}
}

func TestHistQuantileEdgeArgs(t *testing.T) {
	var h Hist
	h.Record(5 * time.Millisecond)
	h.Record(7 * time.Millisecond)
	lo := h.Quantile(0)
	if lo < 5*time.Millisecond || lo > 6*time.Millisecond {
		t.Fatalf("q=0: %v", lo)
	}
	if got := h.Quantile(math.NaN()); got != lo {
		t.Fatalf("NaN q: got %v want %v", got, lo)
	}
	if got := h.Quantile(-1); got != lo {
		t.Fatalf("negative q: got %v want %v", got, lo)
	}
	if got := h.Quantile(2); got != h.Quantile(1) {
		t.Fatalf("q>1: got %v", got)
	}
}

func TestHistOverflowCount(t *testing.T) {
	var h Hist
	h.Record(time.Millisecond)
	h.Record(10 * time.Second)
	h.Record(time.Minute)
	if got := h.Overflow(); got != 2 {
		t.Fatalf("overflow %d", got)
	}
	h.Reset()
	if h.Overflow() != 0 {
		t.Fatal("overflow after reset")
	}
}

func TestHistRecordVsReaders(t *testing.T) {
	var h Hist
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20000; i++ {
				h.Record(time.Duration(i+1) * time.Microsecond)
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_ = h.Quantile(0.99)
				s := h.Snapshot()
				_ = s.Count()
				_ = h.Overflow()
			}
		}
	}()
	wg.Wait()
	close(stop)
	<-done
	if h.Count() != 80000 {
		t.Fatalf("count %d", h.Count())
	}
}
