# Phase 7.2 — Notification Channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a general, source-agnostic notification channel owned by Go, give it three consumers (hotkeys, joystick, audio), surface it through a new Notifications popout plus badge/bell/toast, and fill the audio mixer's notification bus so an alert can be heard as well as seen.

**Architecture:** A pure `internal/notify` package owns a capped item list with two entry points — `Post` for discrete events, `Raise`/`Resolve` for conditions — plus identity dedupe and a per-source coalescing window. Adapters in `internal/app` and `main.go` translate subsystem DTOs into items. The store publishes a full `Snapshot` on `notifications:changed`; every window hydrates from `GetNotifications()` and subscribes. A second `voicePool` in `internal/audio` fills `notifBuf`, which the DSP loop already hands to `mixer.Mix`.

**Tech Stack:** Go 1.x (`log/slog`, `sync`, `time`), Wails v3 events, React 18 + TypeScript + Zustand + Vite, vitest + @testing-library/react.

**Spec:** [`docs/superpowers/specs/2026-09-28-vcs-client-phase-7-2-notification-channel-design.md`](../specs/2026-09-28-vcs-client-phase-7-2-notification-channel-design.md)

## Global Constraints

- Branch `feat/phase-7-2-notification-channel`, off `main` at `ed35789`. Already created.
- Every Go invocation carries `-tags purego` and `GOCACHE=$TMPDIR/vcs-gocache`. The default GOCACHE is sandbox-blocked.
- Typecheck with `(cd frontend && npx tsc --noEmit)`. The `npx --prefix frontend` form prints a help banner and exits 0 **without checking anything**.
- `frontend/bindings/` is gitignored and goes stale. This phase adds eight `App` methods, so `wails3 generate bindings -ts -f "-tags purego" -clean=true` is a required step (Task 6), not a staleness workaround.
- **gopls is unreliable in this repo.** It reported existing symbols as undefined four separate times during Phase 7.1, each contradicted by a clean `go vet`. Never act on its diagnostics.
- **No `Co-Authored-By` trailers.** Attribution is disabled globally per `~/.claude/rules/common/git-workflow.md`. Phase 7.1 added them by mistake across ~15 commits.
- `cmd | grep -v x; echo $?` reports **grep's** status. Use `${PIPESTATUS[0]}`.
- Both window roots render inside `React.StrictMode`. Any component whose effect cleanup has side effects needs a StrictMode test **plus** a control proving the real-unmount path fires once.
- SonarCloud's PR gate evaluates only new code. Any click handler on a non-button element needs `role="button"`, `tabIndex` and Enter/Space handling **in the same commit** (`typescript:S1082`).
- `internal/notify` must not import `internal/app`, `internal/hotkeys`, `internal/joystick` or `internal/audio`. Grepping it for "hotkey", "joystick" or "audio" must return nothing.
- Info-severity items are raised already-read (`Unread: false`) — spec §5.3. This is enforced in the store, not at call sites.
- Sound follows the toast: error severity only. Spec §3.8.
- The notification sample does not exist and must **not** be substituted with a synthesised tone (`internal/audio/assets/README.md`). `notify_alert` reports `available: false` and plays silence.

## Review Focus

Five failure modes the spec implies that no task's happy path exercises. Each has its pinning test assigned to the task that owns the code.

1. **`Overruns`/`Underruns` movement must produce no notification.** The single most important audio test — it is the entire justification for including audio. Pinned in Task 9.
2. **A `Raise` arriving concurrently from two goroutines must not corrupt the list or double-emit.** `emitJoystickState` runs on the joystick poll goroutine, `emitHotkeyState` on Wails binding goroutines, the audio adapter on the audio poll goroutine. Pinned in Task 3 under `-race`.
3. **A coalescing timer that fires after `Clear()` or after its key was dismissed must not resurrect the item.** The trailing timer outlives the state it was scheduled for. Pinned in Task 4.
4. **`PlayNotification` on a stopped Manager must drop, not linger.** A queued id surviving until an unrelated later `Start()` drained it would play a sound for an event minutes past. `PlayEffect` already handles this; the new path must too. Pinned in Task 11.
5. **A notification whose `Body` contains user-controlled text must not break the row's layout or escape into markup.** Device names and OS error strings flow straight into `Title`/`Body`/`Context`. React escapes by default, but the row must also not blow out its container on a 400-character malgo error. Pinned in Task 13.

---

## File Structure

**New — Go:**
| File | Responsibility |
|---|---|
| `internal/notify/notify.go` | `Item`, `Action`, `KV`, `Severity`, `Snapshot`, `Options`, `Notifier`, `New` |
| `internal/notify/store.go` | `Post`/`Raise`/`Resolve`/`MarkRead`/`MarkAllRead`/`Dismiss`/`Clear`, dedupe, suppression, cap |
| `internal/notify/coalesce.go` | per-key trailing-timer coalescing |
| `internal/notify/notify_test.go`, `store_test.go`, `coalesce_test.go` | unit suites |
| `internal/app/notifications.go` | `SetNotifier` + the seven bindings + `NotificationsDTO` |
| `internal/app/notify_keybinds.go` | hotkey + joystick adapters |
| `internal/app/notify_sound.go` | severity→slot gate, mirroring `connsfx.go` |
| `internal/audio/notifsfx.go` | `NotifSFX` sample set + `Manager.PlayNotification` |

**New — frontend:**
| File | Responsibility |
|---|---|
| `frontend/notifications.html`, `frontend/src/notifications.tsx` | window shell + React root |
| `frontend/src/windows/notifications/NotificationsApp.tsx` | header, filter, mark-all-read, clear-all, empty state |
| `frontend/src/windows/notifications/NotifRow.tsx` | one row |
| `frontend/src/shared/store/notifications.ts` | Zustand mirror of `Snapshot` |
| `frontend/src/shared/store/useNotificationsSync.ts` | hydrate + subscribe |
| `frontend/src/shared/components/ToastHost.tsx` | error-severity toast stack |
| `frontend/src/shared/components/activatable.ts` | `role`/`tabIndex`/Enter/Space helper, lifted from `StatusBar.tsx:71` |
| `frontend/src/shared/components/notificationCategories.ts` | the seven categories + colours |
| `frontend/src/windows/main/screens/settings/sections/keybinds/PermissionCard.tsx` | extracted remediation UI |

**Modified:** `internal/events/events.go`, `internal/app/windowfactory.go`, `internal/app/dto.go`, `internal/app/settings.go`, `internal/config/config.go`, `internal/audio/manager.go`, `internal/audio/assets/manifest.toml`, `main.go`, `frontend/vite.config.ts`, `frontend/src/shared/api/events.ts`, `frontend/src/shared/api/client.ts`, `frontend/src/shared/components/TopBar.tsx`, `frontend/src/shared/components/StatusBar.tsx`, `frontend/src/windows/main/MainApp.tsx`, `frontend/src/windows/main/screens/settings/sections/Keybinds.tsx`, `docs/ROADMAP.md`.

---

## Task 1: `internal/notify` types and constructor

**Files:**
- Create: `internal/notify/notify.go`
- Test: `internal/notify/notify_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Severity` (`SeverityError`/`SeverityWarn`/`SeverityInfo`), `KV{Key,Value string}`, `Action{Label,Icon,Kind,Target string; Primary bool}`, `Item{ID,Key,Category string; Severity Severity; Icon,Title,Body string; Context []KV; Actions []Action; Time time.Time; Unread,Resolved bool}`, `Snapshot{Items []Item; Unread int}`, `Options{Now func() time.Time; OnChange func(Snapshot); OnSound func(Item); Cap int}`, `New(Options) *Notifier`, `(*Notifier).Snapshot() Snapshot`.

- [ ] **Step 1: Write the failing test**

Create `internal/notify/notify_test.go`:

```go
package notify

import (
	"testing"
	"time"
)

func TestNewStartsEmpty(t *testing.T) {
	n := New(Options{})
	got := n.Snapshot()
	if len(got.Items) != 0 {
		t.Fatalf("Items = %d, want 0", len(got.Items))
	}
	if got.Unread != 0 {
		t.Fatalf("Unread = %d, want 0", got.Unread)
	}
	if got.Items == nil {
		t.Fatal("Items is nil; must be a non-nil empty slice so it marshals to [] not null")
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	n := New(Options{})
	if n.opt.Cap != DefaultCap {
		t.Fatalf("Cap = %d, want %d", n.opt.Cap, DefaultCap)
	}
	if n.opt.Now == nil {
		t.Fatal("Now must default to time.Now")
	}
}

func TestNewKeepsInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: func() time.Time { return fixed }})
	if got := n.opt.Now(); !got.Equal(fixed) {
		t.Fatalf("Now() = %v, want %v", got, fixed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/notify/ -run TestNew -v`
Expected: FAIL — the package does not compile, `undefined: New`, `undefined: Options`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/notify/notify.go`:

```go
// Package notify owns the client's notification channel: one capped list of
// human-readable items, one owner, one broadcast.
//
// It is deliberately source-agnostic. Nothing here knows what a hotkey, a
// joystick or an audio device is -- callers translate their own subsystem's
// state into an Item and hand it over. That is what lets Phase 7.3's and
// 7.4's popouts raise notifications without touching this package.
//
// Modelled on internal/connhealth: it depends only on injected function
// values and an injected clock, never on internal/app or any subsystem, so
// it is unit-testable with no sockets and no goroutines but its own timers.
package notify

import (
	"sync"
	"time"
)

// DefaultCap bounds the list. The whole Snapshot is broadcast on every
// change (see Options.OnChange), so the cap is what keeps that affordable.
const DefaultCap = 200

// Severity drives DELIVERY, not just colour.
//
//   - SeverityError toasts and plays the notification sound.
//   - SeverityWarn reaches the badge and bell silently.
//   - SeverityInfo is raised already-read and reaches nothing at all.
//
// That last rule is why "joystick unsupported on macOS" can never render as
// a failure: it is enforced here, at the delivery layer, rather than left to
// the colour of a border in the UI.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
	SeverityInfo  Severity = "info"
)

// KV is one ordered context row.
//
// Deliberately a slice element and not a map entry: Go's encoding/json sorts
// map keys alphabetically, which is deterministic but is not DISPLAY order.
// app.AudioSettingsDTO.EffectOrder exists to solve exactly this problem for
// the SFX manifest.
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Action is one button on a notification row.
//
// Kind is a closed set so a new notification source adds no frontend
// dispatch code:
//
//	"open-window" -- Target is a window id ("comms", "ship", ...)
//	"navigate"    -- Target is a main-window nav key ("settings", ...)
//
// Actions is a LIST on Item even though Phase 7.2 only ever populates one,
// because the design prototype's distress example carries two. A singular
// field would force 7.4 to change the wire shape and every consumer.
type Action struct {
	Label   string `json:"label"`
	Icon    string `json:"icon"`
	Kind    string `json:"kind"`
	Target  string `json:"target"`
	Primary bool   `json:"primary"`
}

// Item is one notification, as the user sees it.
//
// Key is "" for a discrete Post and non-empty for a condition raised through
// Raise. Only keyed items participate in dedupe, coalescing, resolution and
// dismissal suppression -- see store.go.
type Item struct {
	ID       string    `json:"id"`
	Key      string    `json:"key"`
	Category string    `json:"category"`
	Severity Severity  `json:"severity"`
	Icon     string    `json:"icon"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	Context  []KV      `json:"context"`
	Actions  []Action  `json:"actions"`
	Time     time.Time `json:"time"`
	Unread   bool      `json:"unread"`
	Resolved bool      `json:"resolved"`
}

// Snapshot is the whole list plus the unread count, newest first.
//
// Broadcast in FULL on every change rather than as a delta, for the reason
// events.Tagged.KeybindsChanged already documents: the list is small (see
// DefaultCap) and a replacement removes a class of frontend/backend
// divergence bug.
type Snapshot struct {
	Items  []Item `json:"items"`
	Unread int    `json:"unread"`
}

// Options configures a Notifier. Every field is optional.
type Options struct {
	// Now is the clock. Injected so the coalescing tests need no sleeping.
	Now func() time.Time

	// OnChange receives every published Snapshot. Serialised under emitMu,
	// so it is never called concurrently with itself.
	//
	// OnChange MUST NOT call back into the Notifier. It runs WITH emitMu
	// held and every mutating method acquires emitMu itself, so a reentrant
	// call deadlocks permanently on the same goroutine. This mirrors
	// connhealth.Options.OnChange's contract exactly.
	OnChange func(Snapshot)

	// OnSound fires for an item that should be AUDIBLE -- error severity
	// only, and only when the item is genuinely created or content-changed,
	// never on a deduped repeat. Called under the same discipline as
	// OnChange and subject to the same must-not-reenter rule.
	OnSound func(Item)

	// Cap bounds the list. Zero means DefaultCap.
	Cap int
}

// Notifier owns the list.
type Notifier struct {
	opt Options

	// emitMu makes one commit-and-deliver sequence atomic with respect to
	// every other one. Always acquired BEFORE mu and held across both the
	// mu-guarded mutation and the delivery that follows, so delivery order
	// matches commit order. Never the reverse order.
	emitMu sync.Mutex

	mu    sync.Mutex
	items []Item
	seq   uint64
	// suppressed records keys the user dismissed, mapped to the fingerprint
	// they were dismissed at. See store.go's Dismiss.
	suppressed map[string]string
	// pending holds coalescing state per key. See coalesce.go.
	pending map[string]*pendingChange
}

// New constructs a Notifier with an empty list.
func New(opt Options) *Notifier {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Cap <= 0 {
		opt.Cap = DefaultCap
	}
	return &Notifier{
		opt:        opt,
		items:      []Item{},
		suppressed: map[string]string{},
		pending:    map[string]*pendingChange{},
	}
}

// Snapshot returns the current list, newest first, with the unread count.
//
// The returned Items slice is a copy: callers marshal it onto the event bus
// and must not be able to mutate the Notifier's own backing array.
func (n *Notifier) Snapshot() Snapshot {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.snapshotLocked()
}

// snapshotLocked builds the Snapshot. Caller holds mu.
func (n *Notifier) snapshotLocked() Snapshot {
	out := make([]Item, len(n.items))
	copy(out, n.items)
	unread := 0
	for _, it := range out {
		if it.Unread {
			unread++
		}
	}
	return Snapshot{Items: out, Unread: unread}
}
```

Create `internal/notify/coalesce.go` with just the type this file references, so the package compiles:

```go
package notify

import "time"

// pendingChange is one key's coalescing state. Filled in by Task 4.
type pendingChange struct {
	timer  *time.Timer
	windowEnd time.Time
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/notify/ -run TestNew -v`
Expected: PASS, three tests.

- [ ] **Step 5: Verify the package has no forbidden imports**

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
grep -rniE "hotkey|joystick|audio|internal/app" internal/notify/ ; echo "exit=$?"
```
Expected: no matches, `exit=1`. A match means the package has leaked knowledge of a source.

- [ ] **Step 6: Commit**

```bash
git add internal/notify/
git commit -m "feat(notify): add the notification channel's types and constructor"
```

---

## Task 2: `Post` — discrete events

**Files:**
- Create: `internal/notify/store.go`
- Test: `internal/notify/store_test.go`

**Interfaces:**
- Consumes: Task 1's `Item`, `Snapshot`, `Options`, `Notifier`, `New`.
- Produces: `(*Notifier).Post(Item) string` returning the assigned `ID`; unexported `publishLocked`, `nextIDLocked`, `insertLocked`.

- [ ] **Step 1: Write the failing test**

Create `internal/notify/store_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/notify/ -run TestPost -v`
Expected: FAIL — `n.Post undefined (type *Notifier has no field or method Post)`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/notify/store.go`:

```go
package notify

import "strconv"

// Post records a DISCRETE event -- something that happened once and is never
// "resolved": a distress beacon, a client joining a frequency, an incoming
// message. It always appends.
//
// This is the entry point Phase 7.3 and 7.4 will mostly use. It is
// deliberately separate from Raise: dedupe, coalescing, resolution and
// dismissal-suppression are properties of the CONDITION path (see Raise),
// and forcing a discrete event through it would mean inventing a synthetic
// key that never resolves.
//
// Returns the ID assigned to the item.
func (n *Notifier) Post(item Item) string {
	n.emitMu.Lock()
	defer n.emitMu.Unlock()

	n.mu.Lock()
	item.Key = "" // a Post is never a condition, whatever the caller passed
	id := n.insertLocked(item)
	snap := n.snapshotLocked()
	sound, wantSound := n.soundForLocked(id)
	n.mu.Unlock()

	n.publish(snap)
	if wantSound {
		n.playSound(sound)
	}
	return id
}

// insertLocked stamps, normalises and prepends one item, evicting the oldest
// if the list is at capacity. Returns the assigned ID. Caller holds mu.
func (n *Notifier) insertLocked(item Item) string {
	item.ID = n.nextIDLocked()
	item.Time = n.opt.Now()
	item.Resolved = false
	// Info is raised ALREADY-READ. This is the one rule that makes
	// "informational, never a failure" structural rather than cosmetic: an
	// info item cannot reach the badge, the bell or a toast, because none of
	// those surfaces counts a read item.
	item.Unread = item.Severity != SeverityInfo
	if item.Context == nil {
		item.Context = []KV{}
	}
	if item.Actions == nil {
		item.Actions = []Action{}
	}

	n.items = append([]Item{item}, n.items...)
	if len(n.items) > n.opt.Cap {
		n.items = n.items[:n.opt.Cap]
	}
	return item.ID
}

// nextIDLocked returns a fresh id. A counter, not a hash of the content or
// the clock: two byte-identical Posts under a frozen test clock must still
// be distinguishable, since Dismiss and MarkRead address items by id.
// Caller holds mu.
func (n *Notifier) nextIDLocked() string {
	n.seq++
	return "n" + strconv.FormatUint(n.seq, 10)
}

// soundForLocked reports whether the item with this id should be audible.
// Sound follows the toast: error severity only. Caller holds mu.
func (n *Notifier) soundForLocked(id string) (Item, bool) {
	for _, it := range n.items {
		if it.ID == id {
			return it, it.Severity == SeverityError
		}
	}
	return Item{}, false
}

// publish delivers one Snapshot. Called with emitMu held and mu released.
func (n *Notifier) publish(s Snapshot) {
	if n.opt.OnChange == nil {
		return
	}
	n.opt.OnChange(s)
}

// playSound delivers one audible item. Called with emitMu held and mu
// released, the same discipline as publish.
func (n *Notifier) playSound(it Item) {
	if n.opt.OnSound == nil {
		return
	}
	n.opt.OnSound(it)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/notify/ -v`
Expected: PASS, all Task 1 and Task 2 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/
git commit -m "feat(notify): add Post for discrete events"
```

---

## Task 3: `Raise` / `Resolve` with identity dedupe

**Files:**
- Modify: `internal/notify/store.go`
- Test: `internal/notify/store_test.go` (append)

**Interfaces:**
- Consumes: Task 2's `insertLocked`, `publish`, `playSound`, `snapshotLocked`, `soundForLocked`.
- Produces: `(*Notifier).Raise(key string, item Item)`, `(*Notifier).Resolve(key string)`, unexported `fingerprint(Item) string`, `findByKeyLocked(key string) int`.

**Note on ordering:** this task implements `Raise` WITHOUT coalescing. Task 4 adds the window on top. Keeping them apart is what lets the dedupe tests here run with no timers at all.

- [ ] **Step 1: Write the failing test**

Append to `internal/notify/store_test.go`:

```go
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
```

Add `"strconv"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/notify/ -run "TestRaise|TestResolve|TestConcurrent" -v`
Expected: FAIL — `n.Raise undefined`, `n.Resolve undefined`, `n.MarkRead undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/notify/store.go`:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"
)

// Raise asserts a CONDITION under a stable key.
//
// Unlike Post, a keyed item participates in identity dedupe: raising the
// same key with byte-identical user-visible content is a TOTAL no-op -- no
// publish, no timestamp bump, no re-mark-unread, no sound.
//
// That no-op is the whole defence against level-triggered sources. Both
// hotkey and audio state are snapshots re-emitted far more often than they
// change: emitHotkeyState fires from nine call sites, so a nineteen-action
// rebind re-emits roughly 38 identical payloads, and audio:state compares a
// struct containing monotonic xrun counters, so it fires every 2s for as
// long as the engine is glitching. Without this, either would produce one
// notification per emission.
//
// A DIFFERING fingerprint updates the item in place, refreshes its
// timestamp, re-marks it unread and sounds. A key whose previous item was
// resolved gets a fresh item: a recurrence is a new occurrence, not a
// revival.
func (n *Notifier) Raise(key string, item Item) {
	if key == "" {
		// A keyless Raise is a Post; treating it as one is kinder than
		// silently keying everything under "".
		n.Post(item)
		return
	}
	n.emitMu.Lock()
	defer n.emitMu.Unlock()

	n.mu.Lock()
	changed, id := n.raiseLocked(key, item)
	if !changed {
		n.mu.Unlock()
		return
	}
	snap := n.snapshotLocked()
	sound, wantSound := n.soundForLocked(id)
	n.mu.Unlock()

	n.publish(snap)
	if wantSound {
		n.playSound(sound)
	}
}

// raiseLocked applies one Raise. Reports whether anything changed, and the
// affected item's id. Caller holds mu.
func (n *Notifier) raiseLocked(key string, item Item) (bool, string) {
	item.Key = key
	fp := fingerprint(item)

	// Dismissed at this exact fingerprint: the user has already said "I know,
	// stop telling me". See Dismiss.
	if got, ok := n.suppressed[key]; ok && got == fp {
		return false, ""
	}

	idx := n.findByKeyLocked(key)
	if idx >= 0 && !n.items[idx].Resolved {
		if fingerprint(n.items[idx]) == fp {
			return false, "" // identical, unresolved: total no-op
		}
		// Real change: update in place, keeping the id so an open UI's
		// selection and any pending MarkRead still address the same row.
		id := n.items[idx].ID
		item.ID = id
		item.Time = n.opt.Now()
		item.Resolved = false
		item.Unread = item.Severity != SeverityInfo
		if item.Context == nil {
			item.Context = []KV{}
		}
		if item.Actions == nil {
			item.Actions = []Action{}
		}
		n.items[idx] = item
		delete(n.suppressed, key) // the condition changed: it is news again
		return true, id
	}

	// No item, or the previous one is resolved: this is a new occurrence.
	delete(n.suppressed, key)
	return true, n.insertLocked(item)
}

// Resolve clears a condition. The item is RETAINED and marked resolved
// rather than deleted, so a user who was away still learns their push-to-talk
// was dead for ten minutes -- while the badge stops counting it immediately.
//
// A no-op for an unknown or already-resolved key: no publish, no churn.
func (n *Notifier) Resolve(key string) {
	if key == "" {
		return
	}
	n.emitMu.Lock()
	defer n.emitMu.Unlock()

	n.mu.Lock()
	idx := n.findByKeyLocked(key)
	if idx < 0 || n.items[idx].Resolved {
		n.mu.Unlock()
		return
	}
	n.items[idx].Resolved = true
	n.items[idx].Unread = false
	// The condition genuinely cleared, so a later recurrence is news again.
	delete(n.suppressed, key)
	snap := n.snapshotLocked()
	n.mu.Unlock()

	n.publish(snap)
}

// findByKeyLocked returns the index of the NEWEST item with this key, or -1.
// Items are newest-first, so the first match is the newest. Caller holds mu.
func (n *Notifier) findByKeyLocked(key string) int {
	for i, it := range n.items {
		if it.Key == key {
			return i
		}
	}
	return -1
}

// fingerprint hashes everything the USER CAN SEE, and nothing else.
//
// Deliberately excludes ID, Time, Unread and Resolved: those are the
// store's own bookkeeping, and folding them in would make every item its
// own fingerprint and defeat dedupe entirely. Callers project their
// subsystem's state down to these fields before raising -- see the audio
// adapter, which drops the xrun counters for exactly this reason.
func fingerprint(it Item) string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			_, _ = io.WriteString(h, p)
			_, _ = h.Write([]byte{0}) // separator, so "ab"+"c" != "a"+"bc"
		}
	}
	write(it.Key, it.Category, string(it.Severity), it.Icon, it.Title, it.Body)
	for _, kv := range it.Context {
		write(kv.Key, kv.Value)
	}
	for _, a := range it.Actions {
		write(a.Label, a.Icon, a.Kind, a.Target, strconv.FormatBool(a.Primary))
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

- [ ] **Step 4: Add `MarkRead`, which the tests above call**

Append to `internal/notify/store.go`:

```go
// MarkRead clears one item's unread flag. Addressed by ID, not key, because
// the UI's row is what the user clicked.
func (n *Notifier) MarkRead(id string) {
	n.emitMu.Lock()
	defer n.emitMu.Unlock()

	n.mu.Lock()
	changed := false
	for i := range n.items {
		if n.items[i].ID == id && n.items[i].Unread {
			n.items[i].Unread = false
			changed = true
			break
		}
	}
	if !changed {
		n.mu.Unlock()
		return
	}
	snap := n.snapshotLocked()
	n.mu.Unlock()

	n.publish(snap)
}
```

- [ ] **Step 5: Run tests to verify they pass, with the race detector**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/notify/ -v`
Expected: PASS, every test including `TestConcurrentRaiseIsSafe`, with no `DATA RACE` output.

- [ ] **Step 6: Commit**

```bash
git add internal/notify/
git commit -m "feat(notify): add Raise/Resolve with identity dedupe

An identical Raise is a total no-op: no publish, no timestamp bump, no
re-mark-unread, no sound. That is what collapses emitHotkeyState's ~38
identical payloads per nineteen-action rebind, and audio:state's 2s
re-emission during a glitch, into one notification each.

The fingerprint hashes only what the user can see, so a caller projecting
its subsystem's state down to the visible fields -- as the audio adapter
does with the xrun counters -- controls its own dedupe identity."
```

---

## Task 4: Dismissal suppression, `MarkAllRead`, `Dismiss`, `Clear`

**Files:**
- Modify: `internal/notify/store.go`
- Test: `internal/notify/store_test.go` (append)

**Interfaces:**
- Consumes: Task 3's `raiseLocked`, `fingerprint`, `findByKeyLocked`, `suppressed`.
- Produces: `(*Notifier).MarkAllRead()`, `(*Notifier).Dismiss(id string)`, `(*Notifier).Clear()`.

- [ ] **Step 1: Write the failing test**

Append to `internal/notify/store_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/notify/ -run "TestDismiss|TestMarkAll|TestClear" -v`
Expected: FAIL — `n.Dismiss undefined`, `n.MarkAllRead undefined`, `n.Clear undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/notify/store.go`:

```go
// Dismiss removes one item.
//
// For a KEYED item it also records the key as suppressed AT THAT
// FINGERPRINT. Without that, dismissing "Global hotkeys unavailable" while
// hotkeys are still unavailable would put it straight back on the next
// applyHotkeys() -- which is every trigger add, every trigger removal and
// every capture end. Suppression is cleared by a Raise whose fingerprint
// differs (the condition changed) or by a Resolve (it cleared), both of
// which mean the condition is news again.
//
// An unkeyed Post needs none of this: nothing can re-raise it.
func (n *Notifier) Dismiss(id string) {
	n.emitMu.Lock()
	defer n.emitMu.Unlock()

	n.mu.Lock()
	idx := -1
	for i := range n.items {
		if n.items[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		n.mu.Unlock()
		return
	}
	it := n.items[idx]
	if it.Key != "" {
		n.suppressed[it.Key] = fingerprint(it)
	}
	n.items = append(n.items[:idx], n.items[idx+1:]...)
	snap := n.snapshotLocked()
	n.mu.Unlock()

	n.publish(snap)
}

// MarkAllRead clears every unread flag. A no-op, with no publish, when
// nothing is unread.
func (n *Notifier) MarkAllRead() {
	n.emitMu.Lock()
	defer n.emitMu.Unlock()

	n.mu.Lock()
	changed := false
	for i := range n.items {
		if n.items[i].Unread {
			n.items[i].Unread = false
			changed = true
		}
	}
	if !changed {
		n.mu.Unlock()
		return
	}
	snap := n.snapshotLocked()
	n.mu.Unlock()

	n.publish(snap)
}

// Clear removes every item, suppressing each keyed one at its current
// fingerprint for the same reason Dismiss does: CLEAR ALL must not be undone
// by the next routine re-emission of a condition that still holds.
func (n *Notifier) Clear() {
	n.emitMu.Lock()
	defer n.emitMu.Unlock()

	n.mu.Lock()
	if len(n.items) == 0 {
		n.mu.Unlock()
		return
	}
	for _, it := range n.items {
		if it.Key != "" {
			n.suppressed[it.Key] = fingerprint(it)
		}
	}
	n.items = []Item{}
	snap := n.snapshotLocked()
	n.mu.Unlock()

	n.publish(snap)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/notify/ -v`
Expected: PASS, every test.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/
git commit -m "feat(notify): add dismissal suppression, MarkAllRead and Clear

Dismissing a keyed item records the key as suppressed at that fingerprint,
so a condition that still holds cannot resurrect itself on the next
routine re-emission -- which for hotkeys is every trigger add, every
removal and every capture end. A changed fingerprint or a Resolve clears
the suppression, because both mean the condition is news again.

Clear applies the same rule to every keyed item it removes."
```

---

## Task 5: The per-source coalescing window

**Files:**
- Modify: `internal/notify/coalesce.go`, `internal/notify/store.go`, `internal/notify/notify.go`
- Test: `internal/notify/coalesce_test.go`

**Interfaces:**
- Consumes: Task 3's `Raise`/`Resolve`/`raiseLocked`.
- Produces: `Window` type (`time.Duration` alias for readability at call sites), `(*Notifier).RaiseWindowed(key string, item Item, window time.Duration)`, `(*Notifier).ResolveWindowed(key string, window time.Duration)`, `(*Notifier).StopTimers()`. `Raise`/`Resolve` keep their signatures and mean "window 0".

Constants exported for adapters: `WindowHotkeys = 0`, `WindowJoystick = 2 * time.Second`, `WindowAudio = 10 * time.Second`.

- [ ] **Step 1: Write the failing test**

Create `internal/notify/coalesce_test.go`:

```go
package notify

import (
	"testing"
	"time"
)

func TestZeroWindowEmitsImmediately(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { changes++ }})
	defer n.StopTimers()

	n.RaiseWindowed("hotkeys.global", hotkeyItem("x"), WindowHotkeys)

	if changes != 1 {
		t.Fatalf("OnChange fired %d times, want 1 -- a zero window must not defer anything", changes)
	}
}

