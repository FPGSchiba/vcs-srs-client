package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildWAV assembles a minimal 16-bit PCM RIFF file.
func buildWAV(sampleRate int, channels int, samples []int16) []byte {
	var b bytes.Buffer
	dataLen := len(samples) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate*channels*2))
	binary.Write(&b, binary.LittleEndian, uint16(channels*2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
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
