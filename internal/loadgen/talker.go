package loadgen

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
)

// frameSender is the part of VoiceConn the talker uses.
type frameSender interface {
	Send(seq uint32, freq voice.KHz, payload []byte) error
}

// talkerLoop sends SyntheticFrame at 50 fps on an absolute schedule: tick k
// is due at t0 + k*20ms, never an accumulating ticker. After a stall the loop
// does not skip or reschedule: it sends the missed frames back to back (open
// loop), and the lag is recorded against each due time, so no coordinated
// omission. The burst can distort loss and burst figures; the D14
// self-health set exists to flag such a stall.
//
// pause and resume are the exception, and a deliberate one: a paused talker
// owes nothing. resume restarts the schedule at time.Now() with k = 0, so the
// first frame is due at once and then every 20 ms, with no catch-up burst
// and no wheel sample for the paused period. The sequence counter is shared
// and is not advanced while paused.
type talkerLoop struct {
	cancel context.CancelFunc
	done   chan struct{}
	ctrl   chan bool // true = pause, false = resume; unbuffered, so synchronous
	once   sync.Once
}

func startTalkerLoop(s frameSender, freq voice.KHz, seq *atomic.Uint32, wheel *Hist, sendErrors *atomic.Uint64, paused bool) *talkerLoop {
	ctx, cancel := context.WithCancel(context.Background())
	t := &talkerLoop{cancel: cancel, done: make(chan struct{}), ctrl: make(chan bool)}
	go func() {
		defer close(t.done)
		var t0 time.Time
		k := 0
		if !paused {
			t0 = time.Now()
		}
		for {
			if paused {
				select {
				case <-ctx.Done():
					return
				case p := <-t.ctrl:
					paused = p
					if !paused {
						t0, k = time.Now(), 0
					}
				}
				continue
			}
			due := t0.Add(time.Duration(k) * frameInterval)
			if d := time.Until(due); d > 0 {
				timer := time.NewTimer(d)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case p := <-t.ctrl:
					timer.Stop()
					paused = p
					if !paused {
						t0, k = time.Now(), 0
					}
					continue
				case <-timer.C:
				}
			}
			lag := time.Since(due)
			if lag < 0 {
				lag = 0
			}
			wheel.Record(lag)
			n := seq.Load()
			if err := s.Send(n, freq, SyntheticFrame(n)); err != nil {
				sendErrors.Add(1)
			}
			seq.Store(n + 1)
			k++
		}
	}()
	return t
}

func (t *talkerLoop) set(p bool) {
	select {
	case t.ctrl <- p:
	case <-t.done:
	}
}

// pause stops sending. It returns once the loop has taken the request, so no
// frame is sent after it returns.
func (t *talkerLoop) pause() { t.set(true) }

// resume restarts the schedule at now.
func (t *talkerLoop) resume() { t.set(false) }

// stop ends the loop and waits for it.
func (t *talkerLoop) stop() {
	t.once.Do(t.cancel)
	<-t.done
}
