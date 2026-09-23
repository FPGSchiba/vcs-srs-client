package audio

import "sync/atomic"

// Ring is a single-producer / single-consumer float32 ring buffer.
//
// It exists because the OS audio callbacks must never block, allocate or
// take a contended lock: a mutex here is a click in the user's headset. Only
// the capture callback writes and only the DSP goroutine reads (and vice
// versa for playback), so atomics on the two indices are sufficient.
//
// Capacity is rounded up to a power of two so the wrap is a mask rather than
// a modulo.
type Ring struct {
	buf  []float32
	mask uint64

	w atomic.Uint64
	r atomic.Uint64

	dropped   atomic.Uint64
	underruns atomic.Uint64
}

// NewRing allocates a ring holding capacityFrames frames of FrameSamples.
func NewRing(capacityFrames int) *Ring {
	n := uint64(capacityFrames * FrameSamples)
	size := uint64(1)
	for size < n {
		size <<= 1
	}
	return &Ring{buf: make([]float32, size), mask: size - 1}
}

// Write copies src into the ring. On overflow it advances the read index,
// discarding the OLDEST samples, and returns how many were dropped.
//
// Dropping the oldest rather than refusing the newest is deliberate: stale
// audio has no value, and a producer that silently stops is far harder to
// diagnose than a counter that climbs.
func (r *Ring) Write(src []float32) (dropped int) {
	w := r.w.Load()
	rd := r.r.Load()
	free := uint64(len(r.buf)) - (w - rd)
	if n := uint64(len(src)); n > free {
		over := n - free
		r.r.Store(rd + over)
		r.dropped.Add(over)
		dropped = int(over)
	}
	for i, v := range src {
		r.buf[(w+uint64(i))&r.mask] = v
	}
	r.w.Store(w + uint64(len(src)))
	return dropped
}

// Read fills dst and returns how many samples were copied. A short or empty
// read increments the underrun counter; the caller emits silence.
func (r *Ring) Read(dst []float32) int {
	w := r.w.Load()
	rd := r.r.Load()
	avail := w - rd
	if avail < uint64(len(dst)) {
		r.underruns.Add(1)
		if avail == 0 {
			return 0
		}
	}
	n := uint64(len(dst))
	if avail < n {
		n = avail
	}
	for i := uint64(0); i < n; i++ {
		dst[i] = r.buf[(rd+i)&r.mask]
	}
	r.r.Store(rd + n)
	return int(n)
}

// Dropped is the cumulative overrun sample count, reported via audio:state.
func (r *Ring) Dropped() uint64 { return r.dropped.Load() }

// Underruns is the cumulative short-read count, reported via audio:state.
func (r *Ring) Underruns() uint64 { return r.underruns.Load() }
