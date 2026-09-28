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
