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

func TestRingOverflowDropsOldestAndCounts(t *testing.T) {
	r := NewRing(2)
	frame := make([]float32, FrameSamples)
	r.Write(frame)
	r.Write(frame)
	// Third frame overflows a 2-frame ring.
	r.Write(frame)
	if r.Dropped() == 0 {
		t.Fatal("Dropped() == 0 after overflowing the ring")
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
