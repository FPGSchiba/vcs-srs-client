package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// fakePermission stands in for the cgo PermissionChecker. Every field is
// guarded: the re-check poll reads Status() from its own goroutine while the
// test writes the grant state, which is the exact interleaving -race exists
// to catch.
//
// Crucially, `prompted` and `status` are INDEPENDENT. That is not a
// convenience -- it is the property under test. The real
// CGRequestListenEventAccess returns a value that is not the user's answer,
// so a fake that derived one from the other would quietly agree with the bug
// this design exists to prevent.
type fakePermission struct {
	mu       sync.Mutex
	status   hotkeys.Permission
	prompted bool
	requests int
	opens    int
	statuses int
	openErr  error
}

func (f *fakePermission) Status() hotkeys.Permission {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses++
	return f.status
}

func (f *fakePermission) Request() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	return f.prompted
}

func (f *fakePermission) OpenSettings() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	return f.openErr
}

// set changes the grant state, standing in for the user answering the OS
// prompt (or flipping the switch in System Settings) out of band.
func (f *fakePermission) set(p hotkeys.Permission) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = p
}

func (f *fakePermission) statusCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses
}

func (f *fakePermission) requestCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakePermission) openCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens
}

// lockedRegistrar counts registrations under a mutex. countingRegistrar
// cannot be reused here: applyHotkeys now also runs on the re-check poll's
// goroutine, so an unguarded counter would be a genuine data race rather
// than a test-harness detail.
type lockedRegistrar struct {
	mu sync.Mutex
	n  int
}

func (r *lockedRegistrar) Register(string, chord.Chord, bool, hotkeys.Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	return nil
}

func (r *lockedRegistrar) UnregisterAll() {}

func (r *lockedRegistrar) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// newPermTestApp wires an App whose permission seam is the supplied fake, and
// whose re-check poll ticks fast enough for a test but keeps a deliberately
// generous timeout: a test that passes only because the poll gave up is not
// testing the poll.
func newPermTestApp(t *testing.T, perm *fakePermission) (*App, *recordingEmitter, *lockedRegistrar) {
	t.Helper()
	em := &recordingEmitter{}
	reg := &lockedRegistrar{}
	a := NewForTest(state.New(), nil, nil)
	a.setPermissionChecker(perm)
	kb := keybinds.New()
	kb.Load(map[string]string{})
	a.SetSettingsBackend(config.Default(), "", kb, hotkeys.New(reg, a), em)
	a.SetHotkeyPermissionPoll(time.Millisecond, 10*time.Second)
	return a, em, reg
}

// waitClosed fails the test if ch is not closed within d.
func waitClosed(t *testing.T, ch <-chan struct{}, d time.Duration, what string) {
	t.Helper()
	if ch == nil {
		t.Fatalf("%s: no re-check poll was armed", what)
	}
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("%s: re-check poll did not finish within %s", what, d)
	}
}

// TestRequestHotkeyPermissionPollDetectsGrant is the happy path, and the
// place the central design rule is enforced: the grant is concluded from
// Status(), never from Request()'s return.
//
// The fake reports prompted=true while still denied, the user "answers" out
// of band, and the poll -- not the request -- is what notices, re-applies the
// OS registrations and emits hotkeys:state with permission "granted".
func TestRequestHotkeyPermissionPollDetectsGrant(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: true}
	a, em, reg := newPermTestApp(t, perm)

	registersBefore := reg.count()
	got := a.RequestHotkeyPermission()

	if !got.Prompted {
		t.Error("Prompted must report what the OS request call returned")
	}
	// The whole point: prompted true did NOT make the state granted.
	if got.Permission != hotkeys.PermissionDenied.String() {
		t.Errorf("Permission = %q immediately after the request, want %q -- a prompt is not an answer",
			got.Permission, hotkeys.PermissionDenied)
	}

	// The user grants access in System Settings while the poll is armed.
	perm.set(hotkeys.PermissionGranted)
	waitClosed(t, a.permissionPollDone(), 5*time.Second, "grant detection")

	if reg.count() <= registersBefore {
		t.Errorf("applyHotkeys did not re-register after the grant (%d registrations, was %d); "+
			"without it the stale CGEventTap stays deaf until a restart", reg.count(), registersBefore)
	}
	state := em.lastHotkeyState(t)
	if state.Permission != hotkeys.PermissionGranted.String() {
		t.Errorf("hotkeys:state after the grant carries permission %q, want %q",
			state.Permission, hotkeys.PermissionGranted)
	}
}

