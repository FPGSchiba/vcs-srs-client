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
// Only macOS has one: golang.design/x/hotkey's darwin backend installs a
// CGEventTap, which the system refuses to deliver events to unless the app
// holds Input Monitoring (TCC's kTCCServiceListenEvent). Windows'
// RegisterHotKey and Linux/X11's XGrabKey need no equivalent grant, so those
// platforms report PermissionNotApplicable rather than pretending to a state
// they cannot observe.
type Permission int

const (
	// PermissionUnknown means the grant state could not be determined.
	PermissionUnknown Permission = iota
	// PermissionGranted means the OS will deliver global key events to us.
	PermissionGranted
	// PermissionDenied means the OS is withholding them until the user grants
	// access. Distinct from "the user pressed Deny": TCC reports not-yet-asked
	// and explicitly-refused identically, which is why Request() exists and
	// why its result is not a grant signal.
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
	// must never be treated as one. macOS's CGRequestListenEventAccess is
	// handled asynchronously by TCC: the prompt outlives the call, its return
	// value is not documented as the user's decision, and there is no public
	// notification for a permission change. A grant is detected by polling
	// Status() and by re-checking on window focus -- never here.
	//
	// The single legitimate use of the result is choosing the frontend's next
	// button label: a false result alongside a still-denied Status() means
	// the one-shot prompt is spent and the user must be sent to System
	// Settings instead.
	Request() (prompted bool)

	// OpenSettings deep-links to the settings pane where the user can grant
	// access, for when the one-shot prompt has been spent.
	OpenSettings() error
}
