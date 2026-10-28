package loadgen

import (
	"math"
	"sync/atomic"
	"time"
)

const (
	histDecades   = 7  // 1 µs .. 10 s
	histPerDecade = 90 // mantissa 10..99
	histBuckets   = histDecades*histPerDecade + 2
	histOverflow  = histBuckets - 1
)

// Hist is a lock-free log-linear latency histogram (about 2 significant
// digits). Index 0 is underflow (< 1 µs), the last index is overflow
// (>= 10 s). The zero value is ready to use. Buckets are plain uint64
// accessed atomically so that Snapshot can return a value copy.
type Hist struct {
	b [histBuckets]uint64
}

func histIndex(d time.Duration) int {
	ns := int64(d)
	if ns < 1000 {
		return 0
	}
	if ns >= 10_000_000_000 {
		return histOverflow
	}
	base := int64(1000)
	k := 0
	for ns >= base*10 {
		base *= 10
		k++
	}
	m := ns / (base / 10) // 10..99
	return 1 + k*histPerDecade + int(m-10)
}

func histUpper(i int) time.Duration {
	if i <= 0 {
		return time.Microsecond
	}
	if i >= histOverflow {
		return 10 * time.Second
	}
	k := (i - 1) / histPerDecade
	m := int64((i-1)%histPerDecade) + 10
	step := int64(100) // base/10 for k=0
	for j := 0; j < k; j++ {
		step *= 10
	}
	return time.Duration((m + 1) * step)
}

// Record adds one observation. Lock-free.
func (h *Hist) Record(d time.Duration) {
	atomic.AddUint64(&h.b[histIndex(d)], 1)
}

// Count returns the total number of observations.
func (h *Hist) Count() uint64 {
	var n uint64
	for i := range h.b {
		n += atomic.LoadUint64(&h.b[i])
	}
	return n
}

// Quantile returns the upper bound of the bucket holding rank ceil(q*count).
// An empty histogram returns 0.
func (h *Hist) Quantile(q float64) time.Duration {
	total := h.Count()
	if total == 0 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(total)))
	if rank < 1 {
		rank = 1
	}
	if rank > total {
		rank = total
	}
	var cum uint64
	for i := range h.b {
		cum += atomic.LoadUint64(&h.b[i])
		if cum >= rank {
			return histUpper(i)
		}
	}
	return 10 * time.Second
}

// Merge adds o's counts into h.
func (h *Hist) Merge(o *Hist) {
	for i := range o.b {
		if v := atomic.LoadUint64(&o.b[i]); v != 0 {
			atomic.AddUint64(&h.b[i], v)
		}
	}
}

// Reset zeroes every bucket.
func (h *Hist) Reset() {
	for i := range h.b {
		atomic.StoreUint64(&h.b[i], 0)
	}
}

// Snapshot returns a point-in-time copy.
func (h *Hist) Snapshot() Hist {
	var s Hist
	for i := range h.b {
		s.b[i] = atomic.LoadUint64(&h.b[i])
	}
	return s
}
