package voice

import (
	"bytes"
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
}
