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
	received := make([]float32, FrameSamples)
	m.Mix(out, monitor, received, sfx, notif)
	// SFX bus is at zero, so only the monitor survives.
	if out[0] <= 0 {
		t.Fatalf("monitor did not reach the output: %v", out[0])
	}
	m.SetLevels(Levels{Master: 0, Voice: 1, SFX: 1, Notification: 1})
	m.Mix(out, monitor, received, sfx, notif)
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
	m.Mix(out, full, full, full, full)
	for i, v := range out {
		if v > 1.0 || v < -1.0 {
			t.Fatalf("sample %d = %v, outside [-1,1]", i, v)
		}
	}
}

// TestMixSumsReceivedOntoTheVoiceBus pins that received voice shares the
// voice bus with the local monitor, as mixer.go's own doc promised: "Phase 5
// adds received voice to the voice bus alongside monitor".
func TestMixSumsReceivedOntoTheVoiceBus(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 1, SFX: 0, Notification: 0})

	out := make([]float32, 4)
	monitor := []float32{0.1, 0.1, 0.1, 0.1}
	received := []float32{0.2, 0.2, 0.2, 0.2}
	silent := make([]float32, 4)

	m.Mix(out, monitor, received, silent, silent)
	for i, got := range out {
		if got < 0.29 || got > 0.31 {
			t.Fatalf("out[%d] = %v, want ~0.3 (monitor 0.1 + received 0.2)", i, got)
		}
	}
}

// TestMixClampsCombinedVoice pins that the hard limiter still applies once a
// second source feeds the voice bus.
func TestMixClampsCombinedVoice(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 1, SFX: 0, Notification: 0})
	out := make([]float32, 2)
	loud := []float32{0.9, -0.9}
	silent := make([]float32, 2)
	m.Mix(out, loud, loud, silent, silent)
	if out[0] != 1 || out[1] != -1 {
		t.Fatalf("out = %v, want [1 -1] (clamped)", out)
	}
}

// TestMixAppliesTheVoiceGainToTheReceivedBus pins that the received bus is
// summed AT THE VOICE GAIN, not at unity. Without this, a Mix that ignored
// the voice level for received audio -- or summed it post-gain -- would
// still pass the two tests above, since both run at Voice: 1.
func TestMixAppliesTheVoiceGainToTheReceivedBus(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 0, SFX: 1, Notification: 1})
	out := make([]float32, 4)
	received := []float32{0.5, 0.5, 0.5, 0.5}
	silent := make([]float32, 4)
	m.Mix(out, silent, received, silent, silent)
	for i, got := range out {
		if got != 0 {
			t.Fatalf("out[%d] = %v with the voice bus at zero, want 0 -- received voice must ride the VOICE gain", i, got)
		}
	}
}

// TestMixKeepsTheBusesDistinct pins the argument ORDER: received rides the
// voice gain while sfx and notif ride their own. A Mix that transposed
// `received` with `sfx` would pass every test above, because they all leave
// SFX and Notification silent.
func TestMixKeepsTheBusesDistinct(t *testing.T) {
	m := NewMixer()
	// Only the SFX bus is open. taper(1) = 1, taper(0) = 0.
	m.SetLevels(Levels{Master: 1, Voice: 0, SFX: 1, Notification: 0})
	out := make([]float32, 3)
	received := []float32{0.25, 0.25, 0.25}
	sfx := []float32{0.5, 0.5, 0.5}
	silent := make([]float32, 3)

	m.Mix(out, silent, received, sfx, silent)
	for i, got := range out {
		if got < 0.49 || got > 0.51 {
			t.Fatalf("out[%d] = %v, want ~0.5 -- only the sfx bus is open, so received (0.25) must not leak into it", i, got)
		}
	}
}
