//go:build !darwin

// This file backs PermissionChecker on every platform that genuinely has no
// OS permission to grant for global key listening:
//
//   - windows: WH_KEYBOARD_LL needs no grant.
//   - linux: XRecord needs no grant. An unreachable DISPLAY -- or a Wayland
//     session, which has no global key-listening API at all -- is a session
//     or connection failure, not a permission one, and surfaces as a
//     registration error instead (see session_linux.go).
//
// The tag is `!darwin`, NOT the old `!darwin || !cgo`. That older, wider tag
// also swept up cgo-less darwin, and answered PermissionNotApplicable for it
// -- "this platform has no such permission". On macOS that is false: the
// Accessibility grant applies and the (now cgo-free) gohook listener needs
// it. Only the PROBE needs cgo. That case belongs to
// permission_darwin_nocgo.go, which answers PermissionUnknown instead.
//
// The three darwin/cgo arms are exhaustive and disjoint; the map is in
// permission_darwin.go.

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
