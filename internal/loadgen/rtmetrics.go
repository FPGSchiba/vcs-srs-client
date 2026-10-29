package loadgen

import (
	"math"
	"runtime/metrics"
	"time"
)

const (
	metricSched = "/sched/latencies:seconds"
	metricGC    = "/sched/pauses/total/gc:seconds"
)

// RuntimeSample is a point-in-time copy of the Go runtime's scheduler-latency
// and GC-pause histograms. Both are cumulative since process start, so a
// window is the difference of two samples (see DeltaQuantile).
type RuntimeSample struct {
	Sched, GC metrics.Float64Histogram
}

// SampleRuntime reads both runtime histograms. The returned slices are
// copies owned by the caller.
func SampleRuntime() RuntimeSample {
	s := []metrics.Sample{{Name: metricSched}, {Name: metricGC}}
	metrics.Read(s)
	return RuntimeSample{Sched: cloneHist(s[0]), GC: cloneHist(s[1])}
}

func cloneHist(s metrics.Sample) metrics.Float64Histogram {
	if s.Value.Kind() != metrics.KindFloat64Histogram {
		return metrics.Float64Histogram{}
	}
	h := s.Value.Float64Histogram()
	return metrics.Float64Histogram{
		Counts:  append([]uint64(nil), h.Counts...),
		Buckets: append([]float64(nil), h.Buckets...),
	}
}

// DeltaCount returns the number of observations added between before and
// after (the sum of the per-bucket count differences). Buckets whose count
// went backwards contribute 0. For the GC-pause histogram it is the number
// of GC pauses in the window, reported as gc_cycles.
func DeltaCount(before, after metrics.Float64Histogram) uint64 {
	var total uint64
	for i, a := range after.Counts {
		var b uint64
		if i < len(before.Counts) {
			b = before.Counts[i]
		}
		if a > b {
			total += a - b
		}
	}
	return total
}

// DeltaQuantile returns the upper bound of the bucket holding rank q of the
// count difference after-before, or 0 when that difference is empty. When
// the bucket's upper bound is +Inf its finite lower bound is returned, which
// underestimates the true latency (a lower bound). q is clamped to (0,1];
// NaN is treated as 1.
func DeltaQuantile(before, after metrics.Float64Histogram, q float64) time.Duration {
	n := len(after.Counts)
	if n == 0 || len(after.Buckets) < n+1 {
		return 0
	}
	delta := make([]uint64, n)
	var total uint64
	for i := range delta {
		var b uint64
		if i < len(before.Counts) {
			b = before.Counts[i]
		}
		if after.Counts[i] > b {
			delta[i] = after.Counts[i] - b
		}
		total += delta[i]
	}
	if total == 0 {
		return 0
	}
	// Fail high, never open: NaN or q above 1 selects the top of the
	// distribution; q at or below 0 selects the lowest populated bucket.
	if math.IsNaN(q) || q > 1 {
		q = 1
	}
	if q < 0 {
		q = 0
	}
	rank := uint64(math.Ceil(q * float64(total)))
	if rank < 1 {
		rank = 1
	}
	var cum uint64
	for i, c := range delta {
		cum += c
		if cum >= rank {
			ub := after.Buckets[i+1]
			if math.IsInf(ub, 1) {
				ub = after.Buckets[i]
			}
			if math.IsInf(ub, -1) || ub < 0 {
				return 0
			}
			return time.Duration(ub * float64(time.Second))
		}
	}
	// Unreachable once q is clamped; return the largest finite bound rather
	// than 0, which would read as healthy.
	for i := len(after.Buckets) - 1; i >= 0; i-- {
		if b := after.Buckets[i]; !math.IsInf(b, 0) && b > 0 {
			return time.Duration(b * float64(time.Second))
		}
	}
	return 0
}