func TestFlapInsideTheWindowCollapsesToOneEmit(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	changes := 0
	n := New(Options{Now: fixedClock(&now), OnChange: func(Snapshot) { changes++ }})
	defer n.StopTimers()

	// A joystick Poll() failing every other tick at 100 Hz: the manager's
	// edge-dedupe passes each edge through, so this is Raise/Resolve at
	// ~50 Hz. Simulated by advancing the injected clock 20ms per edge.
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, WindowJoystick)
		} else {
			n.ResolveWindowed("joystick.global", WindowJoystick)
		}
		now = now.Add(20 * time.Millisecond)
	}

	// 100 edges over 2s. The first opens the window and emits; everything
	// after is coalesced into the pending state.
	if changes > 2 {
		t.Fatalf("OnChange fired %d times for a 50 Hz flap, want at most 2 (the leading edge plus one settle)", changes)
	}
	if changes == 0 {
		t.Fatal("OnChange never fired -- the leading edge must be immediate, not deferred")
	}
}

func TestFlapThatStopsStillEmitsItsSettledState(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	settled := make(chan Snapshot, 8)
	n := New(Options{
		Now:      fixedClock(&now),
		OnChange: func(s Snapshot) { settled <- s },
	})
	defer n.StopTimers()

	// Use a short real window so the trailing timer fires inside the test.
	const window = 40 * time.Millisecond

	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)
	<-settled // the leading edge

	// Flap, then STOP with the condition true. The final Raise lands inside
	// the window, so nothing emits it synchronously -- only the trailing
	// timer can, and without one the error would never be shown.
	n.ResolveWindowed("joystick.global", window)
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)

	select {
	case s := <-settled:
		if len(s.Items) == 0 {
			t.Fatal("settled snapshot is empty")
		}
		if s.Items[0].Resolved {
			t.Fatal("settled state is Resolved; the flap stopped with the condition TRUE")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the trailing timer never fired -- a flap that stops would leave its final state never emitted")
	}
}

func TestTrailingTimerAfterClearDoesNotResurrectTheItem(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	defer n.StopTimers()

	const window = 40 * time.Millisecond

	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)
	// A second change lands inside the window, arming the trailing timer.
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable (2)", Severity: SeverityWarn}, window)

	// The user hits CLEAR ALL before the timer fires.
	n.Clear()

	time.Sleep(3 * window)

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- a trailing timer must not resurrect an item the user cleared", got)
	}
}

func TestTrailingTimerAfterDismissDoesNotResurrectTheItem(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})
	defer n.StopTimers()

	const window = 40 * time.Millisecond

	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable", Severity: SeverityWarn}, window)
	id := n.Snapshot().Items[0].ID
	n.RaiseWindowed("joystick.global", Item{Title: "Joystick unavailable (2)", Severity: SeverityWarn}, window)

	n.Dismiss(id)
	time.Sleep(3 * window)

	items := n.Snapshot().Items
	for _, it := range items {
		if it.Key == "joystick.global" && !it.Resolved {
			t.Fatalf("a dismissed key came back through its trailing timer: %+v", it)
		}
	}
}

func TestStopTimersIsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: fixedClock(&now)})

	n.RaiseWindowed("k", Item{Title: "t", Severity: SeverityWarn}, 10*time.Second)
	n.RaiseWindowed("k", Item{Title: "t2", Severity: SeverityWarn}, 10*time.Second)

	n.StopTimers()
	n.StopTimers() // must not panic on a second call
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/notify/ -run "TestZeroWindow|TestFlap|TestTrailing|TestStopTimers" -v`
Expected: FAIL — `n.RaiseWindowed undefined`, `n.ResolveWindowed undefined`, `n.StopTimers undefined`, `undefined: WindowHotkeys`.

- [ ] **Step 3: Write the coalescing implementation**

Replace `internal/notify/coalesce.go` entirely:

```go
package notify

import "time"

// Coalescing windows, per source.
//
// Deliberately NOT one global constant. The three sources have genuinely
// different cadences, and a window has to be several times its source's own
// period to coalesce anything rather than beat against it:
//
//   - Hotkeys are event-driven with no poll at all. Identity dedupe (see
//     Raise) is the whole defence; a window would only delay an honest edge.
//   - The joystick manager polls at 100 Hz, and a failing Poll() toggles the
//     error edge at up to ~50 Hz.
//   - The audio manager polls at 2s (audio.ManagerOptions.PollInterval). A
//     2s window here would beat against that, letting a device flapping at
//     the poll rate through roughly every other tick.
//
// A new source in a later phase declares its own rather than inheriting a
// value tuned for someone else's loop.
const (
	WindowHotkeys  = time.Duration(0)
	WindowJoystick = 2 * time.Second
	WindowAudio    = 10 * time.Second
)

// pendingChange is one key's coalescing state: the armed trailing timer and
// the change it will apply when the window closes.
type pendingChange struct {
	timer *time.Timer
	// apply performs the deferred mutation. nil means "nothing pending".
	apply func()
}

// coalesce either runs fn now (window open, or no window at all) or defers
// it to a trailing timer, resetting any timer already armed for this key.
//
// The trailing timer is NOT optional. Without it a flap that simply STOPS
// would leave its final state never emitted, so a device settling into a
// persistent error would show nothing at all.
//
// Caller holds neither mu nor emitMu.
func (n *Notifier) coalesce(key string, window time.Duration, fn func()) {
	if window <= 0 {
		fn()
		return
	}

	n.mu.Lock()
	p, open := n.pending[key]
	if !open {
		// Leading edge: apply immediately and open the window so the next
		// change within it is deferred.
		p = &pendingChange{}
		n.pending[key] = p
		p.timer = time.AfterFunc(window, func() { n.windowClosed(key) })
		n.mu.Unlock()
		fn()
		return
	}
	// Inside an open window: record this as the pending state, replacing any
	// earlier one. Only the LAST change in a window survives, which is the
	// whole point -- the settled state is what the user needs.
	p.apply = fn
	n.mu.Unlock()
}

// windowClosed runs when a key's window expires. It applies whatever change
// was pending and, if there was one, opens a fresh window; otherwise the key
// goes idle so the next change is again a leading edge.
func (n *Notifier) windowClosed(key string) {
	n.mu.Lock()
	p, ok := n.pending[key]
	if !ok {
		n.mu.Unlock()
		return
	}
	apply := p.apply
	p.apply = nil
	if apply == nil {
		// Nothing accumulated: the flap has stopped. Retire the key.
		delete(n.pending, key)
		n.mu.Unlock()
		return
	}
	// Something is pending, so the source is still active: keep the window
	// open for another period.
	p.timer = time.AfterFunc(n.windowFor(key), func() { n.windowClosed(key) })
	n.mu.Unlock()

	apply()
}

// windowFor recalls the window a key was last coalesced with, so a
// re-armed trailing timer uses the same period. Caller holds mu.
func (n *Notifier) windowFor(key string) time.Duration {
	if w, ok := n.windows[key]; ok {
		return w
	}
	return WindowJoystick
}

// StopTimers cancels every armed trailing timer. Called on shutdown so a
// pending emit cannot fire into a torn-down event bus. Safe to call more
// than once and from any goroutine.
func (n *Notifier) StopTimers() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for key, p := range n.pending {
		if p.timer != nil {
			p.timer.Stop()
		}
		delete(n.pending, key)
	}
}
```

- [ ] **Step 4: Add the windowed entry points and the `windows` field**

In `internal/notify/notify.go`, add one field to `Notifier` alongside `pending`:

```go
	// windows records the window each key was last coalesced with, so a
	// re-armed trailing timer uses the same period.
	windows map[string]time.Duration
```

and initialise it in `New`:

```go
		pending:    map[string]*pendingChange{},
		windows:    map[string]time.Duration{},
```

Append to `internal/notify/store.go`:

```go
// RaiseWindowed is Raise with a coalescing window. See coalesce.go for why
// the window is per-source rather than one constant.
func (n *Notifier) RaiseWindowed(key string, item Item, window time.Duration) {
	if key == "" {
		n.Post(item)
		return
	}
	n.rememberWindow(key, window)
	n.coalesce(key, window, func() { n.Raise(key, item) })
}

// ResolveWindowed is Resolve with a coalescing window.
func (n *Notifier) ResolveWindowed(key string, window time.Duration) {
	if key == "" {
		return
	}
	n.rememberWindow(key, window)
	n.coalesce(key, window, func() { n.Resolve(key) })
}

// rememberWindow records a key's window for windowFor.
func (n *Notifier) rememberWindow(key string, window time.Duration) {
	n.mu.Lock()
	n.windows[key] = window
	n.mu.Unlock()
}
```

Add `"time"` to `store.go`'s imports.

- [ ] **Step 5: Make `Dismiss` and `Clear` cancel pending timers**

This is Review Focus item 3: a trailing timer scheduled before a dismiss or a clear would otherwise resurrect the item.

In `Dismiss`, immediately after `it := n.items[idx]`, add:

```go
	if it.Key != "" {
		n.cancelPendingLocked(it.Key)
	}
```

In `Clear`, inside the loop over `n.items`, after the `suppressed` write, add:

```go
		if it.Key != "" {
			n.cancelPendingLocked(it.Key)
		}
```

And append to `coalesce.go`:

```go
// cancelPendingLocked drops a key's armed timer and pending change, so a
// window opened before the user dismissed or cleared the item cannot fire
// afterwards and resurrect it. Caller holds mu.
func (n *Notifier) cancelPendingLocked(key string) {
	p, ok := n.pending[key]
	if !ok {
		return
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	delete(n.pending, key)
}
```

Note: `Dismiss` and `Clear` hold `mu` at these points, and `cancelPendingLocked` takes no lock, so this is safe. Suppression (Task 4) is what stops a *later* Raise from re-adding the item; cancelling the timer is what stops an *already-scheduled* one.

- [ ] **Step 6: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/notify/ -v`
Expected: PASS, every test in all three files.

- [ ] **Step 7: Re-verify the package still has no forbidden imports**

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
grep -rniE "internal/(app|hotkeys|joystick|audio)" internal/notify/ ; echo "exit=$?"
```
Expected: no matches, `exit=1`. (The word "audio" appears in a comment in `coalesce.go` explaining the window; that is prose about a caller, not an import. If the grep matches only that comment line, it is fine — check the match before acting.)

- [ ] **Step 8: Commit**

```bash
git add internal/notify/
git commit -m "feat(notify): add the per-source coalescing window

Identity dedupe alone does not stop a flap: error->nil->error against a
condition-backed store is three genuine state changes, and the joystick
manager polls at 100 Hz. Each key gets a leading-edge emit plus a trailing
timer, so a 50 Hz flap collapses to one update per window and a flap that
stops still emits its settled state.

The window is per-source (hotkeys 0, joystick 2s, audio 10s) because a
window must be several times its source's own period to coalesce rather
than beat against it -- audio's own poll is 2s.

Dismiss and Clear cancel any armed timer, so a window opened before the
user cleared an item cannot fire afterwards and resurrect it."
```

---

## Task 6: `App` bindings and the `notifications:changed` event

**Files:**
- Create: `internal/app/notifications.go`
- Modify: `internal/events/events.go`, `internal/app/app.go`
- Test: `internal/app/notifications_test.go`

**Interfaces:**
- Consumes: Task 1–5's `notify.Notifier`, `notify.Snapshot`, `notify.Item`.
- Produces: `(*App).SetNotifier(*notify.Notifier)`, `(*App).Notifier() *notify.Notifier`, `(*App).GetNotifications() notify.Snapshot`, `(*App).MarkNotificationRead(string)`, `(*App).MarkAllNotificationsRead()`, `(*App).DismissNotification(string)`, `(*App).ClearNotifications()`, `(*App).FocusMainWindow()`, `events.EventNotifications = "notifications:changed"`, `(*events.Tagged).Notifications(any)`.

- [ ] **Step 1: Write the failing test**

Create `internal/app/notifications_test.go`:

```go
package app

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
)

func TestGetNotificationsWithNoNotifierIsEmpty(t *testing.T) {
	a := NewForTest(nil, nil, nil)

	got := a.GetNotifications()
	if len(got.Items) != 0 {
		t.Fatalf("Items = %d, want 0", len(got.Items))
	}
	if got.Items == nil {
		t.Fatal("Items is nil; must be [] so the frontend's list type holds")
	}
}

