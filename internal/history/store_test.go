package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func TestLoadMissingFileIsEmptyNotError(t *testing.T) {
	l, err := Load(filepath.Join(t.TempDir(), "nope.json"), 10)
	if err != nil {
		t.Fatalf("a missing history file must load empty, not error: %v", err)
	}
	if l.Len() != 0 {
		t.Fatalf("Len = %d, want 0", l.Len())
	}
}

func TestLoadCorruptFileIsEmptyWithError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(p, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Load(p, 10)
	if err == nil {
		t.Fatal("a corrupt history file must report the error so the user is told")
	}
	if l == nil || l.Len() != 0 {
		t.Fatal("Load must still return a usable empty log so the session can record new entries")
	}
}

func TestFlushRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))
	l.Append(mk(2))
	if err := l.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	back, err := Load(p, 10)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := back.Snapshot()
	if len(got) != 2 || got[0].DurationMS != 1002 {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestFlushIsAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "h.json")
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1 (no .tmp left behind): %+v", len(entries), entries)
	}
}

func TestFlushSkipsWhenNotDirty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	first := st.ModTime()

	time.Sleep(10 * time.Millisecond)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	st2, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.ModTime().Equal(first) {
		t.Fatal("a Flush with nothing new must not rewrite the file")
	}
}

func TestTickDebounces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	c := &fakeClock{t: time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)}
	l := New(10)
	l.SetPersist(p, 5*time.Second, c.now, nil)

	l.Append(mk(1))
	c.add(2 * time.Second)
	l.Tick()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("Tick before the debounce window elapsed must not write")
	}

	c.add(4 * time.Second) // 6s since the append
	l.Tick()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("Tick after the window must write: %v", err)
	}
}

func TestTickCoalescesABurst(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	c := &fakeClock{t: time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)}
	l := New(100)
	l.SetPersist(p, 5*time.Second, c.now, nil)

	// Ten transmissions inside one window produce exactly one write.
	for i := 0; i < 10; i++ {
		l.Append(mk(i))
		c.add(200 * time.Millisecond)
		l.Tick()
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("a burst inside one debounce window must not write yet")
	}
	c.add(5 * time.Second)
	l.Tick()
	back, err := Load(p, 100)
	if err != nil {
		t.Fatal(err)
	}
	if back.Len() != 10 {
		t.Fatalf("Len = %d, want all 10 coalesced into one write", back.Len())
	}
}

func TestFlushErrorReachesCallbackOncePerFailureRun(t *testing.T) {
	// The error hook is how internal/app raises ONE notification for a
	// failing flush instead of one per 5s tick. The package reports every
	// failure; collapsing repeats is the app's job via notify.Raise's
	// fingerprint, but the package must at least not swallow it.
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "h.json") // parent does not exist
	var got []error
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, func(err error) { got = append(got, err) })
	l.Append(mk(1))
	if err := l.Flush(); err == nil {
		t.Fatal("want an error flushing into a missing parent dir")
	}
	if len(got) != 1 {
		t.Fatalf("onErr called %d times, want 1", len(got))
	}
}

func TestPersistUnsetFlushIsNoop(t *testing.T) {
	l := New(10)
	l.Append(mk(1))
	if err := l.Flush(); err != nil {
		t.Fatalf("Flush with no path configured must be a silent no-op: %v", err)
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(p, []byte(`{"schema_version":99,"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Load(p, 10)
	if err == nil {
		t.Fatal("want an error for a newer schema")
	}
	if l == nil {
		t.Fatal("Load must still return a usable log")
	}
}

func TestCSVHeaderAndEscaping(t *testing.T) {
	out := string(CSV([]Entry{{
		At:         time.Date(2026, 9, 30, 21, 15, 51, 0, time.UTC),
		Sender:     `Dab"ble, Jr`,
		FreqKHz:    118500,
		Radio:      "Fleet Common",
		DurationMS: 3200,
		Own:        true,
	}}))
	if !strings.HasPrefix(out, "time,sender,channel,frequency_mhz,duration_s,own\n") {
		t.Fatalf("header wrong:\n%s", out)
	}
	if !strings.Contains(out, `"Dab""ble, Jr"`) {
		t.Fatalf("a sender containing a quote and a comma must be CSV-escaped:\n%s", out)
	}
	if !strings.Contains(out, "118.500") {
		t.Fatalf("frequency must render as MHz to 3dp:\n%s", out)
	}
	if !strings.Contains(out, "3.2") {
		t.Fatalf("duration must render in seconds:\n%s", out)
	}
}

func TestCSVEmptyStillHasHeader(t *testing.T) {
	if got := string(CSV(nil)); !strings.HasPrefix(got, "time,sender,") {
		t.Fatalf("an empty export must still be a valid CSV with a header, got %q", got)
	}
}

// TestFlushDoesNotLoseAppendDuringWrite pins the lost-write race: an Append
// that lands while Flush is writing (Flush does its I/O outside the lock)
// must leave the log dirty, so the next Flush writes it. afterSnapshot runs
// at exactly that point, making the interleaving deterministic.
func TestFlushDoesNotLoseAppendDuringWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))

	l.afterSnapshot = func() { l.Append(mk(2)) }
	if err := l.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	l.afterSnapshot = nil

	l.mu.RLock()
	dirty := l.dirty
	l.mu.RUnlock()
	if !dirty {
		t.Fatal("an Append during the write was marked clean: that transmission would never reach disk")
	}
	if err := l.Flush(); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	back, err := Load(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	if back.Len() != 2 {
		t.Fatalf("Len = %d, want 2: the mid-write append was lost", back.Len())
	}
}

func TestFlushWriteFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	// The target path is a directory, so the rename fails after the temp
	// file has been written.
	p := filepath.Join(dir, "h.json")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))
	if err := l.Flush(); err == nil {
		t.Fatal("want an error renaming over a directory")
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file must be removed on failure, stat err = %v", err)
	}
}

func TestFlushWriteFileFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "h.json")
	tmp := p + ".tmp"
	// A directory at the temp path makes os.WriteFile fail at open. The
	// cleanup must remove it; os.Remove deletes an empty directory.
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))
	if err := l.Flush(); err == nil {
		t.Fatal("want an error when the temp path cannot be written")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temp path must be cleaned up after a WriteFile failure, stat err = %v", err)
	}
}

func TestLoadLeavesLogNotDirty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	seed := New(10)
	seed.SetPersist(p, time.Second, time.Now, nil)
	seed.Append(mk(1))
	if err := seed.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"schema_version":1,"entries":[{"at":"2026-09-30T21:00:00Z","sender":"x","guid":"","freq_khz":1,"radio":"","dur_ms":1,"own":false}]} `), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Load(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	if l.Len() != 1 {
		t.Fatalf("Len = %d, want 1", l.Len())
	}
	l.SetPersist(p, time.Second, time.Now, nil)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), "} ") {
		t.Fatal("a freshly loaded log must not be dirty: Flush rewrote the file")
	}
}
