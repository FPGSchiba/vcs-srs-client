package audio

import (
	"log/slog"
	"testing"
)

func TestNotifSFXAlertIsUnavailableWithoutItsSample(t *testing.T) {
	n := NewNotifSFX(slog.Default())

	// notify_alert.wav does not exist and MUST NOT be substituted with a
	// synthesised tone -- assets/README.md records the pack as "not
	// something to substitute". Reporting false is the honest answer, and
	// it is exactly how all nine existing SFX slots ship today.
	if n.Available(NotifyAlert) {
		t.Fatal("Available(NotifyAlert) = true; the sample does not exist, so this must be false until the pack lands")
	}
	if got := n.sampleFor(NotifyAlert); got != nil {
		t.Fatalf("sampleFor returned %d samples, want nil", len(got))
	}
}

func TestNotifSFXUnknownIDIsUnavailable(t *testing.T) {
	n := NewNotifSFX(slog.Default())

	if n.Available("no-such-slot") {
		t.Fatal("Available(\"no-such-slot\") = true, want false")
	}
	if got := n.sampleFor("no-such-slot"); got != nil {
		t.Fatalf("sampleFor returned %d samples, want nil", len(got))
	}
}

func TestNotifSFXVoicePoolIsIndependentPerCall(t *testing.T) {
	n := NewNotifSFX(slog.Default())

	a := n.NewVoicePool()
	b := n.NewVoicePool()
	if a == b {
		t.Fatal("NewVoicePool returned the same pool twice; each Manager generation needs its own mixing state")
	}
}

func TestNotifSFXMixIntoIsSilentWithNoSample(t *testing.T) {
	n := NewNotifSFX(slog.Default())
	pool := n.NewVoicePool()

	pool.play(NotifyAlert)
	dst := make([]float32, FrameSamples)
	pool.mixInto(dst, n.sampleFor)

	for i, v := range dst {
		if v != 0 {
			t.Fatalf("dst[%d] = %v, want 0 -- a missing sample plays silence, never noise", i, v)
		}
	}
}
