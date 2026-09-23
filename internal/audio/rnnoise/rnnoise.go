//go:build cgo

// Package rnnoise provides thin cgo bindings to the vendored RNNoise C
// library rooted in this directory (see VENDOR.md for provenance).
//
// This package is deliberately minimal: it binds the C API 1:1, in
// RNNoise's native int16-magnitude float scale. It exists as its own
// package -- rather than living directly in ../rnnoise.go -- because cgo
// only auto-compiles .c files that sit in the SAME directory as the Go
// file containing `import "C"`; the vendored sources live here, one
// directory below package audio, so the binding has to live here too.
// internal/audio.Denoiser (one directory up) is the public API real
// callers use: it adds frame-length validation, the int16-magnitude <->
// [-1, 1] scaling documented there, and the Available()/no-op-on-nil
// ergonomics the rest of the DSP chain expects.
package rnnoise

/*
#cgo CFLAGS: -I${SRCDIR} -I${SRCDIR}/include -O2
#include <stdlib.h>
#include "rnnoise.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// FrameSize is the number of samples rnnoise_process_frame requires per
// call. It is a copy of the same 480 the audio package pins for its own
// reasons (see ../format.go) -- RNNoise's C API has no symbol for it, so
// this is upstream's frame size, verified against
// rnnoise_get_frame_size() at v0.1.1.
const FrameSize = 480

// State wraps a vendored RNNoise DenoiseState.
type State struct {
	st *C.DenoiseState
}

// New allocates and initializes a DenoiseState using RNNoise's built-in
// default pre-trained model (passing NULL selects it).
func New() (*State, error) {
	st := C.rnnoise_create(nil)
	if st == nil {
		return nil, errors.New("rnnoise: create failed")
	}
	return &State{st: st}, nil
}

// ProcessFrame denoises exactly FrameSize samples in place. buf must hold
// RNNoise's native int16-magnitude float values, not [-1, 1] -- that
// scaling is the caller's job (internal/audio.Denoiser does it). A buf of
// the wrong length, or a nil/closed State, is a silent no-op: this mirrors
// internal/audio.Denoiser.Process's own documented no-op behavior on a bad
// frame, rather than introducing a different failure mode one layer down.
func (s *State) ProcessFrame(buf []float32) {
	if s == nil || s.st == nil || len(buf) != FrameSize {
		return
	}
	p := (*C.float)(unsafe.Pointer(&buf[0]))
	C.rnnoise_process_frame(s.st, p, p)
}

// Close destroys the underlying DenoiseState. Safe to call on a nil
// receiver or a State whose native state was already destroyed.
func (s *State) Close() {
	if s != nil && s.st != nil {
		C.rnnoise_destroy(s.st)
		s.st = nil
	}
}
