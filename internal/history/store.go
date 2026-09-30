package history

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// SchemaVersion is the history file's format version.
const SchemaVersion = 1

// file is the on-disk shape.
type file struct {
	SchemaVersion int     `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}

// Load reads a history file into a new Log.
//
// A MISSING file is an empty log with no error -- the discipline
// windowstate.Load uses, and the normal first-run case. A CORRUPT or
// too-new file returns an error AND a usable empty log: the session must
// still be able to record new transmissions, and refusing to start logging
// because yesterday's file is damaged would compound the problem.
func Load(path string, capacity int) (*Log, error) {
	l := New(capacity)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, fmt.Errorf("read history: %w", err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return l, fmt.Errorf("parse history: %w", err)
	}
	if f.SchemaVersion > SchemaVersion {
		return l, fmt.Errorf("history: schema version %d is newer than this build reads (%d)",
			f.SchemaVersion, SchemaVersion)
	}
	for _, e := range f.Entries {
		l.Append(e)
	}
	l.mu.Lock()
	l.dirty = false // a freshly loaded log has nothing new to write
	l.mu.Unlock()
	return l, nil
}

// SetPersist configures where and how often this log writes itself.
//
// debounce is the minimum quiet period after the most recent Append before
// Tick will write. now is the clock (injected so the tests need no
// sleeping). onErr, if non-nil, receives every flush failure -- internal/app
// turns that into ONE raised notification rather than one per tick, using
// notify's fingerprint; this package's job is only to not swallow it.
//
// A Log with no path configured flushes as a silent no-op, so a test or a
// build with no app-data dir keeps working.
func (l *Log) SetPersist(path string, debounce time.Duration, now func() time.Time, onErr func(error)) {
	if now == nil {
		now = time.Now
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.path = path
	l.debounce = debounce
	l.now = now
	l.onErr = onErr
}

// Tick writes the log if it is dirty and the debounce window has elapsed
// since the last Append.
//
// Deliberately PULL-based rather than an internal timer: internal/notify's
// coalescing timers needed a StopTimers method and their own test
// apparatus to avoid sleeping, whereas a Tick driven by the caller's
// existing ticker is testable with an injected clock and has nothing to
// stop at shutdown.
func (l *Log) Tick() {
	l.mu.Lock()
	if !l.dirty || l.path == "" || l.now == nil {
		l.mu.Unlock()
		return
	}
	if l.now().Sub(l.lastChange) < l.debounce {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()
	_ = l.Flush()
}

// Flush writes the log now, regardless of the debounce window. Called on
// shutdown, and by Tick once the window has elapsed.
//
// A no-op (nil error) when nothing has changed since the last successful
// write, so a quiet session does not rewrite the file on every tick.
func (l *Log) Flush() error {
	l.mu.Lock()
	if l.path == "" || !l.dirty {
		l.mu.Unlock()
		return nil
	}
	path := l.path
	onErr := l.onErr
	snap := make([]Entry, len(l.entries))
	copy(snap, l.entries)
	snapRev := l.rev
	hook := l.afterSnapshot
	l.mu.Unlock()

	// File I/O happens OUTSIDE the lock so an Append on the RX path never
	// blocks on disk. The price: an Append can land during the write, which
	// is why dirty is cleared below only if rev has not moved.
	if hook != nil {
		hook()
	}

	b, err := json.MarshalIndent(file{SchemaVersion: SchemaVersion, Entries: snap}, "", "  ")
	if err != nil {
		err = fmt.Errorf("marshal history: %w", err)
		if onErr != nil {
			onErr(err)
		}
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		_ = os.Remove(tmp) // WriteFile can fail after creating the file
		err = fmt.Errorf("write history: %w", err)
		if onErr != nil {
			onErr(err)
		}
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		err = fmt.Errorf("rename history: %w", err)
		if onErr != nil {
			onErr(err)
		}
		return err
	}

	l.mu.Lock()
	if l.rev == snapRev {
		l.dirty = false
	}
	l.mu.Unlock()
	return nil
}

// CSV renders entries for the Transmission Log's EXPORT CSV button.
//
// Frequencies render as MHz to three decimals and durations as seconds,
// matching what the table shows -- an export nobody can line up against
// the screen is not much of an export. The kHz integer stays the stored
// form; this is a presentation layer.
func CSV(entries []Entry) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"time", "sender", "channel", "frequency_mhz", "duration_s", "own"})
	for _, e := range entries {
		_ = w.Write([]string{
			e.At.Format(time.RFC3339),
			e.Sender,
			e.Radio,
			strconv.FormatFloat(float64(e.FreqKHz)/1000.0, 'f', 3, 64),
			strconv.FormatFloat(float64(e.DurationMS)/1000.0, 'f', 1, 64),
			strconv.FormatBool(e.Own),
		})
	}
	w.Flush()
	return buf.Bytes()
}
