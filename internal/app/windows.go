package app

import (
	"sort"
	"sync"
	"time"

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

	// debounce coalesces the continuous resize/move events of a drag into one
	// geometry record; zero means defaultBoundsDebounce. timers is per window
	// id. observer, if set, is told each settled geometry (outside r.mu).
	debounce time.Duration
	timers   map[string]*time.Timer
	observer func(id string, g windowstate.Geometry)
}

const defaultBoundsDebounce = 300 * time.Millisecond

// SetGeometryObserver registers fn to be called with a window's geometry
// after a user move/resize settles. It is called without r.mu held.
func (r *Registry) SetGeometryObserver(fn func(id string, g windowstate.Geometry)) {
	r.mu.Lock()
	r.observer = fn
	r.mu.Unlock()
}

// scheduleBounds (re)arms the debounce timer for id.
func (r *Registry) scheduleBounds(id string, h WindowHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.debounce
	if d <= 0 {
		d = defaultBoundsDebounce
	}
	if t := r.timers[id]; t != nil {
		t.Stop()
	}
	r.timers[id] = time.AfterFunc(d, func() { r.settleBounds(id, h) })
}

// settleBounds records the window's current bounds and persists windows.json
// ONCE per drag. It deliberately does not go through SetGeometry, which
// pushes the bounds back into the window and would echo another resize event.
//
// h.Bounds() is read BEFORE r.mu is taken. On Wails it is an InvokeSync that
// blocks until the main thread services it, while every resize event runs
// scheduleBounds (which needs r.mu) on that same main thread through a
// 5-slot event queue: holding r.mu across the call lets a continuing drag
// fill the queue, stall the main thread, and deadlock the UI. The open[id]==h
// re-check after locking keeps a handle closed or replaced in between ignored.
func (r *Registry) settleBounds(id string, h WindowHandle) {
	g := h.Bounds()
	r.mu.Lock()
	if r.open[id] != h { // closed or replaced since the event
		r.mu.Unlock()
		return
	}
	r.geom[id] = g
	_ = windowstate.Save(r.path, r.geom)
	obs := r.observer
	r.mu.Unlock()
	if obs != nil {
		obs(id, g)
	}
}

// NewRegistry builds a Registry that persists to path and broadcasts open-state
// changes via emitter (nil is allowed — no broadcast). Existing geometry is loaded.
func NewRegistry(factory WindowFactory, path string, emitter events.Emitter) *Registry {
	geom, _ := windowstate.Load(path)
	if geom == nil {
		geom = map[string]windowstate.Geometry{}
	}
	return &Registry{factory: factory, path: path, emitter: emitter, open: map[string]WindowHandle{}, geom: geom, timers: map[string]*time.Timer{}}
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
	h := r.factory.Create(id, windowURL(id), g)
	r.open[id] = h
	if bn, ok := h.(boundsNotifier); ok {
		bn.OnBoundsChanged(func() { r.scheduleBounds(id, h) })
	}
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
	if t := r.timers[id]; t != nil {
		t.Stop()
		delete(r.timers, id)
	}
	// Unregister first, so a settle that fires meanwhile sees open[id] != h
	// and leaves geometry alone. Bounds and Close are window-system calls
	// that can block on the main thread (see settleBounds), so they run with
	// r.mu released.
	delete(r.open, id)
	r.mu.Unlock()
	g := h.Bounds()
	h.Close()
	r.mu.Lock()
	r.geom[id] = g
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
