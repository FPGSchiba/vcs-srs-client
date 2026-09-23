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
	for i := range frame {
		if math.Abs(float64(frame[i]-want[i])) > 1e-6 {
			t.Fatalf("sample %d = %v, golden %v", i, frame[i], want[i])
		}
	}
}
