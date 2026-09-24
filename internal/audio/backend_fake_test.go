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
	// Empty id: this test is about the injected open failure, not device
	// identity, so it must not depend on any device being registered.
	if _, err := b.OpenCapture("", func([]float32) {}); !errors.Is(err, want) {
		t.Fatalf("OpenCapture err = %v, want %v", err, want)
	}
	// The failure is one-shot: the next open succeeds, which is what lets
	// the manager's retry path be tested.
	if _, err := b.OpenCapture("", func([]float32) {}); err != nil {
		t.Fatalf("second OpenCapture failed: %v", err)
	}
}

// TestFakeBackendRejectsUnknownDeviceID proves the fake mirrors
// malgoBackend.setDeviceID's behavior: a non-empty id that does not match
// anything Enumerate would return must fail to open, the same as it would
// against real hardware when a persisted device has been unplugged. Without
// this, the manager's "saved device is gone -> fall back to system default"
// path (built on top of this backend) could pass every test against the
// fake and still fail the first time it meets real hardware.
func TestFakeBackendRejectsUnknownDeviceID(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Test Mic", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Test Out", IsDefault: true}},
	)

	if _, err := b.OpenCapture("mic-does-not-exist", func([]float32) {}); err == nil {
		t.Fatal("OpenCapture with unknown id succeeded, want error")
	}
	if _, err := b.OpenPlayback("out-does-not-exist", func([]float32) {}); err == nil {
		t.Fatal("OpenPlayback with unknown id succeeded, want error")
	}

	// A registered id still succeeds.
	if st, err := b.OpenCapture("mic-1", func([]float32) {}); err != nil {
		t.Fatalf("OpenCapture with known id failed: %v", err)
	} else {
		_ = st.Stop()
	}
	if st, err := b.OpenPlayback("out-1", func([]float32) {}); err != nil {
		t.Fatalf("OpenPlayback with known id failed: %v", err)
	} else {
		_ = st.Stop()
	}
}

// TestFakeBackendEmptyDeviceIDAlwaysSucceeds proves the empty id -- "follow
// the system default" (backend.go's DeviceInfo.ID doc) -- is a first-class
// value both backends must agree on, even when no devices are registered at
// all.
func TestFakeBackendEmptyDeviceIDAlwaysSucceeds(t *testing.T) {
	b := NewFakeBackend()
	if _, err := b.OpenCapture("", func([]float32) {}); err != nil {
		t.Fatalf("OpenCapture with empty id failed: %v", err)
	}
	if _, err := b.OpenPlayback("", func([]float32) {}); err != nil {
		t.Fatalf("OpenPlayback with empty id failed: %v", err)
	}
}
