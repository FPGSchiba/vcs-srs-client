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
	mu      sync.Mutex
	n       int
	applies int
}

func (r *lockedRegistrar) Register(string, chord.Chord, bool, hotkeys.Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	return nil
}

// UnregisterAll doubles as the per-APPLY counter: hotkeys.Manager calls it
// exactly once at the top of every registerLocked, so counting it counts
// applies rather than individual Register calls.
func (r *lockedRegistrar) UnregisterAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applies++
}

func (r *lockedRegistrar) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *lockedRegistrar) applyCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.applies
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
	a.setHotkeyPermissionPoll(time.Millisecond, 10*time.Second)
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
	a.setHotkeyPermissionPoll(time.Millisecond, 60*time.Millisecond)

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

// ---- writeMu, poll cancellation and shutdown ----------------------------

// waitFor polls cond until it holds or d elapses. Used instead of a fixed
// sleep so the assertions do not encode a timing guess.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s: condition never held within %s", what, d)
}

// TestRecheckHotkeyPermissionHoldsWriteMu: every other applyHotkeys caller
// holds writeMu, and this one must too.
//
// The hazard is concrete. SetKeybind takes writeMu, mutates the store,
// persists, and rolls back if the persist fails. A window-focus event landing
// between the failed persist and the rollback would, without this lock,
// register the OS against a binding that is about to be undone -- leaving the
// OS holding a hotkey that neither the store nor disk believes in.
func TestRecheckHotkeyPermissionHoldsWriteMu(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied}
	a, _, reg := newPermTestApp(t, perm)
	sb := a.settings

	perm.set(hotkeys.PermissionGranted)
	before := reg.applyCount()

	sb.writeMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.RecheckHotkeyPermission()
	}()

	// While writeMu is held by a stand-in for SetKeybind, the re-check must
	// not have applied anything.
	time.Sleep(30 * time.Millisecond)
	if got := reg.applyCount(); got != before {
		sb.writeMu.Unlock()
		t.Fatalf("the focus re-check applied hotkeys (%d applies, was %d) while writeMu was held", got, before)
	}

	sb.writeMu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RecheckHotkeyPermission never completed after writeMu was released")
	}
	if got := reg.applyCount(); got <= before {
		t.Errorf("the focus re-check never applied (%d applies, was %d) once the lock was free", got, before)
	}
}

// TestPermissionPollHoldsWriteMu is the same guard for the poll goroutine,
// which is the likelier of the two to collide: it ticks every 500ms in
// production, entirely unsynchronised with whatever the user is doing in the
// Keybinds section.
func TestPermissionPollHoldsWriteMu(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: true}
	a, _, reg := newPermTestApp(t, perm)
	sb := a.settings

	sb.writeMu.Lock()
	before := reg.applyCount()

	a.RequestHotkeyPermission()
	perm.set(hotkeys.PermissionGranted)

	// The poll will detect the grant almost immediately at the 1ms test
	// interval, but must block on writeMu rather than applying through it.
	time.Sleep(30 * time.Millisecond)
	if got := reg.applyCount(); got != before {
		sb.writeMu.Unlock()
		t.Fatalf("the poll applied hotkeys (%d applies, was %d) while writeMu was held", got, before)
	}

	sb.writeMu.Unlock()
	waitClosed(t, a.permissionPollDone(), 5*time.Second, "poll blocked on writeMu")
	if got := reg.applyCount(); got <= before {
		t.Errorf("the poll never applied (%d applies, was %d) once the lock was free", got, before)
	}
}

