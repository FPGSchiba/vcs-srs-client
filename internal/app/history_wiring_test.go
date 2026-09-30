package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/history"
	"github.com/FPGSchiba/vcs-srs-client/internal/profile"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

type geomSpy struct{ set []string }

func (g *geomSpy) Open(string)                                   {}
func (g *geomSpy) Close(string)                                  {}
func (g *geomSpy) Toggle(string)                                 {}
func (g *geomSpy) OpenWindows() []string                         { return nil }
func (g *geomSpy) Geometry(string) windowstate.Geometry          { return windowstate.Geometry{} }
func (g *geomSpy) SetGeometry(id string, _ windowstate.Geometry) { g.set = append(g.set, id) }

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

func TestHistoryTickerIsJoinedBeforeFinalFlush(t *testing.T) {
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

func TestCommsWindowResizeDirtiesActiveProfile(t *testing.T) {
	a := activeProfileApp(t)
	spy := &geomSpy{}
	a.windows = spy
	a.settings.mu.Lock()
	a.settings.layoutPending = false
	a.settings.mu.Unlock()

	done := make(chan struct{})
	go func() {
		a.SetWindowGeometry("comms", windowstate.Geometry{X: 1, Y: 2, W: 777, H: 888})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SetWindowGeometry deadlocked: emit must run outside sb.mu")
	}

	a.settings.mu.Lock()
	cl := a.settings.cfg.CommsLayout
	a.settings.mu.Unlock()
	if cl.WindowW != 777 || cl.WindowH != 888 {
		t.Fatalf("CommsLayout window = %dx%d", cl.WindowW, cl.WindowH)
	}
	if !pendingOf(a) {
		t.Fatal("layoutPending must be set so shutdown persists the size")
	}
	if !a.profileDirty() {
		t.Fatal("a comms window resize must dirty the active profile (D3)")
	}
	if len(spy.set) != 1 || spy.set[0] != "comms" {
		t.Fatalf("windows.json path must still be driven: %v", spy.set)
	}
}

func TestCommsWindowResizeToSameSizeIsANoOp(t *testing.T) {
	a := activeProfileApp(t)
	a.windows = &geomSpy{}
	a.SetWindowGeometry("comms", windowstate.Geometry{W: 640, H: 480})
	a.settings.mu.Lock()
	a.settings.layoutPending = false
	a.settings.mu.Unlock()
	dirtyBefore := a.profileDirty()
	a.SetWindowGeometry("comms", windowstate.Geometry{X: 9, W: 640, H: 480})
	if pendingOf(a) {
		t.Fatal("an unchanged size (e.g. the echo of a profile load) must not schedule a write")
	}
	if a.profileDirty() != dirtyBefore {
		t.Fatal("an unchanged size must not change dirty state")
	}
}

func TestOtherWindowResizeDoesNotTouchLayout(t *testing.T) {
	a := activeProfileApp(t)
	a.windows = &geomSpy{}
	a.settings.mu.Lock()
	a.settings.layoutPending = false
	a.settings.mu.Unlock()
	a.SetWindowGeometry("settings", windowstate.Geometry{W: 555, H: 444})
	if pendingOf(a) || a.profileDirty() {
		t.Fatal("a non-comms window resize must not touch the layout or dirty the profile")
	}
}