// TestRequestHotkeyPermissionNonPromptingStaysDenied covers the spent
// one-shot prompt: the OS declines to ask again, nothing changes, and the
// state must stay denied so the frontend can offer OPEN SETTINGS instead of
// a second, futile GRANT ACCESS.
func TestRequestHotkeyPermissionNonPromptingStaysDenied(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: false}
	a, _, _ := newPermTestApp(t, perm)

	got := a.RequestHotkeyPermission()
	if got.Prompted {
		t.Error("Prompted must be false when the OS did not prompt")
	}
	if got.Permission != hotkeys.PermissionDenied.String() {
		t.Errorf("Permission = %q, want %q", got.Permission, hotkeys.PermissionDenied)
	}
	if perm.requestCalls() != 1 {
		t.Errorf("Request called %d times, want exactly 1", perm.requestCalls())
	}
	if dto := a.GetHotkeyState(); dto.Permission != hotkeys.PermissionDenied.String() {
		t.Errorf("GetHotkeyState().Permission = %q, want %q", dto.Permission, hotkeys.PermissionDenied)
	}
}

// TestPermissionPollStopsOnGrant proves the poll terminates on the grant
// rather than running to its deadline. The timeout is 10s and the wait here
// is 5s, so a poll that kept ticking after detecting the grant would fail
// this test instead of quietly burning wakeups for the rest of the window.
func TestPermissionPollStopsOnGrant(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: true}
	a, _, _ := newPermTestApp(t, perm)

	a.RequestHotkeyPermission()
	perm.set(hotkeys.PermissionGranted)
	done := a.permissionPollDone()
	waitClosed(t, done, 5*time.Second, "poll stop on grant")

	// Closed means the goroutine returned, so Status() must go quiet.
	settled := perm.statusCalls()
	time.Sleep(50 * time.Millisecond) // ~50 ticks at the 1ms test interval
	if after := perm.statusCalls(); after != settled {
		t.Errorf("Status() called %d more times after the poll finished; the ticker is still running",
			after-settled)
	}
}

// TestPermissionPollStopsAtTimeout covers the other exit: the user never
// answers. The poll must bound itself and leave the state untouched rather
// than becoming a permanent background loop.
func TestPermissionPollStopsAtTimeout(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: true}
	a, em, reg := newPermTestApp(t, perm)
	a.SetHotkeyPermissionPoll(time.Millisecond, 60*time.Millisecond)

	registersBefore := reg.count()
	emitsBefore := em.count(events.EventHotkeysState)

	a.RequestHotkeyPermission()
	waitClosed(t, a.permissionPollDone(), 5*time.Second, "poll timeout")

	if reg.count() != registersBefore {
		t.Errorf("hotkeys were re-applied (%d registrations, was %d) without a grant",
			reg.count(), registersBefore)
	}
	if got := em.count(events.EventHotkeysState); got != emitsBefore {
		t.Errorf("hotkeys:state emitted %d times during a poll that never saw a grant, want 0",
			got-emitsBefore)
	}
	if dto := a.GetHotkeyState(); dto.Permission != hotkeys.PermissionDenied.String() {
		t.Errorf("Permission = %q after the timeout, want %q", dto.Permission, hotkeys.PermissionDenied)
	}
}

// TestRecheckHotkeyPermissionFlipsState is the window-focus path, and the
// primary trigger in practice: the user leaves the app to grant access and
// comes back, so focus is exactly when the answer can have changed.
func TestRecheckHotkeyPermissionFlipsState(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied}
	a, em, reg := newPermTestApp(t, perm)

	registersBefore := reg.count()
	emitsBefore := em.count(events.EventHotkeysState)

	// Focus with nothing changed must be a no-op, or every window activation
	// would re-register every hotkey.
	a.RecheckHotkeyPermission()
	if reg.count() != registersBefore {
		t.Errorf("a focus re-check with no change re-registered hotkeys (%d, was %d)",
			reg.count(), registersBefore)
	}
	if got := em.count(events.EventHotkeysState); got != emitsBefore {
		t.Errorf("a focus re-check with no change emitted %d hotkeys:state events, want 0", got-emitsBefore)
	}

	// The grant happens entirely outside the app; nothing notifies us.
	perm.set(hotkeys.PermissionGranted)
	a.RecheckHotkeyPermission()

	if reg.count() <= registersBefore {
		t.Fatalf("the focus re-check did not re-apply hotkeys after an out-of-band grant (%d, was %d)",
			reg.count(), registersBefore)
	}
	got := em.lastHotkeyState(t)
	if got.Permission != hotkeys.PermissionGranted.String() {
		t.Errorf("hotkeys:state after the focus re-check carries permission %q, want %q",
			got.Permission, hotkeys.PermissionGranted)
	}
	if !got.Registered {
		t.Error("hotkeys must be reported registered once the grant is applied")
	}
}

// TestRecheckHotkeyPermissionIsIdleOnceGranted: after a grant there is
// nothing left to observe, so the focus hook must stop touching the OS
// entirely rather than preflighting TCC on every window activation.
func TestRecheckHotkeyPermissionIsIdleOnceGranted(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionGranted}
	a, _, _ := newPermTestApp(t, perm)

	before := perm.statusCalls()
	a.RecheckHotkeyPermission()
	a.RecheckHotkeyPermission()
	if after := perm.statusCalls(); after != before {
		t.Errorf("Status() called %d times on focus after the grant, want 0", after-before)
	}
}

