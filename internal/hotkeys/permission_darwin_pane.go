//go:build darwin

package hotkeys

import (
	"fmt"
	"os/exec"
)

// accessibilityPaneURL deep-links System Settings to Privacy & Security ->
// Accessibility.
//
// ACCESSIBILITY, not Input Monitoring. The OS key listener gates on
// AXIsProcessTrusted, so this pane is the one that can actually fix a denied
// hotkey. That was true of golang.design/x/hotkey's registerTap
// (hotkey_darwin.m:110-116, "a keyboard event tap requires Accessibility
// trust ... CGEventTapCreate can otherwise return a non-NULL but inert tap"),
// and it is equally true of what replaced it -- robotn/gohook's darwin purego
// backend refuses to create its tap unless axIsProcessTrusted() (darwin.go:304)
// and emits HookDisabled instead:
//
//	// darwin.go:303-310
//	// A session tap only delivers events with the Accessibility privilege.
//	if !axIsProcessTrusted() {
//		send(Event{Kind: HookDisabled})
//		return
//	}
//
// The predicate surviving that migration unchanged is exactly why the whole
// permission flow survived it unchanged. An earlier revision of the
// permission checker gated on Input Monitoring
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

// openAccessibilityPane opens System Settings at the pane above. Uses `open`,
// which is how a URL scheme is dispatched to its handler from a non-AppKit
// context.
//
// Shared by both darwin PermissionCheckers. The cgo-less one (see
// permission_darwin_nocgo.go) cannot PROBE the grant, but the pane is still
// reachable and the grant the user makes there is still real -- the hotkey
// listener itself needs no cgo -- so it must not be denied the deep link.
func openAccessibilityPane() error {
	if err := openPane(accessibilityPaneURL); err != nil {
		return fmt.Errorf("hotkeys: open Accessibility settings: %w", err)
	}
	return nil
}

// openPane is the one-line indirection the tests substitute. Without it the
// only way to assert that a checker actually ATTEMPTS the deep link is to run
// it, which would pop System Settings open on whoever ran `go test`; with it,
// a test can prove the URL that would be dispatched. Never reassigned outside
// a test.
var openPane = func(url string) error { return exec.Command("open", url).Run() }
