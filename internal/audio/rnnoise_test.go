package audio

import "testing"

func TestDenoiserProcessesAFrameWithoutPanicking(t *testing.T) {
	d, err := NewDenoiser()
	if err != nil {
		t.Fatalf("NewDenoiser: %v", err)
	}
	defer d.Close()

	// RNNoise's pitch/LPC analysis carries history across calls, and a
	// brand new DenoiseState starts with that history zeroed. Measured
	// directly against real RNNoise (v0.1.1): a fresh state's very FIRST
	// processed frame is judged against that all-silence history as an
	// abrupt onset and attenuated by ~60x regardless of content (a clean
	// 0.3-amplitude tone came out at RMS ratio 0.0163); from the second
	// frame on the same tone settles to ratio ~1.0. That is a real,
	// one-time cold-start artifact of the algorithm itself, not something
	// this wrapper should paper over or a bug it introduced -- so prime
	// the state with one warm-up frame before measuring, exactly as
	// TestDenoiserSuppressesWhiteNoiseMoreThanTone already does (over many
	// frames) for the same underlying reason.
	d.Process(sine(0.3))

	frame := sine(0.3)
	before := rms(frame)
	d.Process(frame)
	if len(frame) != FrameSamples {
		t.Fatalf("Process changed the frame length to %d", len(frame))
	}
	if !d.Available() {
		t.Skip("cgo unavailable; Process is a documented no-op")
	}
	// A clean tone must survive denoising with most of its energy. This is a
	// sanity check that the model ran, not a quality assertion.
	if after := rms(frame); after < before*0.1 {
		t.Fatalf("denoiser destroyed a clean tone: RMS %v -> %v", before, after)
	}
}

func TestDenoiserSuppressesWhiteNoiseMoreThanTone(t *testing.T) {
	d, err := NewDenoiser()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.Available() {
		t.Skip("cgo unavailable")
	}
	// RNNoise needs a few frames of context before it settles.
	var noiseAfter, toneAfter float32
	for i := 0; i < 50; i++ {
		n := whiteNoise(0.3, int64(i))
		d.Process(n)
		noiseAfter = rms(n)
	}
	d2, _ := NewDenoiser()
	defer d2.Close()
	for i := 0; i < 50; i++ {
		s := sine(0.3)
		d2.Process(s)
		toneAfter = rms(s)
	}
	if noiseAfter >= toneAfter {
		t.Fatalf("noise RMS %v not suppressed below tone RMS %v", noiseAfter, toneAfter)
	}
}
