package audio

import "testing"

func TestTaperIsMonotonicAndBounded(t *testing.T) {
	prev := float32(-1)
	for i := 0; i <= 100; i++ {
		g := taper(float32(i) / 100)
		if g < 0 || g > 1 {
			t.Fatalf("taper(%v) = %v, outside [0,1]", float32(i)/100, g)
		}
		if g < prev {
			t.Fatalf("taper not monotonic at %v: %v < %v", float32(i)/100, g, prev)
		}
		prev = g
	}
}

func TestTaperGivesFinerControlLowDown(t *testing.T) {
	// The whole point of a taper: half-way on the knob must be well below
	// half the amplitude, or the top of the travel does nothing audible.
	if g := taper(0.5); g >= 0.4 {
		t.Fatalf("taper(0.5) = %v, want clearly below 0.4", g)
	}
}

func TestMixerAppliesMasterAndBusGains(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 1, SFX: 0, Notification: 0})

	out := make([]float32, FrameSamples)
	monitor := make([]float32, FrameSamples)
	sfx := make([]float32, FrameSamples)
	notif := make([]float32, FrameSamples)
	for i := range monitor {
		monitor[i] = 0.5
		sfx[i] = 0.5
	}
	m.Mix(out, monitor, sfx, notif)
	// SFX bus is at zero, so only the monitor survives.
	if out[0] <= 0 {
		t.Fatalf("monitor did not reach the output: %v", out[0])
	}
	m.SetLevels(Levels{Master: 0, Voice: 1, SFX: 1, Notification: 1})
	m.Mix(out, monitor, sfx, notif)
	if out[0] != 0 {
		t.Fatalf("master at zero still produced %v", out[0])
	}
}

func TestMixerNeverClipsTheOutput(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 1, SFX: 1, Notification: 1})
	out := make([]float32, FrameSamples)
	full := make([]float32, FrameSamples)
	for i := range full {
		full[i] = 1.0
	}
	m.Mix(out, full, full, full)
	for i, v := range out {
		if v > 1.0 || v < -1.0 {
			t.Fatalf("sample %d = %v, outside [-1,1]", i, v)
		}
	}
}
