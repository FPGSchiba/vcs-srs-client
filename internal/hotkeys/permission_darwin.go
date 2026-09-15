//go:build darwin && cgo

// This file is the macOS half of the PermissionChecker seam. Its build tag is
// the exact complement of permission_other.go's (!darwin || !cgo), so the two
// together cover every build with no overlap and no gap.
//
// `darwin && cgo` rather than plain `darwin` for two reasons. The obvious one
// is that the CoreGraphics calls below ARE cgo. The substantive one is that
// a darwin build without cgo has no working hotkey backend at all --
// registrar_nocgo.go (!windows && !cgo) claims that build and fails every
// registration with ErrBackendUnavailable -- so there is nothing for an
// Input Monitoring grant to enable, and reporting "denied" there would send
// the user to System Settings to fix a problem that granting cannot fix.
// permission_other.go's PermissionNotApplicable is the honest answer for it.
package hotkeys

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

import (
	"fmt"
	"os/exec"
)

// inputMonitoringPaneURL deep-links System Settings to Privacy & Security ->
// Input Monitoring.
//
// VERIFIED against the shipping OS rather than taken on trust (macOS 26.6.2,
// build 25G83): /System/Library/PreferencePanes/Security.prefPane's
// Info.plist gives CFBundleIdentifier "com.apple.preference.security", and
// its Contents/Resources/PrivacyTCCServices.plist maps the TCC service
// kTCCServiceListenEvent -- the service a CGEventTap needs -- to
// revealElementKeyName "Privacy_ListenEvent". The pane identifier and the
// anchor in the URL below are those two values.
const inputMonitoringPaneURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent"

// darwinPermission implements PermissionChecker over CoreGraphics' TCC
// helpers for kTCCServiceListenEvent (Input Monitoring).
type darwinPermission struct{}

// NewPermissionChecker returns the macOS Input Monitoring checker.
func NewPermissionChecker() PermissionChecker { return darwinPermission{} }

// Status reports whether this process already holds Input Monitoring.
//
// CGPreflightListenEventAccess is the single source of truth for that, and is
// the only way this codebase ever concludes "granted". It is a cheap cached
// TCC lookup, so calling it per poll tick and per window focus is fine.
//
// It never returns PermissionUnknown: the API is a plain bool with no
// third state, and inventing one would just make callers handle a case the
// OS cannot produce. PermissionUnknown exists for the zero value of
// Permission, so a struct that has not been populated does not read as a
// confident "granted".
func (darwinPermission) Status() Permission {
	if bool(C.CGPreflightListenEventAccess()) {
		return PermissionGranted
	}
	return PermissionDenied
}

// Request asks TCC to show the Input Monitoring prompt, once per app
// installation. See PermissionChecker.Request: the returned bool is NOT the
// user's answer.
//
// CGRequestListenEventAccess returns immediately while TCC handles the
// prompt out of process, and reports true only when access is already
// present. A false result therefore means "not granted at the moment of the
// call" -- which covers both "the prompt is now on screen, unanswered" and
// "the prompt was spent long ago and refused". The caller cannot tell those
// apart from this value and must not try; it resolves either way through
// Status(), via the bounded poll the request arms and via the window-focus
// re-check.
func (darwinPermission) Request() bool {
	return bool(C.CGRequestListenEventAccess())
}

// OpenSettings opens System Settings at Privacy & Security -> Input
// Monitoring. Uses `open`, which is how a URL scheme is dispatched to its
// handler from a non-AppKit context.
func (darwinPermission) OpenSettings() error {
	if err := exec.Command("open", inputMonitoringPaneURL).Run(); err != nil {
		return fmt.Errorf("hotkeys: open Input Monitoring settings: %w", err)
	}
	return nil
}
