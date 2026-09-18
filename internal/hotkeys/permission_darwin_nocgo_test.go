//go:build darwin && !cgo

package hotkeys

import "testing"

// Run with: CGO_ENABLED=0 go test -tags purego ./internal/hotkeys/
//
// This arm is not reachable in a shipped build (Wails v3 requires cgo on
// macOS), but it IS reachable by analysis and by `go test`, which is what
// makes it worth pinning rather than merely documenting.

// TestNoCgoDarwinDoesNotClaimNotApplicable is the whole point of
// permission_darwin_nocgo.go.
//
// Before it existed, permission_other.go's `!darwin || !cgo` tag caught this
// build and answered PermissionNotApplicable -- "this platform has no such
// permission". macOS does have one, and since the move to gohook's purego
// backends the listener in this very build needs it. Answering NotApplicable
// would skip the banner and the re-check poll entirely and let hotkeys fail
// silently: the same "reported state does not match reality" shape as the
// Input Monitoring bug permission_darwin.go documents.
func TestNoCgoDarwinDoesNotClaimNotApplicable(t *testing.T) {
	got := NewPermissionChecker().Status()

	if got == PermissionNotApplicable {
		t.Fatal("a cgo-less darwin build must not report PermissionNotApplicable: " +
			"macOS DOES require Accessibility and the purego hotkey backend in this " +
			"build needs it; only the probe is unavailable")
	}
	if got == PermissionGranted {
		t.Fatal("a build that cannot call AXIsProcessTrusted must never claim a grant")
	}
	if got != PermissionUnknown {
		t.Fatalf("Status() = %v, want PermissionUnknown", got)
	}
}

// TestNoCgoDarwinWireFormIsUnknown pins the value the frontend actually sees.
// The store models "unknown" as show-nothing / claim-nothing, which is the
// behaviour this arm needs; "not_applicable" would suppress the permission UI
// as a deliberate decision rather than as an absence of information.
func TestNoCgoDarwinWireFormIsUnknown(t *testing.T) {
	if got := NewPermissionChecker().Status().String(); got != "unknown" {
		t.Errorf("wire form = %q, want %q", got, "unknown")
	}
}

// TestNoCgoDarwinRequestNeverReportsAPrompt: there is no way to raise the
// Accessibility sheet without AXIsProcessTrustedWithOptions, which is cgo.
// The documented invariant still holds either way -- Request()'s result is
// never a grant signal -- but claiming a prompt that cannot have appeared
// would send the UI down the "we asked, now wait" path forever.
func TestNoCgoDarwinRequestNeverReportsAPrompt(t *testing.T) {
	if NewPermissionChecker().Request() {
		t.Error("Request() reported a prompt, but this build cannot raise one")
	}
}

// TestNoCgoDarwinStillOffersTheSettingsPane. The probe needs cgo; the pane
// does not, and the grant a user makes there is real -- the gohook listener
// is cgo-free. Refusing the deep link here would strand the user with a
// permission they cannot reach from the app.
//
// The dispatch itself is stubbed rather than run -- executing it would pop
// System Settings open on whoever ran `go test` -- but the assertion is on the
// real OpenSettings method, so taking permission_other.go's "there is no pane
// on this platform" exit fails here.
func TestNoCgoDarwinStillOffersTheSettingsPane(t *testing.T) {
	prev := openPane
	t.Cleanup(func() { openPane = prev })

	var dispatched string
	openPane = func(url string) error {
		dispatched = url
		return nil
	}

	if err := NewPermissionChecker().OpenSettings(); err != nil {
		t.Fatalf("OpenSettings: %v -- the probe needs cgo, the PANE does not, and "+
			"the grant a user makes there is honoured by the cgo-free listener", err)
	}
	if dispatched != accessibilityPaneURL {
		t.Errorf("opened %q, want the Accessibility pane %q", dispatched, accessibilityPaneURL)
	}
}
