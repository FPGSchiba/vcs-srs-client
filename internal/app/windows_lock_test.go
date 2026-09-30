package app

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// lockProbeHandle fails the test if a window-system call is made while the
// registry's mutex is held. On Wails, Bounds/SetBounds/Close are InvokeSync calls
// that block on the main thread; under r.mu they risk a UI stall.
// TryLock cannot succeed while the calling goroutine (or any other) holds
// r.mu, so a call made under the lock is observed.
type lockProbeHandle struct {
	reg *Registry
	cb  func()

	mu    sync.Mutex
	calls int
	bad   []string
}

func (h *lockProbeHandle) probe(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	if h.reg.mu.TryLock() {
		h.reg.mu.Unlock()
		return
	}
	h.bad = append(h.bad, name)
}

func (h *lockProbeHandle) Focus() {}
func (h *lockProbeHandle) Close() { h.probe("Close") }
func (h *lockProbeHandle) Bounds() windowstate.Geometry {
	h.probe("Bounds")
	return windowstate.Geometry{W: 600, H: 700}
}
func (h *lockProbeHandle) SetBounds(windowstate.Geometry) { h.probe("SetBounds") }
func (h *lockProbeHandle) OnBoundsChanged(fn func())                  { h.cb = fn }

type lockProbeFactory struct{ h *lockProbeHandle }

func (f *lockProbeFactory) Create(string, string, windowstate.Geometry) WindowHandle { return f.h }

func TestRegistryDoesNotCallWindowSystemUnderItsLock(t *testing.T) {
	h := &lockProbeHandle{}
	reg := NewRegistry(&lockProbeFactory{h: h}, filepath.Join(t.TempDir(), "windows.json"), nil)
	reg.debounce = 5 * time.Millisecond
	h.reg = reg
	reg.Open("comms")

	// settleBounds path: native event -> debounce -> Bounds.
	h.cb()
	waitFor(t, 3*time.Second, "settle to read Bounds", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.calls >= 1
	})

	// SetGeometry path: SetBounds on an open window.
	reg.SetGeometry("comms", windowstate.Geometry{W: 640, H: 480})

	// Close path: Bounds and Close.
	reg.Close("comms")

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.calls != 4 {
		t.Fatalf("expected Bounds (settle), SetBounds (SetGeometry), Bounds+Close (close) = 4 calls, got %d", h.calls)
	}
	if len(h.bad) > 0 {
		t.Fatalf("window-system calls made while holding Registry.mu: %v", h.bad)
	}
}
