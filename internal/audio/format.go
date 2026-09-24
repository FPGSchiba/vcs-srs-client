// Package audio implements the client's capture -> process -> playback
// pipeline.
//
// THE INTERNAL FORMAT IS NOT A PREFERENCE. RNNoise accepts only 480-sample
// frames at 48 kHz, so the whole pipeline adopts its granularity; varying
// any of these constants means buffering around the denoiser for no gain.
// 48 kHz is also Opus's native rate, and 10 ms divides both the 20 ms and
// 40 ms frame durations Phase 5 might require -- see the spec's Sink seam.
package audio

import "time"

const (
	SampleRate    = 48000
	Channels      = 1
	FrameSamples  = 480
	FrameDuration = 10 * time.Millisecond
)
