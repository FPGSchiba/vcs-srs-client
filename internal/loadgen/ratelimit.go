package loadgen

import (
	"context"
	"sync"
	"time"
)

// RateLimiter admits joins through a token bucket that holds at most one
// token, so there is no burst credit: after any idle period the next join
// is free and the one after waits a full period.
//
// The refill rate is r = min(Max, Scale/max(1, alive)), where a term that is
// 0 is left out; both 0 means unbounded and Wait returns at once. The rate
// is evaluated at each admission with the caller's current alive count, so
// it falls as the net grows.
type RateLimiter struct {
	Max, Scale float64

	mu    sync.Mutex
	next  time.Time // earliest time the next join may be admitted
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// Rate returns the joins per second allowed at the given alive count, or 0
// when the limiter is unbounded.
func (l *RateLimiter) Rate(alive int) float64 {
	var r float64
	if l.Max > 0 {
		r = l.Max
	}
	if l.Scale > 0 {
		if alive < 1 {
			alive = 1
		}
		if s := l.Scale / float64(alive); r == 0 || s < r {
			r = s
		}
	}
	return r
}

// Wait blocks until a join may start, or returns ctx.Err() when ctx ends
// first. Concurrent callers are served in the order they reserve a slot.
func (l *RateLimiter) Wait(ctx context.Context, alive int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r := l.Rate(alive)
	if r <= 0 {
		return nil
	}
	nowf := l.now
	if nowf == nil {
		nowf = time.Now
	}
	l.mu.Lock()
	admit := nowf()
	if l.next.After(admit) {
		admit = l.next
	}
	l.next = admit.Add(time.Duration(float64(time.Second) / r))
	wait := admit.Sub(nowf())
	l.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	if l.sleep != nil {
		return l.sleep(ctx, wait)
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
