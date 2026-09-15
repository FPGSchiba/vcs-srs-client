//go:build !windows && !cgo

// This file backs internal/hotkeys on builds where golang.design/x/hotkey's
// real backend cannot be used at all: Linux/OpenBSD without cgo enabled. That
// backend is CGO+libX11 based (hotkey_x11.go upstream), and the library's own
// no-cgo fallback (hotkey_nocgo.go) only declares the Key/Modifier types --
// it exports none of the named constants registrar_x.go and the keymap files
// need, and its register()/unregister() methods panic if ever called.
//
// This is a real build-time boundary of the chosen library, not a fake or a
// weakened feature: a genuine deployment build for Linux must have cgo
// enabled and libX11 available (a hard requirement for the OS-level global
// hotkey feature to work there at all), which is the normal case for a
// native Linux build or a properly configured cross toolchain/container.
// What this file guards against is a build that lacks that toolchain (for
// example, `GOOS=linux go build` from a macOS host with no Linux C
// cross-compiler installed) failing to even compile, and it does so by
// reporting a clear runtime error the first time a hotkey is registered
// rather than crashing or silently doing nothing.
package hotkeys

import (
	"errors"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// ErrBackendUnavailable is returned by Register when this binary was built
// without cgo on a platform whose only golang.design/x/hotkey backend
// requires it (Linux/OpenBSD). Rebuild with CGO_ENABLED=1 and the platform's
// hotkey dependency (libX11 on Linux) available.
var ErrBackendUnavailable = errors.New("hotkeys: OS backend unavailable in this build (requires CGO_ENABLED=1)")

// SupportsRelease reports false: with no usable backend, neither press nor
// release can be delivered.
func SupportsRelease() bool { return false }

type nocgoRegistrar struct{}

// NewOSRegistrar returns a Registrar that always fails registration with
// ErrBackendUnavailable. It exists only so the package compiles on hosts
// that lack a working golang.design/x/hotkey backend; it registers nothing.
func NewOSRegistrar() Registrar { return nocgoRegistrar{} }

func (nocgoRegistrar) Register(actionID string, _ chord.Chord, _ bool, _ Handler) error {
	return ErrBackendUnavailable
}

func (nocgoRegistrar) UnregisterAll() {}
