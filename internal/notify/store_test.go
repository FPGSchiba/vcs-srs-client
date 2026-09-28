package notify

import (
	"testing"
	"time"
)

// fixedClock returns a clock whose value the test controls.
func fixedClock(t *time.Time) func() time.Time {
	return func() time.Time { return *t }
}

func TestPostAppendsNewestFirst(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var got []Snapshot
	n := New(Options{
		Now:      fixedClock(&now),
		OnChange: func(s Snapshot) { got = append(got, s) },
	})

	n.Post(Item{Title: "first", Severity: SeverityWarn})
	now = now.Add(time.Second)
	n.Post(Item{Title: "second", Severity: SeverityWarn})

	if len(got) != 2 {
		t.Fatalf("OnChange fired %d times, want 2", len(got))
	}
	last := got[1]
	if len(last.Items) != 2 {
		t.Fatalf("Items = %d, want 2", len(last.Items))
	}
	if last.Items[0].Title != "second" {
		t.Fatalf("Items[0].Title = %q, want \"second\" (newest first)", last.Items[0].Title)
	}
	if last.Unread != 2 {
		t.Fatalf("Unread = %d, want 2", last.Unread)
	}
}

func TestPostStampsIDAndTime(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	id := n.Post(Item{Title: "a", Severity: SeverityWarn})
	if id == "" {
		t.Fatal("Post returned an empty ID")
	}
	it := n.Snapshot().Items[0]
	if it.ID != id {
		t.Fatalf("stored ID = %q, want %q", it.ID, id)
	}
	if !it.Time.Equal(now) {
		t.Fatalf("Time = %v, want %v (the injected clock)", it.Time, now)
	}
	if it.Key != "" {
		t.Fatalf("Key = %q, want \"\" -- a Post is not a condition", it.Key)
	}
}

func TestPostIDsAreUnique(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := n.Post(Item{Title: "x", Severity: SeverityWarn})
		if seen[id] {
			t.Fatalf("duplicate ID %q at i=%d -- identical content and an identical clock must still get distinct ids", id, i)
		}
		seen[id] = true
	}
}

func TestPostInfoIsRaisedAlreadyRead(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Post(Item{Title: "informational", Severity: SeverityInfo})

	snap := n.Snapshot()
	if snap.Items[0].Unread {
		t.Fatal("info item is Unread; info must be raised already-read so it never reaches the badge, bell or toast")
	}
	if snap.Unread != 0 {
		t.Fatalf("Unread = %d, want 0", snap.Unread)
	}
}

func TestPostNeverDedupes(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	// Byte-identical content, posted three times. A Post is a discrete
	// event: three of them are three things that happened.
	for i := 0; i < 3; i++ {
		n.Post(Item{Title: "same", Body: "same", Severity: SeverityWarn})
	}

	if got := len(n.Snapshot().Items); got != 3 {
		t.Fatalf("Items = %d, want 3 -- Post must never dedupe", got)
	}
}

func TestPostFiresSoundForErrorOnly(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var sounded []Item
	n := New(Options{
		Now:     fixedClock(&now),
		OnSound: func(it Item) { sounded = append(sounded, it) },
	})

	n.Post(Item{Title: "e", Severity: SeverityError})
	n.Post(Item{Title: "w", Severity: SeverityWarn})
	n.Post(Item{Title: "i", Severity: SeverityInfo})

	if len(sounded) != 1 {
		t.Fatalf("OnSound fired %d times, want 1 (error only)", len(sounded))
	}
	if sounded[0].Title != "e" {
		t.Fatalf("sounded %q, want \"e\"", sounded[0].Title)
	}
}

func TestPostEvictsOldestAtCap(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now), Cap: 3})

	n.Post(Item{Title: "one", Severity: SeverityWarn})
	n.Post(Item{Title: "two", Severity: SeverityWarn})
	n.Post(Item{Title: "three", Severity: SeverityWarn})
	n.Post(Item{Title: "four", Severity: SeverityWarn})

	snap := n.Snapshot()
	if len(snap.Items) != 3 {
		t.Fatalf("Items = %d, want 3 (the cap)", len(snap.Items))
	}
	if snap.Items[0].Title != "four" {
		t.Fatalf("Items[0] = %q, want \"four\"", snap.Items[0].Title)
	}
	if snap.Items[2].Title != "two" {
		t.Fatalf("Items[2] = %q, want \"two\" -- the OLDEST is evicted", snap.Items[2].Title)
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	n.Post(Item{Title: "original", Severity: SeverityWarn})

	snap := n.Snapshot()
	snap.Items[0].Title = "mutated by the caller"

	if got := n.Snapshot().Items[0].Title; got != "original" {
		t.Fatalf("Title = %q; Snapshot must hand back a copy, not the backing array", got)
	}
}
