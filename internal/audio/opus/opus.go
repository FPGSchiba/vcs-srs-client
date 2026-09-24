//go:build cgo

// Package opus provides thin cgo bindings to the vendored libopus rooted in
// this directory (see VENDOR.md for provenance).
//
// It lives here, rather than one directory up in package audio, for the same
// reason internal/audio/rnnoise does: cgo only auto-compiles .c files that sit
// in the SAME directory as the Go file containing `import "C"`. The vendored
// sources are these files' siblings, so the binding has to be too.
//
// The parameters below are a WIRE CONTRACT with the C# peer, not preferences.
// See the Phase 5 design doc §4: the peer hard-normalises every decoded frame
// to exactly 960 samples, and the server drops any voice payload of 5 bytes or
// fewer -- which is why DTX is never enabled.
package opus

/*
#cgo CFLAGS: -I${SRCDIR} -I${SRCDIR}/include -I${SRCDIR}/celt -I${SRCDIR}/silk -I${SRCDIR}/silk/float -I${SRCDIR}/src
#cgo CFLAGS: -O2 -DOPUS_BUILD -DHAVE_LRINTF -DVAR_ARRAYS
#cgo linux LDFLAGS: -lm
#include <opus.h>
#include <stdlib.h>
#include "ctl_shim.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const (
	// SampleRate, Channels and FrameSamples are fixed by the peer contract.
	SampleRate   = 48000
	Channels     = 1
	FrameSamples = 960 // 20 ms at 48 kHz

	// MaxPacket is the largest payload that survives the server's fixed
	// 1024-byte read buffer, which TRUNCATES rather than rejecting.
	MaxPacket = 997
)

// Application mirrors libopus's application constants.
type Application int

// ApplicationAudio matches the C# peer's OpusEncoder.Create(48000, 1, Application.Audio).
const ApplicationAudio Application = C.OPUS_APPLICATION_AUDIO

// Available reports whether this build has a real codec.
func Available() bool { return true }

// Encoder wraps an OpusEncoder configured for the peer contract.
type Encoder struct{ st *C.OpusEncoder }

// NewEncoder allocates an encoder at the given bitrate in bits per second.
// FEC and DTX are both explicitly disabled: DTX would emit 1-2 byte frames
// that the server silently drops, presenting as speech that cuts out only
// during pauses.
func NewEncoder(bitrate int) (*Encoder, error) {
	var cerr C.int
	st := C.opus_encoder_create(C.opus_int32(SampleRate), C.int(Channels),
		C.int(ApplicationAudio), &cerr)
	if cerr != C.OPUS_OK || st == nil {
		return nil, fmt.Errorf("opus: encoder_create failed: %d", int(cerr))
	}
	e := &Encoder{st: st}
	set := func(req C.int, v C.opus_int32) error {
		if r := C.opus_encoder_ctl_set(st, req, v); r != C.OPUS_OK {
			return fmt.Errorf("opus: encoder_ctl %d failed: %d", int(req), int(r))
		}
		return nil
	}
	if err := set(C.OPUS_SET_BITRATE_REQUEST, C.opus_int32(bitrate)); err != nil {
		e.Close()
		return nil, err
	}
	if err := set(C.OPUS_SET_INBAND_FEC_REQUEST, 0); err != nil {
		e.Close()
		return nil, err
	}
	if err := set(C.OPUS_SET_DTX_REQUEST, 0); err != nil {
		e.Close()
		return nil, err
	}
	return e, nil
}

// Encode compresses exactly FrameSamples float32 samples into dst, returning
// the number of bytes written.
func (e *Encoder) Encode(pcm []float32, dst []byte) (int, error) {
	if e == nil || e.st == nil {
		return 0, errors.New("opus: encoder closed")
	}
	if len(pcm) != FrameSamples {
		return 0, fmt.Errorf("opus: frame must be %d samples, got %d", FrameSamples, len(pcm))
	}
	if len(dst) == 0 {
		return 0, errors.New("opus: empty destination")
	}
	n := C.opus_encode_float(e.st,
		(*C.float)(unsafe.Pointer(&pcm[0])), C.int(FrameSamples),
		(*C.uchar)(unsafe.Pointer(&dst[0])), C.opus_int32(len(dst)))
	if n < 0 {
		return 0, fmt.Errorf("opus: encode failed: %d", int(n))
	}
	return int(n), nil
}

// Close frees the encoder. Safe on a nil or already-closed receiver.
func (e *Encoder) Close() {
	if e != nil && e.st != nil {
		C.opus_encoder_destroy(e.st)
		e.st = nil
	}
}

// Decoder wraps an OpusDecoder at the peer's fixed 48 kHz output rate.
type Decoder struct{ st *C.OpusDecoder }

// NewDecoder allocates a decoder.
func NewDecoder() (*Decoder, error) {
	var cerr C.int
	st := C.opus_decoder_create(C.opus_int32(SampleRate), C.int(Channels), &cerr)
	if cerr != C.OPUS_OK || st == nil {
		return nil, fmt.Errorf("opus: decoder_create failed: %d", int(cerr))
	}
	return &Decoder{st: st}, nil
}

// Decode writes exactly FrameSamples samples into pcm. A nil or empty payload
// requests packet-loss concealment, which is how the jitter buffer fills a gap.
func (d *Decoder) Decode(payload []byte, pcm []float32) error {
	if d == nil || d.st == nil {
		return errors.New("opus: decoder closed")
	}
	if len(pcm) != FrameSamples {
		return fmt.Errorf("opus: output must be %d samples, got %d", FrameSamples, len(pcm))
	}
	var data *C.uchar
	var length C.opus_int32
	fec := C.int(0)
	if len(payload) > 0 {
		data = (*C.uchar)(unsafe.Pointer(&payload[0]))
		length = C.opus_int32(len(payload))
	}
	n := C.opus_decode_float(d.st, data, length,
		(*C.float)(unsafe.Pointer(&pcm[0])), C.int(FrameSamples), fec)
	if n < 0 {
		return fmt.Errorf("opus: decode failed: %d", int(n))
	}
	return nil
}

// Close frees the decoder. Safe on a nil or already-closed receiver.
func (d *Decoder) Close() {
	if d != nil && d.st != nil {
		C.opus_decoder_destroy(d.st)
		d.st = nil
	}
}
