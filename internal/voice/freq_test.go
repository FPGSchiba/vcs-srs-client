package voice

import (
	"math"
	"testing"
)

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
//
// Expected values are from vngd-srs-server/voice/server.go, handleVoicePacket
// → FrequencyAsFloat32(), which computes `float32(p.Frequency) / 1000.0`.
func TestMHz32MatchesServerComputation(t *testing.T) {
	for _, tc := range []struct {
		khz  KHz
		want float32
	}{
		{1, 0.001},
		{118500, 118.5},
		{251300, 251.3},
		{243000, 243},
		{305750, 305.75},
		{127125, 127.125},
		{140625, 140.625},
		{121500, 121.5},
		{156800, 156.8},
		{100001, 100.001},
		{399999, 399.999},
		{16777215, 16777.215},
	} {
		if got := tc.khz.MHz32(); got != tc.want {
			t.Errorf("KHz(%d).MHz32() = %v, want %v", tc.khz, got, tc.want)
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

func TestKHzFromMHz32Bounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    float32
		want KHz
	}{
		// Negative input returns 0.
		{"negative", -100, 0},
		// Zero input returns 0.
		{"zero", 0, 0},
		// Exactly maxKHz (16777215 kHz = 16777.215 MHz) still round-trips.
		{"max valid", 16777.215, 16777215},
		// Value above the 24-bit ceiling returns 0.
		{"1 kHz over max", 16777.216, 0},
		{"1 MHz over max", 16778, 0},
		{"1 GHz over max", 1000000000, 0},
		{"1e10 over max", 1e10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := KHzFromMHz32(tc.f); got != tc.want {
				t.Errorf("KHzFromMHz32(%v) = %d, want %d", tc.f, got, tc.want)
			}
		})
	}
}

func TestKHzFromMHz32SpecialValues(t *testing.T) {
	// NaN returns 0.
	if got := KHzFromMHz32(float32(math.NaN())); got != 0 {
		t.Errorf("KHzFromMHz32(NaN) = %d, want 0", got)
	}
	// Positive infinity returns 0 (exceeds maxKHz).
	if got := KHzFromMHz32(float32(math.Inf(1))); got != 0 {
		t.Errorf("KHzFromMHz32(+Inf) = %d, want 0", got)
	}
}
