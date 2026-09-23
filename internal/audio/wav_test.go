package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildWAV assembles a minimal 16-bit PCM RIFF file.
func buildWAV(sampleRate int, channels int, samples []int16) []byte {
	return buildWAVWithChunk(sampleRate, channels, samples, "", nil)
}

// buildWAVWithChunk assembles a 16-bit PCM RIFF file like buildWAV, but when
// extraID is non-empty it inserts one additional chunk (id extraID, body
// extraBody) between the "fmt " and "data" chunks -- e.g. a LIST/INFO chunk,
// including deliberately odd-sized ones to exercise RIFF word-alignment
// padding.
func buildWAVWithChunk(sampleRate int, channels int, samples []int16, extraID string, extraBody []byte) []byte {
	var extra bytes.Buffer
	if extraID != "" {
		extra.WriteString(extraID)
		binary.Write(&extra, binary.LittleEndian, uint32(len(extraBody)))
		extra.Write(extraBody)
		if len(extraBody)%2 == 1 {
			extra.WriteByte(0) // word-alignment pad byte
		}
	}

	var b bytes.Buffer
	dataLen := len(samples) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+extra.Len()+dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate*channels*2))
	binary.Write(&b, binary.LittleEndian, uint16(channels*2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.Write(extra.Bytes())
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataLen))
	for _, s := range samples {
		binary.Write(&b, binary.LittleEndian, s)
	}
	return b.Bytes()
}

func TestDecodeWAVMono48k(t *testing.T) {
	in := []int16{0, 16384, -16384, 32767}
	got, err := DecodeWAV(buildWAV(48000, 1, in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(in) {
		t.Fatalf("got %d samples, want %d", len(got), len(in))
	}
	if got[1] < 0.49 || got[1] > 0.51 {
		t.Fatalf("16384 decoded to %v, want ~0.5", got[1])
	}
}

func TestDecodeWAVDownmixesStereo(t *testing.T) {
	// Two frames: L/R pairs that average to 0 and to 0.5.
	in := []int16{16384, -16384, 16384, 16384}
	got, err := DecodeWAV(buildWAV(48000, 2, in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d samples, want 2 after downmix", len(got))
	}
	if got[0] < -0.01 || got[0] > 0.01 {
		t.Fatalf("opposed channels averaged to %v, want ~0", got[0])
	}
}

func TestDecodeWAVResamplesOffSpecRate(t *testing.T) {
	in := make([]int16, 24000) // 1 second at 24 kHz
	got, err := DecodeWAV(buildWAV(24000, 1, in))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(got); got < 47000 || got > 49000 {
		t.Fatalf("24 kHz second resampled to %d samples, want ~48000", got)
	}
}

func TestDecodeWAVRejectsTruncatedHeader(t *testing.T) {
	if _, err := DecodeWAV([]byte("RIFF")); err == nil {
		t.Fatal("truncated header decoded without error")
	}
}

func TestDecodeWAVRejectsNonRIFF(t *testing.T) {
	if _, err := DecodeWAV(bytes.Repeat([]byte{0}, 64)); err == nil {
		t.Fatal("non-RIFF bytes decoded without error")
	}
}

func TestDecodeWAVRejectsNonPCM(t *testing.T) {
	w := buildWAV(48000, 1, []int16{0, 1})
	w[20] = 3 // IEEE float format tag
	if _, err := DecodeWAV(w); err == nil {
		t.Fatal("non-PCM format decoded without error")
	}
}

// TestDecodeWAVSkipsUnknownChunkBetweenFmtAndData proves the chunk walker
// skips an unrecognized chunk (as real files do with LIST/INFO metadata)
// sitting between "fmt " and "data", and still finds the data chunk
// afterward. The chunk body is 3 bytes -- deliberately odd-sized -- so the
// RIFF word-alignment pad byte is actually exercised: without it, the walker
// would land one byte short of "data" and fail to find the chunk.
func TestDecodeWAVSkipsUnknownChunkBetweenFmtAndData(t *testing.T) {
	in := []int16{0, 16384, -16384, 32767}
	w := buildWAVWithChunk(48000, 1, in, "LIST", []byte("abc")) // odd-sized body
	got, err := DecodeWAV(w)
	if err != nil {
		t.Fatalf("unexpected error with LIST chunk present: %v", err)
	}
	if len(got) != len(in) {
		t.Fatalf("got %d samples, want %d", len(got), len(in))
	}
	if got[1] < 0.49 || got[1] > 0.51 {
		t.Fatalf("16384 decoded to %v, want ~0.5", got[1])
	}
}

// TestDecodeWAVTruncatesPartialTrailingFrame proves a multi-channel data
// chunk whose length is not an exact multiple of 2*channels is truncated to
// whole frames rather than panicking or reading garbage from a partial tail.
func TestDecodeWAVTruncatesPartialTrailingFrame(t *testing.T) {
	// Stereo (2 channels): 2 full frames (4 samples) plus one dangling
	// sample -- 5 int16s total, not a multiple of 2 channels.
	in := []int16{100, 200, 300, 400, 999}
	w := buildWAV(48000, 2, in)
	got, err := DecodeWAV(w)
	if err != nil {
		t.Fatalf("unexpected error decoding partial trailing frame: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d samples, want 2 whole frames (dangling sample truncated)", len(got))
	}
}

// TestDecodeWAVRejectsImplausibleSampleRate proves a WAV declaring an
// absurdly low sample rate is rejected at parse time rather than being
// handed to resampleLinear, where it would demand an allocation many orders
// of magnitude larger than the file (see wav.go's sample-rate bounds
// comment).
func TestDecodeWAVRejectsImplausibleSampleRate(t *testing.T) {
	w := buildWAV(1, 1, make([]int16, 500000))
	if _, err := DecodeWAV(w); err == nil {
		t.Fatal("sampleRate=1 decoded without error")
	}
}

// TestDecodeWAVAccepts44100 proves the sample-rate bound does not reject
// 44100 Hz -- the single most common sample rate in existence -- and that it
// resamples cleanly to 48 kHz like any other off-spec rate.
func TestDecodeWAVAccepts44100(t *testing.T) {
	in := make([]int16, 44100) // 1 second at 44.1 kHz
	got, err := DecodeWAV(buildWAV(44100, 1, in))
	if err != nil {
		t.Fatalf("44100 Hz file rejected: %v", err)
	}
	if got := len(got); got < 47000 || got > 49000 {
		t.Fatalf("44.1 kHz second resampled to %d samples, want ~48000", got)
	}
}
