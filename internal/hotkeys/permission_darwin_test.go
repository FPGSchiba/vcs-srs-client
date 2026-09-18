//go:build darwin && cgo

package hotkeys

import (
	"os"
	"strings"
	"testing"
)

// The macOS permission checker is the one seam a unit test cannot execute:
// AXIsProcessTrusted reads TCC state for the running process, a `go test`
// binary is never a trusted app, and no test can grant itself trust. Both
// this machine's Accessibility and Input Monitoring states read false for a
// test binary, so the two services cannot even be told apart at runtime here
// -- which is exactly how the original mistake survived review.
//
// The tests below therefore guard what CAN be pinned without the OS: the
// identity of the service we gate on, and the pane the UI sends the user to.
// Both are user-facing contracts, and both fail loudly if someone reaches for
// the Input Monitoring API again.

// TestDarwinPermissionGatesOnAccessibility is the guard for the CRITICAL bug
// this file once had.
//
// golang.design/x/hotkey's registerTap (hotkey_darwin.m:110-116) refuses
// outright unless AXIsProcessTrusted() -- i.e. Accessibility. An earlier
// revision gated on CGPreflightListenEventAccess (Input Monitoring) instead.
// The two are separate TCC services, and the failure mode was invisible
// exactly where it mattered: on a machine that already holds Accessibility
// both read true, so it passed locally, while a fresh user who granted only
// Input Monitoring got a UI reporting "granted" against a process where every
// Register still returned NULL.
//
// Status() cannot be exercised here, so this pins the API it is built on.
func TestDarwinPermissionGatesOnAccessibility(t *testing.T) {
	src := readSource(t, "permission_darwin.go")

	// The trust predicate must be the same one the registrar gates on.
	if !strings.Contains(src, "C.AXIsProcessTrusted()") {
		t.Error("Status() must gate on AXIsProcessTrusted -- the exact predicate " +
			"golang.design/x/hotkey's registerTap uses; anything else can report " +
			"granted while every Register still fails")
	}
	if !strings.Contains(src, "AXIsProcessTrustedWithOptions") {
		t.Error("Request() must prompt via AXIsProcessTrustedWithOptions")
	}
	if !strings.Contains(src, "-framework ApplicationServices") {
		t.Error("the AX trust API lives in ApplicationServices, not CoreGraphics")
	}

	// And the Input Monitoring API must not come back. Checked against the
	// CODE only: the doc comments deliberately discuss the old service to
	// explain why it is wrong, and must stay free to do so.
	for _, banned := range []string{
		"C.CGPreflightListenEventAccess",
		"C.CGRequestListenEventAccess",
	} {
		if strings.Contains(src, banned) {
			t.Errorf("%s gates Input Monitoring (kTCCServiceListenEvent), not the "+
				"Accessibility trust the CGEventTap actually requires", banned)
		}
	}
}

// TestAccessibilityPaneURL pins the deep link. A well-formed URL for the
// wrong pane is worse than no button at all: it sends a stuck user somewhere
// that cannot fix their problem, which is precisely what shipped before.
//
// Both halves were verified against the shipping OS (macOS 26.6.2, build
// 25G83): the pane id is Security.prefPane's CFBundleIdentifier, and
// Privacy_Accessibility is a literal in that pane's own binary, alongside
// Privacy_LocationServices and Privacy_SystemServices.
func TestAccessibilityPaneURL(t *testing.T) {
	const want = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	if accessibilityPaneURL != want {
		t.Errorf("accessibilityPaneURL = %q, want %q", accessibilityPaneURL, want)
	}
	if strings.Contains(accessibilityPaneURL, "ListenEvent") {
		t.Error("the deep link points at Input Monitoring, which cannot fix a missing " +
			"Accessibility grant")
	}
}

// TestDarwinPermissionStatusIsBinary: Status() must commit to granted or
// denied. PermissionUnknown exists only as the zero value of Permission, and
// leaking it from the darwin checker would make the frontend render the
// no-affordance branch on a machine that genuinely needs a grant.
func TestDarwinPermissionStatusIsBinary(t *testing.T) {
	got := darwinPermission{}.Status()
	if got != PermissionGranted && got != PermissionDenied {
		t.Errorf("Status() = %v, want granted or denied", got)
	}
}

// readSource loads a file from the package directory.
func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
