//go:build !cgo

// This stub mirrors ../rnnoise_stub.go's discipline: a CGO_ENABLED=0 release
// build must still compile, link and run -- just without voice. Every symbol
// the cgo build exports is mirrored here with the same signature, and every
// call fails loudly rather than silently producing garbage audio.
package opus

import "errors"

const (
	SampleRate   = 48000
	Channels     = 1
	FrameSamples = 960
	MaxPacket    = 997
)

// Application mirrors libopus's application constants.
type Application int

// ApplicationAudio is OPUS_APPLICATION_AUDIO, spelled out because the C
// header is not available in this build.
const ApplicationAudio Application = 2049

var errNoCGO = errors.New("opus: built without cgo; voice is unavailable")

// Available reports whether this build has a real codec.
func Available() bool { return false }

// Encoder is a non-functional placeholder for the cgo-backed encoder.
type Encoder struct{}

// NewEncoder always fails in a cgo-less build.
func NewEncoder(int) (*Encoder, error) { return nil, errNoCGO }

// Encode always fails in a cgo-less build.
func (*Encoder) Encode([]float32, []byte) (int, error) { return 0, errNoCGO }

// Close is a no-op in a cgo-less build.
func (*Encoder) Close() {}

// Decoder is a non-functional placeholder for the cgo-backed decoder.
type Decoder struct{}

// NewDecoder always fails in a cgo-less build.
func NewDecoder() (*Decoder, error) { return nil, errNoCGO }

// Decode always fails in a cgo-less build.
func (*Decoder) Decode([]byte, []float32) error { return errNoCGO }

// Close is a no-op in a cgo-less build.
func (*Decoder) Close() {}
