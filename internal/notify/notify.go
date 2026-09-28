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
