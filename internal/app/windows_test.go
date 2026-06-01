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
	r := app.NewRegistry(ff, filepath.Join(t.TempDir(), "windows.json"))

	r.Open("comms")
	r.Open("comms") // second open should focus, not recreate
	if len(ff.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(ff.created))
	}
}

func TestRegistry_PersistsGeometryOnClose(t *testing.T) {
	ff := &fakeFactory{}
	path := filepath.Join(t.TempDir(), "windows.json")
	r := app.NewRegistry(ff, path)

	r.Open("comms")
	r.SetGeometry("comms", windowstate.Geometry{X: 5, Y: 6, W: 540, H: 720})
	r.Close("comms")

	saved, _ := windowstate.Load(path)
	if saved["comms"].W != 540 {
		t.Fatalf("expected persisted geometry, got %+v", saved)
	}
}
