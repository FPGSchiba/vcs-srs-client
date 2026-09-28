package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"
)

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
	// The condition genuinely cleared, so a later recurrence is news again --
	// regardless of whether the item is still in the list. Dismiss removes
	// the item from n.items but leaves the key in n.suppressed, so this must
	// run even when findByKeyLocked comes back empty; otherwise a dismissed
	// item's suppression would outlive the very Resolve that should lift it.
	delete(n.suppressed, key)
	idx := n.findByKeyLocked(key)
	if idx < 0 || n.items[idx].Resolved {
		n.mu.Unlock()
		return
	}
	n.items[idx].Resolved = true
	n.items[idx].Unread = false
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
