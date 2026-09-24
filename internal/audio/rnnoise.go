//go:build cgo

package audio

import "github.com/FPGSchiba/vcs-srs-client/internal/audio/rnnoise"

// Denoiser wraps a vendored RNNoise state.
//
// RNNoise accepts ONLY 480-sample frames at 48 kHz, which is why the whole
// pipeline uses that granularity (see format.go). It also works in int16
// range expressed as float, not in [-1, 1], so this wrapper scales on the
// way in and back on the way out; internal/audio/rnnoise carries the raw
// cgo binding (see that package's doc comment for why it is split out into
// its own directory).
type Denoiser struct {
	st  *rnnoise.State
	buf []float32 // pre-allocated scratch buffer; Process must not allocate
}

// NewDenoiser allocates and initializes a vendored RNNoise state using the
// library's built-in pre-trained model.
func NewDenoiser() (*Denoiser, error) {
	st, err := rnnoise.New()
	if err != nil {
		return nil, err
	}
	return &Denoiser{st: st, buf: make([]float32, FrameSamples)}, nil
}

// Available reports whether denoising actually runs.
func (d *Denoiser) Available() bool { return d != nil && d.st != nil }

// Process denoises the frame in place. It does not allocate: buf is
// pre-allocated in NewDenoiser and reused on every call, since Process runs
// on the realtime capture path.
func (d *Denoiser) Process(frame []float32) {
	if d == nil || d.st == nil || len(frame) != FrameSamples {
		return
	}
	for i, v := range frame {
		d.buf[i] = v * 32768.0
	}
	d.st.ProcessFrame(d.buf)
	for i := range frame {
		frame[i] = d.buf[i] / 32768.0
	}
}

// Close releases the underlying RNNoise state. Safe to call on a nil
// receiver or a Denoiser whose state was never created.
func (d *Denoiser) Close() {
	if d != nil && d.st != nil {
		d.st.Close()
		d.st = nil
	}
}
