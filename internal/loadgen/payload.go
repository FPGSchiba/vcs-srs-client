package loadgen

import "encoding/binary"

// PayloadLabel names the frame source in every report. The frames are NOT
// Opus: the server relays payload bytes opaquely, so load needs only size and
// cadence, and nothing here may be mistaken for real audio.
const PayloadLabel = "synthetic-not-opus"

const syntheticFrameLen = 120

// SyntheticFrame returns a deterministic 120-byte payload. Bytes 0-3 are seq
// big-endian; the rest come from a xorshift64 seeded by seq.
func SyntheticFrame(seq uint32) []byte {
	b := make([]byte, syntheticFrameLen)
	binary.BigEndian.PutUint32(b, seq)
	// Offset the seed so seq 0 does not start the generator at its fixed point.
	x := uint64(seq) + 0x9E3779B97F4A7C15
	for i := 4; i < syntheticFrameLen; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		b[i] = byte(x >> 24)
	}
	return b
}
