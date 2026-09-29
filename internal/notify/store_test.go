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

// TestRaiseInfoStaysReadWhenItUPDATESInPlace covers raiseLocked's OTHER
// unread assignment -- the in-place UPDATE branch, not the insert.
//
// The two are byte-identical lines and it would be easy to assume one test
// covers both. It does not: TestRaiseInfoNeverSoundsAndNeverCountsUnread and
// TestPostInfoIsRaisedAlreadyRead only ever reach insertLocked, so mutating
// the update branch's line to `item.Unread = true` left the whole suite
// green. That is asymmetric coverage on the ROADMAP behaviour "informational
// never reaches the badge or the bell", which insertLocked's comment calls
// the one rule that makes it structural rather than cosmetic.
//
// No production path reaches it TODAY -- the sole info raise
// (notifyJoystickState's "unsupported" case) has constant content, so its
// second raise is an identity no-op and never gets here. It becomes live the
// moment either of two things happens, and both are on the near roadmap:
//
//   - a warn -> info transition on the SHARED joystick.global key, which
//     notifyJoystickState's switch already has the shape for; or
//   - 7.3 / 7.4 adding an info condition whose body carries variable content
//     (a client name, a frequency), so consecutive raises differ.
//
// Then a mutated line would silently put an info item in the badge count and
// the bell list -- visible to the user, invisible to the suite.
func TestRaiseInfoStaysReadWhenItUPDATESInPlace(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	const key = "joystick.global"
	n.Raise(key, Item{Title: "Joystick input is unsupported on this platform", Severity: SeverityInfo})

	// A DIFFERING fingerprint under the same key: findByKeyLocked hits an
	// unresolved item, so this takes the update-in-place branch rather than
	// inserting a second item.
	now = now.Add(time.Second)
	n.Raise(key, Item{
		Title:    "Joystick input is unsupported on this platform",
		Body:     "Joystick and gamepad bindings are available on Windows and Linux only.",
		Severity: SeverityInfo,
	})

	snap := n.Snapshot()
	if len(snap.Items) != 1 {
		t.Fatalf("Items = %d, want 1 -- a differing fingerprint UPDATES, it does not stack", len(snap.Items))
	}
	if snap.Items[0].Body == "" {
		t.Fatal("the in-place update did not land; this test is not reaching the branch it exists for")
	}
	if snap.Items[0].Unread {
		t.Fatal("the updated info item is unread -- info is raised already-read on BOTH " +
			"of raiseLocked's paths, or it reaches the badge and the bell")
	}
	if snap.Unread != 0 {
		t.Fatalf("Unread = %d, want 0 -- an info item must never reach the badge", snap.Unread)
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

func TestDismissKeyedItemSuppressesAnIdenticalReRaise(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	id := n.Snapshot().Items[0].ID
	n.Dismiss(id)

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 after dismiss", got)
	}

	// The condition still holds, so the next applyHotkeys() re-raises it
	// within milliseconds. It must NOT come straight back.
	n.Raise("hotkeys.global", hotkeyItem("no backend"))

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- a dismissed condition must not resurrect itself on the next identical Raise", got)
	}
}

func TestDismissSuppressionClearsOnAChangedFingerprint(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	n.Dismiss(n.Snapshot().Items[0].ID)

	// Permission went denied -> granted while registration still fails.
	// That is genuinely new information.
	changed := hotkeyItem("no backend")
	changed.Context = []KV{{Key: "PERMISSION", Value: "granted"}}
	n.Raise("hotkeys.global", changed)

	snap := n.Snapshot()
	if len(snap.Items) != 1 {
		t.Fatalf("Items = %d, want 1 -- a changed fingerprint is news again", len(snap.Items))
	}
	if snap.Items[0].Context[0].Value != "granted" {
		t.Fatalf("Context = %v, want the new value", snap.Items[0].Context)
	}
}

func TestDismissSuppressionClearsOnResolve(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	n.Dismiss(n.Snapshot().Items[0].ID)
	n.Resolve("hotkeys.global") // the condition actually cleared

	// A later recurrence is a new occurrence and must be shown.
	n.Raise("hotkeys.global", hotkeyItem("no backend"))

	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items = %d, want 1 -- resolving clears the suppression", got)
	}
}

func TestDismissUnkeyedItemNeedsNoSuppression(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	id := n.Post(Item{Title: "discrete", Severity: SeverityWarn})
	n.Dismiss(id)

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0", got)
	}
	// A Post has no key, so nothing can re-raise it. Posting again is a
	// genuinely new event and must appear.
	n.Post(Item{Title: "discrete", Severity: SeverityWarn})
	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items = %d, want 1 -- a Post is never suppressed", got)
	}
}

