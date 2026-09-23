package audio

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestVoicePresetsIncludeTheDesignsNames(t *testing.T) {
	got := VoicePresets()
	want := map[string]bool{"": true, "comms_filter_low": true, "comms_filter_mid": true, "comms_filter_high": true}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("unexpected voice preset %q", id)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing voice presets: %v", want)
	}
}

func TestEmptyPresetIsPassthrough(t *testing.T) {
	e := NewEffect("", "")
	in := sine(0.5)
	got := append([]float32(nil), in...)
	e.Process(got)
	for i := range got {
		if got[i] != in[i] {
			t.Fatalf("sample %d changed with no preset selected", i)
		}
	}
}

func TestBandpassAttenuatesOutOfBandEnergy(t *testing.T) {
	e := NewEffect("comms_filter_mid", "")
	// 60 Hz is well below a comms bandpass and must be strongly attenuated.
	low := make([]float32, FrameSamples)
	for i := range low {
		low[i] = 0.5 * float32(math.Sin(2*math.Pi*60*float64(i)/SampleRate))
	}
	before := rms(low)
	// Run several frames so the filter state settles.
	for i := 0; i < 20; i++ {
		e.Process(low)
	}
	if after := rms(low); after >= before*0.5 {
		t.Fatalf("60 Hz RMS %v not attenuated below half of %v", after, before)
	}
}

func TestUnknownPresetIDFallsBackToPassthrough(t *testing.T) {
	e := NewEffect("no_such_preset", "no_such_clip")
	v, c := e.IDs()
	if v != "" || c != "" {
		t.Fatalf("IDs() = (%q, %q), want empty after an unknown id", v, c)
	}
}

// TestCommsFilterMidMatchesGolden pins the preset's response so a tuning
// change shows up as a reviewable diff rather than a silent alteration to
// how every transmission sounds.
func TestCommsFilterMidMatchesGolden(t *testing.T) {
	e := NewEffect("comms_filter_mid", "")
	frame := sine(0.5)
	e.Process(frame)

	const path = "testdata/golden_comms_filter_mid.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		b, _ := json.Marshal(frame)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("golden updated")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (regenerate with UPDATE_GOLDEN=1)", err)
	}
	var want []float32
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(frame) {
		t.Fatalf("golden has %d samples, frame has %d", len(want), len(frame))
	}
	// Tolerance is 1e-4, not the naive 1e-6, because of float32 FMA
	// contraction: the Go spec permits the compiler to fuse a*b+c into a
	// single rounding step. On arm64 (this golden's origin, and the
	// macos-latest CI leg) biquad.step compiles to FMADDS/FMSUBS -- 5
	// roundings per sample. On amd64 (ubuntu-latest and windows-latest)
	// it compiles to separate MULSS/ADDSS/SUBSS -- 9 roundings. That
	// extra rounding is correct-by-spec, not a bug, and across the
	// 480-sample recursion the two code paths diverge by up to ~4e-6 --
	// over 4x a 1e-6 tolerance, which would make two of three CI legs
	// flake on every run. 1e-4 (~0.02% of the ±0.54 signal range) stays
	// comfortably above that cross-architecture noise while still
	// failing on any real coefficient, cascade-order, or gain change,
	// which move samples by orders of magnitude more. Do NOT tighten
	// this back to 1e-6 -- it will reintroduce the amd64 CI flake.
	const tolerance = 1e-4
	for i := range frame {
		if math.Abs(float64(frame[i]-want[i])) > tolerance {
			t.Fatalf("sample %d = %v, golden %v", i, frame[i], want[i])
		}
	}
}

// TestFilterStatePersistsAcrossProcessCalls guards against a regression
// that resets e.hp/e.lp at the top of every Process call. Every other
// bandpass test in this file re-processes the SAME buffer in a loop, which
// masks a per-call reset entirely (each call still "sees" the same input
// history because the buffer never advances). That hides a real bug: in
// production, Process is called once per 10 ms frame on a continuously
// advancing signal, and a state reset there would inject a discontinuity
// at every single frame boundary -- an audible buzz at 100 Hz.
//
// The test builds one continuous multi-frame tone (phase advances sample
// by sample across the whole span, not restarting each frame) and compares
// two ways of running it through comms_filter_mid:
//   - segmented: N sequential Process(frame) calls on one Effect, frame by
//     frame, exactly as real playback does it.
//   - contiguous: the identical samples run through a second Effect in one
//     single Process call.
//
// If biquad state carries over correctly, both must match closely (same
// filter, same signal, same math -- FMA-order differences aside, see the
// golden test's tolerance comment above). If state resets every call, the
// segmented version develops a discontinuity at each 480-sample boundary
// and diverges hard from the contiguous run.
func TestFilterStatePersistsAcrossProcessCalls(t *testing.T) {
	const frames = 5
	const total = frames * FrameSamples
	const toneHz = 1000.0

	continuous := make([]float32, total)
	for i := range continuous {
		continuous[i] = 0.5 * float32(math.Sin(2*math.Pi*toneHz*float64(i)/SampleRate))
	}

	segmented := append([]float32(nil), continuous...)
	segEffect := NewEffect("comms_filter_mid", "")
	for f := 0; f < frames; f++ {
		segEffect.Process(segmented[f*FrameSamples : (f+1)*FrameSamples])
	}

	oneShot := append([]float32(nil), continuous...)
	oneShotEffect := NewEffect("comms_filter_mid", "")
	oneShotEffect.Process(oneShot)

	const tolerance = 1e-4
	for i := range oneShot {
		if diff := math.Abs(float64(segmented[i] - oneShot[i])); diff > tolerance {
			t.Fatalf("sample %d: segmented = %v, contiguous = %v, diff %v exceeds %v -- filter state did not survive the Process call boundary", i, segmented[i], oneShot[i], diff, tolerance)
		}
	}
}
