//go:build !cgo

package audio

import "errors"

// NewMalgoBackend fails cleanly when the build has no cgo.
//
// Unlike Denoiser (rnnoise_stub.go), there is no degraded-but-functional
// fallback here: miniaudio's Go bindings are cgo-only, so a !cgo build has
// no path to real device I/O at all. Task 7 made this package buildable
// without cgo; this stub keeps that guarantee true after Task 8 adds a
// second, unconditionally-cgo dependency, instead of letting a bare
// `go build` (or `go vet`) of this package alone fail with a wall of
// "undefined: malgo.X" errors the moment CGO_ENABLED is 0.
func NewMalgoBackend() (Backend, error) {
	return nil, errors.New("audio: malgo backend requires a cgo-enabled build")
}