func TestMarkAllReadClearsEveryUnread(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Post(Item{Title: "a", Severity: SeverityError})
	n.Post(Item{Title: "b", Severity: SeverityWarn})
	n.MarkAllRead()

	snap := n.Snapshot()
	if snap.Unread != 0 {
		t.Fatalf("Unread = %d, want 0", snap.Unread)
	}
	if len(snap.Items) != 2 {
		t.Fatalf("Items = %d, want 2 -- mark-all-read does not remove anything", len(snap.Items))
	}
}

func TestMarkReadOnAnAlreadyReadItemIsANoop(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { changes++ }})

	id := n.Post(Item{Title: "i", Severity: SeverityWarn}) // raised unread
	n.MarkRead(id)
	changes = 0

	// The popout re-expands the same row. Same rule as MarkAllRead: nothing
	// changed, so nothing is broadcast.
	n.MarkRead(id)
	if changes != 0 {
		t.Fatalf("OnChange fired %d times re-marking a read item, want 0", changes)
	}

	// And an id that is not in the list at all.
	n.MarkRead("no-such-id")
	if changes != 0 {
		t.Fatalf("OnChange fired %d times for an unknown id, want 0", changes)
	}
}

func TestMarkAllReadWithNothingUnreadIsANoop(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { changes++ }})

	n.Post(Item{Title: "i", Severity: SeverityInfo}) // already read
	changes = 0
	n.MarkAllRead()

	if changes != 0 {
		t.Fatalf("OnChange fired %d times, want 0", changes)
	}
}

func TestClearEmptiesTheListAndSuppressesEveryKeyedItem(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	n.Post(Item{Title: "discrete", Severity: SeverityWarn})
	n.Clear()

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0", got)
	}

	// Same reasoning as Dismiss: the hotkey condition still holds, so the
	// next emit re-raises it. CLEAR ALL must not be undone a moment later.
	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- Clear suppresses every keyed item it removed", got)
	}
}

func TestClearOnAnEmptyListIsANoop(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { changes++ }})

	n.Clear()
	if changes != 0 {
		t.Fatalf("OnChange fired %d times, want 0", changes)
	}
}

func TestClearDoesNotSuppressAResolvedCondition(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	// A microphone glitched and recovered.
	n.Raise("audio.input", hotkeyItem("device disappeared"))
	n.Resolve("audio.input")

	// The user tidies away the resolved history.
	n.Clear()
	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items after Clear = %d, want 0", got)
	}

	// The same device dies again with the same error string. Resolve has
	// already ruled that a recurrence is a NEW OCCURRENCE (spec 4.1), so
	// CLEAR ALL must not have muted it: suppressing a resolved item inverts
	// that rule, and raiseLocked checks suppression BEFORE it looks at
	// Resolved, so nothing could ever lift it again.
	n.Raise("audio.input", hotkeyItem("device disappeared"))
	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items after the recurrence = %d, want 1 -- CLEAR ALL permanently disabled this condition", len(items))
	}
	if items[0].Resolved {
		t.Fatal("the recurrence came back already-resolved")
	}
}

func TestDismissDoesNotSuppressAResolvedItem(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.Raise("audio.input", hotkeyItem("device disappeared"))
	n.Resolve("audio.input")
	id := n.Snapshot().Items[0].ID
	n.Dismiss(id)

	n.Raise("audio.input", hotkeyItem("device disappeared"))
	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items after the recurrence = %d, want 1 -- dismissing a RESOLVED item must not mute its next occurrence", got)
	}
}

func TestClearStillSuppressesAConditionThatStillHolds(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	// The control for the two tests above: the narrower rule must not have
	// become no rule at all. An UNRESOLVED condition is still suppressed, or
	// CLEAR ALL is undone by the next routine re-emission.
	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	n.Clear()
	n.Raise("hotkeys.global", hotkeyItem("no backend"))

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- a condition that STILL HOLDS stays suppressed after CLEAR ALL", got)
	}
}

