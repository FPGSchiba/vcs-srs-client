//go:build !darwin || !cgo

// This file backs PermissionChecker everywhere permission_darwin.go does not.
// Its tag is the literal De Morgan complement of that file's `darwin && cgo`,
// so the partition is exhaustive (every build matches one) and disjoint (no
// build matches both) by construction rather than by enumeration. It covers:
//
//   - windows: WH_KEYBOARD_LL needs no grant.
//   - linux: XRecord needs no grant. An unreachable DISPLAY -- or a Wayland
//     session, which has no global key-listening API at all -- is a session
//     or connection failure, not a permission one, and surfaces as a
//     registration error instead (see session_linux.go).
//   - darwin without cgo: the Accessibility APIs in permission_darwin.go ARE
//     cgo, so this build cannot observe the grant state and must not guess.
//   - any other cgo-less build: same reasoning.
//
// That darwin arm is no longer "no hotkey backend exists in this build".
// Since the move to robotn/gohook's purego backends, internal/hotkeys
// compiles and functions with CGO_ENABLED=0 on every target; it is only the
// PERMISSION probe that still needs cgo. A real darwin build of this app
// always has cgo enabled anyway -- Wails v3 requires it -- so the arm is
// unreachable in practice and exists only so the package still compiles.
//
// Note this file's tag pair partitions on "is there an OS permission to ask
// for", which is a different question from "is there a usable hotkey
// backend". Windows and X11 have a backend and no permission, so they land on
// the real registrar and on this no-op checker.
package hotkeys

// notApplicablePermission implements PermissionChecker for every platform
// with no grant to request.
type notApplicablePermission struct{}

// NewPermissionChecker returns a checker that reports PermissionNotApplicable.
func NewPermissionChecker() PermissionChecker { return notApplicablePermission{} }

// Status always reports PermissionNotApplicable. Callers use this to skip the
// permission banner and the re-check poll entirely.
func (notApplicablePermission) Status() Permission { return PermissionNotApplicable }

// Request is a no-op reporting no prompt: there is nothing to prompt for.
func (notApplicablePermission) Request() bool { return false }

// OpenSettings reports ErrNoPermissionSettings. Unreachable through the UI,
// which only offers the affordance when Status() is PermissionDenied, but an
// explicit error beats silently pretending to have opened something.
func (notApplicablePermission) OpenSettings() error { return ErrNoPermissionSettings }