func TestFocusMainWindowWithNoWailsAppDoesNotPanic(t *testing.T) {
	// Every test builds an App with no Wails application. The main window
	// is not a Registry entry -- it is a Wails window resolved by name --
	// so this binding must tolerate that.
	a := NewForTest(nil, nil, nil)
	a.FocusMainWindow()
}

func TestNotificationBindingsWithNoNotifierDoNotPanic(t *testing.T) {
	a := NewForTest(nil, nil, nil)

	// Every binding must tolerate a nil notifier, the same discipline
	// settings.audio and settings.joy already follow: nil in tests and in
	// any build where wiring failed.
	a.MarkNotificationRead("n1")
	a.MarkAllNotificationsRead()
	a.DismissNotification("n1")
	a.ClearNotifications()
}

func TestGetNotificationsReflectsTheNotifier(t *testing.T) {
	a := NewForTest(nil, nil, nil)
	n := notify.New(notify.Options{})
	a.SetNotifier(n)

	n.Post(notify.Item{Title: "hello", Severity: notify.SeverityWarn})

	got := a.GetNotifications()
	if len(got.Items) != 1 {
		t.Fatalf("Items = %d, want 1", len(got.Items))
	}
	if got.Items[0].Title != "hello" {
		t.Fatalf("Title = %q, want \"hello\"", got.Items[0].Title)
	}
	if got.Unread != 1 {
		t.Fatalf("Unread = %d, want 1", got.Unread)
	}
}

func TestMarkAllNotificationsReadReachesTheNotifier(t *testing.T) {
	a := NewForTest(nil, nil, nil)
	n := notify.New(notify.Options{})
	a.SetNotifier(n)
	n.Post(notify.Item{Title: "x", Severity: notify.SeverityError})

	a.MarkAllNotificationsRead()

	if got := a.GetNotifications().Unread; got != 0 {
		t.Fatalf("Unread = %d, want 0", got)
	}
}

func TestDismissNotificationReachesTheNotifier(t *testing.T) {
	a := NewForTest(nil, nil, nil)
	n := notify.New(notify.Options{})
	a.SetNotifier(n)
	id := n.Post(notify.Item{Title: "x", Severity: notify.SeverityWarn})

	a.DismissNotification(id)

	if got := len(a.GetNotifications().Items); got != 0 {
		t.Fatalf("Items = %d, want 0", got)
	}
}

