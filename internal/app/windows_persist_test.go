package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// settleHandle is a WindowHandle whose bounds the test can change and whose
// bounds-changed callback the test can fire.
type settleHandle struct {
	mu sync.Mutex
	g  windowstate.Geometry
	cb func()
}

func (h *settleHandle) Focus() {}
func (h *settleHandle) Close() {}
func (h *settleHandle) Bounds() windowstate.Geometry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.g
}
func (h *settleHandle) SetBounds(g windowstate.Geometry) { h.setGeom(g) }
func (h *settleHandle) OnBoundsChanged(fn func())        { h.cb = fn }
func (h *settleHandle) setGeom(g windowstate.Geometry) {
	h.mu.Lock()
	h.g = g
	h.mu.Unlock()
}

type settleFactory struct{ h *settleHandle }

func (f *settleFactory) Create(string, string, windowstate.Geometry) WindowHandle { return f.h }

func newSettleRegistry(t *testing.T) (*Registry, *settleHandle) {
	t.Helper()
	h := &settleHandle{g: windowstate.Geometry{W: 600, H: 700}}
	r := NewRegistry(&settleFactory{h: h}, filepath.Join(t.TempDir(), "windows.json"), nil)
	r.debounce = 5 * time.Millisecond
	r.Open("comms")
	return r, h
}

// While windows.json is being written, r.mu must be free: every resize
// event's scheduleBounds and every OpenWindows call needs it.
func TestRegistryPersistDoesNotHoldMu(t *testing.T) {
	r, h := newSettleRegistry(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	r.saveFn = func(string, map[string]windowstate.Geometry) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return nil
	}
	defer close(release)

	h.cb()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("settle never reached the save")
	}

	got := make(chan []string, 1)
	go func() { got <- r.OpenWindows() }()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("OpenWindows blocked while windows.json was being saved: r.mu is held across the disk write")
	}
}

type recordingHandler struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.recs = append(h.recs, r)
	h.mu.Unlock()
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func TestRegistryPersistFailureLogsWarn(t *testing.T) {
	rec := &recordingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r, _ := newSettleRegistry(t)
	r.saveFn = func(string, map[string]windowstate.Geometry) error {
		return errors.New("disk full")
	}
	r.SetGeometry("comms", windowstate.Geometry{W: 640, H: 480})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	n := 0
	for _, x := range rec.recs {
		if x.Message == "save windows.json failed" {
			if x.Level != slog.LevelWarn {
				t.Fatalf("save failure logged at %v, want WARN", x.Level)
			}
			n++
		}
	}
	if n != 1 {
		t.Fatalf("got %d 'save windows.json failed' records, want exactly 1 (all records: %d)", n, len(rec.recs))
	}
}

// Two settles racing to disk: the file must end with the later geometry.
// The first save is parked mid-write while the second settle lands; without
// a disk-order lock the second would write first and the parked first save
// would then overwrite it with stale geometry.
func TestRegistryPersistWritesLatestLast(t *testing.T) {
	r, h := newSettleRegistry(t)
	a := windowstate.Geometry{X: 1, W: 600, H: 700}
	b := windowstate.Geometry{X: 2, W: 640, H: 480}

	var mu sync.Mutex
	var saved []windowstate.Geometry
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	first := true
	r.saveFn = func(_ string, m map[string]windowstate.Geometry) error {
		mu.Lock()
		park := first
		first = false
		mu.Unlock()
		if park {
			entered <- struct{}{}
			<-release
		}
		mu.Lock()
		saved = append(saved, m["comms"])
		mu.Unlock()
		return nil
	}

	var wg sync.WaitGroup
	h.setGeom(a)
	wg.Add(1)
	go func() { defer wg.Done(); r.settleBounds("comms", h) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first settle never reached the save")
	}
	h.setGeom(b)
	wg.Add(1)
	go func() { defer wg.Done(); r.settleBounds("comms", h) }()
	time.Sleep(50 * time.Millisecond) // let the second settle reach the save if it can
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(saved) == 0 || saved[len(saved)-1] != b {
		t.Fatalf("saves in order = %+v; the last write must hold the later geometry %+v", saved, b)
	}
	if g := r.Geometry("comms"); g != b {
		t.Fatalf("in-memory geometry = %+v, want %+v", g, b)
	}
}