// TestDismissDoesNotSuppressAConditionWhoseResolveIsPending is the WINDOWED
// analogue of TestDismissDoesNotSuppressAResolvedItem, and the two are not
// redundant: the un-windowed test alone left a whole fault occurrence
// silently swallowed for every source that actually has a window.
//
// Dismiss decides suppression from it.Resolved, but it calls
// cancelPendingLocked FIRST -- which destroys any deferred change. When the
// deferred change IS the Resolve, the committed item still reads
// Resolved == false at the instant Dismiss inspects it, so Dismiss suppressed
// a condition that had in fact already cleared, and the NEXT occurrence of
// that fault produced no item, no badge, no bell, no toast and no sound.
func TestDismissDoesNotSuppressAConditionWhoseResolveIsPending(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	defer n.StopTimers()

	// An hour stands in for WindowAudio's 10s: long enough that no timer
	// fires during the test, so what is asserted is the coalescing state,
	// not a race against real time.
	const window = time.Hour

	// The microphone fails: leading edge, published, window armed.
	n.RaiseWindowed("audio.input", hotkeyItem("device disappeared"), window)
	// It recovers inside the window, so the Resolve is DEFERRED.
	n.ResolveWindowed("audio.input", window)

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	if items[0].Resolved {
		t.Fatal("precondition: the Resolve was committed, not deferred -- this test no longer exercises the coalesced path")
	}

	// The user clicks X on the row that is still on screen.
	n.Dismiss(items[0].ID)

	// The same device dies again with the same error text. The condition had
	// already cleared before the dismissal, so this is a NEW OCCURRENCE.
	n.Raise("audio.input", hotkeyItem("device disappeared"))
	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items after the recurrence = %d, want 1 -- dismissing an item whose Resolve was still pending muted the whole next fault occurrence", got)
	}
}

// TestClearDoesNotSuppressAConditionWhoseResolveIsPending is the windowed
// analogue of TestClearDoesNotSuppressAResolvedCondition; Clear has the
// identical shape to Dismiss and the identical defect.
func TestClearDoesNotSuppressAConditionWhoseResolveIsPending(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	defer n.StopTimers()

	const window = time.Hour

	n.RaiseWindowed("audio.input", hotkeyItem("device disappeared"), window)
	n.ResolveWindowed("audio.input", window)

	if items := n.Snapshot().Items; len(items) != 1 || items[0].Resolved {
		t.Fatalf("precondition: want exactly one unresolved item with a deferred Resolve, got %+v", items)
	}

	n.Clear()

	n.Raise("audio.input", hotkeyItem("device disappeared"))
	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items after the recurrence = %d, want 1 -- CLEAR ALL over an item whose Resolve was still pending muted the whole next fault occurrence", got)
	}
}

// TestDismissStillSuppressesWhenARaiseIsPending is the control for the two
// tests above: the narrower rule must not collapse into "a key with any
// pending change is never suppressed". A deferred RAISE means the condition
// still holds, so dismissing it must still mute the routine re-emission --
// which is the entire reason suppression exists.
func TestDismissStillSuppressesWhenARaiseIsPending(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	defer n.StopTimers()

	const window = time.Hour

	n.RaiseWindowed("hotkeys.global", hotkeyItem("no backend"), window)
	// A differing re-raise inside the window: deferred, and the condition is
	// still very much unresolved.
	n.RaiseWindowed("hotkeys.global", hotkeyItem("permission denied"), window)

	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Body != "no backend" {
		t.Fatalf("precondition: want the first raise still committed, got %+v", items)
	}

	n.Dismiss(items[0].ID)

	// The level-triggered source re-emits what it always emits.
	n.Raise("hotkeys.global", hotkeyItem("no backend"))
	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items after the re-emission = %d, want 0 -- a condition that STILL HOLDS must stay suppressed after a dismissal", got)
	}
}

// TestARevertedConditionIsNotSuppressedByItsOwnStaleFingerprint pins the
// delete(n.suppressed, key) on raiseLocked's NEW-OCCURRENCE path. Removing
// that line left the whole suite green.
//
// The sequence is ordinary: the user dismisses a fault at content A, the
// fault then changes to content B (a different error string from the same
// device is enough), and later reverts to A. Without the delete, the
// suppression recorded at fingerprint(A) survives the B occurrence, so the
// reverting Raise matches it and is swallowed -- and because Raise is the
// only thing that would have updated the row, the list goes on showing a B
// that is no longer true, for the life of the process.
func TestARevertedConditionIsNotSuppressedByItsOwnStaleFingerprint(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	const key = "audio.input"
	fault := func(body string) Item {
		return Item{Category: "system", Severity: SeverityError, Icon: "mic",
			Title: "Microphone unavailable", Body: body}
	}

	n.Raise(key, fault("device not found"))
	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	n.Dismiss(items[0].ID) // suppressed at fingerprint(A)

	// The fault CHANGES. A different fingerprint, so this is news again and
	// a new occurrence -- which is exactly where the suppression must be
	// dropped, because nothing else on this path will ever drop it.
	n.Raise(key, fault("device is busy"))
	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items = %d after the changed fault, want 1", got)
	}

	// The fault REVERTS to what the user dismissed. It is a live, currently
	// true condition again, and the stale entry must not silence it.
	n.Raise(key, fault("device not found"))

	items = n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	if items[0].Body != "device not found" {
		t.Fatalf("Body = %q, want %q -- the reverting Raise matched the fingerprint the "+
			"user dismissed THREE states ago and was swallowed, so the row still shows a "+
			"fault that is no longer the one occurring", items[0].Body, "device not found")
	}
}