// TestNotApplicablePlatformNeverPolls: Windows and Linux/X11 have no grant to
// request. They must never arm the poll and must report "not_applicable", the
// state the UI reads as "offer no permission affordance at all".
func TestNotApplicablePlatformNeverPolls(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionNotApplicable}
	a, _, _ := newPermTestApp(t, perm)

	got := a.RequestHotkeyPermission()
	if got.Permission != hotkeys.PermissionNotApplicable.String() {
		t.Errorf("Permission = %q, want %q", got.Permission, hotkeys.PermissionNotApplicable)
	}
	if got.Prompted {
		t.Error("nothing can be prompted for on a platform with no such permission")
	}
	if ch := a.permissionPollDone(); ch != nil {
		t.Error("a re-check poll was armed on a platform whose permission state cannot change")
	}

	// And the focus hook must stay inert there too.
	before := perm.statusCalls()
	a.RecheckHotkeyPermission()
	if after := perm.statusCalls(); after != before {
		t.Errorf("Status() called %d times on focus, want 0 on a not-applicable platform", after-before)
	}

	if dto := a.GetHotkeyState(); dto.Permission != hotkeys.PermissionNotApplicable.String() {
		t.Errorf("GetHotkeyState().Permission = %q, want %q", dto.Permission, hotkeys.PermissionNotApplicable)
	}
}

// TestRequestHotkeyPermissionAppliesWhenAlreadyGranted: if access is already
// in place there is nothing to wait for, so the request must apply straight
// away rather than making the user sit through a poll interval.
func TestRequestHotkeyPermissionAppliesWhenAlreadyGranted(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionGranted, prompted: true}
	a, em, reg := newPermTestApp(t, perm)

	registersBefore := reg.count()
	got := a.RequestHotkeyPermission()

	if got.Permission != hotkeys.PermissionGranted.String() {
		t.Errorf("Permission = %q, want %q", got.Permission, hotkeys.PermissionGranted)
	}
	if reg.count() <= registersBefore {
		t.Errorf("hotkeys were not re-applied for an already-granted request (%d, was %d)",
			reg.count(), registersBefore)
	}
	if ch := a.permissionPollDone(); ch != nil {
		t.Error("a re-check poll was armed even though the grant was already in place")
	}
	if s := em.lastHotkeyState(t); s.Permission != hotkeys.PermissionGranted.String() {
		t.Errorf("hotkeys:state carries permission %q, want %q", s.Permission, hotkeys.PermissionGranted)
	}
}

// TestOpenHotkeyPermissionSettingsDelegates: the binding is a pass-through,
// and its error must reach the frontend rather than being swallowed -- a
// silent failure here leaves the user staring at a banner whose only button
// appears to do nothing.
func TestOpenHotkeyPermissionSettingsDelegates(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied}
	a, _, _ := newPermTestApp(t, perm)

	if err := a.OpenHotkeyPermissionSettings(); err != nil {
		t.Fatalf("OpenHotkeyPermissionSettings: %v", err)
	}
	if perm.openCalls() != 1 {
		t.Errorf("OpenSettings called %d times, want 1", perm.openCalls())
	}

	perm.openErr = errors.New("launch services refused")
	if err := a.OpenHotkeyPermissionSettings(); err == nil {
		t.Error("the OS failure must be reported, not swallowed")
	}
}

// TestHotkeyStateCarriesPermission is the wire-shape guard: HotkeyStateDTO
// and events.HotkeyStatePayload must agree, because useSettingsSync feeds the
// same store slot from both and a field present on one and absent from the
// other shows up as state that resets itself at random.
func TestHotkeyStateCarriesPermission(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied}
	a, em, _ := newPermTestApp(t, perm)

	dto := a.GetHotkeyState()
	payload := em.lastHotkeyState(t)
	if dto.Permission != payload.Permission {
		t.Errorf("GetHotkeyState().Permission = %q but hotkeys:state carries %q; the two shapes must agree",
			dto.Permission, payload.Permission)
	}
	if dto.Permission != hotkeys.PermissionDenied.String() {
		t.Errorf("Permission = %q, want %q", dto.Permission, hotkeys.PermissionDenied)
	}
}

// TestPermissionStringWireForms pins the strings the frontend branches on.
// These are a contract with Keybinds.tsx, not incidental formatting.
func TestPermissionStringWireForms(t *testing.T) {
	cases := []struct {
		p    hotkeys.Permission
		want string
	}{
		{hotkeys.PermissionUnknown, "unknown"},
		{hotkeys.PermissionGranted, "granted"},
		{hotkeys.PermissionDenied, "denied"},
		{hotkeys.PermissionNotApplicable, "not_applicable"},
		{hotkeys.Permission(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.p.String(); got != tc.want {
			t.Errorf("Permission(%d).String() = %q, want %q", tc.p, got, tc.want)
		}
	}
}
