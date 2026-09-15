//go:build !darwin || !cgo

// This file backs PermissionChecker everywhere permission_darwin.go does not.
// Its tag is the literal De Morgan complement of that file's `darwin && cgo`,
// so the partition is exhaustive (every build matches one) and disjoint (no
// build matches both) by construction rather than by enumeration. It covers:
//
//   - windows: RegisterHotKey needs no grant.
//   - linux/openbsd with cgo: XGrabKey needs no grant. An unreachable DISPLAY
//     is a connection failure, not a permission one, and surfaces as a
//     registration error instead.
//   - darwin without cgo: no hotkey backend exists at all in that build
//     (registrar_nocgo.go claims it and fails every Register with
//     ErrBackendUnavailable), so an Accessibility grant would enable nothing.
//     Reporting "denied" there would point the user at System Settings for a
//     problem only a rebuild can fix.
//   - any other cgo-less build: same reasoning.
//
// Note this is a WIDER tag than registrar_x.go / registrar_nocgo.go's
// `windows || cgo` split, and deliberately so: those two partition on "is
// there a usable hotkey backend", this pair partitions on "is there an OS
// permission to ask for". Windows and X11 have a backend and no permission,
// so they land on the working registrar and on this no-op checker.
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
