package voice

import (
	"sync"
	"time"
)

// jitterFrame is one buffered, still-encoded frame awaiting playout.
type jitterFrame struct {
	seq     uint32
	payload []byte
	addedAt time.Time
}

// jitter is a per-stream jitter buffer: it absorbs network timing, reorders
// arrivals by 24-bit sequence, drops duplicates and late arrivals, conceals
// expired gaps, and bounds its own depth.
//
// It owns no clock of its own (design doc D8). Every method that needs to
// reason about time takes `now` as a parameter instead of calling
// time.Now(). That is a hard design decision, not an oversight: on the RX
// path, dspLoop's ticker is the only clock. A decode goroutine ticking on
// its own 10 ms timer alongside dspLoop's own 10 ms timer would be two
// unsynchronised clocks drifting into periodic underruns. Passing `now` in
// also makes every reorder, loss and wraparound case here testable without
// sleeping.
//
// jitter is safe for concurrent use: push is expected to be called from the
// packet-receive goroutine and pop from the decode goroutine.
type jitter struct {
	mu sync.Mutex

	target   time.Duration // priming delay before playout starts
	max      time.Duration // maximum buffered duration before oldest is dropped
	frameDur time.Duration // duration represented by one frame

	maxFrames int // max, expressed as a frame count, for depth bounding

	frames map[uint32]jitterFrame

	hasFirstPush  bool      // true once the very first frame has ever been pushed
	bufferStartAt time.Time // when the first frame was pushed; anchors the priming deadline

	primed       bool      // true once playout has started
	nextExpected uint32    // next sequence pop() will try to hand out
	playoutBase  time.Time // the expected playout instant of nextExpected
}

// newJitter builds a buffer that holds `target` of audio before playout
// starts and discards the oldest buffered frame once depth would exceed
// `max` (expressed via frameDur, the duration one frame represents).
func newJitter(target, max time.Duration, frameDur time.Duration) *jitter {
	maxFrames := 1
	if frameDur > 0 {
		if n := int(max / frameDur); n > 1 {
			maxFrames = n
		}
	}
	return &jitter{
		target:    target,
		max:       max,
		frameDur:  frameDur,
		maxFrames: maxFrames,
		frames:    make(map[uint32]jitterFrame),
	}
}

// push admits an encoded frame at sequence seq. It returns false, refusing
// the frame, in exactly two cases: the sequence is a duplicate of one still
// buffered, or (once primed) the sequence is behind nextExpected -- i.e. its
// playout slot has already passed, so accepting it would mean replaying
// stale audio out of order. It never blocks and never returns false merely
// because the buffer is full; instead, once depth would exceed the
// configured max, the oldest buffered frame is discarded to make room.
func (j *jitter) push(seq uint32, payload []byte, now time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	seq &= wireMask24

	if j.primed && seqLess(seq, j.nextExpected) {
		return false
	}
	if _, duplicate := j.frames[seq]; duplicate {
		return false
	}

	if !j.hasFirstPush {
		j.hasFirstPush = true
		j.bufferStartAt = now
	}

	j.frames[seq] = jitterFrame{seq: seq, payload: payload, addedAt: now}

	for len(j.frames) > j.maxFrames {
		oldest, found := j.oldestSeqLocked()
		if !found {
			break
		}
		delete(j.frames, oldest)
	}

	return true
}

// pop returns the next frame to decode, in sequence order.
//
// ok=false means there is nothing to play yet: either playout has not
// primed (fewer than `target` of audio buffered), or the buffer has
// genuinely run dry and no frame's playout deadline has expired.
//
// lost=true with ok=true means a sequence gap's delay budget expired: the
// frame at nextExpected never arrived in time, but the stream has moved on
// (a later frame is buffered), so the gap is conceded. The caller should
// decode nil for Opus packet-loss concealment. A gap is only conceded once
// its own playout deadline passes -- while now is still before that
// deadline, a genuinely reordered packet still has time to arrive, and pop
// returns ok=false instead of skipping ahead.
func (j *jitter) pop(now time.Time) (payload []byte, lost bool, ok bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if !j.primed {
		if !j.hasFirstPush {
			return nil, false, false
		}
		deadline := j.bufferStartAt.Add(j.target)
		if now.Before(deadline) {
			return nil, false, false
		}
		seq, found := j.oldestSeqLocked()
		if !found {
			// Priming deadline passed but nothing survives in the buffer
			// (e.g. everything was evicted). Stay unprimed until something
			// arrives.
			return nil, false, false
		}
		j.primed = true
		j.nextExpected = seq
		j.playoutBase = deadline
	}

	if frame, present := j.frames[j.nextExpected]; present {
		delete(j.frames, j.nextExpected)
		j.advanceLocked()
		return frame.payload, false, true
	}

	if len(j.frames) == 0 {
		// Nothing buffered at all: an underrun, not a confirmed loss. Don't
		// advance the schedule so we don't drift ahead of frames that may
		// still arrive.
		return nil, false, false
	}

	if now.Before(j.playoutBase) {
		// nextExpected's playout slot hasn't arrived yet: give a reordered
		// packet more time instead of conceding the gap early.
		return nil, false, false
	}

	// The gap's delay budget expired and later audio is already waiting:
	// concede the loss and let the decoder conceal it.
	j.advanceLocked()
	return nil, true, true
}

// advanceLocked moves nextExpected and playoutBase forward by one frame.
// Callers must hold j.mu.
func (j *jitter) advanceLocked() {
	j.nextExpected = (j.nextExpected + 1) & wireMask24
	j.playoutBase = j.playoutBase.Add(j.frameDur)
}

// oldestSeqLocked returns the sequence among the currently buffered frames
// that is earliest in 24-bit wraparound order. Callers must hold j.mu.
func (j *jitter) oldestSeqLocked() (uint32, bool) {
	var best uint32
	found := false
	for seq := range j.frames {
		if !found || seqLess(seq, best) {
			best = seq
			found = true
		}
	}
	return best, found
}

// depth reports the number of frames currently buffered.
func (j *jitter) depth() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.frames)
}

// reset discards all buffered frames and returns the buffer to its initial,
// unprimed state.
func (j *jitter) reset() {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.frames = make(map[uint32]jitterFrame)
	j.hasFirstPush = false
	j.bufferStartAt = time.Time{}
	j.primed = false
	j.nextExpected = 0
	j.playoutBase = time.Time{}
}

// seqDiff returns the signed distance from b to a on the 24-bit wraparound
// sequence space used by the wire protocol (see wireMask24 in packet.go):
// positive when a comes after b, negative when a comes before b, zero when
// equal. It is only meaningful for sequences within 2^23 of each other,
// which always holds within a jitter buffer's bounded depth.
func seqDiff(a, b uint32) int32 {
	d := (a - b) & wireMask24
	// Sign-extend the 24-bit difference into a full 32-bit signed value by
	// shifting the 24th bit up into the sign bit and back.
	return int32(d<<8) >> 8
}

// seqLess reports whether sequence a precedes b in 24-bit wraparound order.
// A naive a < b breaks at the 2^24 boundary, where 0x000000 must order
// after 0xFFFFFF rather than before it.
func seqLess(a, b uint32) bool {
	return seqDiff(a, b) < 0
}
