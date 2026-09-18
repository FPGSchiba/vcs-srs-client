//go:build darwin && cgo

// This file is the macOS PermissionChecker. Nothing about the PREDICATE below
// has changed since it was written: Accessibility, AXIsProcessTrusted, for the
// reasons documented at accessibilityPaneURL. Only the justification for the
// build tag is new, and it is worth reading before touching either.
//
// `darwin && cgo` rather than plain `darwin`, for exactly one reason now: the
// ApplicationServices calls below ARE cgo. That is the whole argument.
//
// It used to rest on a second, stronger claim -- that a cgo-less darwin build
// had no hotkey backend at all, so an Accessibility grant would enable
// nothing. THAT CLAIM IS DEAD. It described registrar_nocgo.go, which existed
// because golang.design/x/hotkey had no usable no-cgo backend. Since the move
// to robotn/gohook's purego backends the OS listener is genuinely cgo-free on
// every target, and a cgo-less darwin build WOULD have a working listener --
// one that still needs Accessibility, and that this file could no longer
// report on.
//
// Left unhandled, that is the same shape as the Input Monitoring bug below:
// a reported permission state that does not match what the backend actually
// requires. It is closed, not merely documented -- permission_darwin_nocgo.go
// claims `darwin && !cgo` and answers PermissionUnknown ("we cannot tell"),
// rather than letting permission_other.go answer PermissionNotApplicable
// ("this platform has no such permission"), which on macOS is simply false.
//
// The three-way split is exhaustive and disjoint by construction:
//
//	darwin && cgo   -> this file          (the real Accessibility probe)
//	darwin && !cgo  -> permission_darwin_nocgo.go (PermissionUnknown)
//	!darwin         -> permission_other.go        (PermissionNotApplicable)
//
// In practice only the first arm ever ships: Wails v3 requires cgo on macOS,
// so a CGO_ENABLED=0 darwin build of this application cannot be produced at
// all. The second arm exists so that `GOOS=darwin CGO_ENABLED=0` analysis
// builds -- and anyone who later removes the Wails constraint -- get an
// honest answer instead of a confident wrong one.
package hotkeys

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// requestAXTrust calls AXIsProcessTrustedWithOptions with the prompt option
// set, which is what actually surfaces the "wants to control this computer
// using accessibility features" sheet.
//
// Written in plain C over CoreFoundation rather than with an Objective-C
// dictionary literal and a __bridge cast: the option dictionary is the only
// thing needed from ObjC, and building it with CFDictionaryCreate keeps this
// file compilable without -x objective-c and sidesteps ARC bridging rules
// entirely. The dictionary is released before returning; the key and value
// are process-global constants and are not owned by us.
static Boolean requestAXTrust(void) {
	const void *keys[]   = { (const void *)kAXTrustedCheckOptionPrompt };
	const void *values[] = { (const void *)kCFBooleanTrue };
	CFDictionaryRef options = CFDictionaryCreate(
		kCFAllocatorDefault, keys, values, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	Boolean trusted = AXIsProcessTrustedWithOptions(options);
	CFRelease(options);
	return trusted;
}
*/
import "C"

// darwinPermission implements PermissionChecker over the Accessibility trust
// API (TCC's kTCCServiceAccessibility).
type darwinPermission struct{}

// NewPermissionChecker returns the macOS Accessibility checker.
func NewPermissionChecker() PermissionChecker { return darwinPermission{} }

// Status reports whether this process is trusted for Accessibility.
//
// AXIsProcessTrusted is the single source of truth for that, and is the only
// way this codebase ever concludes "granted" -- deliberately the very same
// predicate golang.design/x/hotkey's registerTap gates on, so our reported
// state cannot disagree with whether registration will actually work. It is
// a cheap cached TCC lookup, so calling it per poll tick and per window
// focus is fine.
//
// It never returns PermissionUnknown: the API is a plain Boolean with no
// third state, and inventing one would just make callers handle a case the
// OS cannot produce. PermissionUnknown exists for the zero value of
// Permission, so a struct that has not been populated does not read as a
// confident "granted".
//
// Note the `!= 0`: AXIsProcessTrusted returns CoreFoundation's Boolean (a
// uint8 from MacTypes.h), not C99 bool, so cgo surfaces it as C.uchar and it
// cannot be converted with bool(...).
func (darwinPermission) Status() Permission {
	if C.AXIsProcessTrusted() != 0 {
		return PermissionGranted
	}
	return PermissionDenied
}

// Request asks the system to show the Accessibility trust prompt. See
// PermissionChecker.Request: the returned bool is NOT the user's answer.
//
// AXIsProcessTrustedWithOptions returns the CURRENT trust state and returns
// immediately, while the sheet it raises is answered out of process and
// resolved by TCC afterwards. A false result therefore means "not trusted at
// the moment of the call", which covers both "the sheet is now on screen,
// unanswered" and "the user already refused". The caller cannot tell those
// apart from this value and must not try; it resolves either way through
// Status(), via the bounded poll the request arms and via the window-focus
// re-check.
func (darwinPermission) Request() bool {
	return C.requestAXTrust() != 0
}

// OpenSettings opens System Settings at Privacy & Security -> Accessibility.
// The URL and the `open` call live in permission_darwin_pane.go so the
// cgo-less checker can reach the same pane; see that file.
func (darwinPermission) OpenSettings() error { return openAccessibilityPane() }
