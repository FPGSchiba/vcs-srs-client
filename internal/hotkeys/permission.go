package hotkeys

import "errors"

// ErrNoPermissionSettings is returned by a PermissionChecker.OpenSettings on
// platforms that have no OS permission to grant (Windows, Linux/X11) and
// therefore no settings pane to deep-link into. Callers should never reach
// it: the UI only offers the "open settings" affordance when Status() is
// PermissionDenied, which those platforms never report.
var ErrNoPermissionSettings = errors.New("hotkeys: no OS permission settings pane on this platform")

// Permission is the OS-level grant state for global hotkey capture.
//
// Only macOS has one: the OS key listener installs a CGEventTap, and
// github.com/robotn/gohook's darwin backend refuses to create it unless the
// process is trusted for ACCESSIBILITY -- axIsProcessTrusted() at
// darwin.go:304, i.e. TCC's kTCCServiceAccessibility read through
// AXIsProcessTrusted. (An untrusted CGEventTapCreate can otherwise hand back
// a non-NULL but permanently inert tap.) Windows' WH_KEYBOARD_LL and
// Linux/X11's XRecord need no equivalent grant, so those platforms report
// PermissionNotApplicable rather than pretending to a state they cannot
// observe.
//
// This predicate did not change when the OS layer moved off
// golang.design/x/hotkey: both libraries gate on exactly AXIsProcessTrusted,
// which is why the whole permission flow below is untouched by that
// migration. See permission_darwin.go for what went wrong the one time this
// gated on something else.
//
// Accessibility, NOT Input Monitoring. The two are separate TCC services and
// granting the wrong one changes nothing about whether a hotkey fires; see
// permission_darwin.go for the evidence and for what went wrong when this
// gated on Input Monitoring.
type Permission int

const (
	// PermissionUnknown means the grant state could not be determined.
	PermissionUnknown Permission = iota
	// PermissionGranted means the OS will deliver global key events to us.
	PermissionGranted
	// PermissionDenied means the OS is withholding them until the user grants
	// access. NOT the same as "the user pressed Deny": TCC reports
	// not-yet-asked and explicitly-refused identically, which is why
	// Request() exists and why its result is not a grant signal.
	PermissionDenied
	// PermissionNotApplicable means this platform has no such permission.
	PermissionNotApplicable
)

// String renders the wire form carried in HotkeyStateDTO.Permission, so the
// frontend can branch on a state rather than pattern-match an error string.
func (p Permission) String() string {
	switch p {
	case PermissionGranted:
		return "granted"
	case PermissionDenied:
		return "denied"
	case PermissionNotApplicable:
		return "not_applicable"
	default:
		return "unknown"
	}
}

// PermissionChecker is the OS seam for the grant state above. It lives behind
// an interface for exactly one reason: the darwin implementation is cgo
// calling into CoreGraphics and TCC, which no test can drive. Everything
// above it -- the request flow, the bounded re-check, the focus re-check, the
// emitted state -- is tested against a fake.
type PermissionChecker interface {
	// Status reports the current grant state. On macOS this is the ONLY
	// authority on whether access has been granted; see Request.
	Status() Permission

	// Request fires the OS permission prompt if it has not already been
	// spent, and reports whether the OS indicated a prompt-worthy state.
	//
	// It deliberately does NOT return the user's answer, and the return value
	// must never be treated as one. macOS's AXIsProcessTrustedWithOptions
	// returns the CURRENT trust state and returns immediately: the prompt it
	// raises outlives the call, is answered out of process, and there is no
	// public notification for a permission change. A grant is detected by
	// polling Status() and by re-checking on window focus -- never here.
	//
	// The single legitimate use of the result is choosing the frontend's next
	// button label. Note what a false result does and does not prove: it says
	// only "not granted at the moment of the call", which covers BOTH "the
	// sheet is on screen, unanswered" and "the user already refused". It is
	// evidence that System Settings may be the remaining route, not proof
	// that the prompt is spent -- which is why the UI offers that route
	// alongside a re-check rather than instead of one.
	Request() (prompted bool)

	// OpenSettings deep-links to the settings pane where the user can grant
	// access, for when the one-shot prompt has been spent.
	OpenSettings() error
}
