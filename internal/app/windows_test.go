package app_test

import (
	"path/filepath"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

type fakeHandle struct {
	closed bool
	geom   windowstate.Geometry
	focus  int
}

func (h *fakeHandle) Focus()                           { h.focus++ }
func (h *fakeHandle) Close()                           { h.closed = true }
func (h *fakeHandle) Bounds() windowstate.Geometry     { return h.geom }
func (h *fakeHandle) SetBounds(g windowstate.Geometry) { h.geom = g }

type fakeFactory struct{ created []string }

func (f *fakeFactory) Create(id string, url string, g windowstate.Geometry) app.WindowHandle {
	f.created = append(f.created, id)
	return &fakeHandle{geom: g}
}

func TestRegistry_OpenCreatesOnceThenFocuses(t *testing.T) {
	ff := &fakeFactory{}
	r := app.NewRegistry(ff, filepath.Join(t.TempDir(), "windows.json"), nil)

	r.Open("comms")
	r.Open("comms") // second open should focus, not recreate
	if len(ff.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(ff.created))
	}
}

func TestRegistry_PersistsGeometryOnClose(t *testing.T) {
	ff := &fakeFactory{}
	path := filepath.Join(t.TempDir(), "windows.json")
	r := app.NewRegistry(ff, path, nil)

	r.Open("comms")
	r.SetGeometry("comms", windowstate.Geometry{X: 5, Y: 6, W: 540, H: 720})
	r.Close("comms")

	saved, _ := windowstate.Load(path)
	if saved["comms"].W != 540 {
		t.Fatalf("expected persisted geometry, got %+v", saved)
	}
}

type capEmitter struct{ events []string }

func (c *capEmitter) Emit(name string, _ any) { c.events = append(c.events, name) }

func TestRegistry_ToggleOpensThenCloses(t *testing.T) {
	ff := &fakeFactory{}
	em := &capEmitter{}
	r := app.NewRegistry(ff, filepath.Join(t.TempDir(), "windows.json"), em)

	r.Toggle("comms")
	if got := r.OpenWindows(); len(got) != 1 || got[0] != "comms" {
		t.Fatalf("expected comms open after first toggle, got %v", got)
	}
	r.Toggle("comms")
	if got := r.OpenWindows(); len(got) != 0 {
		t.Fatalf("expected no open windows after second toggle, got %v", got)
	}
	// Each open/close should have broadcast a window:state event.
	if len(em.events) != 2 {
		t.Fatalf("expected 2 window:state broadcasts, got %d (%v)", len(em.events), em.events)
	}
}
