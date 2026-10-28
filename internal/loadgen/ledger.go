package loadgen

const mask24 = 0xFFFFFF

// unwrap24 returns the 64-bit value nearest ref whose low 24 bits equal s.
// Candidates below zero are not representable and are skipped.
func unwrap24(ref uint64, s uint32) uint64 {
	s &= mask24
	hi := ref &^ mask24
	best := hi | uint64(s)
	bestD := absDiff(best, ref)
	if hi >= 1<<24 {
		if c := (hi - 1<<24) | uint64(s); absDiff(c, ref) < bestD {
			best, bestD = c, absDiff(c, ref)
		}
	}
	if c := (hi + 1<<24) | uint64(s); absDiff(c, ref) < bestD {
		best = c
	}
	return best
}

func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

// LedgerResult summarises one (receiver, sender, freq) stream.
type LedgerResult struct {
	Expected, Unique, Lost, Dups, Reordered uint64
	OutOfRange                              uint64 // arrivals outside [base, base+n), ignored by the other counters
	Bursts                                  []int  // lengths of runs of consecutive lost frames
}

// RecvLedger tracks which of [base, base+n) sequence numbers arrived.
// Not safe for concurrent use; one per stream, owned by its receive loop.
type RecvLedger struct {
	base       uint64
	n          int
	bits       []uint64
	highest    uint64
	seen       bool
	unique     uint64
	dups       uint64
	reordered  uint64
	outOfRange uint64
}

func NewRecvLedger(base uint64, n int) *RecvLedger {
	if n < 0 {
		n = 0
	}
	return &RecvLedger{base: base, n: n, bits: make([]uint64, (n+63)/64), highest: base}
}

// Mark records an arrival of the 24-bit wire sequence. dup reports a repeat;
// reordered reports a first arrival below the highest seen (never set with
// dup). Sequences outside [base, base+n) are counted in OutOfRange and
// otherwise ignored. Unwrapping is relative to the highest sequence seen, so
// it assumes n is far below 2^23.
func (l *RecvLedger) Mark(seq24 uint32) (dup, reordered bool) {
	u := unwrap24(l.highest, seq24)
	if u < l.base || u-l.base >= uint64(l.n) {
		l.outOfRange++
		return false, false
	}
	i := u - l.base
	w, b := i/64, uint64(1)<<(i%64)
	if l.bits[w]&b != 0 {
		l.dups++
		return true, false
	}
	l.bits[w] |= b
	l.unique++
	if l.seen && u < l.highest {
		l.reordered++
		return false, true
	}
	l.highest, l.seen = u, true
	return false, false
}

func (l *RecvLedger) Result() LedgerResult {
	r := LedgerResult{Expected: uint64(l.n), Unique: l.unique, Lost: uint64(l.n) - l.unique,
		Dups: l.dups, Reordered: l.reordered, OutOfRange: l.outOfRange}
	run := 0
	for i := 0; i < l.n; i++ {
		if l.bits[i/64]&(1<<(uint(i)%64)) == 0 {
			run++
		} else if run > 0 {
			r.Bursts = append(r.Bursts, run)
			run = 0
		}
	}
	if run > 0 {
		r.Bursts = append(r.Bursts, run)
	}
	return r
}
