//go:build !cgo

package audio

// Denoiser is a no-op when the build has no cgo. Noise suppression is a
// quality feature, not a correctness one: the client must still capture and
// transmit audio without it.
type Denoiser struct{}

// NewDenoiser always succeeds with a no-op Denoiser when cgo is unavailable.
func NewDenoiser() (*Denoiser, error) { return &Denoiser{}, nil }

// Available always reports false: this build has no working denoiser.
func (d *Denoiser) Available() bool { return false }

// Process is a no-op.
func (d *Denoiser) Process([]float32) {}

// Close is a no-op.
func (d *Denoiser) Close() {}