// TestSuppressionNeverCoexistsWithAnUnresolvedItem pins the invariant that
// makes raiseLocked's in-place branch unable to observe a suppression entry
// -- the reason that branch carries no delete(n.suppressed, key).
//
// If this ever fails, restore the delete: the in-place branch would then be
// updating a row whose key is still suppressed, and the next identical Raise
// would be swallowed by an entry the condition has already moved past.
func TestSuppressionNeverCoexistsWithAnUnresolvedItem(t *testing.T) {
	const key = "audio.input"
	fault := func(body string) Item {
		return Item{Category: "system", Severity: SeverityError, Title: "Microphone unavailable", Body: body}
	}

	check := func(t *testing.T, n *Notifier) {
		t.Helper()
		n.mu.Lock()
		defer n.mu.Unlock()
		if len(n.suppressed) == 0 {
			t.Fatal("no suppression was recorded at all; this scenario is not exercising the invariant")
		}
		for k := range n.suppressed {
			for _, it := range n.items {
				if it.Key == k && !it.Resolved {
					t.Fatalf("suppressed[%q] coexists with an UNRESOLVED item under the same "+
						"key (%+v) -- raiseLocked's in-place branch is now reachable with a "+
						"stale suppression present, and needs its delete back", k, it)
				}
			}
		}
	}

	t.Run("dismiss of a lone unresolved item", func(t *testing.T) {
		now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
		n := New(Options{Now: fixedClock(&now)})
		n.Raise(key, fault("a"))
		n.Dismiss(n.Snapshot().Items[0].ID)
		check(t, n)
	})

	t.Run("dismiss with an older resolved item still under the key", func(t *testing.T) {
		now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
		n := New(Options{Now: fixedClock(&now)})
		// Two items end up sharing the key: Resolve retains the first, and
		// the recurrence inserts a second rather than reviving it.
		n.Raise(key, fault("a"))
		n.Resolve(key)
		n.Raise(key, fault("b"))
		items := n.Snapshot().Items
		if len(items) != 2 {
			t.Fatalf("Items = %d, want 2 -- a recurrence is a new occurrence, not a revival", len(items))
		}
		n.Dismiss(items[0].ID) // the newest, which is the unresolved one
		check(t, n)
		// And the survivor is the RESOLVED one, so a differing Raise takes
		// the new-occurrence branch rather than the in-place branch.
		rest := n.Snapshot().Items
		if len(rest) != 1 || !rest[0].Resolved {
			t.Fatalf("remaining items = %+v, want exactly one RESOLVED item", rest)
		}
	})

	t.Run("clear over a mixed list", func(t *testing.T) {
		now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
		n := New(Options{Now: fixedClock(&now)})
		n.Raise(key, fault("a"))
		n.Raise("hotkeys.global", hotkeyItem("no backend"))
		n.Post(Item{Title: "unkeyed", Severity: SeverityWarn})
		n.Clear()
		check(t, n)
		if got := len(n.Snapshot().Items); got != 0 {
			t.Fatalf("Items = %d after Clear, want 0", got)
		}
	})
}

// TestPostIsNeverAConditionEvenWhenHandedAKey pins Post's item.Key = "".
// Removing it left the suite green, and it is load-bearing twice over: a
// keyed Post would become findable by findByKeyLocked and start
// participating in Raise dedupe, Resolve and dismissal suppression -- all of
// which Post's own doc says it is separate from -- and it would break the
// one-unresolved-item-per-key invariant raiseLocked's in-place branch relies
// on for its missing suppression delete.
func TestPostIsNeverAConditionEvenWhenHandedAKey(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	const key = "audio.input"
	body := Item{Category: "system", Severity: SeverityError, Title: "Microphone unavailable", Body: "device not found"}

	posted := body
	posted.Key = key
	postedID := n.Post(posted)

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	if items[0].Key != "" {
		t.Fatalf("the posted item kept Key = %q -- a discrete event that carries a "+
			"condition key is findable by findByKeyLocked and joins dedupe, resolution "+
			"and dismissal suppression, none of which Post has any part in", items[0].Key)
	}

	// The condition path must not see it: an otherwise IDENTICAL Raise under
	// that key is a separate, new item, not a dedupe against the Post.
	n.Raise(key, body)
	items = n.Snapshot().Items
	if len(items) != 2 {
		t.Fatalf("Items = %d, want 2 -- the Raise deduped against a POST, so the condition "+
			"was never recorded and can never be resolved", len(items))
	}

	// Nor may Resolve reach the posted item.
	n.Resolve(key)
	for _, it := range n.Snapshot().Items {
		if it.ID == postedID && it.Resolved {
			t.Fatal("Resolve marked a POSTED item resolved -- a discrete event has no condition to clear")
		}
	}
}

