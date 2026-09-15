//go:build windows || cgo

// Package hotkeys: this file is the real OS seam, implementing Registrar over
// golang.design/x/hotkey. It is compiled wherever that library's platform
// backend is actually usable (Windows always; macOS and Linux/X11 whenever
// cgo is enabled, which it is on any normal build of those targets). The
// per-platform key/modifier tables live in keymap_darwin.go, keymap_windows.go,
// and keymap_linux.go, since the library exports differently-named constants
// per platform. See registrar_nocgo.go for the fallback used when neither
// condition holds (e.g. a Linux build with CGO_ENABLED=0, which cannot use
// this library's X11 backend at all).
package hotkeys

import (
	"fmt"
	"sync"

	"golang.design/x/hotkey"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// SupportsRelease reports whether the backend can detect key release, which
// hold-to-talk PTT bindings require. golang.design/x/hotkey v0.6.1 exposes a
// Keyup() channel, backed by a real release signal on every platform this
// build targets: a CGEventTap key-up on macOS, GetAsyncKeyState polling on
// Windows, and an XRecord key-up callback on Linux/X11 (confirmed by reading
// the library source: hotkey_darwin.go, hotkey_windows.go, hotkey_x11.go).
// See task-5-report.md (R11) for the full evidence trail.
func SupportsRelease() bool { return true }

// osRegistrar implements Registrar using golang.design/x/hotkey. It owns a
// generation channel ("done") that is closed and replaced on every
// UnregisterAll, which is how the per-hotkey goroutines started by Register
// know to stop.
type osRegistrar struct {
	mu   sync.Mutex
	done chan struct{}
	live []*hotkey.Hotkey
}

// NewOSRegistrar returns a Registrar backed by the real OS hotkey APIs.
func NewOSRegistrar() Registrar {
	return &osRegistrar{done: make(chan struct{})}
}

// Register maps the chord to the platform's key/modifier constants, asks the
// OS to register it, and starts one goroutine that delivers Pressed (and,
// when hold is true, Released) calls to h until UnregisterAll is called.
func (r *osRegistrar) Register(actionID string, c chord.Chord, hold bool, h Handler) error {
	mods, key, err := mapChord(c)
	if err != nil {
		return fmt.Errorf("hotkeys: %s: %w", actionID, err)
	}

	hk := hotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		return fmt.Errorf("hotkeys: register %s (%s): %w", actionID, c, err)
	}

	r.mu.Lock()
	done := r.done
	r.live = append(r.live, hk)
	r.mu.Unlock()

	go watch(done, hk, hold, actionID, h)

	return nil
}

// watch delivers hotkey events to h until done is closed. up is left nil when
// hold is false, so the release case in the select below is never ready and
// is effectively dead code for a press-only binding -- a nil channel blocks
// forever and is never chosen by select.
func watch(done <-chan struct{}, hk *hotkey.Hotkey, hold bool, actionID string, h Handler) {
	down := hk.Keydown()
	var up <-chan hotkey.Event
	if hold {
		up = hk.Keyup()
	}
	for {
		select {
		case <-done:
			return
		case _, ok := <-down:
			if !ok {
				return
			}
			h.Pressed(actionID)
		case _, ok := <-up:
			if !ok {
				return
			}
			h.Released(actionID)
		}
	}
}

// UnregisterAll stops every goroutine started by Register and unregisters
// every hotkey. Safe to call when nothing is registered.
func (r *osRegistrar) UnregisterAll() {
	r.mu.Lock()
	done := r.done
	live := r.live
	r.live = nil
	r.done = make(chan struct{})
	r.mu.Unlock()

	close(done)
	for _, hk := range live {
		_ = hk.Unregister()
	}
}
