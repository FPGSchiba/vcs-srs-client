package app

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/history"
	"github.com/FPGSchiba/vcs-srs-client/internal/profile"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

func TestHistoryWiredPredicate(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	if HistoryWired(a) {
		t.Fatal("a fresh App has no log")
	}
	SetHistory(a, history.New(10))
	if !HistoryWired(a) {
		t.Fatal("SetHistory must wire the log")
	}
}

// Proves only the lifecycle: the ticker fields are cleared, a double start is
// harmless and a second flush is safe. The debounce is an hour, so the ticker
// never flushes here; it does NOT prove the join blocks on an in-flight flush.
// That would need history's unexported afterSnapshot seam, which internal/app
// cannot reach.
func TestHistoryTickerLifecycle(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	l := history.New(10)
	p := filepath.Join(t.TempDir(), "h.json")
	l.SetPersist(p, time.Hour, time.Now, nil)
	SetHistory(a, l)
	StartHistoryTicker(a)
	StartHistoryTicker(a) // idempotent
	if err := a.flushHistory(); err != nil {
		t.Fatal(err)
	}
	if a.histStop != nil || a.histDone != nil {
		t.Fatal("ticker must be stopped and joined by flushHistory")
	}
	if err := a.flushHistory(); err != nil {
		t.Fatalf("second flushHistory must be safe: %v", err)
	}
}

func activeProfileApp(t *testing.T) *App {
	t.Helper()
	a := newTestAppWithConfig(t, config.Default(), filepath.Join(t.TempDir(), "config.toml"))
	path := filepath.Join(t.TempDir(), "x"+profile.Ext)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "X",
		Radios:        []profile.Radio{{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true}},
	}
	if err := profile.Write(path, d); err != nil {
		t.Fatal(err)
	}
	loaded, err := profile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.applyProfile(loaded, path); err != nil {
		t.Fatal(err)
	}
	if a.profileDirty() {
		t.Fatal("precondition: freshly loaded profile is clean")
	}
	return a
}

func pendingOf(a *App) bool {
	a.settings.mu.Lock()
	defer a.settings.mu.Unlock()
	return a.settings.layoutPending
}

// boundsHandle is a window handle that can report user resizes, like the
// Wails one. SetBounds optionally lands a pixel or two off, as DPI rounding
// or frame chrome can.
type boundsHandle struct {
	mu   sync.Mutex
	geom windowstate.Geometry
	skew int
	cb   func()
}

func (h *boundsHandle) Focus() {}
func (h *boundsHandle) Close() {}
func (h *boundsHandle) Bounds() windowstate.Geometry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.geom
}
func (h *boundsHandle) SetBounds(g windowstate.Geometry) {
	h.mu.Lock()
	h.geom = windowstate.Geometry{X: g.X, Y: g.Y, W: g.W + h.skew, H: g.H + h.skew}
	h.mu.Unlock()
}
func (h *boundsHandle) OnBoundsChanged(fn func()) { h.cb = fn }
func (h *boundsHandle) resizeTo(w, ht int) {
	h.mu.Lock()
	h.geom.W, h.geom.H = w, ht
	h.mu.Unlock()
	h.cb()
}

type boundsFactory struct{ h *boundsHandle }

func (f *boundsFactory) Create(string, string, windowstate.Geometry) WindowHandle { return f.h }

// realPathApp wires an App to a real Registry over a fake native window, so
// a test drives the same chain production does: native event -> debounce ->
// registry observer -> captureCommsWindowSize.
func realPathApp(t *testing.T, h *boundsHandle) (*App, *Registry) {
	t.Helper()
	a := activeProfileApp(t)
	reg := NewRegistry(&boundsFactory{h: h}, filepath.Join(t.TempDir(), "windows.json"), nil)
	reg.debounce = 10 * time.Millisecond
	a.SetBackend(nil, reg)
	reg.Open("comms")
	return a, reg
}

func TestCommsNativeResizeEventDirtiesActiveProfile(t *testing.T) {
	h := &boundsHandle{}
	a, _ := realPathApp(t, h)
	a.settings.mu.Lock()
	a.settings.layoutPending = false
	a.settings.mu.Unlock()

	// A drag: many events, one settled size.
	for w := 700; w <= 777; w += 7 {
		h.resizeTo(w, 888)
	}
	h.resizeTo(777, 888)

	waitFor(t, 3*time.Second, "profile to go dirty", a.profileDirty)
	a.settings.mu.Lock()
	cl := a.settings.cfg.CommsLayout
	a.settings.mu.Unlock()
	if cl.WindowW != 777 || cl.WindowH != 888 {
		t.Fatalf("CommsLayout window = %dx%d, want 777x888", cl.WindowW, cl.WindowH)
	}
	if !pendingOf(a) {
		t.Fatal("layoutPending must be set so shutdown persists the size")
	}
}

func TestNativeResizeOfAnotherWindowDoesNotTouchLayout(t *testing.T) {
	a := activeProfileApp(t)
	h := &boundsHandle{}
	reg := NewRegistry(&boundsFactory{h: h}, filepath.Join(t.TempDir(), "windows.json"), nil)
	reg.debounce = 10 * time.Millisecond
	a.SetBackend(nil, reg)
	reg.Open("settings")
	a.settings.mu.Lock()
	a.settings.layoutPending = false
	a.settings.mu.Unlock()
	h.resizeTo(555, 444)
	time.Sleep(100 * time.Millisecond)
	if pendingOf(a) || a.profileDirty() {
		t.Fatal("a non-comms resize must not touch the layout or dirty the profile")
	}
}

func TestProfileLoadEchoWithinToleranceDoesNotDirty(t *testing.T) {
	h := &boundsHandle{skew: 2} // the window reports 2px more than it was told
	a, _ := realPathApp(t, h)

	path := filepath.Join(t.TempDir(), "y"+profile.Ext)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "Y",
		Radios:        []profile.Radio{{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true}},
		Layout:        profile.Layout{Window: profile.WindowSize{W: 600, H: 800}},
	}
	if err := profile.Write(path, d); err != nil {
		t.Fatal(err)
	}
	loaded, err := profile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.applyProfile(loaded, path); err != nil {
		t.Fatal(err)
	}
	if got := h.Bounds(); got.W != 602 || got.H != 802 {
		t.Fatalf("precondition: applyProfile must have resized the window, got %+v", got)
	}
	// The native echo of that SetBounds.
	h.cb()
	time.Sleep(150 * time.Millisecond)
	if a.profileDirty() {
		t.Fatal("a 2px echo of the profile's own size must not dirty a freshly loaded profile")
	}
}
