package audio

import "testing"

// sequentialSamples returns n float32 samples with distinct, ordered values
// (0, 1, 2, ...) so a test can detect loss, duplication or reordering by
// simple value comparison.
func sequentialSamples(start, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(start + i)
	}
	return out
}

// TestFrameAccumulatorEmitsCompleteFramesInOrder drives push with the
// mismatched chunk lengths named in the review finding (200, 480, 1000,
// 137) and asserts every sample that can form a complete frame reaches
// onFrame exactly once, in order, in frameSize-sized frames -- proving the
// accumulator adapts to a hint that miniaudio didn't honor instead of
// silently discarding or corrupting data.
func TestFrameAccumulatorEmitsCompleteFramesInOrder(t *testing.T) {
	const frameSize = FrameSamples
	chunkLens := []int{200, 480, 1000, 137}

	var pushed []float32
	var emitted []float32
	var frameCount int

	acc := newFrameAccumulator(frameSize, func(f []float32) {
		frameCount++
		if len(f) != frameSize {
			t.Fatalf("onFrame got %d samples, want %d", len(f), frameSize)
		}
		emitted = append(emitted, append([]float32(nil), f...)...)
	})

	total := 0
	for _, n := range chunkLens {
		chunk := sequentialSamples(total, n)
		pushed = append(pushed, chunk...)
		acc.push(chunk)
		total += n
	}

	wantFrames := total / frameSize
	if frameCount != wantFrames {
		t.Fatalf("emitted %d frames, want %d (total pushed samples = %d)", frameCount, wantFrames, total)
	}

	wantEmitted := pushed[:wantFrames*frameSize]
	if len(emitted) != len(wantEmitted) {
		t.Fatalf("emitted %d samples, want %d", len(emitted), len(wantEmitted))
	}
	for i := range wantEmitted {
		if emitted[i] != wantEmitted[i] {
			t.Fatalf("sample %d = %v, want %v (order/loss/duplication mismatch)", i, emitted[i], wantEmitted[i])
		}
	}
}

// TestFrameAccumulatorCarriesRemainderAcrossPushes proves a partial frame
// left over from one push is combined with the next push rather than
// dropped or emitted short.
func TestFrameAccumulatorCarriesRemainderAcrossPushes(t *testing.T) {
	const frameSize = FrameSamples
	var emitted []float32
	acc := newFrameAccumulator(frameSize, func(f []float32) {
		emitted = append(emitted, append([]float32(nil), f...)...)
	})

	first := sequentialSamples(0, frameSize-10) // 10 short of a frame
	acc.push(first)
	if len(emitted) != 0 {
		t.Fatalf("emitted %d samples before a full frame arrived, want 0", len(emitted))
	}

	second := sequentialSamples(frameSize-10, 10) // exactly completes the frame
	acc.push(second)
	if len(emitted) != frameSize {
		t.Fatalf("emitted %d samples, want exactly one frame (%d)", len(emitted), frameSize)
	}
	want := sequentialSamples(0, frameSize)
	for i := range want {
		if emitted[i] != want[i] {
			t.Fatalf("sample %d = %v, want %v", i, emitted[i], want[i])
		}
	}
}

// TestFrameFillerServesArbitraryLengthsFromFixedFrames proves pull always
// writes the entire requested length, in order, sourcing frameSize-sized
// frames from fill on demand -- covering the playback side of the same
// period-size mismatch.
func TestFrameFillerServesArbitraryLengthsFromFixedFrames(t *testing.T) {
	const frameSize = FrameSamples
	pullLens := []int{200, 480, 1000, 137}

	next := 0
	filler := newFrameFiller(frameSize, func(f []float32) {
		for i := range f {
			f[i] = float32(next + i)
		}
		next += frameSize
	})

	var got []float32
	for _, n := range pullLens {
		out := make([]float32, n)
		filler.pull(out)
		if len(out) != n {
			t.Fatalf("pull(out) of len %d produced %d samples", n, len(out))
		}
		got = append(got, out...)
	}

	want := sequentialSamples(0, len(got))
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %v, want %v (order/loss/duplication mismatch)", i, got[i], want[i])
		}
	}
}
