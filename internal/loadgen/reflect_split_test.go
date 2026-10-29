package loadgen

import (
	"context"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
)

// A full sender queue is counted apart from failed socket writes, and the
// legacy total stays their sum.
func TestReflectorCountsQueueFullSeparately(t *testing.T) {
	r, err := NewReflector("127.0.0.1:0", 2)
	if err != nil {
		t.Fatal(err)
	}
	r.jobDepth = 1
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	const freq = voice.KHz(251000)
	var bots []*VoiceConn
	for i := 0; i < 40; i++ {
		v, err := DialVoice(r.Addr(), DeterministicUUID(3, i))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(v.Close)
		hctx, hcancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = v.Hello(hctx, "", DefaultHelloLadder)
		hcancel()
		if err != nil {
			t.Fatal(err)
		}
		bots = append(bots, v)
	}
	deadline := time.Now().Add(10 * time.Second)
	for seq := uint32(1); r.QueueFullDrops() == 0 && time.Now().Before(deadline); seq++ {
		_ = bots[0].Send(seq, freq, SyntheticFrame(seq))
	}
	if r.QueueFullDrops() == 0 {
		t.Fatal("QueueFullDrops() = 0 after flooding a depth-1 queue")
	}
	// Let the in-flight fan-out settle: counters move until the queues drain.
	for prev := ^uint64(0); prev != r.SendErrors(); {
		prev = r.SendErrors()
		time.Sleep(200 * time.Millisecond)
	}
	if r.WriteErrors() != 0 {
		t.Errorf("WriteErrors() = %d, want 0 (no socket write failed)", r.WriteErrors())
	}
	if got := r.SendErrors(); got != r.QueueFullDrops()+r.WriteErrors() {
		t.Errorf("SendErrors() = %d, want QueueFullDrops()+WriteErrors() = %d", got, r.QueueFullDrops()+r.WriteErrors())
	}
}
