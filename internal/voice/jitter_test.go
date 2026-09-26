package voice

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

// Fixed constants shared by every subtest. No test in this file calls
// time.Sleep or time.Now -- every instant is built from base by explicit
// Add() so priming, gap-concealment and eviction deadlines are exact and
// reproducible.
const (
	testTarget   = 60 * time.Millisecond
	testMax      = 500 * time.Millisecond
	testFrameDur = 20 * time.Millisecond
)

func TestJitter(t *testing.T) {
	base := time.Unix(0, 0)

	t.Run("primes before playing", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		if !j.push(1, []byte{0xAB}, base) {
			t.Fatalf("push(1) = false, want true")
		}

		// Same instant as the push: priming has not had time to elapse.
		if _, _, ok := j.pop(base); ok {
			t.Fatalf("pop at push instant: ok = true, want false (not primed yet)")
		}

		// Exactly `target` later: priming has elapsed and playout starts.
		payload, lost, ok := j.pop(base.Add(testTarget))
		if !ok {
			t.Fatalf("pop after target: ok = false, want true")
		}
		if lost {
			t.Fatalf("pop after target: lost = true, want false")
		}
		if !bytes.Equal(payload, []byte{0xAB}) {
			t.Fatalf("pop after target: payload = %v, want [0xAB]", payload)
		}
	})

	t.Run("reorders", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		if !j.push(3, []byte{3}, base) {
			t.Fatalf("push(3) = false, want true")
		}
		if !j.push(1, []byte{1}, base.Add(5*time.Millisecond)) {
			t.Fatalf("push(1) = false, want true")
		}
		if !j.push(2, []byte{2}, base.Add(10*time.Millisecond)) {
			t.Fatalf("push(2) = false, want true")
		}

		now := base.Add(testTarget)
		for _, want := range [][]byte{{1}, {2}, {3}} {
			payload, lost, ok := j.pop(now)
			if !ok {
				t.Fatalf("pop: ok = false, want true (expecting seq for payload %v)", want)
			}
			if lost {
				t.Fatalf("pop: lost = true, want false (expecting payload %v)", want)
			}
			if !bytes.Equal(payload, want) {
				t.Fatalf("pop: payload = %v, want %v", payload, want)
			}
		}
	})

	t.Run("drops duplicates", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		if !j.push(1, []byte{1}, base) {
			t.Fatalf("first push(1) = false, want true")
		}
		if j.push(1, []byte{1, 1}, base.Add(time.Millisecond)) {
			t.Fatalf("second push(1) = true, want false (duplicate)")
		}
		if got := j.depth(); got != 1 {
			t.Fatalf("depth() = %d, want 1", got)
		}
	})

	t.Run("conceals a gap", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		if !j.push(1, []byte{1}, base) {
			t.Fatalf("push(1) = false, want true")
		}
		if !j.push(3, []byte{3}, base.Add(20*time.Millisecond)) {
			t.Fatalf("push(3) = false, want true")
		}

		// Prime: nextExpected becomes 1, playout deadline for seq 1 is
		// base+target, deadline for seq 2 will be base+target+frameDur.
		payload, lost, ok := j.pop(base.Add(testTarget))
		if !ok || lost || !bytes.Equal(payload, []byte{1}) {
			t.Fatalf("pop seq 1: got (%v, lost=%v, ok=%v), want ([1], false, true)", payload, lost, ok)
		}

		// Before seq 2's playout deadline: a genuinely reordered seq 2
		// still has time to arrive, so the gap must NOT be conceded yet.
		beforeDeadline := base.Add(testTarget).Add(testFrameDur - time.Millisecond)
		if _, _, ok := j.pop(beforeDeadline); ok {
			t.Fatalf("pop before seq 2 deadline: ok = true, want false (must wait out the budget)")
		}

		// At seq 2's playout deadline, with seq 3 already waiting: the gap
		// is conceded and concealed.
		atDeadline := base.Add(testTarget).Add(testFrameDur)
		payload, lost, ok = j.pop(atDeadline)
		if !ok {
			t.Fatalf("pop at seq 2 deadline: ok = false, want true")
		}
		if !lost {
			t.Fatalf("pop at seq 2 deadline: lost = false, want true")
		}
		if payload != nil {
			t.Fatalf("pop at seq 2 deadline: payload = %v, want nil", payload)
		}

		// Then seq 3 plays normally.
		payload, lost, ok = j.pop(atDeadline)
		if !ok || lost || !bytes.Equal(payload, []byte{3}) {
			t.Fatalf("pop seq 3: got (%v, lost=%v, ok=%v), want ([3], false, true)", payload, lost, ok)
		}
	})

	t.Run("ignores a late arrival", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		if !j.push(1, []byte{1}, base) {
			t.Fatalf("push(1) = false, want true")
		}
		if !j.push(2, []byte{2}, base.Add(5*time.Millisecond)) {
			t.Fatalf("push(2) = false, want true")
		}

		if _, _, ok := j.pop(base.Add(testTarget)); !ok {
			t.Fatalf("pop seq 1: ok = false, want true")
		}
		if _, _, ok := j.pop(base.Add(testTarget)); !ok {
			t.Fatalf("pop seq 2: ok = false, want true")
		}

		if j.push(1, []byte{1, 2, 3}, base.Add(90*time.Millisecond)) {
			t.Fatalf("push(1) after it already played = true, want false (late arrival refused)")
		}
	})

	t.Run("bounds depth", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		const pushCount = 40
		for seq := 0; seq < pushCount; seq++ {
			now := base.Add(time.Duration(seq) * testFrameDur)
			if !j.push(uint32(seq), []byte{byte(seq)}, now) {
				t.Fatalf("push(%d) = false, want true", seq)
			}
			if depth := j.depth(); depth > 25 {
				t.Fatalf("after push(%d): depth() = %d, want <= 25", seq, depth)
			}
		}

		if got := j.depth(); got != 25 {
			t.Fatalf("final depth() = %d, want 25", got)
		}

		// The 15 oldest (seq 0..14) must have been discarded, leaving
		// 15..39. Prime and pop once to confirm the survivor at the front
		// is seq 15, not seq 0.
		payload, lost, ok := j.pop(base.Add(time.Duration(pushCount) * testFrameDur).Add(testTarget))
		if !ok || lost {
			t.Fatalf("pop after priming: got (%v, lost=%v, ok=%v)", payload, lost, ok)
		}
		if !bytes.Equal(payload, []byte{15}) {
			t.Fatalf("first surviving payload = %v, want [15] (oldest 0..14 should have been discarded)", payload)
		}
		if got := j.depth(); got != 24 {
			t.Fatalf("depth() after one pop = %d, want 24", got)
		}
	})

	t.Run("wraps at 2^24", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		if !j.push(0xFFFFFE, []byte{0xFE}, base) {
			t.Fatalf("push(0xFFFFFE) = false, want true")
		}
		if !j.push(0xFFFFFF, []byte{0xFF}, base.Add(5*time.Millisecond)) {
			t.Fatalf("push(0xFFFFFF) = false, want true")
		}
		if !j.push(0x000000, []byte{0x00}, base.Add(10*time.Millisecond)) {
			t.Fatalf("push(0x000000) = false, want true")
		}
		if !j.push(0x000001, []byte{0x01}, base.Add(15*time.Millisecond)) {
			t.Fatalf("push(0x000001) = false, want true")
		}

		now := base.Add(testTarget)
		for _, want := range [][]byte{{0xFE}, {0xFF}, {0x00}, {0x01}} {
			payload, lost, ok := j.pop(now)
			if !ok {
				t.Fatalf("pop: ok = false, want true (expecting payload %v)", want)
			}
			if lost {
				t.Fatalf("pop: lost = true, want false (expecting payload %v)", want)
			}
			if !bytes.Equal(payload, want) {
				t.Fatalf("pop: payload = %v, want %v -- 0x000000 must sort AFTER 0xFFFFFF, not before 0xFFFFFE", payload, want)
			}
		}
	})

	// Regression test for Finding 1: a total empty-buffer underrun must not
	// permanently collapse the grace window. After recovery, an ordinary
	// in-budget reorder must still be absorbed, not conceded and concealed.
	t.Run("resyncs the playout schedule after a total underrun, preserving grace for a later reorder", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		if !j.push(1, []byte{1}, base) {
			t.Fatalf("push(1) = false, want true")
		}

		// Prime and play seq 1. nextExpected becomes 2, playoutBase becomes
		// base+target+frameDur.
		payload, lost, ok := j.pop(base.Add(testTarget))
		if !ok || lost || !bytes.Equal(payload, []byte{1}) {
			t.Fatalf("pop seq 1: got (%v, lost=%v, ok=%v), want ([1], false, true)", payload, lost, ok)
		}

		// A total outage: the buffer goes empty and stays empty far longer
		// than one frameDur -- e.g. a radio dropping out of range for a
		// couple hundred ms. dspLoop's ticker (the only clock, per D8) keeps
		// calling pop on schedule throughout, observing the empty buffer.
		outageEnd := base.Add(testTarget).Add(300 * time.Millisecond)
		if _, _, ok := j.pop(outageEnd); ok {
			t.Fatalf("pop during outage: ok = true, want false (empty-buffer underrun)")
		}

		// Traffic resumes, but seq 3 -- an ordinary, in-budget reorder ahead
		// of the still-missing seq 2 -- arrives first.
		resumeAt := outageEnd.Add(5 * time.Millisecond)
		if !j.push(3, []byte{3}, resumeAt) {
			t.Fatalf("push(3) = false, want true")
		}

		// A pop shortly after recovery, with seq 2 still missing. Without
		// the resync, playoutBase would still be anchored at
		// base+target+frameDur -- hundreds of ms in the past relative to
		// resumeAt -- so the grace check would already be false and this
		// call would wrongly concede seq 2 as lost right away. With the
		// resync, seq 2 still gets a frameDur's grace from the moment
		// traffic resumed, so this must NOT concede loss yet.
		soonAfterResume := resumeAt.Add(time.Millisecond)
		if _, _, ok := j.pop(soonAfterResume); ok {
			t.Fatalf("pop just after resync: ok = true, want false (seq 2 must still get its grace window)")
		}

		// seq 2 arrives, still within its (resynced) grace budget.
		if !j.push(2, []byte{2}, soonAfterResume.Add(time.Millisecond)) {
			t.Fatalf("push(2) = false, want true")
		}

		// It must be handed out in order, not concealed.
		payload, lost, ok = j.pop(soonAfterResume.Add(2 * time.Millisecond))
		if !ok || lost || !bytes.Equal(payload, []byte{2}) {
			t.Fatalf("pop seq 2: got (%v, lost=%v, ok=%v), want ([2], false, true) -- the reorder should have been absorbed, not concealed", payload, lost, ok)
		}

		// And seq 3 follows normally.
		payload, lost, ok = j.pop(soonAfterResume.Add(2 * time.Millisecond))
		if !ok || lost || !bytes.Equal(payload, []byte{3}) {
			t.Fatalf("pop seq 3: got (%v, lost=%v, ok=%v), want ([3], false, true)", payload, lost, ok)
		}
	})

	// Regression test for Finding 2: push must reject a sequence implausibly
	// far ahead of its reference point, both before and after priming.
	t.Run("rejects a sequence implausibly far ahead", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		if !j.push(1, []byte{1}, base) {
			t.Fatalf("push(1) = false, want true")
		}

		// Just under half the 24-bit sequence space ahead: seqDiff/seqLess
		// and oldestSeqLocked's pairwise minimum are only well-defined
		// within 2^23 of the reference sequence (see seqDiff's doc
		// comment), and 2^23-1 is the largest offset seqDiff still resolves
		// unambiguously as "ahead" rather than sign-flipping to "behind". A
		// sequence this far out must be rejected outright instead of being
		// admitted and corrupting the wraparound ordering.
		farAhead := uint32(1) + (1<<23 - 1)
		if j.push(farAhead, []byte{0xFF}, base.Add(time.Millisecond)) {
			t.Fatalf("push(%d) = true, want false (implausibly far ahead, unprimed)", farAhead)
		}
		if got := j.depth(); got != 1 {
			t.Fatalf("depth() = %d, want 1 (far-ahead push must not have been admitted)", got)
		}

		// An ordinary within-budget push must still be accepted.
		if !j.push(2, []byte{2}, base.Add(2*time.Millisecond)) {
			t.Fatalf("push(2) = false, want true")
		}

		// Prime and play seq 1, moving nextExpected to 2, then repeat the
		// far-ahead check against the primed (nextExpected-relative) branch.
		payload, lost, ok := j.pop(base.Add(testTarget))
		if !ok || lost || !bytes.Equal(payload, []byte{1}) {
			t.Fatalf("pop seq 1: got (%v, lost=%v, ok=%v), want ([1], false, true)", payload, lost, ok)
		}

		farAheadPrimed := uint32(2) + (1<<23 - 1)
		if j.push(farAheadPrimed, []byte{0xEE}, base.Add(testTarget).Add(time.Millisecond)) {
			t.Fatalf("push(%d) = true, want false (implausibly far ahead, primed)", farAheadPrimed)
		}
		if got := j.depth(); got != 1 {
			t.Fatalf("depth() after primed far-ahead push = %d, want 1 (only seq 2 buffered)", got)
		}
	})

	// Regression test for Finding 4a: eviction combined with wraparound was
	// previously unexercised -- "bounds depth" never wraps, and "wraps at
	// 2^24" never overflows.
	t.Run("evicts correctly across the 2^24 wraparound boundary", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		const (
			pushCount = 30
			startSeq  = uint32(0xFFFFF0) // 16 values remain before wrapping to 0x000000
		)

		for i := 0; i < pushCount; i++ {
			seq := startSeq + uint32(i) // push masks internally via seq &= wireMask24
			now := base.Add(time.Duration(i) * testFrameDur)
			if !j.push(seq, []byte{byte(i)}, now) {
				t.Fatalf("push(seq=%#x, i=%d) = false, want true", seq&wireMask24, i)
			}
		}

		if got := j.depth(); got != 25 {
			t.Fatalf("depth() after %d pushes crossing the wraparound boundary = %d, want 25", pushCount, got)
		}

		// The 5 oldest in wraparound order (i = 0..4, seq 0xFFFFF0..0xFFFFF4)
		// must have been evicted, leaving i = 5..29 (seq 0xFFFFF5 wrapping
		// through 0x00000D). Prime and pop once to confirm the survivor at
		// the front is i=5, not i=0 -- i.e. eviction picked the
		// wraparound-oldest sequence, not the smallest raw uint32 value.
		primeAt := base.Add(time.Duration(pushCount) * testFrameDur).Add(testTarget)
		payload, lost, ok := j.pop(primeAt)
		if !ok || lost {
			t.Fatalf("pop after priming: got (%v, lost=%v, ok=%v)", payload, lost, ok)
		}
		if !bytes.Equal(payload, []byte{5}) {
			t.Fatalf("first surviving payload = %v, want [5] (i=0..4 should have been evicted across the wraparound boundary)", payload)
		}
		if got := j.depth(); got != 24 {
			t.Fatalf("depth() after one pop = %d, want 24", got)
		}
	})

	// Regression test for Finding 4b: the type carries a mutex and a doc
	// comment promising concurrent-use safety, but all other subtests are
	// single-goroutine. This exercises push and pop concurrently and must
	// pass under -race.
	t.Run("push and pop are safe for concurrent use", func(t *testing.T) {
		j := newJitter(testTarget, testMax, testFrameDur)

		const frameCount = 500
		// A fixed, generous instant far beyond priming and every pushed
		// frame's nominal slot -- passed to every pop call. This is still a
		// plain parameter built from base via Add(), never time.Now().
		farFuture := base.Add(time.Duration(frameCount+1) * testFrameDur).Add(testTarget)

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			for seq := 0; seq < frameCount; seq++ {
				now := base.Add(time.Duration(seq) * testFrameDur)
				j.push(uint32(seq), []byte{byte(seq)}, now)
			}
		}()

		go func() {
			defer wg.Done()
			for i := 0; i < frameCount*2; i++ {
				j.pop(farFuture)
				j.depth()
			}
		}()

		wg.Wait()

		// The point of this test is that it runs clean under -race, not a
		// specific interleaving (the two goroutines race by design, so the
		// exact outcome depends on scheduling). Just confirm the buffer is
		// left internally consistent: bounded depth, no panic.
		if depth := j.depth(); depth < 0 || depth > 25 {
			t.Fatalf("depth() after concurrent push/pop = %d, want within [0, 25]", depth)
		}
	})
}
