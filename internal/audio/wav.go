package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// DecodeWAV decodes a 16-bit PCM RIFF file into the pipeline's internal
// format: 48 kHz, mono, float32.
//
// The asset contract (spec §12) is 48 kHz mono 16-bit, but off-spec files are
// downmixed and linearly resampled rather than rejected: for short chirps the
// quality cost is inaudible, and a mis-specified asset that plays slightly
// imperfectly is far easier to diagnose than one that silently does nothing.
func DecodeWAV(b []byte) ([]float32, error) {
	if len(b) < 44 {
		return nil, errors.New("wav: too short to contain a header")
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("wav: not a RIFF/WAVE file")
	}

	var (
		format     uint16
		channels   int
		sampleRate int
		bits       uint16
		data       []byte
		haveFmt    bool
	)

	// Walk the chunk list rather than assuming fmt is immediately followed by
	// data -- real files carry LIST/INFO chunks between them.
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := off + 8
		if size < 0 || body+size > len(b) {
			return nil, fmt.Errorf("wav: chunk %q overruns the file", id)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("wav: fmt chunk too small")
			}
			format = binary.LittleEndian.Uint16(b[body : body+2])
			channels = int(binary.LittleEndian.Uint16(b[body+2 : body+4]))
			sampleRate = int(binary.LittleEndian.Uint32(b[body+4 : body+8]))
			bits = binary.LittleEndian.Uint16(b[body+14 : body+16])
			haveFmt = true
		case "data":
			data = b[body : body+size]
		}
		off = body + size
		if size%2 == 1 {
			off++ // RIFF chunks are word-aligned
		}
	}

	if !haveFmt {
		return nil, errors.New("wav: no fmt chunk")
	}
	if format != 1 {
		return nil, fmt.Errorf("wav: format %d is not 16-bit PCM", format)
	}
	if bits != 16 {
		return nil, fmt.Errorf("wav: %d-bit samples, want 16", bits)
	}
	if channels < 1 {
		return nil, errors.New("wav: zero channels")
	}
	// A uint16 channel count can go as high as 65535; the divide-by-zero
	// guard above only rules out zero. Cap it generously -- 64 covers every
	// real multichannel format (7.1.4 Atmos beds included) with headroom to
	// spare, while still rejecting a channel count that exists only to pair
	// with a large data chunk and blow up the downmix loop below.
	const maxChannels = 64
	if channels > maxChannels {
		return nil, fmt.Errorf("wav: %d channels exceeds the %d-channel limit", channels, maxChannels)
	}
	// Reject an implausible declared sample rate before it ever reaches
	// resampleLinear: a tiny rate makes resampleLinear's to/from ratio
	// enormous, turning a modest data chunk into a multi-gigabyte
	// allocation -- a fatal OOM rather than a recoverable panic. 8000 Hz
	// (telephony) is the lowest rate in common use and 768000 Hz is well
	// above any real-world audio interface, so the bounds below give both
	// sides margin -- the point is to reject absurdity, not to police format
	// choices, so 1000..768000 comfortably admits anything a legitimate file
	// would declare.
	const (
		minSampleRate = 1000
		maxSampleRate = 768000
	)
	if sampleRate < minSampleRate || sampleRate > maxSampleRate {
		return nil, fmt.Errorf("wav: sample rate %d Hz outside the plausible %d-%d Hz range", sampleRate, minSampleRate, maxSampleRate)
	}
	if data == nil {
		return nil, errors.New("wav: no data chunk")
	}

	// Decode and downmix to mono.
	frames := len(data) / 2 / channels
	mono := make([]float32, frames)
	for i := 0; i < frames; i++ {
		var sum float32
		for c := 0; c < channels; c++ {
			raw := int16(binary.LittleEndian.Uint16(data[(i*channels+c)*2:]))
			sum += float32(raw) / 32768.0
		}
		mono[i] = sum / float32(channels)
	}

	if sampleRate == SampleRate || sampleRate <= 0 {
		return mono, nil
	}
	return resampleLinear(mono, sampleRate, SampleRate), nil
}

// resampleLinear is adequate for short SFX. Voice never passes through here.
func resampleLinear(in []float32, from, to int) []float32 {
	if len(in) == 0 {
		return in
	}
	ratio := float64(to) / float64(from)
	outLen := int(float64(len(in)) * ratio)
	out := make([]float32, outLen)
	for i := range out {
		src := float64(i) / ratio
		i0 := int(src)
		if i0 >= len(in)-1 {
			out[i] = in[len(in)-1]
			continue
		}
		frac := float32(src - float64(i0))
		out[i] = in[i0]*(1-frac) + in[i0+1]*frac
	}
	return out
}
