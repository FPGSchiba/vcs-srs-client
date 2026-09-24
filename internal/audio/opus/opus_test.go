package opus

import (
	"math"
	"testing"
)

// sine fills buf with a 440 Hz tone so encode/decode has real signal to
// preserve. Silence would pass a round-trip test that a broken codec would
// also pass.
func sine(buf []float32) {
	for i := range buf {
		buf[i] = 0.5 * float32(math.Sin(2*math.Pi*440*float64(i)/float64(SampleRate)))
	}
}

func rms(buf []float32) float64 {
	var sum float64
	for _, s := range buf {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(buf)))
}

func TestEncodeDecodeRoundTripPreservesEnergy(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	enc, err := NewEncoder(48000)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()
	dec, err := NewDecoder()
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	in := make([]float32, FrameSamples)
	sine(in)
	pkt := make([]byte, MaxPacket)

	// Opus needs a few frames to converge; assert on a later one.
	var n int
	out := make([]float32, FrameSamples)
	for i := 0; i < 10; i++ {
		n, err = enc.Encode(in, pkt)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if err := dec.Decode(pkt[:n], out); err != nil {
			t.Fatalf("Decode: %v", err)
		}
	}

	if n <= 5 {
		t.Fatalf("encoded length %d is <= 5 bytes; the server drops those (DTX must be off)", n)
	}
	if n > MaxPacket {
		t.Fatalf("encoded length %d exceeds MaxPacket %d", n, MaxPacket)
	}
	inRMS, outRMS := rms(in), rms(out)
	if outRMS < inRMS*0.5 || outRMS > inRMS*1.5 {
		t.Fatalf("round-trip RMS %.4f not within 50%% of input %.4f", outRMS, inRMS)
	}
}

func TestDecodeNilPayloadDoesPacketLossConcealment(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	dec, err := NewDecoder()
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()
	out := make([]float32, FrameSamples)
	if err := dec.Decode(nil, out); err != nil {
		t.Fatalf("PLC decode: %v", err)
	}
}

func TestEncodeRejectsWrongFrameLength(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	enc, err := NewEncoder(48000)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()
	if _, err := enc.Encode(make([]float32, 480), make([]byte, MaxPacket)); err == nil {
		t.Fatal("expected an error for a 480-sample frame; only 960 is valid")
	}
}
