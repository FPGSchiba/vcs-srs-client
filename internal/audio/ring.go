package audio

import "sync/atomic"

// Ring is a single-producer / single-consumer float32 ring buffer.
//
// It exists because the OS audio callbacks must never block, allocate or
// take a contended lock: a mutex here is a click in the user's headset.
//
// Index ownership is strict and one-directional: Write owns w and only ever
// loads r; Read (and Drain) owns r and only ever loads w. Neither side ever
// stores the other's index. This is load-bearing, not stylistic: an earlier
// version had Write force r forward on overflow to implement drop-oldest,
// which meant two goroutines could unconditionally Store the same atomic
// index (a lost update with no CAS or forward-only guard) and, worse, meant
// the producer could go on to write into buffer slots the consumer was
// concurrently mid-copy on -- an unsynchronized access to the same
// []float32 elements. That corrupts audio silently instead of crashing, and
// the overlap window is narrow and timing-dependent enough that -race does
// not reliably catch it. Strict ownership removes the hazard by
// construction: there is no code path on either side that writes the
// other's index, so there is nothing left to race.
//
// The cost of that guarantee is that overflow now drops the incoming
// (newest) chunk instead of evicting the oldest buffered one, and drops it
// whole rather than partially -- a partial write would tear a frame at the
// boundary, and this package's contract is frame-sized chunks in and out.
// Drop-oldest cannot be reintroduced without the producer touching r, so it
// is not an option here regardless of how much more intuitive "keep the
// newest audio" sounds. What preserves the original intent -- stale
// backlog has no value and latency must recover -- is Drain: the consumer
// (the only side allowed to move r) can jump r to the current w whenever it
// observes Dropped() climbing, clearing the backlog from the side that
// legally owns the index instead of the side that doesn't.
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

// Write copies src into the ring. If there isn't enough free space for all
// of src, it writes nothing, counts the whole of src as dropped, and
// returns that count: a partial write would tear a frame across the
// overflow boundary, so the incoming chunk is dropped whole or not at all.
//
// Write owns w exclusively; it only loads r to compute free space and never
// stores it. See the Ring doc comment for why that split matters.
func (r *Ring) Write(src []float32) (dropped int) {
	w := r.w.Load()
	rd := r.r.Load()
	free := uint64(len(r.buf)) - (w - rd)
	n := uint64(len(src))
	if n > free {
		r.dropped.Add(n)
		return int(n)
	}
	for i, v := range src {
		r.buf[(w+uint64(i))&r.mask] = v
	}
	r.w.Store(w + n)
	return 0
}

// Read fills dst and returns how many samples were copied. A short or empty
// read increments the underrun counter; the caller emits silence.
//
// Read owns r exclusively; it only loads w to compute available samples and
// never stores it. See the Ring doc comment for why that split matters.
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

// Drain discards everything currently buffered by advancing r to the
// current w, resetting queued latency to zero. It is consumer-side only,
// since only the consumer may legally move r: the DSP goroutine should call
// it when it observes Dropped() advancing, so a backlog left behind by
// dropped Writes doesn't linger. See the Ring doc comment for why this,
// rather than producer-side drop-oldest, is how latency recovers.
func (r *Ring) Drain() {
	r.r.Store(r.w.Load())
}

// Available reports how many samples are currently buffered, i.e. how much
// backlog the consumer is behind by.
//
// It is consumer-side, like Read and Drain: it loads both indices and
// stores neither, so it respects the strict index ownership the Ring doc
// describes. Loading w first and r second means the result is a LOWER BOUND
// on what a Read issued immediately after would find -- the producer may
// add more in between, and only the consumer (the caller) moves r. That
// direction of error is the one dspLoop's bounded catch-up needs: it must
// never plan to read more frames than are really there (that would inject
// silence), while reading fewer than are there merely defers the rest to
// the next tick.
func (r *Ring) Available() int {
	w := r.w.Load()
	rd := r.r.Load()
	return int(w - rd)
}

// Dropped is the cumulative count of samples discarded because a Write
// arrived with insufficient free space, reported via audio:state.
func (r *Ring) Dropped() uint64 { return r.dropped.Load() }

// Underruns is the cumulative short-read count, reported via audio:state.
func (r *Ring) Underruns() uint64 { return r.underruns.Load() }
