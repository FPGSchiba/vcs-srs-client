package notify

import (
	"strconv"
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

func hotkeyItem(reason string) Item {
	return Item{
		Category: "system",
		Severity: SeverityError,
		Icon:     "bolt",
		Title:    "Global hotkeys unavailable",
		Body:     reason,
		Context:  []KV{{Key: "PERMISSION", Value: "denied"}},
		Actions:  []Action{{Label: "OPEN KEYBIND SETTINGS", Kind: "navigate", Target: "settings", Primary: true}},
	}
}

func TestRaiseIdenticalIsATotalNoop(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	sounds := 0
	n := New(Options{
		Now:      fixedClock(&now),
		OnChange: func(Snapshot) { changes++ },
		OnSound:  func(Item) { sounds++ },
	})

	// This is the 38-emission rebind from spec 1.3: emitHotkeyState fires
	// from nine call sites, so rebinding nineteen actions re-emits the same
	// payload roughly 38 times.
	for i := 0; i < 38; i++ {
		now = now.Add(time.Second) // the clock moves; the CONTENT does not
		n.Raise("hotkeys.global", hotkeyItem("no backend"))
	}

	if changes != 1 {
		t.Fatalf("OnChange fired %d times, want 1 -- an identical Raise must be a TOTAL no-op", changes)
	}
	if sounds != 1 {
		t.Fatalf("OnSound fired %d times, want 1", sounds)
	}
	snap := n.Snapshot()
	if len(snap.Items) != 1 {
		t.Fatalf("Items = %d, want 1", len(snap.Items))
	}
	if !snap.Items[0].Time.Equal(time.Date(2026, 9, 28, 12, 0, 1, 0, time.UTC)) {
		t.Fatalf("Time = %v; a deduped repeat must not bump the timestamp", snap.Items[0].Time)
	}
}

func TestRaiseIdenticalDoesNotRemarkUnread(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	id := n.Snapshot().Items[0].ID
	n.MarkRead(id)
	n.Raise("hotkeys.global", hotkeyItem("no backend"))

	if n.Snapshot().Items[0].Unread {
		t.Fatal("an identical Raise re-marked a read item unread")
	}
}

func TestRaiseDifferentFingerprintUpdatesInPlace(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	first := n.Snapshot().Items[0]
	n.MarkRead(first.ID)

	now = now.Add(time.Minute)
	changed := hotkeyItem("no backend")
	changed.Context = []KV{{Key: "PERMISSION", Value: "granted"}}
	n.Raise("hotkeys.global", changed)

	snap := n.Snapshot()
	if len(snap.Items) != 1 {
		t.Fatalf("Items = %d, want 1 -- a changed fingerprint UPDATES, it does not stack", len(snap.Items))
	}
	got := snap.Items[0]
	if got.Context[0].Value != "granted" {
		t.Fatalf("Context = %v, want the new value", got.Context)
	}
	if !got.Time.Equal(now) {
		t.Fatalf("Time = %v, want %v -- a real change refreshes the timestamp", got.Time, now)
	}
	if !got.Unread {
		t.Fatal("a real change must re-mark the item unread")
	}
}

func TestResolveMarksResolvedAndClearsUnread(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	n.Resolve("hotkeys.global")

	snap := n.Snapshot()
	if len(snap.Items) != 1 {
		t.Fatalf("Items = %d, want 1 -- resolving RETAINS the item", len(snap.Items))
	}
	if !snap.Items[0].Resolved {
		t.Fatal("item is not Resolved")
	}
	if snap.Items[0].Unread {
		t.Fatal("a resolved item must not stay unread -- the badge must stop nagging")
	}
	if snap.Unread != 0 {
		t.Fatalf("Unread = %d, want 0", snap.Unread)
	}
}

func TestResolveUnknownOrAlreadyResolvedIsANoop(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { changes++ }})

	n.Resolve("never.raised")
	if changes != 0 {
		t.Fatalf("OnChange fired %d times for an unknown key, want 0", changes)
	}

	n.Raise("hotkeys.global", hotkeyItem("x"))
	n.Resolve("hotkeys.global")
	changes = 0
	n.Resolve("hotkeys.global")
	if changes != 0 {
		t.Fatalf("OnChange fired %d times for an already-resolved key, want 0", changes)
	}
}

func TestRaiseAfterResolveProducesAFreshItem(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn})
	n.Resolve("joystick.global")
	now = now.Add(time.Hour)
	n.Raise("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn})

	snap := n.Snapshot()
	if len(snap.Items) != 2 {
		t.Fatalf("Items = %d, want 2 -- a recurrence after resolution is a NEW occurrence, not a revived one", len(snap.Items))
	}
	if snap.Items[0].Resolved {
		t.Fatal("the new occurrence must not be resolved")
	}
	if !snap.Items[1].Resolved {
		t.Fatal("the original must stay resolved")
	}
}

func TestRaiseInfoNeverSoundsAndNeverCountsUnread(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	sounds := 0
	n := New(Options{Now: fixedClock(&now), OnSound: func(Item) { sounds++ }})

	// macOS: joystick input is unsupported. Informational, never a failure.
	n.Raise("joystick.global", Item{
		Title:    "Joystick input is unsupported on this platform",
		Severity: SeverityInfo,
	})

	if sounds != 0 {
		t.Fatalf("OnSound fired %d times for an info item, want 0", sounds)
	}
	if got := n.Snapshot().Unread; got != 0 {
		t.Fatalf("Unread = %d, want 0", got)
	}
}

func TestConcurrentRaiseIsSafe(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{
		Now:      fixedClock(&now),
		OnChange: func(s Snapshot) { _ = len(s.Items) }, // touch it, to catch races on the copy
	})

	// Three real goroutines: emitJoystickState runs on the joystick poll
	// goroutine, emitHotkeyState on Wails binding goroutines, and the audio
	// adapter on the audio poll goroutine.
	done := make(chan struct{})
	for g := 0; g < 3; g++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			key := "src" + strconv.Itoa(g)
			for i := 0; i < 200; i++ {
				if i%2 == 0 {
					n.Raise(key, Item{Title: "t" + strconv.Itoa(i), Severity: SeverityWarn})
				} else {
					n.Resolve(key)
				}
			}
		}(g)
	}
	for g := 0; g < 3; g++ {
		<-done
	}

	// No assertion on the exact count -- the point is that -race sees no
	// data race and neither the list nor the unread count is corrupted.
	snap := n.Snapshot()
	unread := 0
	for _, it := range snap.Items {
		if it.Unread {
			unread++
		}
	}
	if unread != snap.Unread {
		t.Fatalf("Unread = %d but %d items are unread -- the count and the list disagree", snap.Unread, unread)
	}
}
