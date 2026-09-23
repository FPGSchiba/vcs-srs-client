package audio

import (
	"math"
	"testing"
)

// sine fills a frame with a full-cycle-aligned tone at the given amplitude.
func sine(amp float32) []float32 {
	f := make([]float32, FrameSamples)
	for i := range f {
		f[i] = amp * float32(math.Sin(2*math.Pi*float64(i)/float64(FrameSamples)*10))
	}
	return f
}

func TestAGCRaisesQuietSignalTowardTarget(t *testing.T) {
	a := NewAGC()
	var got float32
	// Feed 2 seconds of a quiet tone; AGC is a smoothed follower, not a
	// per-frame normaliser, so it needs time to converge.
	for i := 0; i < 200; i++ {
		f := sine(0.02)
		a.Process(f)
		got = rms(f)
	}
	if got < agcTargetRMS*0.5 {
		t.Fatalf("quiet signal settled at RMS %v, want at least %v", got, agcTargetRMS*0.5)
	}
}

func TestAGCLowersLoudSignalTowardTarget(t *testing.T) {
	a := NewAGC()
	var got float32
	for i := 0; i < 200; i++ {
		f := sine(0.9)
		a.Process(f)
		got = rms(f)
	}
	if got > agcTargetRMS*2 {
		t.Fatalf("loud signal settled at RMS %v, want at most %v", got, agcTargetRMS*2)
	}
}

func TestAGCNeverExceedsFullScale(t *testing.T) {
	a := NewAGC()
	// Drive the gain up on near-silence, then hit it with a full-scale
	// transient -- the classic way a naive AGC produces a clipped bang.
	for i := 0; i < 500; i++ {
		a.Process(sine(0.001))
	}
	f := sine(1.0)
	a.Process(f)
	for i, v := range f {
		if v > 1.0 || v < -1.0 {
			t.Fatalf("sample %d = %v, outside [-1, 1]", i, v)
		}
	}
}

func TestAGCDoesNotAmplifyDigitalSilence(t *testing.T) {
	a := NewAGC()
	for i := 0; i < 500; i++ {
		f := make([]float32, FrameSamples)
		a.Process(f)
		for _, v := range f {
			if v != 0 {
				t.Fatal("AGC produced non-zero output from digital silence")
			}
		}
	}
}
