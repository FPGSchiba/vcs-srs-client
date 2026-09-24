package voice

import "testing"

// TestMHz32MatchesServerComputation is the load-bearing test of this file.
//
// The server decides whether to relay a packet with an EXACT float32
// equality: `radio.Frequency == float32(pkt.Frequency)/1000.0`, comparing
// the float32 MHz we advertised via UpdateRadioInfo against the float32 it
// computes from the 24-bit kHz on the wire. If those two float32 values
// differ by one bit, the packet is silently not relayed and no component
// logs anything.
//
// We make them equal BY CONSTRUCTION: MHz32 evaluates the identical
// expression the server evaluates. This test pins that, and would catch a
// future "optimisation" to float64 intermediates.
func TestMHz32MatchesServerComputation(t *testing.T) {
	for _, khz := range []KHz{
		118500, 251300, 243000, 305750, 127125, 140625,
		121500, 156800, 100001, 399999, 1, 16777215,
	} {
		advertised := khz.MHz32()               // what we send in UpdateRadioInfo
		server := float32(uint32(khz)) / 1000.0 // what the server computes from the packet
		if advertised != server {
			t.Errorf("kHz %d: advertised %v != server-computed %v", khz, advertised, server)
		}
	}
}

func TestKHzFromMHz32RoundTrips(t *testing.T) {
	for _, khz := range []KHz{118500, 251300, 243000, 305750, 127125, 100001, 399999} {
		if got := KHzFromMHz32(khz.MHz32()); got != khz {
			t.Errorf("round trip of %d kHz gave %d", khz, got)
		}
	}
}

// TestKHzFromMHz32Rounds pins that we ROUND where the C# peer truncates
// (VcsVoicePacket.SetFrequencyHz does `(uint)(freqHz / 1000.0)`). On the kHz
// grid the two agree; off-grid, rounding is the only rule that round-trips,
// so a float32 that is a hair under its intended kHz must not lose a kHz.
func TestKHzFromMHz32Rounds(t *testing.T) {
	if got := KHzFromMHz32(251.2999); got != 251300 {
		t.Errorf("251.2999 MHz gave %d kHz, want 251300 (must round, not truncate)", got)
	}
	if got := KHzFromMHz32(251.3001); got != 251300 {
		t.Errorf("251.3001 MHz gave %d kHz, want 251300", got)
	}
}

func TestValidRejectsAbove24Bits(t *testing.T) {
	if !KHz(16777215).Valid() {
		t.Error("16777215 kHz is the largest 24-bit value and must be valid")
	}
	if KHz(16777216).Valid() {
		t.Error("16777216 kHz does not fit in 24 bits and must be invalid")
	}
}