// TestRecheckHotkeyPermissionCancelsPoll covers the realistic sequence: click
// GRANT ACCESS (poll armed), leave for System Settings, grant, come back.
// Focus detects the grant and applies -- and the poll must be STOPPED by it,
// not left to tick later and tear down the event tap that was just built.
// Manager.mu makes the end state right either way, which is exactly why this
// needs its own assertion: the bug is invisible in the final state.
//
// The timings are the whole test, so they are chosen to discriminate rather
// than to pass. The tick interval is 2s and the grant is applied by the focus
// path at t=0; the poll therefore cannot have ticked yet. An earlier version
// of this test waited up to 5s for the poll to finish and then counted
// applies -- which passed with the cancellation removed, because the poll
// simply finished on its own 2s tick (applying a second time on the way out)
// well inside that window. It proved nothing. Both assertions below are now
// framed against the tick:
//
//	(1) the poll must end FAST -- far sooner than one tick could arrive;
//	(2) after a full tick interval has elapsed, there must still be exactly
//	    one apply, which is the direct statement of "the tap was not rebuilt".
func TestRecheckHotkeyPermissionCancelsPoll(t *testing.T) {
	const tick = 2 * time.Second

	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: true}
	a, _, reg := newPermTestApp(t, perm)
	a.setHotkeyPermissionPoll(tick, 30*time.Second)

	a.RequestHotkeyPermission()
	done := a.permissionPollDone()
	if done == nil {
		t.Fatal("no poll was armed")
	}

	before := reg.applyCount()
	perm.set(hotkeys.PermissionGranted)
	a.RecheckHotkeyPermission()

	if got := reg.applyCount(); got != before+1 {
		t.Fatalf("the focus re-check produced %d applies, want exactly 1", got-before)
	}

	// (1) Cancelled, not merely destined to finish. A poll left running would
	// still be asleep in its 2s tick here.
	waitClosed(t, done, tick/4, "poll cancelled by the focus re-check")

	// (2) The real statement: let a full tick go by and confirm the tap was
	// never rebuilt. This is what fails if the cancellation is removed.
	time.Sleep(tick + 250*time.Millisecond)
	if got := reg.applyCount(); got != before+1 {
		t.Errorf("%d applies after a full tick interval, want exactly 1; a late poll tick "+
			"tore down and rebuilt the freshly granted event tap", got-before)
	}
}

// TestCancelPermissionPollIsSafeConcurrently: Wails dispatches window hooks
// with `go a.handleWindowEvent(...)`, so two rapid focus events really can
// run the re-check at once. A naive close-if-non-nil would panic on the
// second close; taking and nilling the channel in one step under sb.mu is
// what prevents that.
func TestCancelPermissionPollIsSafeConcurrently(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: true}
	a, _, _ := newPermTestApp(t, perm)
	a.setHotkeyPermissionPoll(time.Second, 30*time.Second)

	a.RequestHotkeyPermission()
	done := a.permissionPollDone()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.cancelPermissionPoll() // a double close would panic here
		}()
	}
	wg.Wait()
	waitClosed(t, done, 5*time.Second, "concurrent cancel")
}

// TestServiceShutdownStopsPermissionPoll: quitting within the 30s window
// after clicking GRANT ACCESS must not leave a goroutine free to call into
// the hotkey library and emit a Wails event after teardown.
func TestServiceShutdownStopsPermissionPoll(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: true}
	a, _, _ := newPermTestApp(t, perm)
	a.setHotkeyPermissionPoll(time.Second, 30*time.Second)

	a.RequestHotkeyPermission()
	done := a.permissionPollDone()
	if done == nil {
		t.Fatal("no poll was armed")
	}

	if err := a.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}
	// stopPermissionPoll waits for the goroutine, so by the time shutdown
	// returns the channel is already closed -- no polling for it here.
	select {
	case <-done:
	default:
		t.Error("ServiceShutdown returned with the permission poll still running")
	}
}

// TestStopPermissionPollIsBounded: the goroutine may be blocked acquiring
// writeMu behind an in-flight keybind write. Quit must not hang on that.
func TestStopPermissionPollIsBounded(t *testing.T) {
	perm := &fakePermission{status: hotkeys.PermissionDenied, prompted: true}
	a, _, _ := newPermTestApp(t, perm)
	sb := a.settings

	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

	a.RequestHotkeyPermission()
	perm.set(hotkeys.PermissionGranted)
	// Let the poll detect the grant and wedge itself on writeMu.
	waitFor(t, 2*time.Second, "poll reaching writeMu", func() bool { return perm.statusCalls() > 2 })

	start := time.Now()
	a.stopPermissionPoll(100 * time.Millisecond)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("stopPermissionPoll blocked for %s on a wedged goroutine; quit would hang", elapsed)
	}
}
