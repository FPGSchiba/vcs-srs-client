//go:build darwin && !cgo

package hotkeys

// This file closes a gap the move to robotn/gohook opened up.
//
// permission_other.go used to claim `!darwin || !cgo`, which swept cgo-less
// darwin into PermissionNotApplicable -- "this platform has no such
// permission". That was defensible only while a cgo-less darwin build had no
// hotkey backend at all (registrar_nocgo.go, now deleted). With gohook's
// purego backends the listener works fine without cgo and still needs the
// macOS Accessibility grant, so NotApplicable became a lie: the permission
// very much applies, we simply cannot observe it, because
// AXIsProcessTrusted is a cgo call and this build has no cgo.
//
// A reported permission state that disagrees with what the backend actually
// requires is precisely the failure this package has been bitten by once
// already (see permission_darwin.go on Input Monitoring). PermissionUnknown
// is the honest answer, and it is already modelled end to end: permission.go
// documents it as "the grant state could not be determined", and the
// frontend store treats "unknown" as show-nothing / claim-nothing rather
// than as an optimistic guess.
//
// Wails v3 requires cgo on macOS, so no shipped build reaches this file. It
// exists so `GOOS=darwin CGO_ENABLED=0` analysis builds -- and anyone who
// later lifts that constraint -- get "we don't know" instead of a confident
// wrong answer.

// unknownDarwinPermission implements PermissionChecker for a darwin build
// that cannot call into ApplicationServices.
type unknownDarwinPermission struct{}

// NewPermissionChecker returns a checker that cannot determine the grant
// state, and says so.
func NewPermissionChecker() PermissionChecker { return unknownDarwinPermission{} }

// Status reports PermissionUnknown. macOS DOES have a hotkey permission and
// this build DOES need it; there is simply no way to read it from here.
func (unknownDarwinPermission) Status() Permission { return PermissionUnknown }

// Request reports no prompt. AXIsProcessTrustedWithOptions is the only thing
// that raises the Accessibility sheet and it is unreachable without cgo.
// Returning false keeps the documented invariant intact: the result is not a
// grant signal, and a grant is only ever detected through Status().
func (unknownDarwinPermission) Request() bool { return false }

// OpenSettings deep-links to the same Accessibility pane the cgo build uses.
// The pane is reachable even though the probe is not, and sending the user
// there is strictly better than refusing: the grant it makes is real and will
// be honoured by the hotkey listener, which does not need cgo.
func (unknownDarwinPermission) OpenSettings() error { return openAccessibilityPane() }
