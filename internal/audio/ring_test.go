package audio

import (
	"sync"
	"testing"
)

func TestRingWriteReadRoundTrip(t *testing.T) {
	r := NewRing(4) // 4 frames of capacity
	in := make([]float32, FrameSamples)
	for i := range in {
		in[i] = float32(i)
	}
	if dropped := r.Write(in); dropped != 0 {
		t.Fatalf("Write dropped %d samples into an empty ring", dropped)
	}
	out := make([]float32, FrameSamples)
	if n := r.Read(out); n != FrameSamples {
		t.Fatalf("Read returned %d samples, want %d", n, FrameSamples)
	}
	for i := range out {
		if out[i] != in[i] {
			t.Fatalf("sample %d = %v, want %v", i, out[i], in[i])
		}
	}
}

// TestRingOverflowDropsNewestAndCounts covers overflow under the strict
// index-ownership design: a Write that doesn't fit is refused whole (not
// partially copied), the dropped counter advances by the refused amount,
// and -- the assertion that would have caught the old lost-update/torn-
// buffer bug -- the frames already buffered before the overflowing Write
// come back out fully intact.
func TestRingOverflowDropsNewestAndCounts(t *testing.T) {
	r := NewRing(2)
	frame0 := make([]float32, FrameSamples)
	frame1 := make([]float32, FrameSamples)
	for i := range frame0 {
		frame0[i] = 1
		frame1[i] = 2
	}
	if dropped := r.Write(frame0); dropped != 0 {
		t.Fatalf("Write(frame0) dropped %d, want 0", dropped)
	}
	if dropped := r.Write(frame1); dropped != 0 {
		t.Fatalf("Write(frame1) dropped %d, want 0", dropped)
	}

	overflow := make([]float32, FrameSamples)
	for i := range overflow {
		overflow[i] = 3
	}
	dropped := r.Write(overflow)
	if dropped == 0 {
		t.Fatal("Write did not report any dropped samples when the ring was full")
	}
	if got := r.Dropped(); got != uint64(dropped) {
		t.Fatalf("Dropped() = %d, want %d", got, dropped)
	}

	// The two frames buffered before the overflow must still be there,
	// untouched and intact -- not overwritten, not torn.
	out := make([]float32, FrameSamples)
	if n := r.Read(out); n != FrameSamples {
		t.Fatalf("Read #1 returned %d, want %d", n, FrameSamples)
	}
	for i, v := range out {
		if v != 1 {
			t.Fatalf("frame0 sample %d = %v, want 1 (buffered frame was corrupted)", i, v)
		}
	}
	if n := r.Read(out); n != FrameSamples {
		t.Fatalf("Read #2 returned %d, want %d", n, FrameSamples)
	}
	for i, v := range out {
		if v != 2 {
			t.Fatalf("frame1 sample %d = %v, want 2 (buffered frame was corrupted)", i, v)
		}
	}
}

func TestRingUnderrunCounts(t *testing.T) {
	r := NewRing(2)
	out := make([]float32, FrameSamples)
	if n := r.Read(out); n != 0 {
		t.Fatalf("Read on an empty ring returned %d, want 0", n)
	}
	if r.Underruns() != 1 {
		t.Fatalf("Underruns() = %d, want 1", r.Underruns())
	}
}

// TestRingDrain verifies that Drain resets the backlog: after filling the
// ring, Drain must advance r to w so the buffered-but-unread count is zero
// and the next Read reports empty.
func TestRingDrain(t *testing.T) {
	r := NewRing(2)
	frame := make([]float32, FrameSamples)
	r.Write(frame)
	r.Write(frame)

	r.Drain()

	if backlog := r.w.Load() - r.r.Load(); backlog != 0 {
		t.Fatalf("w-r = %d after Drain, want 0", backlog)
	}
	out := make([]float32, FrameSamples)
	if n := r.Read(out); n != 0 {
		t.Fatalf("Read after Drain returned %d, want 0", n)
	}
}

func TestRingConcurrentProducerConsumer(t *testing.T) {
	r := NewRing(8)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		f := make([]float32, FrameSamples)
		for i := 0; i < 1000; i++ {
			r.Write(f)
		}
	}()
	go func() {
		defer wg.Done()
		f := make([]float32, FrameSamples)
		for i := 0; i < 1000; i++ {
			r.Read(f)
		}
	}()
	wg.Wait()
}

// TestRingConcurrentProducerConsumerFramesStayIntact runs a producer and a
// consumer concurrently for enough iterations to force many overflows, and
// asserts every frame the consumer reads is internally self-consistent:
// every sample in the frame written for iteration N equals N (mod a small
// period, so values repeat and stay easy to compare). A torn frame -- part
// written by one Write, a stale leftover from a previous one, or a slot
// being overwritten mid-Read -- is exactly what the old dual-Store,
// producer-forces-r design could produce. This test would have failed
// against that code; it passes against strict index ownership.
func TestRingConcurrentProducerConsumerFramesStayIntact(t *testing.T) {
	r := NewRing(4)
	const iterations = 20000

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		f := make([]float32, FrameSamples)
		for i := 0; i < iterations; i++ {
			v := float32(i % 997)
			for j := range f {
				f[j] = v
			}
			r.Write(f)
		}
	}()

	go func() {
		defer wg.Done()
		out := make([]float32, FrameSamples)
		for i := 0; i < iterations; i++ {
			n := r.Read(out)
			if n == 0 {
				continue
			}
			first := out[0]
			for j := 1; j < n; j++ {
				if out[j] != first {
					t.Errorf("torn frame: sample %d = %v, want %v (same as sample 0)", j, out[j], first)
					return
				}
			}
		}
	}()

	wg.Wait()
}
