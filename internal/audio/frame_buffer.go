package audio

// miniaudio documents PeriodSizeInFrames as a HINT, not a guarantee: "you
// cannot assume you will get exactly what you ask for." backend.go's
// Backend contract promises the pipeline exactly FrameSamples per call --
// RNNoise in particular accepts only 480-sample frames -- so this file
// absorbs whatever length the hardware actually delivers per callback and
// re-slices it into the contracted frame size. This makes the period-size
// hint irrelevant to callers instead of merely diagnosable.
//
// Both types are pure Go with no cgo/malgo dependency, so they are unit
// tested directly with no device involved, and both back their steady
// -state work with buffers allocated once at construction: push/pull never
// allocate, which is required of anything called from the callbacks that
// use them (OS realtime audio threads).

// frameAccumulator reassembles a stream of arbitrarily-sized capture
// chunks into fixed-size frames, invoking onFrame exactly once per complete
// frame, in order, carrying any incomplete remainder over to the next push.
// Nothing pushed in is ever discarded.
type frameAccumulator struct {
	frameSize int
	frame     []float32 // reused scratch of length frameSize, handed to onFrame
	pending   []float32 // leftover samples between calls; len < frameSize always
	onFrame   func([]float32)
}

func newFrameAccumulator(frameSize int, onFrame func([]float32)) *frameAccumulator {
	return &frameAccumulator{
		frameSize: frameSize,
		frame:     make([]float32, frameSize),
		pending:   make([]float32, 0, frameSize-1),
		onFrame:   onFrame,
	}
}

// push feeds in newly-arrived samples. It emits zero or more complete
// frames (calling onFrame once per frame, synchronously, before returning)
// and stashes any incomplete tail in a. pending for the next call.
//
// Allocation-free in steady state: a.pending's capacity (frameSize-1) can
// never be exceeded, because every path that would grow it past that bound
// instead drains a full frame first.
func (a *frameAccumulator) push(in []float32) {
	i := 0
	for {
		need := a.frameSize - len(a.pending)
		avail := len(in) - i
		if avail < need {
			a.pending = append(a.pending, in[i:]...)
			return
		}
		copy(a.frame, a.pending)
		copy(a.frame[len(a.pending):], in[i:i+need])
		a.onFrame(a.frame)
		i += need
		a.pending = a.pending[:0]
	}
}

// frameFiller serves playback data of whatever length the hardware
// requests per callback, sourcing it from fixed-size frames pulled from
// fill exactly when the internal buffer runs dry. The entire buffer passed
// to pull is always written in full.
type frameFiller struct {
	frameSize int
	frame     []float32 // reused scratch populated by fill
	pos       int       // next unread index in frame; == frameSize means empty
	fill      func([]float32)
}

func newFrameFiller(frameSize int, fill func([]float32)) *frameFiller {
	return &frameFiller{
		frameSize: frameSize,
		frame:     make([]float32, frameSize),
		pos:       frameSize, // start empty so the first pull triggers a fill
		fill:      fill,
	}
}

// pull writes exactly len(out) samples into out, calling fill for a fresh
// frameSize-sample frame whenever the current one is exhausted.
//
// Allocation-free in steady state: it only slices/copies frameFiller's own
// pre-allocated frame buffer.
func (f *frameFiller) pull(out []float32) {
	i := 0
	for i < len(out) {
		if f.pos == f.frameSize {
			f.fill(f.frame)
			f.pos = 0
		}
		n := copy(out[i:], f.frame[f.pos:])
		f.pos += n
		i += n
	}
}
