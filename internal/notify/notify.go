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
//
// # The three named window constants are a deliberate exception
//
// WindowHotkeys, WindowJoystick and WindowAudio (coalesce.go) name sources
// inside a package that is otherwise studiously ignorant of them, and a
// future reader will be tempted to "fix" that. Do not. The binding invariant
// is on the IMPORT graph, not on vocabulary -- verify it with
//
//	go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/notify/
//
// which must show only standard-library packages, never a text grep (ruling
// R7: the doc comments here name hotkeys, joystick and audio precisely in
// order to explain what the package does NOT know about them, so a grep
// cannot answer an import question). These three are the package's own
// vocabulary for per-source tuning: a coalescing window has to be several
// times its source's period to coalesce rather than beat against it, so the
// value is meaningless without saying whose period it was chosen against.
// Collapsing them into one constant, or moving them out to the adapters,
// would either mistune a source or scatter the reasoning that keeps them
// tuned. See coalesce.go's constant block for that reasoning.
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
	//
	// Deliberately NOT pruned, which is the opposite treatment to pending and
	// windows (see retirePendingLocked) and worth the asymmetry:
	//
	//   - There is no moment at which pruning would be correct. An entry's
	//     whole job is to outlive the item: Dismiss deletes the item and
	//     KEEPS the entry, because a condition that still holds would
	//     otherwise be re-announced on the next routine re-emission. Pruning
	//     against n.items would undo the very dismissal the user asked for,
	//     and pruning on cap eviction would do it silently. The two events
	//     that DO mean "this is news again" -- a Raise at a differing
	//     fingerprint and a Resolve -- already delete the entry.
	//   - The growth argument is different in kind. pending and windows are
	//     written by every incoming Raise/Resolve, so a 7.3/7.4 per-client or
	//     per-frequency key set would let a REMOTE party's traffic grow them.
	//     An entry here is only ever created by a deliberate human click
	//     (Dismiss, or Clear over what is on screen), on a key that is
	//     already in the capped list. It is bounded by the user's own
	//     dismissals, at one map entry -- a key plus a 64-char hex digest --
	//     each.
	suppressed map[string]string
	// cancelGen counts user-driven retirements -- Dismiss, Clear and
	// StopTimers, via cancelPendingLocked. coalesce samples it before running
	// the leading edge and re-checks it before arming, so a cancel that lands
	// in that gap cannot leave a window armed for a key that has just been
	// dismissed, cleared or shut down. Guarded by mu like everything below
	// it. See coalesce.
	//
	// It is ONE counter for the whole Notifier, not one per key, so it is
	// coarser than "a cancel for this key": a Dismiss of item A landing in
	// that gap also skips the arm of an unrelated key B. Accepted
	// deliberately -- see coalesce for the cost/benefit, which turns on
	// n.suppressed's unprunability argument just above.
	cancelGen uint64
	// pendingGen stamps each pendingChange as it is created, so a fired
	// trailing timer can tell its own entry from a successor that reused the
	// key while it was running. Monotonic across all keys; only equality
	// within one key is ever asked. See coalesce.go's pendingChange.
	pendingGen uint64
	// pending holds coalescing state per key. See coalesce.go.
	pending map[string]*pendingChange
	// windows records the window each key's OPEN coalescing window was armed
	// with, so a re-armed trailing timer uses the same period. Written and
	// pruned in lockstep with pending -- see retirePendingLocked for why it
	// is pruned rather than left to accumulate.
	windows map[string]time.Duration
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
		windows:    map[string]time.Duration{},
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
