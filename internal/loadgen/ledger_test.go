package loadgen

import (
	"reflect"
	"testing"
)

func TestRecvLedgerNoLoss(t *testing.T) {
	l := NewRecvLedger(0, 100)
	for i := uint32(0); i < 100; i++ {
		if dup, re := l.Mark(i); dup || re {
			t.Fatalf("seq %d: dup=%v reordered=%v", i, dup, re)
		}
	}
	r := l.Result()
	if r.Expected != 100 || r.Unique != 100 || r.Lost != 0 || r.Dups != 0 || r.Reordered != 0 || len(r.Bursts) != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestRecvLedgerLossAndBursts(t *testing.T) {
	l := NewRecvLedger(0, 100)
	for i := uint32(0); i < 100; i++ {
		if i == 10 || i == 11 || i == 12 || i == 50 {
			continue
		}
		l.Mark(i)
	}
	r := l.Result()
	if r.Lost != 4 || !reflect.DeepEqual(r.Bursts, []int{3, 1}) {
		t.Fatalf("got %+v", r)
	}
}

func TestRecvLedgerDupAndReorder(t *testing.T) {
	l := NewRecvLedger(0, 4)
	var dups, res int
	for _, s := range []uint32{0, 1, 3, 2, 3} {
		d, r := l.Mark(s)
		if d {
			dups++
		}
		if r {
			res++
		}
	}
	r := l.Result()
	if r.Dups != 1 || r.Reordered != 1 || r.Lost != 0 || dups != 1 || res != 1 {
		t.Fatalf("got %+v dups=%d res=%d", r, dups, res)
	}
}

func TestRecvLedgerWrap(t *testing.T) {
	const base = 0xFFFFF0
	run := func(skip int) LedgerResult {
		l := NewRecvLedger(base, 32)
		for i := 0; i < 32; i++ {
			if i == skip {
				continue
			}
			l.Mark(uint32((base + i) & 0xFFFFFF))
		}
		return l.Result()
	}
	if r := run(-1); r.Lost != 0 || r.Reordered != 0 || r.Dups != 0 {
		t.Fatalf("got %+v", r)
	}
	// 0xFFFFF0+18 = 0x1000002 -> 0x000002
	if r := run(18); r.Lost != 1 || !reflect.DeepEqual(r.Bursts, []int{1}) {
		t.Fatalf("got %+v", r)
	}
}

func TestUnwrap24(t *testing.T) {
	cases := []struct {
		ref  uint64
		s    uint32
		want uint64
	}{
		{0, 0, 0},
		{5, 7, 7},
		{0xFFFFFF, 0, 0x1000000},
		{0x1000001, 0xFFFFFF, 0xFFFFFF},
		{0xFFFFF0, 0x000002, 0x1000002},
		{0x1000002, 0xFFFFF0, 0xFFFFF0},
		{0x2000005, 0x000005, 0x2000005},
		{3, 0xFFFFFF, 0xFFFFFF}, // below zero is not representable; stays unsigned-nearest
	}
	for _, c := range cases {
		if got := unwrap24(c.ref, c.s); got != c.want {
			t.Errorf("unwrap24(%#x,%#x)=%#x want %#x", c.ref, c.s, got, c.want)
		}
	}
}

func TestRecvLedgerOutOfRangeIgnoredAndCounted(t *testing.T) {
	const base = 0xFFFFF0
	l := NewRecvLedger(base, 32)
	l.Mark(base & 0xFFFFFF)
	l.Mark((base + 1) & 0xFFFFFF)
	before := l.Result()
	// 0x000020 unwraps to base+48 (past the end); 0xFFFFE0 is below base.
	for _, s := range []uint32{0x000020, 0xFFFFE0} {
		if dup, re := l.Mark(s); dup || re {
			t.Fatalf("seq %#x: dup=%v reordered=%v", s, dup, re)
		}
	}
	after := l.Result()
	if after.Unique != before.Unique || after.Dups != before.Dups || after.Reordered != before.Reordered {
		t.Fatalf("out-of-range changed counters: before %+v after %+v", before, after)
	}
	if after.OutOfRange != 2 {
		t.Fatalf("OutOfRange=%d want 2", after.OutOfRange)
	}
}

func TestRecvLedgerLateFrameAfterWrap(t *testing.T) {
	const base = 0xFFFFF0
	l := NewRecvLedger(base, 32)
	for i := 0; i < 32; i++ {
		if i == 10 { // 0xFFFFFA, delivered late
			continue
		}
		l.Mark(uint32((base + i) & 0xFFFFFF))
	}
	// Highest seen is 0x1000000+0xF; the late frame is across the wrap.
	if dup, re := l.Mark(uint32((base + 10) & 0xFFFFFF)); dup || !re {
		t.Fatalf("late frame: dup=%v reordered=%v", dup, re)
	}
	// A duplicate across the wrap is a dup, never also reordered.
	if dup, re := l.Mark(0x000002); !dup || re {
		t.Fatalf("dup across wrap: dup=%v reordered=%v", dup, re)
	}
	r := l.Result()
	if r.Lost != 0 || r.Reordered != 1 || r.Dups != 1 || r.Unique != 32 || r.OutOfRange != 0 {
		t.Fatalf("got %+v", r)
	}
}
