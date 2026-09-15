//go:build darwin && cgo

// This file is the macOS half of the PermissionChecker seam. Its build tag is
// the exact complement of permission_other.go's (!darwin || !cgo), so the two
// together cover every build with no overlap and no gap.
//
// `darwin && cgo` rather than plain `darwin` for two reasons. The obvious one
// is that the ApplicationServices calls below ARE cgo. The substantive one is
// that a darwin build without cgo has no working hotkey backend at all --
// registrar_nocgo.go (!windows && !cgo) claims that build and fails every
// registration with ErrBackendUnavailable -- so there is nothing for an
// Accessibility grant to enable, and reporting "denied" there would send the
// user to System Settings to fix a problem that granting cannot fix.
// permission_other.go's PermissionNotApplicable is the honest answer for it.
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

import (
	"fmt"
	"os/exec"
)

// accessibilityPaneURL deep-links System Settings to Privacy & Security ->
// Accessibility.
//
// ACCESSIBILITY, not Input Monitoring. golang.design/x/hotkey's darwin
// backend gates every registration on AXIsProcessTrusted():
//
//	// hotkey_darwin.m:110-116
//	void* registerTap(uintptr_t handle, int isMedia, int code, uint64_t flags) {
//		// A keyboard event tap requires Accessibility trust. Check explicitly:
//		// CGEventTapCreate can otherwise return a non-NULL but inert tap when
//		// untrusted, which would look like success but never fire.
//		if (!AXIsProcessTrusted()) {
//			return NULL;
//		}
//
// and the library's own package doc (hotkey.go:23-25) says to grant it in
// "System Settings -> Privacy & Security -> Accessibility". An earlier
// revision of this file gated on Input Monitoring
// (CGPreflightListenEventAccess / kTCCServiceListenEvent) instead. That was
// silently wrong in the worst possible way: on a developer machine that
// already holds Accessibility the two agree, so it looked fine; on a fresh
// user's machine, granting Input Monitoring flipped our reported state to
// "granted" while every Register still returned NULL, stranding the UI on
// "permission granted -- restart VCS" forever with an OPEN SETTINGS button
// aimed at a pane that could not fix it. See the report for the full trail.
//
// VERIFIED against the shipping OS rather than taken on trust (macOS 26.6.2,
// build 25G83). The pane identifier comes from
// /System/Library/PreferencePanes/Security.prefPane's Info.plist
// (CFBundleIdentifier "com.apple.preference.security"). The anchor is a
// literal in that pane's own binary (Contents/MacOS/Security), alongside
// Privacy_LocationServices and Privacy_SystemServices -- the anchors for the
// panes that are NOT generic TCC service tables. Accessibility has its own
// service type there (PrivacyAccessibilityServicesType, distinct from
// PrivacyTCCServicesType), which is exactly why -- unlike
// kTCCServiceListenEvent -- it does not appear in
// Contents/Resources/PrivacyTCCServices.plist.
const accessibilityPaneURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"

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
// Uses `open`, which is how a URL scheme is dispatched to its handler from a
// non-AppKit context.
func (darwinPermission) OpenSettings() error {
	if err := exec.Command("open", accessibilityPaneURL).Run(); err != nil {
		return fmt.Errorf("hotkeys: open Accessibility settings: %w", err)
	}
	return nil
}
