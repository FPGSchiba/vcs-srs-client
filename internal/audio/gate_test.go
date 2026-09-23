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

func TestGateMuteOverridesPTT(t *testing.T) {
	g := NewGate(testGateConfig()) // zero start delay: PTT opens immediately
	if !g.Step(GateInput{PTT: true}) {
		t.Fatal("PTT did not open the gate; mute-override claim is untestable")
	}
	if g.Step(GateInput{PTT: true, Muted: true}) {
		t.Fatal("gate opened while muted with PTT held -- mute must win unconditionally")
	}
}

func TestGateMuteOverridesVOX(t *testing.T) {
	cfg := testGateConfig()
	cfg.VOXEnabled = true
	g := NewGate(cfg)
	// Drive VOX genuinely open: 5 frames of sustain are required.
	var open bool
	for i := 0; i < 5; i++ {
		open = g.Step(GateInput{Level: 0.2})
	}
	if !open {
		t.Fatal("VOX did not open after min length; mute-override claim is untestable")
	}
	if g.Step(GateInput{Level: 0.9, Muted: true}) {
		t.Fatal("gate opened while muted with VOX above threshold -- mute must win unconditionally")
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

// TestGateRaisingPTTStartDelayMidTransmissionDoesNotClosetGate reproduces the
// exact trace from the Finding 1 review: a small start delay opens the gate,
// then SetConfig raises the delay while PTT is still held. The gate must
// stay open -- re-deriving pttOpen from the new (larger) threshold against
// the already-satisfied pttHeld counter must not cut the user off.
func TestGateRaisingPTTStartDelayMidTransmissionDoesNotCloseGate(t *testing.T) {
	cfg := testGateConfig()
	cfg.PTTStartDelayMS = 10 // startDelay = 1 frame
	g := NewGate(cfg)

	if g.Step(GateInput{PTT: true}) {
		t.Fatal("frame 1: gate should still be closed (pttHeld=1, startDelay=1)")
	}
	if !g.Step(GateInput{PTT: true}) {
		t.Fatal("frame 2: gate should have opened (pttHeld=2 > startDelay=1)")
	}

	// Raise the start delay mid-transmission, PTT still held throughout.
	cfg.PTTStartDelayMS = 50 // startDelay = 5 frames
	g.SetConfig(cfg)

	for i := 0; i < 5; i++ {
		if !g.Step(GateInput{PTT: true}) {
			t.Fatalf("frame %d after SetConfig raised the start delay: gate closed under a held PTT", i+3)
		}
	}
}

// TestGateRaisingVOXMinLengthMidTransmissionDoesNotCloseGate is the VOX
// equivalent of the PTT case above: VOX opens under the old min length, then
// SetConfig raises VOXMinLengthMS while the level is still sustained above
// threshold. The gate must stay open.
func TestGateRaisingVOXMinLengthMidTransmissionDoesNotCloseGate(t *testing.T) {
	cfg := testGateConfig()
	cfg.VOXEnabled = true
	cfg.VOXMinLengthMS = 50 // voxMinLen = 5 frames
	g := NewGate(cfg)

	var open bool
	for i := 0; i < 5; i++ {
		open = g.Step(GateInput{Level: 0.2})
	}
	if !open {
		t.Fatal("VOX should have opened after 5 sustained frames at the old min length")
	}

	// Raise the min length mid-transmission, level still sustained above threshold.
	cfg.VOXMinLengthMS = 100 // voxMinLen = 10 frames
	g.SetConfig(cfg)

	for i := 0; i < 5; i++ {
		if !g.Step(GateInput{Level: 0.2}) {
			t.Fatalf("frame %d after SetConfig raised VOXMinLengthMS: gate closed under sustained level", i+6)
		}
	}
}
