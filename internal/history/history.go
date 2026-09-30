// Package history is the local transmission log: a bounded ring of
// completed transmissions, plus its JSON persistence.
//
// It imports nothing outside the standard library. Entries arrive FULLY
// RESOLVED -- callsign already looked up, channel name already matched --
// so this package never needs internal/state, internal/config or
// internal/voice, and can be tested without any of them.
package history

import (
	"sync"
	"time"
)

// DefaultCap bounds the ring: roughly a long evening's operations. Old
// entries are evicted rather than the log growing without limit, because
// the file is rewritten in full on every flush.
const DefaultCap = 2000

// Entry is one completed transmission.
//
// Radio is the LOCAL channel name the frequency matched, and is empty for a
// server global channel -- those are accepted without a tuned radio, so
// there is no local name to show and inventing one would be a lie. The UI
// renders the frequency alone in that case.
//
// Sender may be empty when the talker's GUID is not in the client store
// (they had already left by the time the transmission ended). The row still
// records: the frequency and duration are the parts worth keeping.
type Entry struct {
	At         time.Time `json:"at"`
	Sender     string    `json:"sender"`
	GUID       string    `json:"guid"`
	FreqKHz    uint32    `json:"freq_khz"`
	Radio      string    `json:"radio"`
	DurationMS int       `json:"dur_ms"`
	Own        bool      `json:"own"`
}

// Log is the ring. Safe for concurrent use: the RX delivery goroutine and
// the TX press path both Append, and bindings read Snapshot from a third.
type Log struct {
	mu      sync.RWMutex
	cap     int
	entries []Entry // oldest first

	// dirty is set by every mutation and cleared by the persistence layer
	// after a successful flush. See store.go.
	dirty bool
	// rev increments on every mutation. Flush captures it with its snapshot
	// and clears dirty only if it is unchanged, so an Append that lands
	// during the (unlocked) file write is not marked clean.
	rev uint64
	// lastChange is when the most recent mutation happened; Tick reads it to
	// decide whether the debounce window has elapsed.
	lastChange time.Time

	path     string
	debounce time.Duration
	now      func() time.Time
	onErr    func(error)

	// afterSnapshot is a test seam: Flush calls it, unlocked, between taking
	// its snapshot and writing. Nil in production.
	afterSnapshot func()
}

// New builds a Log. A non-positive cap means DefaultCap.
func New(capacity int) *Log {
	if capacity <= 0 {
		capacity = DefaultCap
	}
	return &Log{cap: capacity, entries: make([]Entry, 0, capacity)}
}

// Cap reports the ring capacity.
func (l *Log) Cap() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cap
}

// Append records one transmission, evicting the oldest if the ring is full.
func (l *Log) Append(e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) >= l.cap {
		copy(l.entries, l.entries[len(l.entries)-l.cap+1:])
		l.entries = l.entries[:l.cap-1]
	}
	l.entries = append(l.entries, e)
	l.dirty = true
	l.rev++
	if l.now != nil {
		l.lastChange = l.now()
	}
}

// Snapshot returns a copy, NEWEST FIRST -- the order the table renders and
// the order a reader wants. A copy, not the backing array, so a caller
// cannot mutate the log by editing what it was handed.
//
// Always non-nil: the frontend types the list as an array and a nil slice
// marshals to null.
func (l *Log) Snapshot() []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Entry, len(l.entries))
	for i, e := range l.entries {
		out[len(l.entries)-1-i] = e
	}
	return out
}

// Len reports how many entries are held.
func (l *Log) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// Clear empties the log.
func (l *Log) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
	l.dirty = true
	l.rev++
	if l.now != nil {
		l.lastChange = l.now()
	}
}
