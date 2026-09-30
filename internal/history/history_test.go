package history

import (
	"sync"
	"testing"
	"time"
)

func mk(i int) Entry {
	return Entry{
		At:         time.Date(2026, 9, 30, 21, 0, i, 0, time.UTC),
		Sender:     "Dabble",
		GUID:       "g",
		FreqKHz:    118500,
		Radio:      "Fleet Common",
		DurationMS: 1000 + i,
	}
}

func TestSnapshotIsNewestFirst(t *testing.T) {
	l := New(10)
	for i := 0; i < 3; i++ {
		l.Append(mk(i))
	}
	got := l.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].DurationMS != 1002 {
		t.Fatalf("Snapshot must be newest-first; got[0] = %+v", got[0])
	}
}

func TestRingEvictsOldest(t *testing.T) {
	l := New(3)
	for i := 0; i < 5; i++ {
		l.Append(mk(i))
	}
	got := l.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want cap 3", len(got))
	}
	if got[0].DurationMS != 1004 || got[2].DurationMS != 1002 {
		t.Fatalf("wrong window retained: %+v", got)
	}
}

func TestZeroCapUsesDefault(t *testing.T) {
	if got := New(0).Cap(); got != DefaultCap {
		t.Fatalf("Cap = %d, want DefaultCap %d", got, DefaultCap)
	}
}

func TestClearEmpties(t *testing.T) {
	l := New(10)
	l.Append(mk(0))
	l.Clear()
	if l.Len() != 0 {
		t.Fatalf("Len = %d after Clear, want 0", l.Len())
	}
	if got := l.Snapshot(); got == nil {
		t.Fatal("Snapshot must return a non-nil empty slice: the frontend types it as an array and nil marshals to null")
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	l := New(10)
	l.Append(mk(0))
	s := l.Snapshot()
	s[0].Sender = "mutated"
	if l.Snapshot()[0].Sender != "Dabble" {
		t.Fatal("Snapshot must hand out a copy, not the backing array")
	}
}

func TestConcurrentAppendAndSnapshot(t *testing.T) {
	// The RX delivery goroutine and the TX press path both Append, and a
	// binding reads Snapshot from a third. Run under -race.
	l := New(64)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				l.Append(mk(i))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = l.Snapshot()
		}
	}()
	wg.Wait()
	if l.Len() != 64 {
		t.Fatalf("Len = %d, want 64", l.Len())
	}
}
