package loadgen

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
)

// fakeSender records the arrival time of every frame.
type fakeSender struct {
	mu sync.Mutex
	at []time.Time
}

func (f *fakeSender) Send(seq uint32, freq voice.KHz, payload []byte) error {
	f.mu.Lock()
	f.at = append(f.at, time.Now())
	f.mu.Unlock()
	return nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.at)
}

func TestTalkerPauseResumeNoBurst(t *testing.T) {
	var (
		seq  atomic.Uint32
		errs atomic.Uint64
		wh   Hist
	)
	seq.Store(1)
	fs := &fakeSender{}
	tk := startTalkerLoop(fs, 0, &seq, &wh, &errs, false)
	defer tk.stop()

	time.Sleep(110 * time.Millisecond)
	tk.pause()
	paused := fs.count()
	if paused < 3 {
		t.Fatalf("only %d frames before the pause", paused)
	}
	wh.Reset()
	pausedSeq := seq.Load()

	// Nothing is sent while paused, and the stall is long enough that a
	// catch-up would owe many frames.
	time.Sleep(300 * time.Millisecond)
	if got := fs.count(); got != paused {
		t.Fatalf("%d frames were sent while paused", got-paused)
	}
	if seq.Load() != pausedSeq {
		t.Fatalf("seq advanced while paused: %d -> %d", pausedSeq, seq.Load())
	}

	tk.resume()
	resumedAt := time.Now()
	time.Sleep(110 * time.Millisecond)
	tk.pause()
	fs.mu.Lock()
	var post []time.Time
	for _, a := range fs.at[paused:] {
		post = append(post, a)
	}
	fs.mu.Unlock()
	// 110 ms at 50 fps is at most 6 frames. A catch-up of the 300 ms stall
	// would add 15 more.
	if len(post) < 3 || len(post) > 7 {
		t.Fatalf("%d frames in 110 ms after resume, want 3..7 (no catch-up burst)", len(post))
	}
	if d := post[0].Sub(resumedAt); d > 15*time.Millisecond {
		t.Fatalf("first frame came %v after resume, want it due at once", d)
	}
	for i := 1; i < len(post); i++ {
		if gap := post[i].Sub(post[i-1]); gap < 10*time.Millisecond {
			t.Fatalf("frames %d and %d only %v apart: a burst", i-1, i, gap)
		}
	}
	// The wheel has no sample from the paused period: no lag near 300 ms.
	if p := wh.Quantile(1); p > 50*time.Millisecond {
		t.Fatalf("wheel max lag %v: the paused period was recorded", p)
	}
}

func TestTalkerStartsPaused(t *testing.T) {
	var (
		seq  atomic.Uint32
		errs atomic.Uint64
		wh   Hist
	)
	seq.Store(1)
	fs := &fakeSender{}
	tk := startTalkerLoop(fs, 0, &seq, &wh, &errs, true)
	defer tk.stop()
	time.Sleep(100 * time.Millisecond)
	if fs.count() != 0 {
		t.Fatalf("%d frames from a talker started paused", fs.count())
	}
	tk.resume()
	time.Sleep(60 * time.Millisecond)
	if fs.count() == 0 {
		t.Fatal("no frame after resume")
	}
}

func TestTalkerPauseAfterStopDoesNotBlock(t *testing.T) {
	var (
		seq  atomic.Uint32
		errs atomic.Uint64
		wh   Hist
	)
	tk := startTalkerLoop(&fakeSender{}, 0, &seq, &wh, &errs, false)
	tk.stop()
	done := make(chan struct{})
	go func() { tk.pause(); tk.resume(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pause/resume blocked on a stopped talker")
	}
}
