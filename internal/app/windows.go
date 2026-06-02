package app

import (
	"sort"
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// Registry tracks open windows and persists their geometry. When the set of
// open windows changes it broadcasts EventWindowState (a sorted []string of open
// window ids) so launcher buttons in every window can reflect open/closed state.
type Registry struct {
	factory WindowFactory
	path    string
	emitter events.Emitter // may be nil (e.g. in tests)

	mu   sync.Mutex
	open map[string]WindowHandle
	geom map[string]windowstate.Geometry
}

// NewRegistry builds a Registry that persists to path and broadcasts open-state
// changes via emitter (nil is allowed — no broadcast). Existing geometry is loaded.
func NewRegistry(factory WindowFactory, path string, emitter events.Emitter) *Registry {
	geom, _ := windowstate.Load(path)
	if geom == nil {
		geom = map[string]windowstate.Geometry{}
	}
	return &Registry{factory: factory, path: path, emitter: emitter, open: map[string]WindowHandle{}, geom: geom}
}

// Open creates the window (at persisted or default geometry) or focuses it if open.
func (r *Registry) Open(id string) {
	r.mu.Lock()
	if h, ok := r.open[id]; ok {
		r.mu.Unlock()
		h.Focus()
		return
	}
	g, ok := r.geom[id]
	if !ok {
		g = defaultGeometry(id)
	}
	r.open[id] = r.factory.Create(id, windowURL(id), g)
	r.mu.Unlock()
	r.broadcast()
}

// Close closes the window and persists its last geometry.
func (r *Registry) Close(id string) {
	r.mu.Lock()
	h, ok := r.open[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	r.geom[id] = h.Bounds()
	h.Close()
	delete(r.open, id)
	_ = windowstate.Save(r.path, r.geom)
	r.mu.Unlock()
	r.broadcast()
}

// Toggle opens the window if it is closed, or closes it if it is open.
func (r *Registry) Toggle(id string) {
	r.mu.Lock()
	_, open := r.open[id]
	r.mu.Unlock()
	if open {
		r.Close(id)
		return
	}
	r.Open(id)
}

// OpenWindows returns the sorted ids of currently-open windows.
func (r *Registry) OpenWindows() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.openIDsLocked()
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

func (r *Registry) openIDsLocked() []string {
	ids := make([]string, 0, len(r.open))
	for id := range r.open {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// broadcast emits the current open-window set (if an emitter is configured).
func (r *Registry) broadcast() {
	if r.emitter == nil {
		return
	}
	r.mu.Lock()
	ids := r.openIDsLocked()
	r.mu.Unlock()
	r.emitter.Emit(events.EventWindowState, ids)
}
