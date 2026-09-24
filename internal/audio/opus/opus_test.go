package opus

import (
	"math"
	"sync"
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

// TestEncoderCloseRacesEncode reproduces the finding: st is a raw C pointer
// read by Encode and freed by Close. Without a lock held across the whole C
// call, a Close landing between Encode's nil check and its use of st runs
// opus_encode_float on freed memory -- a use-after-free, not just a Go data
// race. Each iteration creates a fresh encoder and races one Encode against
// one Close; run with -race, and it must neither report a race nor crash.
func TestEncoderCloseRacesEncode(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	in := make([]float32, FrameSamples)
	sine(in)

	const iterations = 200
	for i := 0; i < iterations; i++ {
		enc, err := NewEncoder(48000)
		if err != nil {
			t.Fatalf("NewEncoder: %v", err)
		}
		pkt := make([]byte, MaxPacket)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := enc.Encode(in, pkt); err != nil && err != errEncoderClosed {
				t.Errorf("Encode: unexpected error: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			enc.Close()
		}()
		wg.Wait()
		enc.Close() // trailing Close must remain safe too
	}
}

// TestEncoderDoubleCloseSafe asserts concurrent Close calls never double-free
// the underlying C pointer, and that Encode after Close returns the clean
// errEncoderClosed rather than touching freed memory.
func TestEncoderDoubleCloseSafe(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	enc, err := NewEncoder(48000)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enc.Close()
		}()
	}
	wg.Wait()

	if _, err := enc.Encode(make([]float32, FrameSamples), make([]byte, MaxPacket)); err != errEncoderClosed {
		t.Fatalf("Encode after Close: got %v, want errEncoderClosed", err)
	}
}

// TestDecoderCloseRacesDecode mirrors TestEncoderCloseRacesEncode for the
// decoder, which has the identical st-pointer shape.
func TestDecoderCloseRacesDecode(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	const iterations = 200
	for i := 0; i < iterations; i++ {
		dec, err := NewDecoder()
		if err != nil {
			t.Fatalf("NewDecoder: %v", err)
		}
		out := make([]float32, FrameSamples)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			// A nil payload requests packet-loss concealment, so this
			// exercises the real decode path without needing an encoder.
			if err := dec.Decode(nil, out); err != nil && err != errDecoderClosed {
				t.Errorf("Decode: unexpected error: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			dec.Close()
		}()
		wg.Wait()
		dec.Close()
	}
}

// TestDecoderDoubleCloseSafe mirrors TestEncoderDoubleCloseSafe for the
// decoder.
func TestDecoderDoubleCloseSafe(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	dec, err := NewDecoder()
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dec.Close()
		}()
	}
	wg.Wait()

	if err := dec.Decode(nil, make([]float32, FrameSamples)); err != errDecoderClosed {
		t.Fatalf("Decode after Close: got %v, want errDecoderClosed", err)
	}
}

// TestEncoderWireContractCTLs reads back, from the live encoder, every CTL
// that forms the wire contract with the C# peer and the server. These are
// not tuning knobs -- each one is load-bearing, and a regression in any of
// them is invisible to the round-trip test above (a loud tone encodes to the
// same frame size whether DTX is on or off):
//
//   - DTX must read back 0. Opus's discontinuous-transmission mode emits
//     1-2 byte frames during silence; the server silently drops any voice
//     payload of 5 bytes or fewer, so DTX re-enabling itself presents as
//     speech that cuts out only during pauses -- the exact failure mode
//     this codebase has already hit in production once.
//   - Inband FEC must read back 0. The C# peer does not expect
//     FEC-augmented packets; turning it on changes the bitstream framing
//     the peer parses, not just quality.
//   - Bitrate must read back the value NewEncoder was given (48000 here),
//     confirming the setter call actually took effect on the encoder
//     rather than silently no-oping.
//   - Application must read back OPUS_APPLICATION_AUDIO (2049), matching
//     the C# peer's OpusEncoder.Create(48000, 1, Application.Audio)
//     exactly. A mismatched application retunes the codec internally
//     enough that the two ends are no longer bit-compatible.
//
// Do not "optimise" any of these values without updating the peer and the
// server in lockstep -- that is the entire point of this test existing.
func TestEncoderWireContractCTLs(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	enc, err := NewEncoder(48000)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	cases := []struct {
		name    string
		request int
		want    int32
	}{
		{"DTX", ctlGetDTX, 0},
		{"InbandFEC", ctlGetInbandFEC, 0},
		{"Bitrate", ctlGetBitrate, 48000},
		{"Application", ctlGetApplication, int32(ApplicationAudio)},
	}
	for _, c := range cases {
		got, err := enc.ctlGet(c.request)
		if err != nil {
			t.Fatalf("ctlGet(%s): %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s = %d, want %d", c.name, got, c.want)
		}
	}
}
