package loadgen

import (
	"math"
	"runtime/metrics"
	"testing"
	"time"
)

func hist(counts []uint64) metrics.Float64Histogram {
	return metrics.Float64Histogram{Counts: counts, Buckets: []float64{0, 1e-3, 2e-3, 3e-3}}
}

func TestDeltaQuantile(t *testing.T) {
	before := hist([]uint64{5, 0, 0})
	after := hist([]uint64{5, 98, 2})
	if got := DeltaQuantile(before, after, 0.99); got != 3*time.Millisecond {
		t.Fatalf("p99 = %v, want 3ms", got)
	}
	if got := DeltaQuantile(before, after, 0.5); got != 2*time.Millisecond {
		t.Fatalf("p50 = %v, want 2ms", got)
	}
	if got := DeltaQuantile(after, after, 0.99); got != 0 {
		t.Fatalf("identical = %v, want 0", got)
	}
}

func TestDeltaQuantileInfiniteTop(t *testing.T) {
	inf := metrics.Float64Histogram{Counts: []uint64{0, 3}, Buckets: []float64{0, 1e-3, math.Inf(1)}}
	zero := metrics.Float64Histogram{Counts: []uint64{0, 0}, Buckets: inf.Buckets}
	if got := DeltaQuantile(zero, inf, 0.99); got != time.Millisecond {
		t.Fatalf("got %v, want 1ms (finite lower bound)", got)
	}
}

func TestSampleRuntimeHasBuckets(t *testing.T) {
	s := SampleRuntime()
	if len(s.Sched.Buckets) <= 1 || len(s.GC.Buckets) <= 1 {
		t.Fatalf("sched %d buckets, gc %d buckets", len(s.Sched.Buckets), len(s.GC.Buckets))
	}
}