// TestFingerprintCoversEveryActionField pins the Actions block in
// fingerprint. Dropping it left the suite green, and the doc's claim is that
// the hash covers everything the USER CAN SEE -- buttons included. Without
// it, a Raise that changes only its action buttons dedupes as identical, so
// the row keeps a button that no longer applies and no publish ever corrects
// it.
func TestFingerprintCoversEveryActionField(t *testing.T) {
	base := Action{Label: "OPEN AUDIO SETTINGS", Icon: "settings", Kind: "navigate", Target: "settings"}
	for _, tc := range []struct {
		name string
		next Action
	}{
		{"label", Action{Label: "RETRY", Icon: "settings", Kind: "navigate", Target: "settings"}},
		{"icon", Action{Label: "OPEN AUDIO SETTINGS", Icon: "mic", Kind: "navigate", Target: "settings"}},
		{"kind", Action{Label: "OPEN AUDIO SETTINGS", Icon: "settings", Kind: "open-window", Target: "settings"}},
		{"target", Action{Label: "OPEN AUDIO SETTINGS", Icon: "settings", Kind: "navigate", Target: "comms"}},
		{"primary", Action{Label: "OPEN AUDIO SETTINGS", Icon: "settings", Kind: "navigate", Target: "settings", Primary: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
			published := 0
			n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { published++ }})

			const key = "audio.input"
			item := Item{Category: "system", Severity: SeverityWarn, Title: "Microphone unavailable"}
			first := item
			first.Actions = []Action{base}
			n.Raise(key, first)

			second := item
			second.Actions = []Action{tc.next}
			n.Raise(key, second)

			if published != 2 {
				t.Fatalf("OnChange fired %d times, want 2 -- a Raise that changes only its "+
					"action buttons was deduped as identical", published)
			}
			got := n.Snapshot().Items[0].Actions
			if len(got) != 1 || got[0] != tc.next {
				t.Fatalf("Actions = %+v, want %+v -- the row is left showing a button the "+
					"condition no longer offers", got, tc.next)
			}
		})
	}
}

// TestFingerprintSeparatesAdjacentFields pins the h.Write([]byte{0}) after
// every written part. Dropping it left the suite green while reintroducing
// exactly the collision its own comment names: "ab"+"c" hashes the same as
// "a"+"bc", so two genuinely different conditions share a fingerprint and
// the second is deduped away as identical.
func TestFingerprintSeparatesAdjacentFields(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second Item
	}{
		{
			name:   "title/body boundary",
			first:  Item{Severity: SeverityWarn, Title: "ab", Body: "c"},
			second: Item{Severity: SeverityWarn, Title: "a", Body: "bc"},
		},
		{
			name:   "context key/value boundary",
			first:  Item{Severity: SeverityWarn, Title: "t", Context: []KV{{Key: "ab", Value: "c"}}},
			second: Item{Severity: SeverityWarn, Title: "t", Context: []KV{{Key: "a", Value: "bc"}}},
		},
		{
			name:   "action label/icon boundary",
			first:  Item{Severity: SeverityWarn, Title: "t", Actions: []Action{{Label: "ab", Icon: "c"}}},
			second: Item{Severity: SeverityWarn, Title: "t", Actions: []Action{{Label: "a", Icon: "bc"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const key = "audio.input"
			a, b := tc.first, tc.second
			a.Key, b.Key = key, key
			if fingerprint(a) == fingerprint(b) {
				t.Fatalf("fingerprint collision between %+v and %+v -- adjacent fields are "+
					"concatenated with no separator, so the second condition is deduped "+
					"away as identical to the first and never reaches the user", a, b)
			}

			// And the consequence, through the public path.
			now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
			n := New(Options{Now: fixedClock(&now)})
			n.Raise(key, tc.first)
			n.Raise(key, tc.second)
			got := n.Snapshot().Items[0]
			if got.Title != tc.second.Title || got.Body != tc.second.Body {
				t.Fatalf("the row shows %q/%q, want the SECOND condition %q/%q",
					got.Title, got.Body, tc.second.Title, tc.second.Body)
			}
		})
	}
}
