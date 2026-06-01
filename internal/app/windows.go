package app

import (
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// Registry tracks open windows and persists their geometry.
type Registry struct {
	factory WindowFactory
	path    string

	mu   sync.Mutex
	open map[string]WindowHandle
	geom map[string]windowstate.Geometry
}

// NewRegistry builds a Registry that persists to path. Existing geometry is loaded.
func NewRegistry(factory WindowFactory, path string) *Registry {
	geom, _ := windowstate.Load(path)
	if geom == nil {
		geom = map[string]windowstate.Geometry{}
	}
	return &Registry{factory: factory, path: path, open: map[string]WindowHandle{}, geom: geom}
}

// Open creates the window (at persisted or default geometry) or focuses it if open.
func (r *Registry) Open(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.open[id]; ok {
		h.Focus()
		return
	}
	g, ok := r.geom[id]
	if !ok {
		g = defaultGeometry(id)
	}
	r.open[id] = r.factory.Create(id, windowURL(id), g)
}

// Close closes the window and persists its last geometry.
func (r *Registry) Close(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.open[id]
	if !ok {
		return
	}
	r.geom[id] = h.Bounds()
	h.Close()
	delete(r.open, id)
	_ = windowstate.Save(r.path, r.geom)
}

// Geometry returns the last known geometry for id.
func (r *Registry) Geometry(id string) windowstate.Geometry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.geom[id]; ok {
		return g
	}
	return defaultGeometry(id)
}

// SetGeometry records geometry (called from debounced move/resize) and persists.
func (r *Registry) SetGeometry(id string, g windowstate.Geometry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.geom[id] = g
	if h, ok := r.open[id]; ok {
		h.SetBounds(g)
	}
	_ = windowstate.Save(r.path, r.geom)
}