func TestClearNotificationsReachesTheNotifier(t *testing.T) {
	a := NewForTest(nil, nil, nil)
	n := notify.New(notify.Options{})
	a.SetNotifier(n)
	n.Post(notify.Item{Title: "x", Severity: notify.SeverityWarn})
	n.Post(notify.Item{Title: "y", Severity: notify.SeverityWarn})

	a.ClearNotifications()

	if got := len(a.GetNotifications().Items); got != 0 {
		t.Fatalf("Items = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestNotification -v`
Expected: FAIL — `a.GetNotifications undefined`, `a.SetNotifier undefined`.

- [ ] **Step 3: Add the event constant and emitter**

In `internal/events/events.go`, add to the const block after `EventConnectionState`:

```go
	// EventNotifications carries the FULL notification Snapshot, not a
	// delta -- same reasoning as EventKeybindsChanged: the list is capped
	// (notify.DefaultCap) and a replacement removes a class of
	// frontend/backend divergence bug.
	//
	// Broadcast to every window, because the notification store is
	// Go-owned. A popout is a separate webview with its own JS heap, so a
	// list held in the main window's Zustand store would be invisible to
	// the Notifications window -- the same reason connection:state and
	// window:state are Go-owned.
	EventNotifications = "notifications:changed"
```

and add the emitter method near `ConnectionHealth`:

```go
// Notifications emits EventNotifications with the full snapshot. The payload
// is typed as any for the same reason KeybindsChanged's is: the concrete
// shape is notify.Snapshot, and naming it here would make this package
// depend on one of its own consumers' collaborators for no benefit.
func (t *Tagged) Notifications(payload any) { t.em.Emit(EventNotifications, payload) }
```

- [ ] **Step 4: Add the `App` field and bindings**

In `internal/app/app.go`, add to the `App` struct after `health`:

```go
	// notif is the notification channel. Optional, the same discipline as
	// settings.audio, settings.joy and health: nil in tests and in any
	// build where wiring failed, so every use site must check.
	notif *notify.Notifier
```

Add the `notify` import.

Create `internal/app/notifications.go`:

```go
package app

import "github.com/FPGSchiba/vcs-srs-client/internal/notify"

// SetNotifier wires the notification channel. A nil notifier is legal and
// leaves every binding below inert, so a build whose wiring failed degrades
// to "no notifications" rather than crashing.
func (a *App) SetNotifier(n *notify.Notifier) { a.notif = n }

// Notifier returns the wired channel, or nil. Used by main.go's audio
// adapter, which sits outside internal/app because audio has two emit sites
// and only one of them is a Manager callback.
func (a *App) Notifier() *notify.Notifier { return a.notif }

// GetNotifications returns the current snapshot for a window hydrating on
// mount. Returns an empty, non-nil list when nothing is wired: the frontend
// types Items as an array, and a nil slice marshals to null.
func (a *App) GetNotifications() notify.Snapshot {
	if a.notif == nil {
		return notify.Snapshot{Items: []notify.Item{}}
	}
	return a.notif.Snapshot()
}

// MarkNotificationRead clears one item's unread flag.
func (a *App) MarkNotificationRead(id string) {
	if a.notif == nil {
		return
	}
	a.notif.MarkRead(id)
}

// MarkAllNotificationsRead clears every unread flag.
func (a *App) MarkAllNotificationsRead() {
	if a.notif == nil {
		return
	}
	a.notif.MarkAllRead()
}

// DismissNotification removes one item. For a keyed item this also suppresses
// an identical re-raise, so a condition that still holds cannot put the row
// straight back -- see notify.Notifier.Dismiss.
func (a *App) DismissNotification(id string) {
	if a.notif == nil {
		return
	}
	a.notif.Dismiss(id)
}

// ClearNotifications removes every item.
func (a *App) ClearNotifications() {
	if a.notif == nil {
		return
	}
	a.notif.Clear()
}

// FocusMainWindow shows and focuses the main client window.
//
// Needed because a notification's `navigate` action fires from the
// Notifications POPOUT, and the main window is NOT a Registry entry: it is
// created directly in main.go as a Wails window named MainWindowName, so
// windows.Open("main") would fall through windowURL's default and spawn a
// SECOND window at /main.html rather than focusing the existing one.
//
// Delegates to showMainWindow (internal/app/tray.go), which the tray menu
// and the dock-icon reactivation path already use -- one implementation of
// "reveal and focus the main window", not two.
//
// The nil check is this function's own: showMainWindow dereferences
// a.wailsApp unguarded, which is safe from the tray (it cannot exist without
// one) but not from a frontend binding, and every test builds an App with no
// Wails application at all.
func (a *App) FocusMainWindow() {
	if a.wailsApp == nil {
		return
	}
	a.showMainWindow()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/app/ ./internal/events/ -v 2>&1 | tail -30`
Expected: PASS, including the six new tests.

- [ ] **Step 6: Regenerate the TypeScript bindings**

The seven new `App` methods must reach `frontend/bindings/`, which is gitignored and stale.

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
wails3 generate bindings -ts -f "-tags purego" -clean=true
grep -c "GetNotifications\|MarkNotificationRead\|ClearNotifications" frontend/bindings/github.com/FPGSchiba/vcs-srs-client/internal/app/*.ts
```
Expected: a non-zero count. If `wails3` is unavailable, stop and report it — later frontend tasks cannot typecheck without this.

- [ ] **Step 7: Commit**

```bash
git add internal/app/notifications.go internal/app/notifications_test.go internal/app/app.go internal/events/events.go
git commit -m "feat(app): add the notification bindings and notifications:changed

The store is Go-owned because a popout is a separate webview with its own
JS heap: a list held in the main window's Zustand store would be invisible
to the Notifications window. Same reason connection:state and window:state
are Go-owned.

A nil notifier leaves every binding inert, matching the discipline
settings.audio, settings.joy and health already follow."
```

---

## Task 7: Wire the notifier in `main.go`

**Files:**
- Modify: `main.go`
- Test: `main_wiring_test.go` (append)

**Interfaces:**
- Consumes: Task 6's `SetNotifier`, `events.Tagged.Notifications`.
- Produces: a live `*notify.Notifier` on the `App`, publishing to the event bus.

- [ ] **Step 1: Write the failing test**

Append to `main_wiring_test.go`:

```go
func TestNotifierPublishesOntoTheEventBus(t *testing.T) {
	rec := &recordingEmitter{}
	ev := vcsevents.New(rec)

	n := notify.New(notify.Options{
		OnChange: func(s notify.Snapshot) { ev.Notifications(s) },
	})

	n.Post(notify.Item{Title: "wired", Severity: notify.SeverityWarn})

	if got := rec.countOf(vcsevents.EventNotifications); got != 1 {
		t.Fatalf("emitted %d notifications:changed events, want 1", got)
	}
}

func TestNotifierSoundHookIsGatedBySeverity(t *testing.T) {
	var played []string
	n := notify.New(notify.Options{
		OnSound: func(it notify.Item) { played = append(played, it.Title) },
	})

	n.Post(notify.Item{Title: "err", Severity: notify.SeverityError})
	n.Post(notify.Item{Title: "warn", Severity: notify.SeverityWarn})

	if len(played) != 1 || played[0] != "err" {
		t.Fatalf("played = %v, want [err] -- sound follows the toast, error only", played)
	}
}
```

If `recordingEmitter` / `countOf` do not already exist in `main_wiring_test.go`, read the file first and reuse whatever fake emitter it already defines, adapting the assertions to that fake's API. Do **not** add a second fake emitter alongside an existing one.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego . -run TestNotifier -v`
Expected: FAIL — `undefined: notify` (the import is missing) or `undefined: vcsevents.EventNotifications` if Task 6 was skipped.

- [ ] **Step 3: Wire it in `main.go`**

Add the import:

```go
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
```

Immediately after the `monitor.Start()` line (which follows `gui.SetConnHealth(monitor)`), insert:

```go
	// The notification channel. Constructed before the settings, joystick
	// and audio backends because all three raise into it during their own
	// wiring -- applyHotkeys runs inside SetSettingsBackend, and the
	// joystick manager's first enumeration fires as soon as Start is called.
	//
	// OnSound is routed through gui rather than straight to the audio
	// Manager: the Manager does not exist yet at this point, and the gate
	// also has to consult general.play_notification_sounds, which lives in
	// the settings backend. See app.PlayNotificationSFX.
	notifEvents := vcsevents.New(emitter)
	notifier := notify.New(notify.Options{
		OnChange: func(s notify.Snapshot) { notifEvents.Notifications(s) },
		OnSound:  func(it notify.Item) { gui.PlayNotificationSFX(string(it.Severity)) },
	})
	gui.SetNotifier(notifier)
	// Cancels any armed trailing timer so a coalesced emit cannot fire into
	// a torn-down event bus during shutdown.
	defer notifier.StopTimers()
```

`gui.PlayNotificationSFX` does not exist yet — Task 12 adds it. Until then this will not compile, so **for this task only**, use a stub that Task 12 replaces:

```go
		OnSound:  func(it notify.Item) { _ = it },
```

and leave a `// TASK 12: route to gui.PlayNotificationSFX` marker on that line so the later task finds it. This is the one place in this plan where a temporary stub is correct: the sound engine is three tasks away and blocking the whole channel on it would be worse.

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./... && GOCACHE=$TMPDIR/vcs-gocache go test -tags purego . -run TestNotifier -v`
Expected: build succeeds, both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go main_wiring_test.go
git commit -m "feat(main): wire the notification channel onto the event bus

Constructed before the settings, joystick and audio backends because all
three raise into it during their own wiring: applyHotkeys runs inside
SetSettingsBackend and the joystick manager enumerates as soon as Start
is called.

The sound hook is a stub until the engine lands; StopTimers is deferred so
a coalesced emit cannot fire into a torn-down event bus."
```

---

## Task 8: The hotkey and joystick adapters

**Files:**
- Create: `internal/app/notify_keybinds.go`
- Modify: `internal/app/settings.go` (two call sites)
- Test: `internal/app/notify_keybinds_test.go`

**Interfaces:**
- Consumes: Task 6's `a.notif`; existing `HotkeyStateDTO`, `JoystickStateDTO`, `a.labelFor`, `keybinds.ActionID`.
- Produces: `(*App).notifyHotkeyState(HotkeyStateDTO)`, `(*App).notifyJoystickState(JoystickStateDTO)`.

- [ ] **Step 1: Write the failing test**

Create `internal/app/notify_keybinds_test.go`:

```go
package app

import (
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
)

// withNotifier builds an App with a live notifier and returns both.
func withNotifier(t *testing.T) (*App, *notify.Notifier) {
	t.Helper()
	a := NewForTest(nil, nil, nil)
	n := notify.New(notify.Options{})
	a.SetNotifier(n)
	return a, n
}

func TestGlobalHotkeyFailureRaisesOnceWithPermission(t *testing.T) {
	a, n := withNotifier(t)

	dto := HotkeyStateDTO{
		Registered: false,
		Error:      "no backend",
		Failed:     map[string]string{"global.ptt": "unsupported key"},
		Permission: "denied",
	}
	// Emitted repeatedly, as emitHotkeyState genuinely is.
	for i := 0; i < 10; i++ {
		a.notifyHotkeyState(dto)
	}

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1 -- a global failure notifies ONCE, and the per-binding rows must be suppressed while Registered is false", len(items))
	}
	it := items[0]
	if it.Key != "hotkeys.global" {
		t.Fatalf("Key = %q, want \"hotkeys.global\"", it.Key)
	}
	if it.Severity != notify.SeverityError {
		t.Fatalf("Severity = %q, want error", it.Severity)
	}
	if !strings.Contains(it.Body, "no backend") {
		t.Fatalf("Body = %q, want it to carry the error text", it.Body)
	}
	found := false
	for _, kv := range it.Context {
		if kv.Key == "PERMISSION" && kv.Value == "denied" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Context = %v, want a PERMISSION row carrying \"denied\"", it.Context)
	}
	if len(it.Actions) != 1 || it.Actions[0].Kind != "navigate" || it.Actions[0].Target != "settings" {
		t.Fatalf("Actions = %v, want one navigate->settings action", it.Actions)
	}
}

func TestPermissionChangeUpdatesInPlaceRatherThanStacking(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e", Permission: "denied"})
	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e", Permission: "granted"})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1 -- the permission state is inside the fingerprint, so this UPDATES", len(items))
	}
	if !strings.Contains(contextValue(items[0], "PERMISSION"), "granted") {
		t.Fatalf("PERMISSION = %q, want \"granted\"", contextValue(items[0], "PERMISSION"))
	}
}

func contextValue(it notify.Item, key string) string {
	for _, kv := range it.Context {
		if kv.Key == key {
			return kv.Value
		}
	}
	return ""
}

func TestPerBindingFailuresOnlyWhileRegistered(t *testing.T) {
	a, n := withNotifier(t)

	// Registered TRUE with a partial failure: nineteen bindings work, one
	// Numpad7 does not.
	a.notifyHotkeyState(HotkeyStateDTO{
		Registered: true,
		Failed:     map[string]string{"global.ptt": "Numpad7 cannot be registered"},
		Permission: "granted",
	})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	if items[0].Key != "hotkeys.binding.global.ptt" {
		t.Fatalf("Key = %q, want \"hotkeys.binding.global.ptt\"", items[0].Key)
	}
	if items[0].Severity != notify.SeverityWarn {
		t.Fatalf("Severity = %q, want warn", items[0].Severity)
	}
	if !strings.Contains(items[0].Title, "Push-To-Talk") && !strings.Contains(items[0].Title, "global.ptt") {
		t.Fatalf("Title = %q, want it to NAME the action", items[0].Title)
	}
}

func TestPerBindingFailureResolvesWhenItLeavesFailed(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyHotkeyState(HotkeyStateDTO{
		Registered: true,
		Failed:     map[string]string{"global.ptt": "bad key"},
		Permission: "granted",
	})
	a.notifyHotkeyState(HotkeyStateDTO{Registered: true, Failed: map[string]string{}, Permission: "granted"})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1 (retained, resolved)", len(items))
	}
	if !items[0].Resolved {
		t.Fatal("the per-binding item did not resolve when the action left Failed")
	}
}

func TestGlobalSuccessResolvesEverything(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e", Permission: "denied"})
	a.notifyHotkeyState(HotkeyStateDTO{Registered: true, Failed: map[string]string{}, Permission: "granted"})

	for _, it := range n.Snapshot().Items {
		if !it.Resolved {
			t.Fatalf("item %q is still unresolved after a successful registration", it.Key)
		}
	}
	if got := n.Snapshot().Unread; got != 0 {
		t.Fatalf("Unread = %d, want 0", got)
	}
}

func TestJoystickUnsupportedIsInfoAndNeverAnError(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyJoystickState(JoystickStateDTO{Supported: false, Devices: []JoystickDeviceDTO{}})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	it := items[0]
	if it.Severity != notify.SeverityInfo {
		t.Fatalf("Severity = %q, want info -- \"unsupported\" must NEVER render as a failure", it.Severity)
	}
	if it.Unread {
		t.Fatal("the unsupported notice is unread; info is raised already-read so it never reaches the badge, bell or toast")
	}
	if len(it.Actions) != 0 {
		t.Fatalf("Actions = %v, want none -- there is nothing for the user to grant", it.Actions)
	}
	if got := n.Snapshot().Unread; got != 0 {
		t.Fatalf("Unread = %d, want 0", got)
	}
}

func TestJoystickErrorIsWarnAndResolves(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyJoystickState(JoystickStateDTO{Supported: true, Error: "permission denied on /dev/input", Devices: []JoystickDeviceDTO{}})
	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Severity != notify.SeverityWarn {
		t.Fatalf("items = %v, want one warn item", items)
	}

	a.notifyJoystickState(JoystickStateDTO{Supported: true, Error: "", Devices: []JoystickDeviceDTO{}})
	if !n.Snapshot().Items[0].Resolved {
		t.Fatal("the joystick item did not resolve when the error cleared")
	}
}

func TestNotifyWithNoNotifierDoesNotPanic(t *testing.T) {
	a := NewForTest(nil, nil, nil)
	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e"})
	a.notifyJoystickState(JoystickStateDTO{Supported: false})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run "TestGlobalHotkey|TestPerBinding|TestJoystick|TestPermissionChange|TestGlobalSuccess|TestNotifyWithNo" -v`
Expected: FAIL — `a.notifyHotkeyState undefined`, `a.notifyJoystickState undefined`.

- [ ] **Step 3: Write the adapters**

Create `internal/app/notify_keybinds.go`:

```go
package app

import (
	"sort"

	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
)

// notifyCategory is the only category Phase 7.2 emits. The full seven-value
// table lives in the frontend (shared/components/notificationCategories.ts);
// the backend only ever needs to name one.
const notifyCategory = "system"

// notifyHotkeyState translates one HotkeyStateDTO into notifications.
//
// Two of the ROADMAP's three required cases, and they must stay distinct:
//
//   - A GLOBAL failure (Registered == false) notifies ONCE, carrying the
//     permission state so the UI branches on a state rather than on the
//     registrar's error text.
//   - A PER-BINDING failure notifies per action, NAMING the action -- but
//     only while Registered is true. Registered == false implies Failed
//     names every bound action, so raising them alongside the global item
//     would reprint one message nineteen times. This mirrors the rule
//     Keybinds.tsx:332 already enforces for the inline per-row text.
//
// Called from emitHotkeyState, which is the sole caller of
// events.Tagged.HotkeysState -- so no second path can bypass the channel.
func (a *App) notifyHotkeyState(dto HotkeyStateDTO) {
	n := a.notif
	if n == nil {
		return
	}

	if dto.Registered {
		n.ResolveWindowed(keyHotkeysGlobal, notify.WindowHotkeys)
	} else {
		n.RaiseWindowed(keyHotkeysGlobal, notify.Item{
			Category: notifyCategory,
			Severity: notify.SeverityError,
			Icon:     "bolt",
			Title:    "Global hotkeys unavailable",
			Body:     dto.Error,
			Context:  []notify.KV{{Key: "PERMISSION", Value: dto.Permission}},
			Actions: []notify.Action{{
				Label:   "OPEN KEYBIND SETTINGS",
				Icon:    "settings",
				Kind:    "navigate",
				Target:  "settings",
				Primary: true,
			}},
		}, notify.WindowHotkeys)
	}

	// Per-binding rows. Sorted so the raise order is deterministic, which
	// makes the resulting list order stable across runs and testable.
	failed := dto.Failed
	if !dto.Registered {
		failed = nil // suppressed; the global item covers it
	}
	ids := make([]string, 0, len(failed))
	for id := range failed {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		n.RaiseWindowed(bindingKey(id), notify.Item{
			Category: notifyCategory,
			Severity: notify.SeverityWarn,
			Icon:     "bolt",
			Title:    "Binding not registered · " + a.labelFor(keybinds.ActionID(id)),
			Body:     failed[id],
			Actions: []notify.Action{{
				Label:  "OPEN KEYBIND SETTINGS",
				Icon:   "settings",
				Kind:   "navigate",
				Target: "settings",
			}},
		}, notify.WindowHotkeys)
	}

	// Resolve any per-binding item whose action is no longer failing. The
	// previous snapshot is the notifier's own list, so this needs no extra
	// bookkeeping here.
	for _, it := range n.Snapshot().Items {
		if it.Resolved || !isBindingKey(it.Key) {
			continue
		}
		if _, still := failed[actionIDFromBindingKey(it.Key)]; !still {
			n.ResolveWindowed(it.Key, notify.WindowHotkeys)
		}
	}
}

// notifyJoystickState translates one JoystickStateDTO into notifications.
//
// The third required case, and the one with a trap: "unsupported" is
// INFORMATIONAL, never a failure. macOS reports it because Star Citizen has
// no macOS build, so there is nothing for the user to grant and no action to
// offer. Info severity is raised already-read by the store, so it reaches
// neither the badge, the bell nor a toast -- the rule is enforced at the
// delivery layer rather than in the colour of a border.
//
// Called from emitJoystickState, the sole caller of
// events.Tagged.JoystickState.
func (a *App) notifyJoystickState(dto JoystickStateDTO) {
	n := a.notif
	if n == nil {
		return
	}

	switch {
	case !dto.Supported:
		n.RaiseWindowed(keyJoystickGlobal, notify.Item{
			Category: notifyCategory,
			Severity: notify.SeverityInfo,
			Icon:     "knob",
			Title:    "Joystick input is unsupported on this platform",
			Body:     "Joystick and gamepad bindings are available on Windows and Linux only.",
		}, notify.WindowJoystick)
	case dto.Error != "":
		n.RaiseWindowed(keyJoystickGlobal, notify.Item{
			Category: notifyCategory,
			Severity: notify.SeverityWarn,
			Icon:     "knob",
			Title:    "Joystick unavailable",
			Body:     dto.Error,
			Actions: []notify.Action{{
				Label:  "OPEN KEYBIND SETTINGS",
				Icon:   "settings",
				Kind:   "navigate",
				Target: "settings",
			}},
		}, notify.WindowJoystick)
	default:
		n.ResolveWindowed(keyJoystickGlobal, notify.WindowJoystick)
	}
}

// Notification keys. Stable strings, because dedupe, resolution and
// dismissal suppression are all keyed on them.
const (
	keyHotkeysGlobal  = "hotkeys.global"
	keyJoystickGlobal = "joystick.global"
	bindingKeyPrefix  = "hotkeys.binding."
)

func bindingKey(actionID string) string { return bindingKeyPrefix + actionID }

func isBindingKey(key string) bool {
	return len(key) > len(bindingKeyPrefix) && key[:len(bindingKeyPrefix)] == bindingKeyPrefix
}

func actionIDFromBindingKey(key string) string {
	if !isBindingKey(key) {
		return ""
	}
	return key[len(bindingKeyPrefix):]
}
```

- [ ] **Step 4: Call the adapters from the two existing funnels**

In `internal/app/settings.go`, in `emitHotkeyState`, after the existing emit:

```go
func (a *App) emitHotkeyState() {
	sb := a.settings
	dto := hotkeyStateDTO(sb.hk.State(), a.permissionStatus())
	sb.em.HotkeysState(dto.Registered, dto.Error, dto.Failed, dto.Permission)
	// The notification channel's hotkey source. Hung off this funnel rather
	// than off the event bus because this is the SOLE caller of
	// HotkeysState, so nothing can emit the state without also notifying.
	a.notifyHotkeyState(dto)
}
```

and in `emitJoystickState`, after `sb.em.JoystickState(...)`:

```go
	// The notification channel's joystick source -- same reasoning as
	// emitHotkeyState's.
	a.notifyJoystickState(dto)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/app/ -v 2>&1 | tail -40`
Expected: PASS, including the eight new tests and every pre-existing `internal/app` test.

- [ ] **Step 6: Commit**

```bash
git add internal/app/notify_keybinds.go internal/app/notify_keybinds_test.go internal/app/settings.go
git commit -m "feat(app): route hotkey and joystick state into the notification channel

Both adapters hang off the existing emit funnels rather than the event bus,
because emitHotkeyState and emitJoystickState are each the sole caller of
their emitter -- so no second path can bypass the channel.

Per-binding rows are raised only while Registered is true, mirroring the
rule Keybinds.tsx:332 already enforces: Registered == false implies Failed
names every bound action, so raising them beside the global item would
reprint one message nineteen times.

macOS's \"joystick unsupported\" is info severity, which the store raises
already-read, so it reaches neither badge, bell nor toast."
```

---

## Task 9: The audio adapter

**Files:**
- Create: `internal/app/notify_audio.go`
- Modify: `main.go`
- Test: `internal/app/notify_audio_test.go`

**Interfaces:**
- Consumes: Task 6's `a.notif`; existing `AudioStateDTO`.
- Produces: `(*App).NotifyAudioState(AudioStateDTO)` — exported, because `main.go` calls it from both audio emit sites.

**Why this lives in `internal/app` but is called from `main.go`:** audio has two emit sites and only one is a Manager callback. Putting the translation here keeps it testable without a Manager; calling it from `main.go` is what covers the no-backend path.

- [ ] **Step 1: Write the failing test**

Create `internal/app/notify_audio_test.go`:

```go
package app

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
)

func TestXrunCountersProduceNoNotification(t *testing.T) {
	a, n := withNotifier(t)

	// THE critical audio test. emitStateIfChanged compares the whole State
	// struct, xrun counters included, and runs every 2s poll tick -- so a
	// glitching engine emits ~1800 times an hour with nothing the user can
	// see having changed. The adapter's projection is what makes including
	// audio safe at all.
	base := AudioStateDTO{Running: true, InputDevice: "mic-1", OutputDevice: "spk-1"}
	for i := 0; i < 1800; i++ {
		st := base
		st.Overruns = uint64(i)
		st.Underruns = uint64(i * 2)
		a.NotifyAudioState(st)
	}

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- xrun counters must never produce a notification", got)
	}
}

func TestRunningAndStartingAreNotFaults(t *testing.T) {
	a, n := withNotifier(t)

	a.NotifyAudioState(AudioStateDTO{Running: false, Starting: true})
	a.NotifyAudioState(AudioStateDTO{Running: true, Starting: false})

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- lifecycle is not a fault", got)
	}
}

func TestInputErrorRaisesAnErrorNotification(t *testing.T) {
	a, n := withNotifier(t)

	a.NotifyAudioState(AudioStateDTO{InputError: "device not found"})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	it := items[0]
	if it.Key != "audio.input" {
		t.Fatalf("Key = %q, want \"audio.input\"", it.Key)
	}
	if it.Severity != notify.SeverityError {
		t.Fatalf("Severity = %q, want error -- a dead microphone in a voice-comms client is critical", it.Severity)
	}
	if it.Body != "device not found" {
		t.Fatalf("Body = %q, want the backend's error text", it.Body)
	}
}

func TestInputAndOutputAreIndependentKeys(t *testing.T) {
	a, n := withNotifier(t)

	a.NotifyAudioState(AudioStateDTO{InputError: "mic gone", OutputSubstituted: true, OutputDevice: "spk-default"})

	items := n.Snapshot().Items
	if len(items) != 2 {
		t.Fatalf("Items = %d, want 2 -- a failed input and a substituted output are separate items", len(items))
	}
	keys := map[string]bool{}
	for _, it := range items {
		keys[it.Key] = true
	}
	if !keys["audio.input"] || !keys["audio.output.substituted"] {
		t.Fatalf("keys = %v, want audio.input and audio.output.substituted", keys)
	}
}

func TestSubstitutionIsWarnNotError(t *testing.T) {
	a, n := withNotifier(t)

	a.NotifyAudioState(AudioStateDTO{InputSubstituted: true, InputDevice: "mic-default"})

	it := n.Snapshot().Items[0]
	if it.Severity != notify.SeverityWarn {
		t.Fatalf("Severity = %q, want warn -- audio still works, just not on the chosen device", it.Severity)
	}
	if it.Key != "audio.input.substituted" {
		t.Fatalf("Key = %q, want \"audio.input.substituted\"", it.Key)
	}
	if contextValue(it, "IN USE") != "mic-default" {
		t.Fatalf("IN USE = %q, want \"mic-default\"", contextValue(it, "IN USE"))
	}
}

func TestEachAudioKeyResolvesIndependently(t *testing.T) {
	a, n := withNotifier(t)

	a.NotifyAudioState(AudioStateDTO{InputError: "mic gone", OutputError: "spk gone"})
	// The output recovers; the input does not.
	a.NotifyAudioState(AudioStateDTO{InputError: "mic gone"})

	var input, output notify.Item
	for _, it := range n.Snapshot().Items {
		switch it.Key {
		case "audio.input":
			input = it
		case "audio.output":
			output = it
		}
	}
	if input.Resolved {
		t.Fatal("the input item resolved while its error was still present")
	}
	if !output.Resolved {
		t.Fatal("the output item did not resolve when its error cleared")
	}
}

func TestNoBackendDTORaisesBothErrorKeys(t *testing.T) {
	a, n := withNotifier(t)

	// main.go:225's hand-pushed DTO when NewMalgoBackend fails: no Manager
	// exists, so this is the ONLY signal that audio is dead entirely. An
	// adapter hung only off the Manager's OnState would miss it.
	a.NotifyAudioState(AudioStateDTO{
		InputError:  "malgo: no backend",
		OutputError: "malgo: no backend",
	})

	items := n.Snapshot().Items
	if len(items) != 2 {
		t.Fatalf("Items = %d, want 2 -- both directions genuinely are dead", len(items))
	}
	for _, it := range items {
		if it.Severity != notify.SeverityError {
			t.Fatalf("item %q severity = %q, want error", it.Key, it.Severity)
		}
	}
}

func TestAudioNotifyWithNoNotifierDoesNotPanic(t *testing.T) {
	a := NewForTest(nil, nil, nil)
	a.NotifyAudioState(AudioStateDTO{InputError: "e"})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run "TestXrun|TestRunningAnd|TestInput|TestSubstitution|TestEachAudio|TestNoBackend|TestAudioNotify" -v`
Expected: FAIL — `a.NotifyAudioState undefined`.

- [ ] **Step 3: Write the adapter**

Create `internal/app/notify_audio.go`:

```go
package app

import "github.com/FPGSchiba/vcs-srs-client/internal/notify"

// Audio notification keys. Four independent keys, not one, so a failed
// input and a substituted output are separate items that resolve
// independently.
const (
	keyAudioInput            = "audio.input"
	keyAudioOutput           = "audio.output"
	keyAudioInputSubstituted = "audio.input.substituted"
	keyAudioOutputSubstituted = "audio.output.substituted"
)

// NotifyAudioState translates one AudioStateDTO into notifications.
//
// This function is a PROJECTION, and the projection is the whole reason
// audio is safe to route here at all. AudioStateDTO has nine fields;
// exactly four are faults:
//
//	InputError, OutputError                 -- the fault itself
//	InputSubstituted, OutputSubstituted     -- works, but not on the chosen device
//
// The other five are deliberately dropped:
//
//	Overruns, Underruns  -- MONOTONIC COUNTERS. audio.Manager's
//	                        emitStateIfChanged compares the whole State
//	                        struct and runs at the end of every poll tick
//	                        (PollInterval defaults to 2s), so a glitching
//	                        engine re-emits roughly 1800 times an hour with
//	                        nothing the user can see having changed. Folding
//	                        these into the notification would produce 1800
//	                        notifications; dropping them produces one.
//	Running, Starting    -- lifecycle, not a fault.
//	InputDevice,
//	OutputDevice         -- which device is in use is already carried by the
//	                        substitution flags, and putting the id in the
//	                        fingerprint would re-notify on every hot-plug
//	                        reshuffle.
//
// Exported because main.go calls it from BOTH audio emit sites: the
// Manager's OnState callback, and the hand-pushed DTO for the case where
// NewMalgoBackend failed and no Manager was ever constructed. An adapter
// hung only off OnState would miss the most severe audio failure there is.
func (a *App) NotifyAudioState(dto AudioStateDTO) {
	n := a.notif
	if n == nil {
		return
	}

	raiseOrResolve := func(key string, cond bool, item notify.Item) {
		if cond {
			n.RaiseWindowed(key, item, notify.WindowAudio)
			return
		}
		n.ResolveWindowed(key, notify.WindowAudio)
	}

	raiseOrResolve(keyAudioInput, dto.InputError != "", notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityError,
		Icon:     "mic",
		Title:    "Microphone unavailable",
		Body:     dto.InputError,
		Actions:  []notify.Action{audioSettingsAction()},
	})

	raiseOrResolve(keyAudioOutput, dto.OutputError != "", notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityError,
		Icon:     "volume",
		Title:    "Audio output unavailable",
		Body:     dto.OutputError,
		Actions:  []notify.Action{audioSettingsAction()},
	})

	raiseOrResolve(keyAudioInputSubstituted, dto.InputSubstituted, notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityWarn,
		Icon:     "mic",
		Title:    "Using the system default microphone",
		Body:     "The microphone selected in Settings could not be opened, so the system default is in use.",
		Context:  []notify.KV{{Key: "IN USE", Value: dto.InputDevice}},
		Actions:  []notify.Action{audioSettingsAction()},
	})

	raiseOrResolve(keyAudioOutputSubstituted, dto.OutputSubstituted, notify.Item{
		Category: notifyCategory,
		Severity: notify.SeverityWarn,
		Icon:     "volume",
		Title:    "Using the system default audio output",
		Body:     "The output device selected in Settings could not be opened, so the system default is in use.",
		Context:  []notify.KV{{Key: "IN USE", Value: dto.OutputDevice}},
		Actions:  []notify.Action{audioSettingsAction()},
	})
}

func audioSettingsAction() notify.Action {
	return notify.Action{
		Label:  "OPEN AUDIO SETTINGS",
		Icon:   "settings",
		Kind:   "navigate",
		Target: "settings",
	}
}
```

- [ ] **Step 4: Call it from both `main.go` emit sites**

In the `NewMalgoBackend` failure branch, after the existing `audioEvents.AudioState(...)`:

```go
	if backend, err := audio.NewMalgoBackend(); err != nil {
		appLog.Warn("audio backend unavailable; audio features are disabled", "err", err)
		noBackend := app.AudioStateDTO{
			InputError:  err.Error(),
			OutputError: err.Error(),
		}
		audioEvents.AudioState(noBackend)
		// The notification channel's audio source, for the case with NO
		// Manager at all. This is the most severe audio failure there is,
		// and the OnState hook below can never see it.
		gui.NotifyAudioState(noBackend)
	} else {
```

and in the `OnState` callback:

```go
			OnState: func(st audio.State) {
				dto := app.AudioStateDTOFrom(st)
				audioEvents.AudioState(dto)
				gui.NotifyAudioState(dto)
			},
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./... && GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/app/ . -v 2>&1 | tail -40`
Expected: build succeeds, all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/notify_audio.go internal/app/notify_audio_test.go main.go
git commit -m "feat(app): route audio device faults into the notification channel

The adapter is a projection, and the projection is what makes including
audio safe. AudioStateDTO has nine fields; four are faults. Overruns and
Underruns are dropped because they are monotonic counters inside
emitStateIfChanged's whole-struct comparison, so a glitching engine
re-emits ~1800 times an hour with nothing visible having changed. Dropping
them turns that into one notification.

Called from BOTH audio emit sites. The hand-pushed DTO for a failed
NewMalgoBackend has no Manager behind it, so an adapter hung only off
OnState would miss the case where audio is dead entirely."
```

---

*Tasks 10–17 continue below.*

---

## Task 10: The notification sample set

**Files:**
- Create: `internal/audio/notifsfx.go`, `internal/audio/notifsfx_test.go`
- Modify: `internal/audio/assets/README.md`

**Interfaces:**
- Consumes: existing `DecodeWAV([]byte) ([]float32, error)`, `assetFS`, `effectSlot`.
- Produces: `NotifyAlert` const, `NotifSFX` type, `NewNotifSFX(*slog.Logger) *NotifSFX`, `(*NotifSFX).Available(id string) bool`, `(*NotifSFX).sampleFor(id string) []float32`, `(*NotifSFX).NewVoicePool() *voicePool`.

**Why no manifest:** there is exactly one notification sound, and it is not a Radio Effect. Putting it in `manifest.toml` would put it in `EffectIDs()`, which is the single source of truth for the Radio Effects panel's row set — the slot would appear in a UI it does not belong in. A single named constant is both smaller and more honest; adding a second slot later is a map literal.

- [ ] **Step 1: Write the failing test**

Create `internal/audio/notifsfx_test.go`:

```go
package audio

import (
	"log/slog"
	"testing"
)

func TestNotifSFXAlertIsUnavailableWithoutItsSample(t *testing.T) {
	n := NewNotifSFX(slog.Default())

	// notify_alert.wav does not exist and MUST NOT be substituted with a
	// synthesised tone -- assets/README.md records the pack as "not
	// something to substitute". Reporting false is the honest answer, and
	// it is exactly how all nine existing SFX slots ship today.
	if n.Available(NotifyAlert) {
		t.Fatal("Available(NotifyAlert) = true; the sample does not exist, so this must be false until the pack lands")
	}
	if got := n.sampleFor(NotifyAlert); got != nil {
		t.Fatalf("sampleFor returned %d samples, want nil", len(got))
	}
}

func TestNotifSFXUnknownIDIsUnavailable(t *testing.T) {
	n := NewNotifSFX(slog.Default())

	if n.Available("no-such-slot") {
		t.Fatal("Available(\"no-such-slot\") = true, want false")
	}
	if got := n.sampleFor("no-such-slot"); got != nil {
		t.Fatalf("sampleFor returned %d samples, want nil", len(got))
	}
}

func TestNotifSFXVoicePoolIsIndependentPerCall(t *testing.T) {
	n := NewNotifSFX(slog.Default())

	a := n.NewVoicePool()
	b := n.NewVoicePool()
	if a == b {
		t.Fatal("NewVoicePool returned the same pool twice; each Manager generation needs its own mixing state")
	}
}

func TestNotifSFXMixIntoIsSilentWithNoSample(t *testing.T) {
	n := NewNotifSFX(slog.Default())
	pool := n.NewVoicePool()

	pool.play(NotifyAlert)
	dst := make([]float32, FrameSamples)
	pool.mixInto(dst, n.sampleFor)

	for i, v := range dst {
		if v != 0 {
			t.Fatalf("dst[%d] = %v, want 0 -- a missing sample plays silence, never noise", i, v)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/audio/ -run TestNotifSFX -v`
Expected: FAIL — `undefined: NewNotifSFX`, `undefined: NotifyAlert`.

- [ ] **Step 3: Write the implementation**

Create `internal/audio/notifsfx.go`:

```go
package audio

import "log/slog"

// NotifyAlert is the one notification sound slot Phase 7.2 defines.
//
// Sound follows the toast: only error-severity notifications are audible,
// so one slot covers every case. A distinct warn sound can be added later
// by extending notifFiles -- the engine needs no change.
const NotifyAlert = "notify_alert"

// notifFiles maps a notification slot id to its WAV filename under assets/.
//
// Deliberately NOT in manifest.toml. That file drives SFX.EffectIDs(),
// which is the single source of truth for the Radio Effects panel's row
// set -- a notification sound listed there would appear in a UI it does not
// belong in.
var notifFiles = map[string]string{
	NotifyAlert: "notify_alert.wav",
}

// NotifSFX owns the decoded notification sample set. Populated once in
// NewNotifSFX and never mutated afterward, so any goroutine may read it
// without a lock -- construction happens-before every use via the returned
// pointer. The same ownership discipline as SFX.
//
// A missing asset is a first-class, expected state. The sample pack is
// supplied by the project (assets/README.md) and must not be substituted,
// so until it lands Available reports false and the slot plays silence.
type NotifSFX struct {
	samples map[string][]float32
	log     *slog.Logger
}

// NewNotifSFX decodes whatever notification samples are embedded.
func NewNotifSFX(log *slog.Logger) *NotifSFX {
	if log == nil {
		log = slog.Default()
	}
	n := &NotifSFX{samples: map[string][]float32{}, log: log}
	for id, file := range notifFiles {
		b, err := assetFS.ReadFile("assets/" + file)
		if err != nil {
			// Expected until the pack lands. Info, not Warn: a known
			// pending dependency is not a malfunction. Mirrors
			// SFX.loadSamples exactly.
			n.log.Info("audio: notification sample absent; slot silent", "slot", id, "file", file)
			continue
		}
		samples, err := DecodeWAV(b)
		if err != nil {
			n.log.Warn("audio: notification sample undecodable", "slot", id, "file", file, "err", err)
			continue
		}
		n.samples[id] = samples
	}
	return n
}

// Available reports whether a decoded sample currently backs the slot. False
// for every slot until the pack lands -- the honest current answer, not a
// placeholder.
func (n *NotifSFX) Available(id string) bool { return len(n.samples[id]) > 0 }

// sampleFor is the lookup handed to voicePool.mixInto.
func (n *NotifSFX) sampleFor(id string) []float32 { return n.samples[id] }

// NewVoicePool builds this set's mixing state for one Manager generation.
//
// Per-generation, exactly like SFX.NewVoicePool: Stop()'s bounded joins can
// leave a previous generation's dspLoop running alongside a new one, and two
// dspLoops sharing one pool is a data race -race has already caught once in
// this package.
func (n *NotifSFX) NewVoicePool() *voicePool { return newVoicePool() }
```

- [ ] **Step 4: Record the new asset in the README**

Append to `internal/audio/assets/README.md`:

```markdown
## Notification sounds

`notify_alert.wav` backs the notification channel's audible alert
(Phase 7.2). It is **not** listed in `manifest.toml`: that file drives the
Radio Effects panel's row set, and a notification sound does not belong
there. `internal/audio/notifsfx.go` embeds it by name instead.

It plays for error-severity notifications only — a dead microphone, an
output device that will not open, global hotkeys that failed to register.
It should be short and unobtrusive: it can fire while a transmission is
being received.

**This brings the outstanding pack to ten files, not nine.**
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/audio/ -run TestNotifSFX -v`
Expected: PASS, four tests.

- [ ] **Step 6: Commit**

```bash
git add internal/audio/notifsfx.go internal/audio/notifsfx_test.go internal/audio/assets/README.md
git commit -m "feat(audio): add the notification sample set

One slot, notify_alert, because sound follows the toast and only
error-severity notifications are audible. Deliberately not in
manifest.toml: that file drives EffectIDs, which is the Radio Effects
panel's row set, and a notification sound does not belong in that UI.

Ships silent. The sample does not exist and assets/README.md records the
pack as not something to substitute, so Available reports false and the
slot plays silence -- exactly how all nine existing SFX slots ship. The
outstanding ask is now ten files."
```

---

## Task 11: `Manager.PlayNotification` and the DSP mix

**Files:**
- Modify: `internal/audio/manager.go`
- Test: `internal/audio/manager_test.go` (append)

**Interfaces:**
- Consumes: Task 10's `NotifSFX`, `NewNotifSFX`, `NotifyAlert`.
- Produces: `(*Manager).PlayNotification(id string)`, `(*Manager).NotificationAvailable(id string) bool`, new `Manager` fields `notif *NotifSFX` and `notifVoices *voicePool`.

- [ ] **Step 1: Write the failing test**

Append to `internal/audio/manager_test.go`:

```go
func TestPlayNotificationOnStoppedManagerDrops(t *testing.T) {
	m := NewManager(newFakeBackend(), ManagerOptions{Log: slog.Default()})

	// Never started. A queued id surviving until some later, unrelated
	// Start() drained it would play an alert for an event minutes past --
	// the same reasoning PlayEffect's doc gives for dropping.
	m.PlayNotification(NotifyAlert)

	if m.notifVoices != nil {
		t.Fatal("notifVoices is non-nil on a manager that was never started")
	}
}

func TestPlayNotificationUnavailableIDIsANoop(t *testing.T) {
	m := NewManager(newFakeBackend(), ManagerOptions{Log: slog.Default()})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	// notify_alert has no sample, so this must not queue anything.
	m.PlayNotification(NotifyAlert)
	m.PlayNotification("no-such-slot")

	m.mu.Lock()
	pool := m.notifVoices
	m.mu.Unlock()
	if pool == nil {
		t.Fatal("notifVoices is nil after Start")
	}
	pool.mu.Lock()
	pending := len(pool.pending)
	pool.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending = %d, want 0 -- an unavailable id must be filtered before it reaches the pool", pending)
	}
}

func TestNotificationVoicePoolIsPerGeneration(t *testing.T) {
	m := NewManager(newFakeBackend(), ManagerOptions{Log: slog.Default()})

	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.mu.Lock()
	first := m.notifVoices
	m.mu.Unlock()
	m.Stop()

	if err := m.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	m.mu.Lock()
	second := m.notifVoices
	m.mu.Unlock()
	defer m.Stop()

	if first == second {
		t.Fatal("both generations share one notification voice pool; Stop's bounded joins can leave two dspLoops live, and -race has already caught that for sfxVoices")
	}
}

func TestStopClearsTheNotificationVoicePool(t *testing.T) {
	m := NewManager(newFakeBackend(), ManagerOptions{Log: slog.Default()})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.Stop()

	m.mu.Lock()
	pool := m.notifVoices
	m.mu.Unlock()
	if pool != nil {
		t.Fatal("notifVoices survived Stop; it must not hold a reference to a possibly-zombie generation")
	}
}

func TestNotificationVoicePoolMixesIntoItsBuffer(t *testing.T) {
	// The engine's contract, proven without a sample: a pool fed a known
	// sample mixes it into the destination. This is what notifBuf gets.
	n := NewNotifSFX(slog.Default())
	pool := n.NewVoicePool()

	lookup := func(id string) []float32 {
		if id == NotifyAlert {
			return []float32{0.5, 0.5, 0.5}
		}
		return nil
	}
	pool.play(NotifyAlert)
	dst := make([]float32, FrameSamples)
	pool.mixInto(dst, lookup)

	if dst[0] != 0.5 {
		t.Fatalf("dst[0] = %v, want 0.5 -- the pool must mix a present sample into the notification buffer", dst[0])
	}
}
```

If `newFakeBackend()` is not the helper name `manager_test.go` already uses, read the file and use whatever fake it defines. Do not add a second fake backend.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/audio/ -run "TestPlayNotification|TestNotificationVoice|TestStopClearsTheNotification" -v`
Expected: FAIL — `m.PlayNotification undefined`, `m.notifVoices undefined`.

- [ ] **Step 3: Add the Manager fields**

In `internal/audio/manager.go`, beside the existing `sfx` field, add:

```go
	// notif is the notification sample set. Immutable after NewManager, the
	// same ownership as sfx.
	notif *NotifSFX
```

and beside `sfxVoices`:

```go
	notifVoices *voicePool // current generation's notification mixing state; published under mu in Start alongside sfxVoices and the rings, for the same reason.
```

In `NewManager`, wherever `m.sfx` is constructed, add alongside it:

```go
	m.notif = NewNotifSFX(opts.Log)
```

- [ ] **Step 4: Thread the pool through `Start`, `dspLoop` and `Stop`**

In `Start`, beside `sfxVoices := m.sfx.NewVoicePool()`:

```go
	notifVoices := m.notif.NewVoicePool()
```

In the `mu`-guarded publish block, beside `m.sfxVoices = sfxVoices`:

```go
	m.notifVoices = notifVoices
```

Change the `dspLoop` launch to pass it:

```go
	go m.dspLoop(denoiser, stopDSP, dspDone, captureRing, playbackRing, sfxVoices, notifVoices, m.dspTick)
```

Change `dspLoop`'s signature to match:

```go
func (m *Manager) dspLoop(denoiser *Denoiser, stopDSP, dspDone chan struct{}, captureRing, playbackRing *Ring, sfxVoices, notifVoices *voicePool, dspTick <-chan time.Time) {
```

Replace the `notifBuf` declaration:

```go
	// notifBuf is the notification bus. Filled by this generation's own
	// notification voice pool, exactly as sfxBuf is by sfxVoices -- see
	// the mix below. (Before Phase 7.2 this was declared and left at zero:
	// mixer.Mix already took it, but nothing ever wrote to it.)
	notifBuf := make([]float32, FrameSamples)
```

And in the tick body, immediately after the existing SFX mix at `manager.go:1115-1116`:

```go
		clear(sfxBuf)
		sfxVoices.mixInto(sfxBuf, m.sfx.sampleFor)
		clear(notifBuf)
		notifVoices.mixInto(notifBuf, m.notif.sampleFor)
```

In `Stop`, beside `m.sfxVoices = nil`:

```go
	m.notifVoices = nil // same reasoning as sfxVoices above
```

- [ ] **Step 5: Add `PlayNotification` and `NotificationAvailable`**

Append near `PlayEffect` in `manager.go`:

```go
// PlayNotification starts a notification one-shot. Unknown or absent ids are
// ignored, so this is a silent no-op until notify_alert.wav lands.
//
// Also a no-op when the manager isn't running: there is no current
// generation's voice pool to queue into, and letting a request linger until
// some later, unrelated Start() drained it would play an alert for an event
// minutes past. Identical discipline to PlayEffect.
func (m *Manager) PlayNotification(id string) {
	if !m.notif.Available(id) {
		return
	}
	m.mu.Lock()
	v, running := m.notifVoices, m.running
	m.mu.Unlock()
	if running && v != nil {
		v.play(id)
	}
}

// NotificationAvailable reports whether a decoded sample backs the slot.
// False for every slot until the pack lands.
func (m *Manager) NotificationAvailable(id string) bool { return m.notif.Available(id) }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/audio/ -v 2>&1 | tail -30`
Expected: PASS, the five new tests and every pre-existing `internal/audio` test.

- [ ] **Step 7: Commit**

```bash
git add internal/audio/manager.go internal/audio/manager_test.go
git commit -m "feat(audio): fill the notification bus

notifBuf already reached mixer.Mix and the bus already had its own gain,
taper and settings slider; nothing ever wrote to it. voicePool.mixInto is
generic over the sample lookup, so the engine is a second pool over a
second sample set -- the identical call the DSP loop already makes for SFX
one line earlier.

The pool is per-generation like sfxVoices, because Stop's bounded joins
can leave two dspLoops live and -race has already caught that sharing
once. PlayNotification drops on a stopped manager rather than lingering
until an unrelated later Start."
```

---

## Task 12: The severity gate and the `play_notification_sounds` setting

**Files:**
- Create: `internal/app/notify_sound.go`, `internal/app/notify_sound_test.go`
- Modify: `internal/config/config.go`, `internal/app/dto.go`, `internal/app/settings.go`, `main.go`
- Test: also `internal/config/config_test.go` (append)

**Interfaces:**
- Consumes: Task 11's `Manager.PlayNotification`, `audio.NotifyAlert`; existing `a.audioManager()`, `sb.cfg.General`.
- Produces: `(*App).PlayNotificationSFX(severity string)`, `config.General.PlayNotificationSounds bool`, `SettingsDTO.PlayNotificationSounds bool` (JSON `play_notification_sounds`).

This task also replaces the Task 7 stub in `main.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/app/notify_sound_test.go`:

```go
package app

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
)

func TestNotificationSFXIDIsErrorSeverityOnly(t *testing.T) {
	cases := []struct {
		severity string
		want     string
	}{
		{"error", audio.NotifyAlert},
		{"warn", ""},
		{"info", ""},
		{"", ""},
		{"ERROR", ""}, // exact match only; the store emits lowercase
	}
	for _, tc := range cases {
		a := appWithSoundSetting(t, true)
		if got := a.notificationSFXID(tc.severity); got != tc.want {
			t.Errorf("notificationSFXID(%q) = %q, want %q -- sound follows the toast, error only", tc.severity, got, tc.want)
		}
	}
}

func TestNotificationSFXIDRespectsTheSetting(t *testing.T) {
	a := appWithSoundSetting(t, false)
	if got := a.notificationSFXID("error"); got != "" {
		t.Fatalf("notificationSFXID(\"error\") = %q with sounds disabled, want \"\"", got)
	}
}

func TestNotificationSFXIDWithNoSettingsBackendIsSilent(t *testing.T) {
	a := NewForTest(nil, nil, nil)
	if got := a.notificationSFXID("error"); got != "" {
		t.Fatalf("notificationSFXID = %q with no settings backend, want \"\"", got)
	}
}

func TestPlayNotificationSFXWithNoAudioManagerDoesNotPanic(t *testing.T) {
	a := appWithSoundSetting(t, true)
	a.PlayNotificationSFX("error")
}
```

`appWithSoundSetting` must build an `App` whose `settings.cfg.General.PlayNotificationSounds` is the given value. Read how `connsfx_test.go` builds its equivalent for `PlayConnectionSounds` and mirror it exactly — that test already solves this problem, and duplicating its helper under a new name is the wrong move if one is already exported within the package.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestNotificationSFX -v`
Expected: FAIL — `a.notificationSFXID undefined`, and `PlayNotificationSounds` is not a field of `config.General`.

- [ ] **Step 3: Add the config field**

In `internal/config/config.go`, add to the `General` struct beside `PlayConnectionSounds`:

```go
	PlayNotificationSounds bool `toml:"play_notification_sounds"`
```

and to the defaults literal at `config.go:296`:

```go
			PlayNotificationSounds: true,
```

Update the struct's doc comment to name the new field alongside the others it lists.

- [ ] **Step 4: Add the DTO field and the two mappings**

In `internal/app/dto.go`, beside `PlayConnectionSounds`:

```go
	PlayNotificationSounds bool `json:"play_notification_sounds"`
```

In `internal/app/settings.go`, add the field to **both** mapping literals — the one at line ~225 (config → DTO) and the one at ~251 (DTO → config). Missing either one silently drops the setting in one direction; that exact bug is what `AudioStateDTOFrom` exists to prevent elsewhere in this file.

- [ ] **Step 5: Write the gate**

Create `internal/app/notify_sound.go`:

```go
package app

import (
	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
)

// notificationSFXID maps a notification's severity to the sound slot that
// marks it, or "" for one that gets no sound.
//
// SOUND FOLLOWS THE TOAST: error severity only. A sound with no visible
// cause is the worst outcome available here, so rather than a per-severity
// palette, exactly what toasts also sounds. Warn reaches the badge and bell
// silently; info is raised already-read by the store and reaches nothing.
//
// Gated on general.play_notification_sounds. Mirrors connectionSFXID, which
// does the same job for control-link transitions.
func (a *App) notificationSFXID(severity string) string {
	sb := a.settings
	if sb == nil || sb.cfg == nil {
		return ""
	}
	sb.mu.Lock()
	enabled := sb.cfg.General.PlayNotificationSounds
	sb.mu.Unlock()
	if !enabled {
		return ""
	}
	if severity != string(notify.SeverityError) {
		return ""
	}
	return audio.NotifyAlert
}

// PlayNotificationSFX plays the sound marking a notification, if its
// severity earns one.
//
// It ships SILENT: notify_alert.wav does not exist, bringing the outstanding
// sample pack to ten files. The engine is asset-agnostic by design -- a
// missing sample plays silence and is logged once, never a crash -- so this
// wiring is correct and testable today and goes audible the moment the
// sample lands.
//
// Exported for main.go's notify.Options.OnSound hook. It cannot route
// straight to the audio Manager there: the Manager does not exist yet when
// the notifier is constructed, and the gate has to consult the settings
// backend anyway.
func (a *App) PlayNotificationSFX(severity string) {
	id := a.notificationSFXID(severity)
	if id == "" {
		return
	}
	m := a.audioManager()
	if m == nil {
		return
	}
	m.PlayNotification(id)
}
```

- [ ] **Step 6: Replace the Task 7 stub in `main.go`**

Find the `// TASK 12: route to gui.PlayNotificationSFX` marker and replace that line with:

```go
		OnSound:  func(it notify.Item) { gui.PlayNotificationSFX(string(it.Severity)) },
```

Delete the marker comment.

- [ ] **Step 7: Add a config round-trip test**

Append to `internal/config/config_test.go` a test in the style the file already uses, asserting that `PlayNotificationSounds` defaults to `true` and survives a save/load round trip. Read an existing test in that file for the exact helper and assertion style before writing it.

- [ ] **Step 8: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./... && GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/... . 2>&1 | tail -25`
Expected: build succeeds, every package PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/app/notify_sound.go internal/app/notify_sound_test.go internal/config/config.go internal/config/config_test.go internal/app/dto.go internal/app/settings.go main.go
git commit -m "feat(app): gate the notification sound on severity and a setting

Sound follows the toast: error severity only. A sound with no visible
cause is the worst outcome available, so rather than a per-severity
palette, exactly what toasts also sounds.

Mirrors connectionSFXID, including shipping silent: notify_alert.wav does
not exist, and the pack must not be substituted. The wiring is correct and
testable today and goes audible the moment the sample lands."
```

---

## Task 13: Frontend store, sync hook, categories and the `activatable` helper

**Files:**
- Create: `frontend/src/shared/store/notifications.ts`, `frontend/src/shared/store/useNotificationsSync.ts`, `frontend/src/shared/components/notificationCategories.ts`, `frontend/src/shared/components/activatable.ts`
- Modify: `frontend/src/shared/api/events.ts`, `frontend/src/shared/api/client.ts`, `frontend/src/shared/components/StatusBar.tsx`
- Test: `frontend/src/shared/store/useNotificationsSync.test.tsx`

**Interfaces:**
- Consumes: Task 6's bindings (`App.GetNotifications` etc., regenerated in Task 6 step 6) and `notifications:changed`.
- Produces: `NotifyItem`, `NotifyAction`, `NotifyKV`, `NotifySnapshot`, `NotifySeverity` types; `useNotifications` store with `snap`, `setSnapshot`; `useNotificationsSync()`; `CATEGORIES` / `categoryFor`; `activatable(onActivate)`.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/shared/store/useNotificationsSync.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { StrictMode } from "react";
import { render, waitFor } from "@testing-library/react";

const listeners: Record<string, (data: unknown) => void> = {};
const offSpy = vi.fn();

vi.mock("../api/events", async () => {
  const actual = await vi.importActual<typeof import("../api/events")>("../api/events");
  return {
    ...actual,
    on: (name: string, cb: (data: unknown) => void) => {
      listeners[name] = cb;
      return () => {
        delete listeners[name];
        offSpy();
      };
    },
  };
});

const getNotifications = vi.fn();
vi.mock("../api/client", () => ({
  api: { getNotifications: () => getNotifications() },
}));

import { useNotificationsSync } from "./useNotificationsSync";
import { useNotifications, emptySnapshot } from "./notifications";
import { EV } from "../api/events";

function Harness() {
  useNotificationsSync();
  return null;
}

const snap = (unread: number) => ({
  items: [
    {
      id: "n1",
      key: "hotkeys.global",
      category: "system",
      severity: "error",
      icon: "bolt",
      title: "Global hotkeys unavailable",
      body: "no backend",
      context: [{ key: "PERMISSION", value: "denied" }],
      actions: [],
      time: "2026-09-28T12:00:00Z",
      unread: unread > 0,
      resolved: false,
    },
  ],
  unread,
});

describe("useNotificationsSync", () => {
  beforeEach(() => {
    useNotifications.setState({ snap: emptySnapshot() });
    offSpy.mockClear();
    getNotifications.mockReset();
    getNotifications.mockResolvedValue(snap(1));
    for (const k of Object.keys(listeners)) delete listeners[k];
  });

  it("hydrates from the backend on mount", async () => {
    render(<Harness />);
    await waitFor(() => {
      expect(useNotifications.getState().snap.unread).toBe(1);
    });
  });

  it("keeps the store live via notifications:changed", async () => {
    render(<Harness />);
    await waitFor(() => expect(listeners[EV.notifications]).toBeDefined());

    listeners[EV.notifications](snap(0));

    expect(useNotifications.getState().snap.unread).toBe(0);
  });

  it("survives StrictMode's simulated unmount with a live subscription", async () => {
    render(
      <StrictMode>
        <Harness />
      </StrictMode>,
    );
    await waitFor(() => expect(listeners[EV.notifications]).toBeDefined());

    // StrictMode mounts, unmounts and remounts. If the cleanup tore down a
    // subscription the remount did not replace, the store would go deaf --
    // the exact failure that hid a total keybind-capture break for two
    // phases.
    listeners[EV.notifications](snap(7));
    expect(useNotifications.getState().snap.unread).toBe(7);
  });

  it("unsubscribes exactly once on a real unmount", async () => {
    const { unmount } = render(<Harness />);
    await waitFor(() => expect(listeners[EV.notifications]).toBeDefined());
    offSpy.mockClear();

    unmount();

    // The control for the StrictMode test above: proving the subscription
    // survives a simulated unmount is only meaningful alongside proof that
    // a REAL unmount still releases it.
    expect(offSpy).toHaveBeenCalledTimes(1);
    expect(listeners[EV.notifications]).toBeUndefined();
  });

  it("leaves the store empty when hydration fails", async () => {
    getNotifications.mockRejectedValue(new Error("not wired"));
    render(<Harness />);
    await waitFor(() => {
      expect(useNotifications.getState().snap.items).toEqual([]);
    });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/shared/store/useNotificationsSync.test.tsx`
Expected: FAIL — cannot resolve `./useNotificationsSync` or `./notifications`.

- [ ] **Step 3: Add the event name and the api methods**

In `frontend/src/shared/api/events.ts`, add to `EV`:

```ts
  notifications: "notifications:changed",
```

In `frontend/src/shared/api/client.ts`, add the import of the notification types and these methods to `api`:

```ts
  getNotifications: (): Promise<NotifySnapshot> =>
    App.GetNotifications() as Promise<NotifySnapshot>,
  markNotificationRead: (id: string): Promise<void> =>
    App.MarkNotificationRead(id) as Promise<void>,
  markAllNotificationsRead: (): Promise<void> =>
    App.MarkAllNotificationsRead() as Promise<void>,
  dismissNotification: (id: string): Promise<void> =>
    App.DismissNotification(id) as Promise<void>,
  clearNotifications: (): Promise<void> => App.ClearNotifications() as Promise<void>,
  focusMainWindow: (): Promise<void> => App.FocusMainWindow() as Promise<void>,
```

adding `import type { NotifySnapshot } from "../store/notifications";` to the existing type imports.

- [ ] **Step 4: Write the store**

Create `frontend/src/shared/store/notifications.ts`:

```ts
import { create } from "zustand";

/** Mirrors Go's `notify.Severity`. Drives DELIVERY, not just colour:
 *  `error` toasts and sounds, `warn` reaches the badge and bell silently,
 *  and `info` is raised already-read by the backend so it reaches nothing
 *  at all. */
export type NotifySeverity = "error" | "warn" | "info";

/** One ordered context row, mirroring Go's `notify.KV`. An array rather
 *  than an object because a JSON object's key order is not display order --
 *  the same reason `AudioSettingsDTO.effect_order` exists. */
export interface NotifyKV {
  key: string;
  value: string;
}

/** One action button, mirroring Go's `notify.Action`. `kind` is a closed
 *  set so a new notification source adds no dispatch code here. */
export interface NotifyAction {
  label: string;
  icon: string;
  kind: "open-window" | "navigate";
  target: string;
  primary: boolean;
}

/** One notification, mirroring Go's `notify.Item`. `key` is "" for a
 *  discrete Post and non-empty for a raised condition. `time` is an RFC3339
 *  string -- Go's time.Time marshals that way. */
export interface NotifyItem {
  id: string;
  key: string;
  category: string;
  severity: NotifySeverity;
  icon: string;
  title: string;
  body: string;
  context: NotifyKV[];
  actions: NotifyAction[];
  time: string;
  unread: boolean;
  resolved: boolean;
}

/** The whole list plus the unread count, newest first. Mirrors Go's
 *  `notify.Snapshot`, which is broadcast in full on every change. */
export interface NotifySnapshot {
  items: NotifyItem[];
  unread: number;
}

export const emptySnapshot = (): NotifySnapshot => ({ items: [], unread: 0 });

interface NotificationsState {
  snap: NotifySnapshot;
  setSnapshot: (snap: NotifySnapshot) => void;
}

/**
 * The notification store is a MIRROR, never a source of truth. Go owns the
 * list (see internal/notify), because a popout is a separate webview with
 * its own JS heap: a list held here in the main window would be invisible
 * to the Notifications window. Every mutation round-trips through the
 * backend and comes back on `notifications:changed`.
 */
export const useNotifications = create<NotificationsState>((set) => ({
  snap: emptySnapshot(),
  setSnapshot: (snap) => set({ snap }),
}));
```

- [ ] **Step 5: Write the sync hook**

Create `frontend/src/shared/store/useNotificationsSync.ts`:

```ts
import { useEffect } from "react";
import { api } from "../api/client";
import { on, EV } from "../api/events";
import { useNotifications, type NotifySnapshot } from "./notifications";

/**
 * useNotificationsSync hydrates the notification store from the backend and
 * keeps it live for as long as the calling window is mounted.
 *
 * It belongs to a WINDOW, not a screen -- the same contract
 * `useSettingsSync` documents. Mount it once per window shell that has a
 * notification surface: `MainApp` (badge, bell, toasts) and
 * `NotificationsApp` (the list). Comms has no notification surface and does
 * not mount it.
 */
export function useNotificationsSync(): void {
  useEffect(() => {
    api
      .getNotifications()
      .then((s) => useNotifications.getState().setSnapshot(s))
      .catch((err) => {
        // Logged rather than swallowed, for the same reason
        // getHotkeyState's rejection is: the store's empty default renders
        // an honest "all clear", so a failure here is otherwise completely
        // invisible -- the UI would claim there is nothing to report when
        // nothing has confirmed that.
        console.error("getNotifications failed; the notification list is unknown", err);
      });

    const off = on<NotifySnapshot>(EV.notifications, (s) =>
      useNotifications.getState().setSnapshot(s),
    );
    return off;
  }, []);
}
```

- [ ] **Step 6: Write the category table**

Create `frontend/src/shared/components/notificationCategories.ts`:

```ts
/**
 * The notification categories and their accent colours, ported from the
 * design prototype (`design/vcs/project/screens/misc.jsx:404-412`).
 *
 * The full set is fixed HERE, once, rather than grown per phase. Phase 7.2
 * only ever emits `system`; 7.3 and 7.4 will emit the rest. The popout's
 * filter populates from the categories actually PRESENT, so it is never a
 * dropdown of six empty options -- but nothing later renegotiates this
 * table.
 */
export interface NotifyCategory {
  key: string;
  label: string;
  color: string;
}

export const CATEGORIES: NotifyCategory[] = [
  { key: "distress", label: "Distress", color: "#ef4f4f" },
  { key: "fleet", label: "Fleet alert", color: "#f5a524" },
  { key: "sync", label: "Sync", color: "#f5a524" },
  { key: "comms", label: "Comms", color: "#60a5fa" },
  { key: "profile", label: "Profile", color: "#4ade80" },
  { key: "system", label: "System", color: "#a78bfa" },
  { key: "operation", label: "Operation", color: "#60a5fa" },
];

const FALLBACK: NotifyCategory = { key: "system", label: "System", color: "#a78bfa" };

/** Resolves a category key to its entry, falling back to System rather than
 *  rendering an uncoloured row for a category a later phase added without
 *  updating this table. */
export function categoryFor(key: string): NotifyCategory {
  return CATEGORIES.find((c) => c.key === key) ?? FALLBACK;
}
```

- [ ] **Step 7: Lift `activatable` out of `StatusBar.tsx`**

Create `frontend/src/shared/components/activatable.ts` with the helper currently at `StatusBar.tsx:71-84`, moved verbatim:

```ts
import type React from "react";

/**
 * Makes a non-button element activate like one from the keyboard.
 *
 * The design prototype uses plain `div`/`span` with `onClick` and the ported
 * CSS keys off those classNames, so the elements stay as they are and gain
 * the semantics instead: focusable, announced as a button, and activated by
 * Enter or Space like a real one.
 *
 * Also what keeps SonarCloud's `typescript:S1082` satisfied -- a click
 * handler on a non-button element with no keyboard path. Phase 6 tripped
 * that rule; every clickable non-button added or touched since uses this.
 */
export function activatable(onActivate: () => void) {
  return {
    role: "button",
    tabIndex: 0,
    onClick: onActivate,
    onKeyDown: (e: React.KeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") {
        // Space would otherwise scroll the page.
        e.preventDefault();
        onActivate();
      }
    },
  };
}
```

In `StatusBar.tsx`, delete the local `activatable` function and its doc comment, and import it instead:

```ts
import { activatable } from "./activatable";
```

Leave every call site unchanged.

- [ ] **Step 8: Run tests to verify they pass**

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client/frontend
npx vitest run src/shared/store/useNotificationsSync.test.tsx src/shared/components/StatusBar.test.tsx
npx tsc --noEmit
```
Expected: both suites PASS (StatusBar's existing tests must still pass after the helper move), `tsc` clean.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/shared/store/notifications.ts frontend/src/shared/store/useNotificationsSync.ts frontend/src/shared/store/useNotificationsSync.test.tsx frontend/src/shared/components/notificationCategories.ts frontend/src/shared/components/activatable.ts frontend/src/shared/components/StatusBar.tsx frontend/src/shared/api/events.ts frontend/src/shared/api/client.ts
git commit -m "feat(frontend): add the notification store, sync hook and shared helpers

The store is a mirror, never a source of truth: Go owns the list because a
popout is a separate webview with its own JS heap.

The sync hook gets a StrictMode test plus a real-unmount control. A bare
StrictMode test alone proves nothing -- the pair is what caught the
keybind-capture break that hid for two phases.

activatable moves out of StatusBar into shared/components so the TopBar
launcher and NotifRow use the same keyboard path, which is also what keeps
SonarCloud's typescript:S1082 satisfied on every clickable non-button this
phase adds or touches."
```

---

## Task 14: `NotifRow`

**Files:**
- Create: `frontend/src/windows/notifications/NotifRow.tsx`, `frontend/src/windows/notifications/NotifRow.test.tsx`

**Interfaces:**
- Consumes: Task 13's `NotifyItem`, `categoryFor`, `activatable`; existing `Icon`.
- Produces: `NotifRow({ item, expanded, onToggle, onAction })`.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/windows/notifications/NotifRow.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { NotifRow } from "./NotifRow";
import type { NotifyItem } from "../../shared/store/notifications";

const item = (over: Partial<NotifyItem> = {}): NotifyItem => ({
  id: "n1",
  key: "hotkeys.global",
  category: "system",
  severity: "error",
  icon: "bolt",
  title: "Global hotkeys unavailable",
  body: "no backend",
  context: [{ key: "PERMISSION", value: "denied" }],
  actions: [],
  time: "2026-09-28T12:00:00Z",
  unread: true,
  resolved: false,
  ...over,
});

describe("NotifRow", () => {
  it("renders the title, category label and unread dot", () => {
    const { container } = render(
      <NotifRow item={item()} expanded={false} onToggle={() => {}} onAction={() => {}} />,
    );

    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();
    expect(screen.getByText("SYSTEM")).toBeInTheDocument();
    expect(container.querySelector("[data-unread]")).not.toBeNull();
  });

  it("hides the body and context until expanded", () => {
    const { rerender } = render(
      <NotifRow item={item()} expanded={false} onToggle={() => {}} onAction={() => {}} />,
    );
    expect(screen.queryByText("no backend")).toBeNull();

    rerender(<NotifRow item={item()} expanded onToggle={() => {}} onAction={() => {}} />);
    expect(screen.getByText("no backend")).toBeInTheDocument();
    expect(screen.getByText("PERMISSION")).toBeInTheDocument();
    expect(screen.getByText("denied")).toBeInTheDocument();
  });

  it("activates from a click and from the keyboard", () => {
    const onToggle = vi.fn();
    const { container } = render(
      <NotifRow item={item()} expanded={false} onToggle={onToggle} onAction={() => {}} />,
    );
    const header = container.querySelector('[role="button"]') as HTMLElement;

    // typescript:S1082 -- a clickable non-button needs a keyboard path.
    expect(header).not.toBeNull();
    expect(header.getAttribute("tabindex")).toBe("0");

    fireEvent.click(header);
    fireEvent.keyDown(header, { key: "Enter" });
    fireEvent.keyDown(header, { key: " " });
    expect(onToggle).toHaveBeenCalledTimes(3);
  });

  it("renders actions only when expanded and reports them by kind and target", () => {
    const onAction = vi.fn();
    const withAction = item({
      actions: [
        { label: "OPEN KEYBIND SETTINGS", icon: "settings", kind: "navigate", target: "settings", primary: true },
      ],
    });

    const { rerender } = render(
      <NotifRow item={withAction} expanded={false} onToggle={() => {}} onAction={onAction} />,
    );
    expect(screen.queryByText("OPEN KEYBIND SETTINGS")).toBeNull();

    rerender(<NotifRow item={withAction} expanded onToggle={() => {}} onAction={onAction} />);
    fireEvent.click(screen.getByText("OPEN KEYBIND SETTINGS"));

    expect(onAction).toHaveBeenCalledWith(withAction.actions[0]);
  });

  it("marks a resolved item and drops its unread dot", () => {
    const { container } = render(
      <NotifRow
        item={item({ resolved: true, unread: false })}
        expanded={false}
        onToggle={() => {}}
        onAction={() => {}}
      />,
    );

    expect(container.querySelector("[data-resolved]")).not.toBeNull();
    expect(container.querySelector("[data-unread]")).toBeNull();
  });

  it("does not blow out its container on a long body, and does not interpret markup", () => {
    // Device names and OS error strings flow straight into title/body. A
    // 400-character malgo error must wrap rather than stretch the row, and
    // an error containing angle brackets must render as text.
    const long = "x".repeat(400);
    const { container } = render(
      <NotifRow
        item={item({ body: `${long} <img src=x onerror="boom">` })}
        expanded
        onToggle={() => {}}
        onAction={() => {}}
      />,
    );

    expect(container.querySelector("img")).toBeNull();
    const body = container.querySelector("[data-body]") as HTMLElement;
    expect(body).not.toBeNull();
    expect(body.style.overflowWrap).toBe("anywhere");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/windows/notifications/NotifRow.test.tsx`
Expected: FAIL — cannot resolve `./NotifRow`.

- [ ] **Step 3: Write the component**

Create `frontend/src/windows/notifications/NotifRow.tsx`:

```tsx
import { Icon } from "../../shared/components/Icon";
import { activatable } from "../../shared/components/activatable";
import { categoryFor } from "../../shared/components/notificationCategories";
import type { NotifyItem, NotifyAction } from "../../shared/store/notifications";

interface NotifRowProps {
  item: NotifyItem;
  expanded: boolean;
  onToggle: () => void;
  onAction: (action: NotifyAction) => void;
}

/** Renders the item's timestamp as HH:MM:SS, matching the design
 *  prototype's `time` field. Falls back to the raw string rather than
 *  rendering "Invalid Date" if the backend ever sends something unparseable. */
function clock(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString(undefined, { hour12: false });
}

/**
 * NotifRow is one notification, ported from the design prototype's
 * `screens/misc.jsx:346-402`.
 *
 * It renders the GENERIC item and branches on nothing about what produced
 * it. Grepping this directory for "hotkey", "joystick" or "audio" should
 * find nothing -- that is what lets Phase 7.3 and 7.4 add sources without
 * touching the UI.
 *
 * The header is a clickable non-button, as the prototype's CSS requires, so
 * it carries `role`/`tabIndex`/Enter/Space through the shared `activatable`
 * helper (SonarCloud `typescript:S1082`).
 */
export function NotifRow({ item, expanded, onToggle, onAction }: NotifRowProps) {
  const cat = categoryFor(item.category);
  return (
    <div
      data-key={item.key || undefined}
      data-resolved={item.resolved ? "" : undefined}
      style={{
        border: "1px solid var(--bd-2)",
        borderLeft: `2px solid ${cat.color}`,
        borderRadius: 3,
        background: item.unread ? "var(--bg-2)" : "var(--bg-1)",
        overflow: "hidden",
        opacity: item.resolved ? 0.55 : 1,
      }}
    >
      <div
        {...activatable(onToggle)}
        aria-expanded={expanded}
        style={{
          padding: "10px 12px",
          cursor: "pointer",
          display: "grid",
          gridTemplateColumns: "auto 1fr auto auto",
          gap: 10,
          alignItems: "center",
        }}
      >
        <div style={{ display: "grid", placeItems: "center", width: 24, height: 24, color: cat.color }}>
          <Icon name={item.icon} size={14} />
        </div>
        <div className="col" style={{ minWidth: 0 }}>
          <div className="row acenter gap-3">
            <span className="cap" style={{ color: cat.color }}>
              {cat.label.toUpperCase()}
            </span>
            {item.unread && (
              <span
                data-unread=""
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: "50%",
                  background: "var(--ac-primary)",
                  boxShadow: "0 0 4px var(--ac-primary)",
                }}
              />
            )}
            {item.resolved && (
              <span className="cap-dim" style={{ fontSize: 9 }}>
                RESOLVED
              </span>
            )}
          </div>
          <div
            style={{
              fontSize: 12,
              color: "var(--tx-0)",
              marginTop: 2,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: expanded ? "normal" : "nowrap",
              overflowWrap: "anywhere",
            }}
          >
            {item.title}
          </div>
        </div>
        <span className="mono" style={{ fontSize: 10, color: "var(--tx-3)" }}>
          {clock(item.time)}
        </span>
        <Icon name={expanded ? "chevronU" : "chevronD"} size={11} style={{ color: "var(--tx-3)" }} />
      </div>

      {expanded && (
        <div style={{ padding: "0 12px 12px 46px", color: "var(--tx-2)", fontSize: 12, lineHeight: 1.6 }}>
          {item.body && (
            // overflowWrap is not cosmetic: body carries OS error strings
            // and device names verbatim, and a 400-character malgo error
            // with no spaces would otherwise stretch the row.
            <div data-body="" style={{ textWrap: "pretty", overflowWrap: "anywhere" }}>
              {item.body}
            </div>
          )}
          {item.context.length > 0 && (
            <div
              className="mono"
              style={{
                marginTop: 8,
                padding: 8,
                background: "var(--bg-1)",
                border: "1px solid var(--bd-1)",
                borderRadius: 3,
                fontSize: 11,
                color: "var(--tx-2)",
              }}
            >
              {item.context.map((kv) => (
                <div key={kv.key} className="row gap-3" style={{ padding: "1px 0" }}>
                  <span style={{ color: "var(--tx-3)", width: 80, flexShrink: 0 }}>
                    {kv.key.toUpperCase()}
                  </span>
                  <span style={{ color: "var(--tx-0)", overflowWrap: "anywhere" }}>{kv.value}</span>
                </div>
              ))}
            </div>
          )}
          {item.actions.length > 0 && (
            <div className="row gap-2" style={{ marginTop: 10 }}>
              {item.actions.map((a) => (
                <button
                  key={a.label}
                  type="button"
                  className={`btn btn-sm ${a.primary ? "btn-primary" : ""}`}
                  onClick={() => onAction(a)}
                >
                  <Icon name={a.icon || "chevron"} size={11} /> {a.label}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/windows/notifications/NotifRow.test.tsx && npx tsc --noEmit`
Expected: six tests PASS, `tsc` clean.

- [ ] **Step 5: Verify the component is source-agnostic**

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
grep -niE "hotkey|joystick|audio" frontend/src/windows/notifications/NotifRow.tsx ; echo "exit=$?"
```
Expected: no matches, `exit=1`. A match means the row has learned what produced an item.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/windows/notifications/
git commit -m "feat(frontend): add NotifRow

Renders the generic item and branches on nothing about what produced it.

The header is a clickable non-button, as the ported CSS requires, so it
carries role/tabIndex/Enter/Space through the shared activatable helper.
Body and context wrap with overflowWrap: anywhere -- they carry OS error
strings and device names verbatim, and a 400-character malgo error with no
spaces would otherwise stretch the row."
```

---

## Task 15: The Notifications popout window

**Files:**
- Create: `frontend/notifications.html`, `frontend/src/notifications.tsx`, `frontend/src/windows/notifications/NotificationsApp.tsx`, `frontend/src/windows/notifications/NotificationsApp.test.tsx`
- Modify: `frontend/vite.config.ts`, `internal/app/windowfactory.go`
- Test: also `internal/app/windowfactory_test.go` (append, or create if absent)

**Interfaces:**
- Consumes: Task 13's store/sync/categories, Task 14's `NotifRow`.
- Produces: the `notifications` window id, served at `/notifications.html`.

- [ ] **Step 1: Write the failing Go test**

Append to `internal/app/windowfactory_test.go` (create it if it does not exist, with `package app`):

```go
func TestNotificationsWindowHasItsOwnURLAndGeometry(t *testing.T) {
	if got := windowURL("notifications"); got != "/notifications.html" {
		t.Fatalf("windowURL(\"notifications\") = %q, want \"/notifications.html\"", got)
	}

	g := defaultGeometry("notifications")
	main := defaultGeometry("unknown-id")
	if g == main {
		t.Fatal("the notifications popout fell through to the main-window geometry; it needs its own arm")
	}
	if g.W <= 0 || g.H <= 0 {
		t.Fatalf("geometry = %+v, want positive dimensions", g)
	}
}

func TestCommsWindowIsUnchanged(t *testing.T) {
	// Regression guard: adding an arm to either switch must not disturb the
	// one popout that already works.
	if got := windowURL("comms"); got != "/comms.html" {
		t.Fatalf("windowURL(\"comms\") = %q, want \"/comms.html\"", got)
	}
	if got := defaultGeometry("comms"); got.W != 540 || got.H != 720 {
		t.Fatalf("comms geometry = %+v, want 540x720", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run "TestNotificationsWindow|TestCommsWindowIsUnchanged" -v`
Expected: FAIL — `windowURL("notifications")` returns `/main.html`.

- [ ] **Step 3: Add the two switch arms**

In `internal/app/windowfactory.go`:

```go
func defaultGeometry(id string) windowstate.Geometry {
	switch id {
	case "comms":
		return windowstate.Geometry{X: 1190, Y: 70, W: 540, H: 720}
	case "notifications":
		// Narrower and shorter than Comms: this is a reading list, not a
		// live control surface, and it is opened transiently.
		return windowstate.Geometry{X: 1150, Y: 120, W: 520, H: 640}
	default:
		return windowstate.Geometry{X: 160, Y: 90, W: 1440, H: 900}
	}
}

func windowURL(id string) string {
	switch id {
	case "comms":
		return "/comms.html"
	case "notifications":
		return "/notifications.html"
	default:
		return "/main.html"
	}
}
```

- [ ] **Step 4: Write the failing frontend test**

Create `frontend/src/windows/notifications/NotificationsApp.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

vi.mock("../../shared/store/useNotificationsSync", () => ({
  useNotificationsSync: () => {},
}));

const markAllNotificationsRead = vi.fn();
const clearNotifications = vi.fn();
const markNotificationRead = vi.fn();
const closeWindow = vi.fn();
const openWindow = vi.fn();
const focusMainWindow = vi.fn();
vi.mock("../../shared/api/client", () => ({
  api: {
    markAllNotificationsRead: () => markAllNotificationsRead(),
    clearNotifications: () => clearNotifications(),
    markNotificationRead: (id: string) => markNotificationRead(id),
    closeWindow: (id: string) => closeWindow(id),
    openWindow: (id: string) => openWindow(id),
    focusMainWindow: () => focusMainWindow(),
  },
}));

import { NotificationsApp } from "./NotificationsApp";
import { useNotifications, emptySnapshot, type NotifyItem } from "../../shared/store/notifications";

const item = (over: Partial<NotifyItem> = {}): NotifyItem => ({
  id: "n1",
  key: "hotkeys.global",
  category: "system",
  severity: "error",
  icon: "bolt",
  title: "Global hotkeys unavailable",
  body: "no backend",
  context: [],
  actions: [],
  time: "2026-09-28T12:00:00Z",
  unread: true,
  resolved: false,
  ...over,
});

describe("NotificationsApp", () => {
  beforeEach(() => {
    useNotifications.setState({ snap: emptySnapshot() });
    vi.clearAllMocks();
  });

  it("shows the ALL CLEAR empty state with no items", () => {
    render(<NotificationsApp />);
    expect(screen.getByText("ALL CLEAR")).toBeInTheDocument();
  });

  it("renders one row per item", () => {
    useNotifications.setState({
      snap: { items: [item(), item({ id: "n2", title: "Second", key: "" })], unread: 2 },
    });
    render(<NotificationsApp />);

    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();
    expect(screen.getByText("Second")).toBeInTheDocument();
  });

  it("populates the filter only from categories actually present", () => {
    useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    render(<NotificationsApp />);

    const options = Array.from(
      (screen.getByRole("combobox") as HTMLSelectElement).options,
    ).map((o) => o.value);

    // The table has seven categories; only `system` is present, so the
    // dropdown must not offer six empty ones.
    expect(options).toEqual(["all", "system"]);
  });

  it("filters by the selected category", () => {
    useNotifications.setState({
      snap: {
        items: [item(), item({ id: "n2", category: "comms", title: "Comms thing" })],
        unread: 2,
      },
    });
    render(<NotificationsApp />);

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "comms" } });

    expect(screen.getByText("Comms thing")).toBeInTheDocument();
    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
  });

  it("routes MARK ALL READ and CLEAR ALL through the backend", () => {
    useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    render(<NotificationsApp />);

    fireEvent.click(screen.getByText("MARK ALL READ"));
    fireEvent.click(screen.getByText("CLEAR ALL"));

    // The store is a mirror: every mutation round-trips through Go and
    // comes back on notifications:changed. Nothing is applied optimistically.
    expect(markAllNotificationsRead).toHaveBeenCalledTimes(1);
    expect(clearNotifications).toHaveBeenCalledTimes(1);
    expect(useNotifications.getState().snap.items).toHaveLength(1);
  });

  it("marks an item read when it is expanded", () => {
    useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    render(<NotificationsApp />);

    fireEvent.click(screen.getByText("Global hotkeys unavailable"));

    expect(markNotificationRead).toHaveBeenCalledWith("n1");
  });

  it("dispatches an open-window action by target", () => {
    useNotifications.setState({
      snap: {
        items: [
          item({
            actions: [
              { label: "OPEN COMMS", icon: "comms", kind: "open-window", target: "comms", primary: true },
            ],
          }),
        ],
        unread: 1,
      },
    });
    render(<NotificationsApp />);
    fireEvent.click(screen.getByText("Global hotkeys unavailable"));
    fireEvent.click(screen.getByText("OPEN COMMS"));

    expect(openWindow).toHaveBeenCalledWith("comms");
  });

  it("focuses the main window for a navigate action rather than opening one", () => {
    useNotifications.setState({
      snap: {
        items: [
          item({
            actions: [
              { label: "OPEN KEYBIND SETTINGS", icon: "settings", kind: "navigate", target: "settings", primary: true },
            ],
          }),
        ],
        unread: 1,
      },
    });
    render(<NotificationsApp />);
    fireEvent.click(screen.getByText("Global hotkeys unavailable"));
    fireEvent.click(screen.getByText("OPEN KEYBIND SETTINGS"));

    // openWindow("main") would spawn a SECOND main window: the real one is
    // a Wails window resolved by name, not a Registry entry.
    expect(focusMainWindow).toHaveBeenCalledTimes(1);
    expect(openWindow).not.toHaveBeenCalled();
  });

  it("closes through the Go registry rather than the webview", () => {
    render(<NotificationsApp />);
    fireEvent.click(screen.getByLabelText("close"));

    // Same reasoning as CommsApp: closing through the registry persists
    // geometry and is more reliable than the in-webview Window.Close().
    expect(closeWindow).toHaveBeenCalledWith("notifications");
  });
});
```

- [ ] **Step 5: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/windows/notifications/NotificationsApp.test.tsx`
Expected: FAIL — cannot resolve `./NotificationsApp`.

- [ ] **Step 6: Write the component**

Create `frontend/src/windows/notifications/NotificationsApp.tsx`:

```tsx
import { useMemo, useState } from "react";
import { api } from "../../shared/api/client";
import { Icon } from "../../shared/components/Icon";
import { categoryFor } from "../../shared/components/notificationCategories";
import { useNotifications, type NotifyAction } from "../../shared/store/notifications";
import { useNotificationsSync } from "../../shared/store/useNotificationsSync";
import { NotifRow } from "./NotifRow";

/**
 * NotificationsApp is the Notifications pop-out shell, ported from the
 * design prototype's `screens/misc.jsx:292-343`.
 *
 * The store is a MIRROR: mark-read, mark-all-read and clear-all all
 * round-trip through Go and come back on `notifications:changed`. Nothing is
 * applied optimistically, for the same reason CommsApp never mutates the
 * radios store directly -- the backend is the single source of truth.
 *
 * Close routes through `api.closeWindow("notifications")` rather than the
 * in-webview `Window.Close()`, which is what persists geometry through the
 * registry.
 */
export function NotificationsApp() {
  const snap = useNotifications((s) => s.snap);
  const [filter, setFilter] = useState("all");
  const [expanded, setExpanded] = useState<string | null>(null);

  useNotificationsSync();

  // Only categories actually PRESENT. The shared table has seven; offering
  // all of them here would be a dropdown of six empty options.
  const present = useMemo(() => {
    const keys = new Set(snap.items.map((i) => i.category));
    return Array.from(keys).sort();
  }, [snap.items]);

  const filtered = useMemo(
    () => (filter === "all" ? snap.items : snap.items.filter((i) => i.category === filter)),
    [snap.items, filter],
  );

  const handleToggle = (id: string, unread: boolean) => {
    setExpanded((cur) => (cur === id ? null : id));
    // Opening a row is the user reading it. Routed through the backend so
    // every window's badge agrees.
    if (unread) void api.markNotificationRead(id);
  };

  const handleAction = (a: NotifyAction) => {
    // A closed set of kinds, so a notification source added in a later
    // phase needs no new dispatch code here.
    if (a.kind === "open-window") {
      void api.openWindow(a.target);
      return;
    }
    if (a.kind === "navigate") {
      // The main window owns navigation, and it is NOT a Registry entry --
      // it is a Wails window resolved by name, so openWindow("main") would
      // spawn a second one. Focusing it is the honest thing a popout can
      // do; routing the nav key across windows is Phase 7.4's problem.
      void api.focusMainWindow();
    }
  };

  return (
    <div
      className="popout"
      style={{
        position: "static",
        width: "100%",
        height: "100%",
        border: "none",
        borderRadius: 0,
        boxShadow: "none",
        minWidth: 0,
        minHeight: 0,
      }}
    >
      <div className="popout-chrome">
        <Icon name="bell" size={14} />
        <span className="ttl">Notifications</span>
        <div className="ctrl">
          <button
            type="button"
            className="close"
            aria-label="close"
            title="Close"
            onClick={() => void api.closeWindow("notifications")}
          >
            <Icon name="close" size={14} />
          </button>
        </div>
      </div>

      <div
        style={{
          padding: "10px 14px",
          background: "var(--bg-0)",
          borderBottom: "1px solid var(--bd-1)",
          display: "flex",
          alignItems: "center",
          gap: 10,
        }}
      >
        <span className="cap-dim mono">
          {filtered.length} OF {snap.items.length}
        </span>
        <span className="flex" style={{ flex: 1 }} />
        <select
          className="input"
          aria-label="Filter by category"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          style={{ width: 150, height: 24 }}
        >
          <option value="all">All categories</option>
          {present.map((k) => (
            <option key={k} value={k}>
              {categoryFor(k).label}
            </option>
          ))}
        </select>
        <button type="button" className="btn btn-sm" onClick={() => void api.markAllNotificationsRead()}>
          MARK ALL READ
        </button>
        <button type="button" className="btn btn-sm btn-danger" onClick={() => void api.clearNotifications()}>
          CLEAR ALL
        </button>
      </div>

      <div className="popout-body" style={{ overflow: "auto", padding: 10 }}>
        {filtered.length === 0 ? (
          <div className="state-card" style={{ marginTop: 32 }}>
            <div className="state-glyph">
              <Icon name="bell" size={20} />
            </div>
            <div className="state-title">ALL CLEAR</div>
            <div style={{ fontSize: 12, color: "var(--tx-3)", maxWidth: 320, textWrap: "pretty" }}>
              {snap.items.length === 0
                ? "No notifications. New events will appear here as they happen."
                : "No notifications match the current filter."}
            </div>
          </div>
        ) : (
          <div className="col gap-2">
            {filtered.map((it) => (
              <NotifRow
                key={it.id}
                item={it}
                expanded={expanded === it.id}
                onToggle={() => handleToggle(it.id, it.unread)}
                onAction={handleAction}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 7: Add the window shell and the Vite entry**

Create `frontend/notifications.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>VCS — Notifications</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/notifications.tsx"></script>
  </body>
</html>
```

Create `frontend/src/notifications.tsx`:

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import "./shared/styles/global.css";
import { NotificationsApp } from "./windows/notifications/NotificationsApp";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <NotificationsApp />
  </React.StrictMode>,
);
```

In `frontend/vite.config.ts`, add the input:

```ts
        notifications: resolve(__dirname, "notifications.html"),
```

- [ ] **Step 8: Run every check**

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run "TestNotificationsWindow|TestCommsWindow" -v
cd frontend && npx vitest run src/windows/notifications/ && npx tsc --noEmit && npm run build
```
Expected: Go tests PASS, nine frontend tests PASS, `tsc` clean, and the build emits a `notifications` bundle. Confirm the bundle exists: `ls dist/notifications.html`.

- [ ] **Step 9: Commit**

```bash
git add frontend/notifications.html frontend/src/notifications.tsx frontend/src/windows/notifications/ frontend/vite.config.ts internal/app/windowfactory.go internal/app/windowfactory_test.go
git commit -m "feat(frontend): add the Notifications popout

Two switch arms, a Vite entry and an HTML shell -- the Registry was
already window-id-agnostic, so the cost really was the UI port.

The store is a mirror: mark-read, mark-all-read and clear-all all
round-trip through Go and come back on notifications:changed, so every
window's badge agrees. The category filter populates from the categories
actually present rather than offering six empty options."
```

---

## Task 16: Badge, bell and the toast stack

**Files:**
- Create: `frontend/src/shared/components/ToastHost.tsx`, `frontend/src/shared/components/ToastHost.test.tsx`
- Modify: `frontend/src/shared/components/TopBar.tsx`, `frontend/src/shared/components/StatusBar.tsx`, `frontend/src/windows/main/MainApp.tsx`
- Test: also `frontend/src/shared/components/TopBar.test.tsx` and `StatusBar.test.tsx` (append)

**Interfaces:**
- Consumes: Task 13's `useNotifications`, `activatable`.
- Produces: `ToastHost()`.

- [ ] **Step 1: Write the failing ToastHost test**

Create `frontend/src/shared/components/ToastHost.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { StrictMode } from "react";
import { render, screen, act } from "@testing-library/react";

import { ToastHost } from "./ToastHost";
import { useNotifications, emptySnapshot, type NotifyItem } from "../store/notifications";

const item = (over: Partial<NotifyItem> = {}): NotifyItem => ({
  id: "n1",
  key: "hotkeys.global",
  category: "system",
  severity: "error",
  icon: "bolt",
  title: "Global hotkeys unavailable",
  body: "no backend",
  context: [],
  actions: [],
  time: "2026-09-28T12:00:00Z",
  unread: true,
  resolved: false,
  ...over,
});

describe("ToastHost", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useNotifications.setState({ snap: emptySnapshot() });
  });
  afterEach(() => {
    vi.runOnlyPendingTimers();
    vi.useRealTimers();
  });

  it("toasts an error-severity item", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();
  });

  it("does not toast warn or info", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({
        snap: {
          items: [
            item({ id: "w", severity: "warn", title: "warned" }),
            item({ id: "i", severity: "info", title: "informed", unread: false }),
          ],
          unread: 1,
        },
      });
    });

    expect(screen.queryByText("warned")).toBeNull();
    expect(screen.queryByText("informed")).toBeNull();
  });

  it("auto-dismisses after 6.5s", () => {
    render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    expect(screen.getByText("Global hotkeys unavailable")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(6500);
    });

    expect(screen.queryByText("Global hotkeys unavailable")).toBeNull();
  });

  it("toasts an item only once, however often the snapshot is republished", () => {
    render(<ToastHost />);
    const snap = { items: [item()], unread: 1 };

    act(() => {
      useNotifications.setState({ snap });
    });
    act(() => {
      // A full Snapshot is broadcast on EVERY change, including changes to
      // other items. Re-toasting on each would turn one failure into a
      // stream of identical toasts.
      useNotifications.setState({ snap: { ...snap } });
      useNotifications.setState({ snap: { ...snap } });
    });

    expect(screen.getAllByText("Global hotkeys unavailable")).toHaveLength(1);
  });

  it("schedules exactly one dismiss timer under StrictMode", () => {
    const setSpy = vi.spyOn(globalThis, "setTimeout");
    render(
      <StrictMode>
        <ToastHost />
      </StrictMode>,
    );
    setSpy.mockClear();

    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });

    // StrictMode mounts, unmounts and remounts every effect. A dismiss
    // timer scheduled per mount would fire twice and could dismiss a toast
    // that a later item had replaced. This is the exact shape that broke
    // keybind capture silently for two phases.
    const dismissTimers = setSpy.mock.calls.filter((c) => c[1] === 6500);
    expect(dismissTimers).toHaveLength(1);
    setSpy.mockRestore();
  });

  it("clears its timers exactly once on a real unmount", () => {
    const clearSpy = vi.spyOn(globalThis, "clearTimeout");
    const { unmount } = render(<ToastHost />);
    act(() => {
      useNotifications.setState({ snap: { items: [item()], unread: 1 } });
    });
    clearSpy.mockClear();

    unmount();

    // The control for the StrictMode test above. Proving no double-schedule
    // is only meaningful alongside proof that a real unmount still releases
    // the timer -- otherwise it would fire into an unmounted tree.
    expect(clearSpy).toHaveBeenCalledTimes(1);
    clearSpy.mockRestore();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/shared/components/ToastHost.test.tsx`
Expected: FAIL — cannot resolve `./ToastHost`.

- [ ] **Step 3: Write ToastHost**

Create `frontend/src/shared/components/ToastHost.tsx`:

```tsx
import { useEffect, useRef, useState } from "react";
import { Icon } from "./Icon";
import { useNotifications, type NotifyItem } from "../store/notifications";

/** How long a toast stays up. The design prototype's value
 *  (`design/vcs/project/app.jsx:234`). */
const DISMISS_MS = 6500;

/**
 * ToastHost surfaces ERROR-severity notifications transiently, so a failure
 * reaches a user who is looking at something other than the Notifications
 * popout.
 *
 * Without it, replacing Phase 3's inline Keybinds banner with a popout would
 * be a regression: the failure used to be visible the moment you opened
 * Keybinds, and would otherwise become invisible unless you opened a window
 * you have no reason to open.
 *
 * Error only, deliberately -- sound follows the same rule (see Go's
 * App.notificationSFXID). A warn item reaches the badge and bell silently,
 * and an info item is raised already-read by the backend so it reaches
 * nothing at all. That is what makes macOS's "joystick unsupported"
 * structurally incapable of rendering as a failure.
 *
 * Mounted in the MAIN window only. Comms is a narrow radio panel and a toast
 * stack would cover the radios -- a deliberate limit, not an oversight.
 */
export function ToastHost() {
  const items = useNotifications((s) => s.snap.items);
  const [visible, setVisible] = useState<NotifyItem[]>([]);

  // Ids already toasted. A ref, not state: a full Snapshot is broadcast on
  // EVERY change -- including changes to other items -- so without this each
  // republish would re-toast every error still in the list.
  const seen = useRef<Set<string>>(new Set());

  // Live dismiss timers, keyed by item id. A ref so the cleanup can clear
  // them without the effect depending on them and rescheduling.
  const timers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  const dismiss = (id: string) => {
    const t = timers.current.get(id);
    if (t !== undefined) {
      clearTimeout(t);
      timers.current.delete(id);
    }
    setVisible((cur) => cur.filter((i) => i.id !== id));
  };

  useEffect(() => {
    const fresh = items.filter(
      (i) => i.severity === "error" && !i.resolved && !seen.current.has(i.id),
    );
    if (fresh.length === 0) return;

    for (const i of fresh) {
      seen.current.add(i.id);
      // Guard against a double-schedule. StrictMode runs every effect
      // twice, and a second timer for the same id would fire into a toast
      // a later item may already have replaced.
      if (timers.current.has(i.id)) continue;
      timers.current.set(
        i.id,
        setTimeout(() => {
          timers.current.delete(i.id);
          setVisible((cur) => cur.filter((x) => x.id !== i.id));
        }, DISMISS_MS),
      );
    }
    setVisible((cur) => [...cur, ...fresh]);
  }, [items]);

  // Real-unmount cleanup, kept SEPARATE from the effect above so it runs
  // once on unmount rather than on every items change. Clearing timers in
  // the subscribing effect's cleanup would cancel a live toast's dismiss
  // every time an unrelated notification arrived.
  useEffect(() => {
    const live = timers.current;
    return () => {
      for (const t of live.values()) clearTimeout(t);
      live.clear();
    };
  }, []);

  if (visible.length === 0) return null;

  return (
    <div className="toast-stack">
      {visible.map((t) => (
        <div key={t.id} className="toast alert">
          <div className="row between acenter">
            <span style={{ fontSize: 12, color: "var(--tx-0)", overflowWrap: "anywhere" }}>
              {t.title}
            </span>
            <button
              type="button"
              className="btn btn-ghost btn-icon btn-sm"
              aria-label={`Dismiss ${t.title}`}
              onClick={() => dismiss(t.id)}
            >
              <Icon name="x" size={10} />
            </button>
          </div>
          {t.body && (
            <div
              style={{
                fontSize: 11,
                color: "var(--tx-2)",
                marginTop: 4,
                textWrap: "pretty",
                overflowWrap: "anywhere",
              }}
            >
              {t.body}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Write the failing TopBar and StatusBar tests**

Append to `frontend/src/shared/components/TopBar.test.tsx`:

```tsx
it("wires the notifications launcher and shows the unread badge", () => {
  useNotifications.setState({ snap: { items: [], unread: 3 } });
  const { container } = render(<TopBar view="home" />);

  const launcher = container.querySelector('[data-launcher="notifications"]') as HTMLElement;
  expect(launcher).not.toBeNull();
  expect(launcher.getAttribute("aria-disabled")).toBeNull();
  expect(launcher.querySelector(".badge")?.textContent).toBe("3");

  fireEvent.click(launcher);
  expect(toggleWindow).toHaveBeenCalledWith("notifications");
});

it("gives the notifications launcher a keyboard path", () => {
  const { container } = render(<TopBar view="home" />);
  const launcher = container.querySelector('[data-launcher="notifications"]') as HTMLElement;

  // typescript:S1082 -- touching this line makes it new code for the gate.
  expect(launcher.getAttribute("role")).toBe("button");
  expect(launcher.getAttribute("tabindex")).toBe("0");

  fireEvent.keyDown(launcher, { key: "Enter" });
  expect(toggleWindow).toHaveBeenCalledWith("notifications");
});

it("hides the badge when nothing is unread", () => {
  useNotifications.setState({ snap: { items: [], unread: 0 } });
  const { container } = render(<TopBar view="home" />);
  const launcher = container.querySelector('[data-launcher="notifications"]') as HTMLElement;
  expect(launcher.querySelector(".badge")).toBeNull();
});
```

Read the existing `TopBar.test.tsx` first and reuse whatever mock it already has for `api`; add `toggleWindow` to that mock rather than creating a second one. Import `useNotifications` and reset it in the file's `beforeEach`.

Append to `frontend/src/shared/components/StatusBar.test.tsx`:

```tsx
it("wires the ALERTS bell to the notifications popout with its unread count", () => {
  useNotifications.setState({ snap: { items: [], unread: 2 } });
  const { container } = render(<StatusBar />);

  const bell = container.querySelector('[data-bell="alerts"]') as HTMLElement;
  expect(bell).not.toBeNull();
  expect(bell.className).toContain("has-unread");
  expect(bell.querySelector(".sb-bell-count")?.textContent).toBe("2");

  fireEvent.click(bell);
  expect(toggleWindow).toHaveBeenCalledWith("notifications");
});

it("drops has-unread and the count when nothing is unread", () => {
  useNotifications.setState({ snap: { items: [], unread: 0 } });
  const { container } = render(<StatusBar />);
  const bell = container.querySelector('[data-bell="alerts"]') as HTMLElement;

  expect(bell.className).not.toContain("has-unread");
  expect(bell.querySelector(".sb-bell-count")).toBeNull();
});
```

`StatusBar.test.tsx` has no `api` mock today — add one in the same style `TopBar.test.tsx` uses.

- [ ] **Step 5: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/shared/components/ToastHost.test.tsx src/shared/components/TopBar.test.tsx src/shared/components/StatusBar.test.tsx`
Expected: FAIL — no `[data-launcher="notifications"]`, no `[data-bell="alerts"]`.

- [ ] **Step 6: Wire TopBar**

In `TopBar.tsx`, import `useNotifications` and `activatable`, read the count:

```tsx
  const unread = useNotifications((s) => s.snap.unread);
```

and replace the launcher `<span>` body. The whole map becomes:

```tsx
          {POPOUT_LAUNCHERS.map((l) => {
            const wired = l.key === "comms" || l.key === "notifications";
            const isOpen = openWindows.includes(l.key);
            const badge = l.key === "notifications" ? unread : 0;
            return (
              <span
                key={l.key}
                data-launcher={l.key}
                className={`launcher-btn${isOpen ? " open" : ""}`}
                // The prototype's CSS keys off this className, so the
                // element stays a span and gains button semantics instead.
                // Touching this line makes it new code for SonarCloud's
                // gate (typescript:S1082), so the keyboard path is not
                // optional -- and adding it here also fixes a pre-existing
                // gap for the Comms launcher.
                {...(wired ? activatable(() => void api.toggleWindow(l.key)) : {})}
                title={
                  wired
                    ? `${l.label} · ${isOpen ? "Open — click to close" : "Closed — click to open"}`
                    : "Arrives in a later phase"
                }
                aria-disabled={wired ? undefined : true}
                aria-pressed={wired ? isOpen : undefined}
                style={wired ? undefined : { opacity: 0.45, pointerEvents: "none" }}
              >
                <Icon name={l.icon} size={11} />
                {l.label}
                <span className="dot" />
                {badge > 0 && <span className="badge">{badge}</span>}
              </span>
            );
          })}
```

- [ ] **Step 7: Wire StatusBar**

In `StatusBar.tsx`, import `useNotifications` and `api`, read the count, and replace the inert ALERTS span:

```tsx
      <span
        data-bell="alerts"
        className={`sb-btn${unread > 0 ? " has-unread" : ""}`}
        {...activatable(() => void api.toggleWindow("notifications"))}
        title="Open Notifications"
      >
        <Icon name="bell" size={11} />
        ALERTS
        {unread > 0 && <span className="sb-bell-count">{unread}</span>}
      </span>
```

- [ ] **Step 8: Mount the sync hook and ToastHost in MainApp**

In `MainApp.tsx`, add the imports, call `useNotificationsSync()` beside the existing `useSettingsSync()`, and render the host inside the root div, after `<StatusBar />`:

```tsx
      <StatusBar onNavigate={setView} />
      <ToastHost />
```

- [ ] **Step 9: Run every frontend check**

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client/frontend
npx vitest run && npx tsc --noEmit && npm run build
```
Expected: the whole suite PASS, `tsc` clean, build succeeds.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/shared/components/ToastHost.tsx frontend/src/shared/components/ToastHost.test.tsx frontend/src/shared/components/TopBar.tsx frontend/src/shared/components/TopBar.test.tsx frontend/src/shared/components/StatusBar.tsx frontend/src/shared/components/StatusBar.test.tsx frontend/src/windows/main/MainApp.tsx
git commit -m "feat(frontend): surface unread through the badge, the bell and toasts

Without these, replacing the inline Keybinds banner with a popout would be
a regression: the failure used to be visible the moment you opened
Keybinds and would otherwise be invisible unless you opened a window you
have no reason to open.

Toasts are error-severity only, matching the sound rule, so warn reaches
the badge and bell silently and info reaches nothing.

ToastHost's dismiss timers get a StrictMode test plus a real-unmount
control -- an effect cleanup with side effects is the exact shape that
broke keybind capture silently for two phases. Its cleanup is a separate
effect so an unrelated notification arriving cannot cancel a live toast's
dismiss.

The launcher gains role/tabIndex/Enter/Space, which also fixes a
pre-existing keyboard gap on the Comms launcher."
```

---

## Task 17: Retire the inline Keybinds banner

**Files:**
- Create: `frontend/src/windows/main/screens/settings/sections/keybinds/PermissionCard.tsx`, `.../keybinds/PermissionCard.test.tsx`
- Modify: `frontend/src/windows/main/screens/settings/sections/Keybinds.tsx`
- Test: `frontend/src/windows/main/screens/settings/sections/Keybinds.test.tsx` (append)

**Interfaces:**
- Consumes: existing `useSettings`, `api.requestHotkeyPermission`, `api.openHotkeyPermissionSettings`, `api.recheckHotkeyPermission`.
- Produces: `PermissionCard()`.

**The split:** the banner's *announcement* half is now a notification (Task 8). Its *remediation* half stays in Settings, where the user can act on it in context — and moves from `!hotkeys.registered` to `permission === "denied"`, so it cannot vanish while the grant is still missing. The per-row `failedReason` text at `Keybinds.tsx:345` **stays**: it is row-contextual rather than a banner, and deleting it would force cross-referencing a popout against a table row.

- [ ] **Step 1: Write the failing PermissionCard test**

Create `.../sections/keybinds/PermissionCard.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

const requestHotkeyPermission = vi.fn();
const openHotkeyPermissionSettings = vi.fn();
const recheckHotkeyPermission = vi.fn();
vi.mock("../../../../../../shared/api/client", () => ({
  api: {
    requestHotkeyPermission: () => requestHotkeyPermission(),
    openHotkeyPermissionSettings: () => openHotkeyPermissionSettings(),
    recheckHotkeyPermission: () => recheckHotkeyPermission(),
  },
}));

import { PermissionCard } from "./PermissionCard";
import { useSettings } from "../../../../../../shared/store/settings";

const setHotkeys = (over: Partial<ReturnType<typeof useSettings.getState>["hotkeys"]>) =>
  useSettings.setState((s) => ({ hotkeys: { ...s.hotkeys, ...over } }));

describe("PermissionCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    requestHotkeyPermission.mockResolvedValue({ prompted: true, permission: "unknown" });
    setHotkeys({ registered: true, error: "", failed: {}, permission: "not_applicable" });
  });

  it("renders nothing when there is nothing to grant", () => {
    // Windows and Linux/X11 report not_applicable; a button would be a dead
    // end, so the card must not render at all.
    const { container } = render(<PermissionCard />);
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing for unknown", () => {
    setHotkeys({ permission: "unknown" });
    const { container } = render(<PermissionCard />);
    expect(container.firstChild).toBeNull();
  });

  it("renders GRANT ACCESS when permission is denied", () => {
    setHotkeys({ permission: "denied", registered: false, error: "not trusted" });
    render(<PermissionCard />);

    expect(screen.getByText("GRANT ACCESS")).toBeInTheDocument();
    expect(screen.queryByText("RE-CHECK")).toBeNull();
  });

  it("stays visible while the grant is still missing even after a re-check", () => {
    // Keyed on permission, not on !registered: the card must not vanish the
    // instant registration succeeds while the grant is still absent.
    setHotkeys({ permission: "denied", registered: true });
    render(<PermissionCard />);
    expect(screen.getByText("GRANT ACCESS")).toBeInTheDocument();
  });

  it("unlocks RE-CHECK after a request and swaps to OPEN SETTINGS once the prompt is spent", async () => {
    setHotkeys({ permission: "denied", registered: false });
    requestHotkeyPermission.mockResolvedValue({ prompted: false, permission: "denied" });
    render(<PermissionCard />);

    fireEvent.click(screen.getByText("GRANT ACCESS"));

    await waitFor(() => expect(screen.getByText("OPEN SETTINGS")).toBeInTheDocument());
    expect(screen.getByText("RE-CHECK")).toBeInTheDocument();
  });

  it("never treats prompted as a grant", async () => {
    setHotkeys({ permission: "denied", registered: false });
    requestHotkeyPermission.mockResolvedValue({ prompted: true, permission: "unknown" });
    render(<PermissionCard />);

    fireEvent.click(screen.getByText("GRANT ACCESS"));

    // macOS resolves the prompt asynchronously and never reports the
    // decision through this call; the answer only ever arrives on
    // hotkeys:state. So GRANT ACCESS must stay, not become OPEN SETTINGS.
    await waitFor(() => expect(screen.getByText("RE-CHECK")).toBeInTheDocument());
    expect(screen.getByText("GRANT ACCESS")).toBeInTheDocument();
    expect(screen.queryByText("OPEN SETTINGS")).toBeNull();
  });

  it("shows the restart hint when access is granted but registration still fails", () => {
    setHotkeys({ permission: "granted", registered: false });
    render(<PermissionCard />);
    expect(screen.getByText(/restart VCS/i)).toBeInTheDocument();
  });
});
```

Adjust the `vi.mock` path depth to match the real directory nesting — count the segments from `sections/keybinds/` back to `shared/api/client` before running.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/windows/main/screens/settings/sections/keybinds/PermissionCard.test.tsx`
Expected: FAIL — cannot resolve `./PermissionCard`.

- [ ] **Step 3: Write PermissionCard**

Create `.../sections/keybinds/PermissionCard.tsx`, moving the remediation half of `Keybinds.tsx:429-473` verbatim — including `requested` / `promptSpent` and all three handlers — with this doc comment:

```tsx
/**
 * PermissionCard is the REMEDIATION half of what used to be the Keybinds
 * banner. Its announcement half is now a notification (see Go's
 * App.notifyHotkeyState), which is the right home: a registration failure is
 * exactly the kind of thing the user must learn about WITHOUT having
 * Settings open.
 *
 * What stays here is what the user can only act on here. `requested` and
 * `promptSpent` are local rather than store-backed because neither is
 * observable from the backend: macOS gives no way to ask "has this app's
 * one-shot prompt been used", so the only evidence is a request that came
 * back without prompting. Lifting them into Go would mean inventing backend
 * state that exists solely to track a macOS prompt.
 *
 * Rendered on `permission === "denied"` rather than on `!registered`, which
 * is a deliberate change from the banner it replaces: keyed on the old
 * condition it would vanish the moment registration happened to succeed
 * while the grant was still absent. Only macOS ever reports "denied";
 * "not_applicable" (Windows, Linux/X11) and "unknown" render nothing,
 * because there is nothing to grant and a button would be a dead end.
 */
```

The `grantedButUnregistered` hint ("Accessibility is granted — restart VCS for hotkeys to take effect") moves here too and renders on `permission === "granted" && !registered`.

- [ ] **Step 4: Write the failing Keybinds test**

Append to `Keybinds.test.tsx`:

```tsx
it("no longer renders the global-failure banner", () => {
  useSettings.setState((s) => ({
    hotkeys: { ...s.hotkeys, registered: false, error: "no backend", permission: "not_applicable" },
  }));
  render(<Keybinds />);

  // The announcement is a notification now. Only the remediation card
  // stays, and not on this platform.
  expect(screen.queryByText(/Global hotkeys unavailable/)).toBeNull();
});

it("no longer renders the joystick error banner", () => {
  useSettings.setState((s) => ({
    joystick: { ...s.joystick, supported: true, error: "permission denied on /dev/input", devices: [] },
  }));
  render(<Keybinds />);

  expect(screen.queryByText(/Joystick unavailable/)).toBeNull();
});

it("still renders the per-row failure reason", () => {
  useSettings.setState((s) => ({
    hotkeys: { ...s.hotkeys, registered: true, failed: { "global.ptt": "Numpad7 cannot be registered" }, permission: "granted" },
  }));
  render(<Keybinds />);

  // Row-contextual, not a banner. Deleting it would force the user to
  // cross-reference a popout against a table row.
  expect(screen.getByText("Numpad7 cannot be registered")).toBeInTheDocument();
});
```

Read `Keybinds.test.tsx` first and reuse its existing setup and mocks; seed `keybinds` with at least one row whose `action_id` is `global.ptt` for the third test.

- [ ] **Step 5: Edit Keybinds.tsx**

- Delete the `{!hotkeys.registered && (...)}` block (lines ~429-473) entirely.
- Delete the `{joystick.supported && joystick.error && (...)}` block (lines ~477-485) entirely.
- Delete `requested`, `promptSpent`, `handleGrantPermission`, `handleOpenPermissionSettings`, `handleRecheckPermission`, `permissionDenied` and `grantedButUnregistered` — all moved to `PermissionCard`.
- Render `<PermissionCard />` as the first child of the returned `<div className="col gap-5">`.
- Leave `renderChip`'s `failedReason` (line ~332 and ~345) untouched.
- Update the component's doc comment: property 4 currently describes the banner. Replace it with a note that the announcement moved to the notification channel and that `PermissionCard` owns remediation, keyed on permission rather than on `registered`.

- [ ] **Step 6: Run every frontend check**

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client/frontend
npx vitest run && npx tsc --noEmit && npm run build
```
Expected: the whole suite PASS, `tsc` clean, build succeeds.

- [ ] **Step 7: Confirm Keybinds.tsx shrank**

Run: `wc -l frontend/src/windows/main/screens/settings/sections/Keybinds.tsx`
Expected: meaningfully below 542. The extraction is also what keeps this file off the 800-line ceiling in the global coding-style rules.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/windows/main/screens/settings/sections/
git commit -m "refactor(keybinds): retire the inline banner for the notification channel

The announcement half is a notification now, which is the right home: a
registration failure is exactly what the user must learn about without
having Settings open.

The remediation half becomes PermissionCard, keyed on permission ===
'denied' rather than on !registered so it cannot vanish while the grant is
still missing. requested/promptSpent stay local because neither is
observable from the backend -- macOS gives no way to ask whether an app's
one-shot prompt has been spent.

The per-row failure reason stays: it is row-contextual, not a banner."
```

---

## Task 18: Docs, the manual checklist, and whole-suite verification

**Files:**
- Create: `docs/superpowers/plans/2026-09-28-phase-7-2-manual-verification.md`
- Modify: `docs/ROADMAP.md`, `CLAUDE.md`, `internal/events/events.go`

- [ ] **Step 1: Confirm `events.go:56`'s comment is now accurate**

The comment claims the notification channel "absorbs all three uniformly". As of Task 9 that is true. Update it to say so in the present tense and name where the adapters live, rather than leaving it reading as a forward promise:

```go
	// EventAudioState is the audio subsystem's health, the sibling of
	// EventHotkeysState and EventJoystickState. Phase 7.2's notification
	// channel absorbs all three uniformly -- see internal/app's
	// notify_keybinds.go and notify_audio.go -- so the shape stays
	// parallel. The audio adapter is called from main.go rather than from
	// internal/app, because audio has TWO emit sites and only one is a
	// Manager callback: a failed NewMalgoBackend has no Manager at all.
	EventAudioState = "audio:state"
```

- [ ] **Step 2: Write the manual verification checklist**

Create `docs/superpowers/plans/2026-09-28-phase-7-2-manual-verification.md`, in the style of the six existing checklists. It must cover every item the spec's §7 records as unobserved:

1. **The joystick flap.** Unplug/replug a stick repeatedly on Windows and on Linux; confirm the notification list does not fill with duplicates and that the final state shown matches reality. **The 2 s window is a guess sized to a poll loop read from source, never measured** — record the observed behaviour and whether the window needs changing.
2. **The audio error text.** Unplug a USB microphone mid-session. Record malgo's exact error string, and whether it is stable across retry attempts or varies — the spec's §7 item 2 names this as the one remaining audio unknown, and a varying string would mean one notification per retry.
3. **Audio hot-unplug transitions.** Confirm an unplug yields a clean `Substituted` transition rather than flapping through an error state first.
4. **Xrun churn.** Drive the audio engine into sustained xruns (heavy CPU load, tiny buffer). Confirm **no** notification appears — the single most important audio behaviour.
5. **macOS Accessibility denial.** Deny the grant. Confirm the notification carries `PERMISSION: denied`, that `PermissionCard` renders with GRANT ACCESS, that granting resolves the notification, and that the restart hint appears when granted-but-unregistered.
6. **macOS joystick unsupported.** Confirm the item appears in the popout as informational, and that the badge, the bell and the toast stack all stay silent and uncoloured.
7. **The notification sound.** **Cannot be verified until `notify_alert.wav` is delivered.** Once it is: confirm it is audible, correctly levelled against voice via the notification bus slider, respects `play_notification_sounds`, and is not startling when it fires during a received transmission.
8. **Cross-window agreement.** Open the popout and the main window; confirm badge, bell count and list agree, and that mark-all-read and clear-all in one are reflected in the other.
9. **Geometry persistence.** Move and resize the popout, close and reopen it.
10. **Dismissal.** Dismiss a live hotkey failure; confirm it does not reappear on the next keybind edit, and that it does reappear after the condition clears and recurs.

- [ ] **Step 3: Update the ROADMAP**

Change the Phase 7.2 row's status to `[x]` complete with today's date, matching the format of the 7.1 row directly above it. State plainly:

- What landed, including **both additions beyond the ROADMAP's original deliverables**: audio as a third source, and the notification sound engine. Say they were design-time additions, not in the original row.
- The verification status: automated suite green, **not field-verified**, checklist path.
- That the notification sound ships **silent**, and that **the outstanding sample pack is now ten files, not nine**.
- The remaining unknowns from spec §7.

Also update `docs/PROTO_GAPS.md` §8 only if its "Today: Local notifications only" line is now inaccurate — read it and decide; no proto change was made, so it very likely stands as written.

- [ ] **Step 4: Update CLAUDE.md**

Add a Phase 7.2 row to the status table, and a short subsection in the style of the existing ones recording the two things a future session would otherwise get wrong:

- **The level-to-edge contract.** `hotkeys:state` has no dedupe at its emit site and fires from nine call sites; `audio:state` compares a struct containing monotonic xrun counters and fires every 2 s during a glitch. Anything consuming either through the notification channel must project to the user-visible fields and rely on `internal/notify`'s fingerprint. Do not "fix" the chatty emitters — every other consumer is already idempotent.
- **Notification sound ships silent by missing asset, not missing code.** `notify_alert.wav` is the tenth outstanding WAV. Do not synthesise a placeholder; `internal/audio/assets/README.md` records the pack as not something to substitute.

- [ ] **Step 5: Run the complete verification suite**

Run each, and record the actual output — do not claim any of them passed without seeing it:

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
export GOCACHE=$TMPDIR/vcs-gocache
go build -tags purego ./... ; echo "build=$?"
go vet -tags purego ./... ; echo "vet=$?"
go test -tags purego -race ./... ; echo "test=$?"
cd frontend
npx vitest run ; echo "vitest=$?"
npx tsc --noEmit ; echo "tsc=$?"
npm run build ; echo "build=$?"
```

`internal/voice`'s tests need `dangerouslyDisableSandbox` — the sandbox blocks `bind(2)`, so they fail inside it and pass outside. Run that package separately if `go test ./...` reports `listen udp: operation not permitted`.

Every one must report 0. A non-zero exit is a blocker, not a note.

- [ ] **Step 6: Verify the source-agnosticism claim end to end**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
grep -rniE "hotkey|joystick|audio" internal/notify/*.go | grep -v "_test.go" | grep -v "^.*://"
grep -rniE "hotkey|joystick|audio" frontend/src/windows/notifications/ frontend/src/shared/store/notifications.ts
```
Expected: matches only inside explanatory comments, never in an import or an identifier. Anything else means a source leaked into the generic layer.

- [ ] **Step 7: Commit**

```bash
git add docs/ CLAUDE.md internal/events/events.go
git commit -m "docs(phase-7-2): record the notification channel and its verification debt

Adds the manual checklist -- a seventh joining the six already unrun --
covering everything spec section 7 records as unobserved: the joystick
flap the 2s window is sized against, malgo's error-text stability, xrun
churn producing no notification, macOS Accessibility denial, and the
sound, which cannot be verified at all until notify_alert.wav exists.

events.go:56's comment is now accurate rather than a forward promise: all
three sources are absorbed."
```

---

## Self-Review Notes

Recorded so an executor can see what was checked rather than re-deriving it.

**Spec coverage.** Every section maps to a task: §3.1–3.2 → Tasks 1–4; §3.3 → Tasks 8, 9; §3.4–3.5 → Tasks 1, 13; §3.6 → Task 6; §3.7 → Tasks 1, 3; §3.8 → Tasks 10–12; §4.1 → Task 3; §4.1's dismissal rule → Task 4; §4.2 → Task 5; §5.1–5.2 → Task 8; §5.3 → Task 8; §5.4 → Task 9; §6.1 → Tasks 14, 15; §6.2 → Task 16; §6.3 → Task 13; §6.4 → Task 17; §7 → Task 18; §9 DoD 1–17 → all tasks.

**Type consistency.** `Item`/`Action`/`KV`/`Snapshot` are defined once in Task 1 and used unchanged thereafter; the TS mirror in Task 13 matches field-for-field including JSON casing. `RaiseWindowed`/`ResolveWindowed` (Task 5) are what Tasks 8 and 9 call — never bare `Raise`/`Resolve`. `notifyHotkeyState`/`notifyJoystickState` are unexported (called from `internal/app`); `NotifyAudioState` is exported (called from `main.go`).

**Known temporary stub.** Task 7 leaves `notify.Options.OnSound` as a no-op with a `// TASK 12:` marker, replaced in Task 12 step 6. This is the only stub in the plan and it is deliberate: blocking the whole channel on an audio engine three tasks away would be worse.

**Review Focus coverage.** Item 1 → Task 9 `TestXrunCountersProduceNoNotification`. Item 2 → Task 3 `TestConcurrentRaiseIsSafe` under `-race`. Item 3 → Task 5 `TestTrailingTimerAfterClear...` and `...AfterDismiss...`. Item 4 → Task 11 `TestPlayNotificationOnStoppedManagerDrops`. Item 5 → Task 14's long-body/markup test.
