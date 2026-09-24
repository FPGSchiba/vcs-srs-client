// Package voice implements the client's UDP voice path: the VCS wire
// protocol, Opus transport, and the session that carries them.
package voice

import "math"

// KHz is a frequency in whole kilohertz and is the client's ONLY canonical
// frequency representation. Nothing in this client stores a frequency as a
// float.
//
// Three components compare frequencies three different ways:
//
//   - the Go server, deciding whether to relay, uses EXACT float32 equality
//     between the radio frequency we advertised and float32(kHz)/1000.0
//   - the C# peer's receive filter uses a 1 Hz tolerance in Hz
//   - the wire carries 24-bit unsigned kHz, big-endian
//
// Holding the integer and deriving every other form from it satisfies all
// three at once, and in particular makes the server's exact-equality match
// true by construction rather than by luck. See the Phase 5 design doc §5.
type KHz uint32

// maxKHz is the largest value the 24-bit wire field can carry.
const maxKHz = 1<<24 - 1

// KHzFromMHz32 converts a float32 MHz frequency -- the form the server sends
// in RadioInfo -- to canonical kHz.
//
// It ROUNDS where the C# peer's SetFrequencyHz truncates. On the kHz grid the
// two agree; off the grid, rounding is the only rule that round-trips, so a
// float32 sitting a hair below its intended kHz does not lose a whole kHz.
//
// The returned value is always Valid(); 0 means the input was not a
// representable frequency (negative, NaN, +Inf, or exceeds the 24-bit ceiling).
func KHzFromMHz32(f float32) KHz {
	if f <= 0 {
		return 0
	}
	// Compute in float64 to preserve precision during multiplication.
	rounded := math.Round(float64(f) * 1000)
	// Check for overflow: reject any value that exceeds maxKHz.
	if rounded > float64(maxKHz) || math.IsNaN(rounded) {
		return 0
	}
	return KHz(rounded)
}

// MHz32 returns the float32 MHz value to advertise in UpdateRadioInfo.
//
// The expression is deliberately IDENTICAL to the one voice/server.go
// evaluates on a received packet (`float32(pkt.Frequency) / 1000.0`). Do not
// "improve" it into float64 intermediates: the server compares the two
// results with ==, and a one-bit difference silently stops relaying with no
// error anywhere.
func (k KHz) MHz32() float32 {
	return float32(uint32(k)) / 1000.0
}

// Valid reports whether k fits in the 24-bit wire field.
func (k KHz) Valid() bool { return uint32(k) <= maxKHz }
