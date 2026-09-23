package audio

import (
	"math"
	"math/rand"
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

// whiteNoise fills a frame with deterministic pseudo-random noise.
func whiteNoise(amp float32, seed int64) []float32 {
	r := rand.New(rand.NewSource(seed))
	f := make([]float32, FrameSamples)
	for i := range f {
		f[i] = amp * float32(r.Float64()*2-1)
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
	// At the spec'd release rate, 200 frames of this tone converges to
	// RMS ~= 0.0985. A release rate 2x slower than spec only reaches
	// RMS ~= 0.088, so the old >= target*0.5 bound let a ~10x error slip
	// through. Tighten it to sit strictly between the two so a
	// meaningfully-wrong release rate actually fails here.
	const wantMin = agcTargetRMS * 0.93
	if got < wantMin {
		t.Fatalf("quiet signal settled at RMS %v, want at least %v", got, wantMin)
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
	// The all-zero-output check above is tautological by itself: 0 * gain
	// == 0 for any finite gain, so it cannot detect the agcSilenceFloor
	// guard being removed (with the guard gone, gain winds up toward
	// agcMaxGain while the output samples still read zero). Assert the
	// gain itself never moved off its initial value.
	const wantMaxGain = 1.0 + 1e-6
	if g := a.Gain(); g > wantMaxGain {
		t.Fatalf("AGC gain wound up to %v on digital silence, want ~1.0 (unchanged)", g)
	}
}

// TestAGCAttackFasterThanRelease pins the required attack/release
// asymmetry documented on agcAttack/agcRelease in agc.go: gain must come
// down from a too-loud signal much faster than it climbs up from a
// too-quiet one. It measures actual convergence speed in each direction
// rather than comparing the raw constants, so it also catches a
// same-branch-logic regression where the two constants' VALUES are
// swapped.
func TestAGCAttackFasterThanRelease(t *testing.T) {
	const settleFrames = 4000 // enough to fully converge at either rate

	// framesToHalfway reports how many frames it takes a fresh AGC fed a
	// constant tone to cross halfway between its starting gain (1.0) and
	// that tone's steady-state gain. The steady-state gain is measured by
	// running to convergence first, so the halfway point doesn't assume
	// which rate constant is attached to which direction.
	framesToHalfway := func(amp float32, rising bool) int {
		steady := NewAGC()
		for i := 0; i < settleFrames; i++ {
			steady.Process(sine(amp))
		}
		target := (1.0 + steady.Gain()) / 2

		a := NewAGC()
		for n := 1; n <= settleFrames; n++ {
			a.Process(sine(amp))
			g := a.Gain()
			if rising && g >= target {
				return n
			}
			if !rising && g <= target {
				return n
			}
		}
		return settleFrames // never crossed halfway within the budget
	}

	framesDown := framesToHalfway(0.9, false) // loud: gain must fall
	framesUp := framesToHalfway(0.02, true)   // quiet: gain must rise

	if framesDown >= framesUp {
		t.Fatalf("downward convergence (%d frames) not faster than upward (%d frames)", framesDown, framesUp)
	}
	// "Substantially faster": require at least a 3x margin, derived from
	// the measured frame counts rather than a hard-coded frame budget.
	// With the spec constants this margin holds by roughly 11x; if
	// agcAttack and agcRelease were swapped, downward convergence would
	// instead be the SLOW direction and this assertion fails.
	const margin = 3
	if framesDown*margin > framesUp {
		t.Fatalf("downward convergence (%d frames) not substantially faster than upward (%d frames), want at least %dx margin", framesDown, framesUp, margin)
	}
}
