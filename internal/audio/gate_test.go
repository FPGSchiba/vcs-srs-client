package audio

import "testing"

func testGateConfig() GateConfig {
	return GateConfig{
		VOXEnabled:        false,
		VOXThreshold:      0.05,
		VOXMinLengthMS:    50,  // 5 frames
		VOXHangMS:         100, // 10 frames
		PTTStartDelayMS:   0,
		PTTReleaseDelayMS: 0,
	}
}

func TestGatePTTOpensImmediatelyWithNoDelay(t *testing.T) {
	g := NewGate(testGateConfig())
	if g.Step(GateInput{PTT: true}) != true {
		t.Fatal("gate closed on the first PTT frame with zero start delay")
	}
}

func TestGatePTTStartDelayHoldsGateClosed(t *testing.T) {
	cfg := testGateConfig()
	cfg.PTTStartDelayMS = 30 // 3 frames
	g := NewGate(cfg)
	for i := 0; i < 3; i++ {
		if g.Step(GateInput{PTT: true}) {
			t.Fatalf("gate opened at frame %d, want it held until frame 3", i)
		}
	}
	if !g.Step(GateInput{PTT: true}) {
		t.Fatal("gate still closed after the start delay elapsed")
	}
}

func TestGatePTTReleaseDelayKeepsGateOpen(t *testing.T) {
	cfg := testGateConfig()
	cfg.PTTReleaseDelayMS = 30 // 3 frames
	g := NewGate(cfg)
	g.Step(GateInput{PTT: true})
	for i := 0; i < 3; i++ {
		if !g.Step(GateInput{PTT: false}) {
			t.Fatalf("gate closed at tail frame %d, clipping the word ending", i)
		}
	}
	if g.Step(GateInput{PTT: false}) {
		t.Fatal("gate still open after the release tail elapsed")
	}
}

func TestGateVOXRequiresMinLengthBeforeOpening(t *testing.T) {
	cfg := testGateConfig()
	cfg.VOXEnabled = true
	g := NewGate(cfg)
	// 5 frames of sustain required; frames 0-3 must stay shut.
	for i := 0; i < 4; i++ {
		if g.Step(GateInput{Level: 0.2}) {
			t.Fatalf("VOX opened at frame %d, before min length", i)
		}
	}
	if !g.Step(GateInput{Level: 0.2}) {
		t.Fatal("VOX did not open once min length was satisfied")
	}
}

func TestGateVOXHangKeepsGateOpenThroughAPause(t *testing.T) {
	cfg := testGateConfig()
	cfg.VOXEnabled = true
	g := NewGate(cfg)
	for i := 0; i < 5; i++ {
		g.Step(GateInput{Level: 0.2})
	}
	for i := 0; i < 10; i++ {
		if !g.Step(GateInput{Level: 0.0}) {
			t.Fatalf("VOX closed at hang frame %d", i)
		}
	}
	if g.Step(GateInput{Level: 0.0}) {
		t.Fatal("VOX still open after the hang elapsed")
	}
}

func TestGateMuteOverridesPTTAndVOX(t *testing.T) {
	cfg := testGateConfig()
	cfg.VOXEnabled = true
	g := NewGate(cfg)
	if g.Step(GateInput{PTT: true, Level: 0.9, Muted: true}) {
		t.Fatal("gate opened while muted -- mute must win unconditionally")
	}
}

func TestGateVOXDisabledIgnoresLevel(t *testing.T) {
	g := NewGate(testGateConfig()) // VOXEnabled false
	for i := 0; i < 50; i++ {
		if g.Step(GateInput{Level: 0.9}) {
			t.Fatal("gate opened on level alone with VOX disabled")
		}
	}
}
