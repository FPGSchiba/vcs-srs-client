package audio

import (
	"errors"
	"testing"
)

func TestFakeBackendRoundTripsFrames(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Test Mic", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Test Out", IsDefault: true}},
	)

	var got [][]float32
	st, err := b.OpenCapture("mic-1", func(f []float32) {
		got = append(got, append([]float32(nil), f...))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Stop()

	b.PushFrame(sine(0.5))
	if len(got) != 1 {
		t.Fatalf("capture callback fired %d times, want 1", len(got))
	}
	if len(got[0]) != FrameSamples {
		t.Fatalf("callback got %d samples, want %d", len(got[0]), FrameSamples)
	}
}

func TestFakeBackendRecordsPlayback(t *testing.T) {
	b := NewFakeBackend()
	st, err := b.OpenPlayback("", func(f []float32) {
		for i := range f {
			f[i] = 0.25
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Stop()

	b.PullFrame()
	rec := b.Captured()
	if len(rec) != 1 || rec[0][0] != 0.25 {
		t.Fatalf("playback recording = %v, want one frame of 0.25", rec)
	}
}

func TestFakeBackendCanFailAnOpen(t *testing.T) {
	b := NewFakeBackend()
	want := errors.New("device busy")
	b.FailNextOpen(want)
	if _, err := b.OpenCapture("mic-1", func([]float32) {}); !errors.Is(err, want) {
		t.Fatalf("OpenCapture err = %v, want %v", err, want)
	}
	// The failure is one-shot: the next open succeeds, which is what lets
	// the manager's retry path be tested.
	if _, err := b.OpenCapture("mic-1", func([]float32) {}); err != nil {
		t.Fatalf("second OpenCapture failed: %v", err)
	}
}
