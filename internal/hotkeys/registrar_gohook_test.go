package hotkeys

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	hook "github.com/robotn/gohook"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// fakeSource is a synthetic eventSource. Its channel is UNBUFFERED, which is
// what makes every test below deterministic rather than timing-dependent: a
// send returns only once the reader goroutine has received it, so sending one
// more event proves the previous one was fully handled. flush() exploits
// exactly that.
type fakeSource struct {
	mu     sync.Mutex
	ch     chan hook.Event
	starts int
	ends   int

	// blockEnd, when non-nil, makes End() wait for it to be closed. Models a
	// slow or wedged gohook teardown so a test can prove no lock is held
	// across it.
	blockEnd chan struct{}

	// probe is sampled at the TOP of End(), before anything else happens, so
	// a test can capture how many reader goroutines were alive at exactly the
	// moment the real gohook End() would have started draining.
	probe func() int32
	alive []int32

	// entered is closed the first time End() is reached. Without it a test
	// that wants to observe the system WHILE End() is in flight races the
	// goroutine driving the teardown and can take its measurement before the
	// teardown has even begun -- which makes the test pass no matter what.
	entered     chan struct{}
	enteredOnce sync.Once
}

func newFakeSource() *fakeSource { return &fakeSource{entered: make(chan struct{})} }

func (f *fakeSource) Start() <-chan hook.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ch = make(chan hook.Event)
	f.starts++
	return f.ch
}

func (f *fakeSource) End() {
	f.enteredOnce.Do(func() { close(f.entered) })

	f.mu.Lock()
	probe, block := f.probe, f.blockEnd
	f.mu.Unlock()

	if probe != nil {
		v := probe()
		f.mu.Lock()
		f.alive = append(f.alive, v)
		f.mu.Unlock()
	}
	if block != nil {
		<-block
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.ends++
	if f.ch != nil {
		close(f.ch)
		f.ch = nil
	}
}

// readersAliveAtEnd returns the highest reader count observed at the start of
// any End() call. Must be 0: gohook's End() drains the very channel the
// reader receives from, so overlapping them is the I2 deadlock.
func (f *fakeSource) readersAliveAtEnd() int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var worst int32
	for _, v := range f.alive {
		if v > worst {
			worst = v
		}
	}
	return worst
}

func (f *fakeSource) current() chan hook.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ch
}

// send delivers one event and returns once the reader has taken it.
func (f *fakeSource) send(t *testing.T, e hook.Event) {
	t.Helper()
	ch := f.current()
	if ch == nil {
		t.Fatal("fakeSource.send: stream is not running")
	}
	select {
	case ch <- e:
	case <-time.After(2 * time.Second):
		t.Fatal("fakeSource.send: reader did not take the event")
	}
}

// flush blocks until everything sent so far has been fully handled. See the
// type comment: receiving the filler event can only happen after the previous
// one came back out of dispatcher.handle.
func (f *fakeSource) flush(t *testing.T) {
	t.Helper()
	f.send(t, hook.Event{Kind: hook.MouseMove})
}

func (f *fakeSource) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

func (f *fakeSource) endCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ends
}

// recorder records Handler edges in order as "id:down" / "id:up".
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) Pressed(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id+":down")
}

func (r *recorder) Released(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id+":up")
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *recorder) want(t *testing.T, want ...string) {
	t.Helper()
	got := r.got()
	if len(got) != len(want) {
		t.Fatalf("handler calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("handler calls = %v, want %v", got, want)
		}
	}
}

// keyOf resolves a canonical key name to this platform's physical code, so
// the synthetic events below are exactly what the real backend would deliver.
func keyOf(t *testing.T, name string) uint16 {
	t.Helper()
	code, ok := osKeyCode(name)
	if !ok {
		t.Skipf("key %q is not bindable on %s", name, runtime.GOOS)
	}
	return uint16(code)
}

// down/up build a synthetic gohook key event for this platform, putting the
// key code in whichever field this platform's backend uses (see
// eventKeyCode). mask is a gohook modifier mask.
func down(t *testing.T, name string, mask uint16) hook.Event {
	t.Helper()
	return rawEvent(hook.KeyDown, keyOf(t, name), mask)
}

func up(t *testing.T, name string, mask uint16) hook.Event {
	t.Helper()
	return rawEvent(hook.KeyUp, keyOf(t, name), mask)
}

func rawEvent(kind uint8, code, mask uint16) hook.Event {
	e := hook.Event{Kind: kind, Mask: mask}
	if runtime.GOOS == "linux" {
		e.Keycode = code
	} else {
		e.Rawcode = code
	}
	return e
}

// newTestRegistrar wires a registrar over a fake source and registers the
// given bindings, returning everything a test needs.
func newTestRegistrar(t *testing.T) (*hookRegistrar, *fakeSource, *recorder) {
	t.Helper()
	src := newFakeSource()
	r := newHookRegistrar(src)
	rec := &recorder{}
	t.Cleanup(func() { src.End() })
	return r, src, rec
}

func mustRegister(t *testing.T, r *hookRegistrar, id, chordStr string, hold bool, h Handler) {
	t.Helper()
	c, err := chord.Parse(chordStr)
	if err != nil {
		t.Fatalf("chord.Parse(%q): %v", chordStr, err)
	}
	if err := r.Register(id, c, hold, h); err != nil {
		t.Fatalf("Register(%s, %s): %v", id, chordStr, err)
	}
}

// ---------------------------------------------------------------------------
// Auto-repeat debounce -- the safety-critical behaviour
// ---------------------------------------------------------------------------

// TestAutoRepeatFiresPressedOnce is the most important test in this package.
// gohook does not deduplicate a held key (issue #47): macOS and Windows
// re-deliver KeyDown for as long as the key is down. A push-to-talk binding
// that re-latched on every repeat, or that lost its release, would leave the
// microphone open with nothing to close it.
func TestAutoRepeatFiresPressedOnce(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	for i := 0; i < 25; i++ {
		src.send(t, down(t, "F1", 0))
	}
	src.flush(t)
	rec.want(t, "global.ptt:down")

	src.send(t, up(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down", "global.ptt:up")

	if held := r.d.heldCount(); held != 0 {
		t.Errorf("after key-up %d actions still latched, want 0", held)
	}
}

// TestAutoRepeatCycleIsRepeatable: a second press after a clean release must
// fire again. A debounce that latched permanently would pass the test above
// and still be broken.
func TestAutoRepeatCycleIsRepeatable(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	for cycle := 0; cycle < 3; cycle++ {
		src.send(t, down(t, "F1", 0))
		src.send(t, down(t, "F1", 0))
		src.send(t, up(t, "F1", 0))
	}
	src.flush(t)

	rec.want(t,
		"global.ptt:down", "global.ptt:up",
		"global.ptt:down", "global.ptt:up",
		"global.ptt:down", "global.ptt:up",
	)
}

// TestStrayKeyUpDoesNotRelease: a key-up with no matching latch (the key went
// down before we registered, or belongs to nobody) must not manufacture a
// Released.
func TestStrayKeyUpDoesNotRelease(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, up(t, "F1", 0))
	src.flush(t)
	rec.want(t)
}

// TestKeyHoldEventsAreIgnored: on X11 auto-repeat arrives as KeyHold, and on
// Windows KeyHold is a synthetic "typed character" event with a zero keycode.
// Neither is a key transition.
func TestKeyHoldEventsAreIgnored(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	// A KeyHold with no preceding KeyDown must fire nothing. Sending these
	// AFTER a down would prove little: the auto-repeat latch would swallow
	// them anyway, so the test would pass even if KeyHold were treated as a
	// transition.
	for i := 0; i < 10; i++ {
		src.send(t, rawEvent(hook.KeyHold, keyOf(t, "F1"), 0))
	}
	src.flush(t)
	rec.want(t)

	// And a KeyHold must not close a held key either -- on X11 it IS the
	// repeat of a key that is still down.
	src.send(t, down(t, "F1", 0))
	src.send(t, rawEvent(hook.KeyHold, keyOf(t, "F1"), 0))
	src.flush(t)
	rec.want(t, "global.ptt:down")
	if held := r.d.heldCount(); held != 1 {
		t.Errorf("a KeyHold released the latch: heldCount = %d, want 1", held)
	}
}

// TestPressOnlyBindingIsAlsoDebounced: a non-hold binding gets no Released,
// but must still fire exactly once per physical press. Without the latch a
// held mute-toggle key would toggle dozens of times a second.
func TestPressOnlyBindingIsAlsoDebounced(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.mute_toggle", "M", false, rec)

	for i := 0; i < 10; i++ {
		src.send(t, down(t, "M", 0))
	}
	src.send(t, up(t, "M", 0))
	src.send(t, down(t, "M", 0))
	src.flush(t)

	rec.want(t, "global.mute_toggle:down", "global.mute_toggle:down")
}

// ---------------------------------------------------------------------------
// Matching: registered vs unregistered, and modifiers
// ---------------------------------------------------------------------------

// TestOnlyRegisteredChordsFire drives both a bound and an unbound key through
// the same stream. The stream carries EVERY keystroke on the machine, so
// "ignores what it was not asked about" is the load-bearing property.
func TestOnlyRegisteredChordsFire(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, down(t, "F2", 0))
	src.send(t, up(t, "F2", 0))
	src.send(t, down(t, "A", 0))
	src.send(t, up(t, "A", 0))
	src.send(t, down(t, "F1", 0))
	src.send(t, up(t, "F1", 0))
	src.flush(t)

	rec.want(t, "global.ptt:down", "global.ptt:up")
}

// TestModifierMatchIsExactOnPress pins both directions of the modifier rule.
func TestModifierMatchIsExactOnPress(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "with_ctrl", "Ctrl+E", true, rec)
	mustRegister(t, r, "bare", "E", true, rec)

	// Bare E must fire ONLY the bare binding.
	src.send(t, down(t, "E", 0))
	src.send(t, up(t, "E", 0))
	src.flush(t)
	rec.want(t, "bare:down", "bare:up")

	// Ctrl+E must fire ONLY the ctrl binding.
	src.send(t, down(t, "E", hookMaskCtrl))
	src.send(t, up(t, "E", hookMaskCtrl))
	src.flush(t)
	rec.want(t, "bare:down", "bare:up", "with_ctrl:down", "with_ctrl:up")

	// A superset of the chord's modifiers is not the chord.
	src.send(t, down(t, "E", hookMaskCtrl|hookMaskShift))
	src.send(t, up(t, "E", hookMaskCtrl|hookMaskShift))
	src.flush(t)
	rec.want(t, "bare:down", "bare:up", "with_ctrl:down", "with_ctrl:up")
}

// TestModifierMatchAcceptsEitherSide: the darwin and X11 backends only ever
// set the LEFT modifier bit, Windows sets the correct side. Both must match.
func TestModifierMatchAcceptsEitherSide(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "a", "Ctrl+E", false, rec)

	src.send(t, down(t, "E", 1<<1)) // left ctrl
	src.send(t, up(t, "E", 1<<1))
	src.send(t, down(t, "E", 1<<5)) // right ctrl
	src.flush(t)

	rec.want(t, "a:down", "a:down")
}

// TestLockKeysDoNotAffectMatching: CapsLock/NumLock/ScrollLock set mask bits
// too. If they participated in the comparison, every binding would stop
// working the moment a user left CapsLock on.
func TestLockKeysDoNotAffectMatching(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "a", "F1", true, rec)

	const capsLock, numLock, scrollLock = 1 << 14, 1 << 13, 1 << 15
	src.send(t, down(t, "F1", capsLock|numLock|scrollLock))
	src.send(t, up(t, "F1", capsLock|numLock|scrollLock))
	src.flush(t)

	rec.want(t, "a:down", "a:up")
}

// TestReleaseIgnoresModifiers is the counterpart to the exact-match rule, and
// it is a microphone-safety test. Releasing Ctrl before releasing E produces
// a key-up with NO modifiers set; if release also required an exact chord
// match, that release would be dropped and PTT would stay open forever.
func TestReleaseIgnoresModifiers(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "Ctrl+E", true, rec)

	src.send(t, down(t, "E", hookMaskCtrl))
	src.send(t, up(t, "E", 0)) // user let go of Ctrl first
	src.flush(t)

	rec.want(t, "global.ptt:down", "global.ptt:up")
	if held := r.d.heldCount(); held != 0 {
		t.Errorf("%d actions still latched after release, want 0", held)
	}
}

// TestReleaseOnlyAffectsLatchedActions: a key-up on a shared key must not
// release a binding that never latched.
func TestReleaseOnlyAffectsLatchedActions(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "with_ctrl", "Ctrl+E", true, rec)
	mustRegister(t, r, "bare", "E", true, rec)

	src.send(t, down(t, "E", hookMaskCtrl)) // latches with_ctrl only
	src.send(t, up(t, "E", hookMaskCtrl))
	src.flush(t)

	rec.want(t, "with_ctrl:down", "with_ctrl:up")
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// TestStreamStartsOnceAcrossApplyCycles is the stream-lifecycle decision made
// executable: one gohook stream for the process, kept alive across every
// rebind. Manager calls UnregisterAll before every re-registration, so a
// registrar that tore the stream down there would stop and restart the OS
// listener on every keystroke typed into the capture UI -- and gohook's
// End()/Start() pair is both slow (it sleeps and drains) and racy.
func TestStreamStartsOnceAcrossApplyCycles(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec) // starts the stream
	before := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		r.UnregisterAll() // what Manager.Apply does first
		mustRegister(t, r, "global.ptt", "F1", true, rec)
		mustRegister(t, r, "global.mute_toggle", "M", false, rec)
	}

	if got := r.startCount(); got != 1 {
		t.Errorf("OS stream started %d times across 20 Apply cycles, want 1", got)
	}
	if got := src.endCount(); got != 0 {
		t.Errorf("OS stream torn down %d times across 20 Apply cycles, want 0", got)
	}

	// The registrations must still be live afterwards.
	src.send(t, down(t, "F1", 0))
	src.send(t, up(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down", "global.ptt:up")

	assertNoGoroutineGrowth(t, before)
}

// TestSuspendResumeCyclesDoNotLeakOrDoubleStart covers the Suspend/Resume
// shape Manager drives: UnregisterAll with nothing re-registered, then
// re-registration, over and over.
func TestSuspendResumeCyclesDoNotLeakOrDoubleStart(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)
	before := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		r.UnregisterAll()                                 // Suspend
		mustRegister(t, r, "global.ptt", "F1", true, rec) // Resume
	}

	if got := r.startCount(); got != 1 {
		t.Errorf("OS stream started %d times, want 1", got)
	}
	if got := src.endCount(); got != 0 {
		t.Errorf("OS stream torn down %d times, want 0", got)
	}
	assertNoGoroutineGrowth(t, before)
}

// TestNoEventsDeliveredWhileSuspended: while suspended the chord table is
// empty, so the stream keeps running and matches nothing. Nothing may reach
// the Handler, and the events must not be queued up and replayed on resume
// either.
func TestNoEventsDeliveredWhileSuspended(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	r.UnregisterAll() // Suspend

	src.send(t, down(t, "F1", 0))
	src.send(t, up(t, "F1", 0))
	src.flush(t)
	rec.want(t)

	mustRegister(t, r, "global.ptt", "F1", true, rec) // Resume
	src.flush(t)
	rec.want(t) // nothing replayed

	src.send(t, down(t, "F1", 0))
	src.send(t, up(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down", "global.ptt:up")
}

// TestSuspendReleasesAHeldHotkey is the other microphone-safety test. A user
// can open the keybind UI while holding PTT -- or a rebind can land mid
// transmission. Clearing the registrations without releasing what is held
// would leave the app believing PTT is down with no event able to close it.
func TestSuspendReleasesAHeldHotkey(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, down(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down")

	r.UnregisterAll()
	rec.want(t, "global.ptt:down", "global.ptt:up")

	if held := r.d.heldCount(); held != 0 {
		t.Errorf("%d actions still latched after UnregisterAll, want 0", held)
	}
}

// TestUnregisterAllDoesNotReleasePressOnlyBindings: a press-only action has
// no release semantics, so clearing the table must not invent one.
func TestUnregisterAllDoesNotReleasePressOnlyBindings(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.mute_toggle", "M", false, rec)

	src.send(t, down(t, "M", 0))
	src.flush(t)
	r.UnregisterAll()

	rec.want(t, "global.mute_toggle:down")
}

// ---------------------------------------------------------------------------
// Failure surfaces
// ---------------------------------------------------------------------------

// TestHookDisabledSurfacesAsRegistrationFailure: gohook's loop goroutine
// returns for good after emitting HookDisabled, so a stream in that state
// matches nothing. Registration must FAIL rather than succeed silently --
// that failure is what drives Manager.Registered() false and puts the
// "global hotkeys unavailable" banner on screen.
func TestHookDisabledSurfacesAsRegistrationFailure(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)

	// The first cycle after the failure is allowed one restart attempt.
	r.UnregisterAll()
	c, _ := chord.Parse("F1")
	if err := r.Register("global.ptt", c, true, rec); err != nil {
		t.Fatalf("the armed retry should have restarted the stream, got %v", err)
	}
	if got := r.startCount(); got != 2 {
		t.Fatalf("stream started %d times, want 2 (one restart)", got)
	}

	// Still dead, and the retry is spent: now it must report the failure.
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)
	if err := r.Register("global.mute_toggle", c, false, rec); !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("Register on a dead stream = %v, want ErrBackendUnavailable", err)
	}
}

// TestRestartIsArmedOncePerCycle: nineteen bindings against a permanently
// dead stream must not mean nineteen End/Start cycles. gohook's End() sleeps
// before closing, so that would be seconds of stalled startup.
func TestRestartIsArmedOncePerCycle(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "a", "F1", true, rec)
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)

	r.UnregisterAll()
	c, _ := chord.Parse("F1")
	for i := 0; i < 19; i++ {
		_ = r.Register("action", c, true, rec)
		// Re-assert the dead state the way the real backend would.
		if ch := src.current(); ch != nil {
			select {
			case ch <- hook.Event{Kind: hook.HookDisabled}:
			case <-time.After(time.Second):
				t.Fatal("reader did not take the event")
			}
			src.flush(t)
		}
	}

	if got := src.endCount(); got != 1 {
		t.Errorf("stream torn down %d times in one cycle, want 1", got)
	}
	if got := r.startCount(); got != 2 {
		t.Errorf("stream started %d times, want 2 (initial + one restart)", got)
	}
}

// TestRestartRecoversTheStream is the macOS "user just granted Accessibility"
// path: the stream died because permission was missing, the user granted it,
// window focus drove another Apply, and hotkeys must work WITHOUT restarting
// the app.
func TestRestartRecoversTheStream(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)

	r.UnregisterAll()
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, hook.Event{Kind: hook.HookEnabled})
	src.send(t, down(t, "F1", 0))
	src.send(t, up(t, "F1", 0))
	src.flush(t)

	rec.want(t, "global.ptt:down", "global.ptt:up")
}

// TestRestartEndsTheOldReader proves the restart path leaves exactly one
// reader goroutine alive, not two racing over the same dispatcher.
func TestRestartEndsTheOldReader(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)
	before := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		src.send(t, hook.Event{Kind: hook.HookDisabled})
		src.flush(t)
		r.UnregisterAll()
		mustRegister(t, r, "global.ptt", "F1", true, rec)
	}

	assertNoGoroutineGrowth(t, before)
}

// TestUnmappableKeyIsAPerActionFailure: internal/chord accepts keys some
// platforms cannot express (F21-F24 on macOS, numpad Enter on Windows). Those
// must fail per action so Manager.Failed() can name them, not take the whole
// backend down.
func TestUnmappableKeyIsAPerActionFailure(t *testing.T) {
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "F21"
	case "windows":
		name = "NumpadEnter"
	default:
		t.Skip("every canonical key is mappable on this platform")
	}

	r, _, rec := newTestRegistrar(t)
	c, err := chord.Parse(name)
	if err != nil {
		t.Fatalf("chord.Parse(%q): %v", name, err)
	}
	err = r.Register("global.ptt", c, true, rec)
	if err == nil {
		t.Fatalf("Register(%s) on %s should fail", name, runtime.GOOS)
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("an unmappable KEY must not report the whole backend unavailable: %v", err)
	}

	// The stream must not have been started for a binding that cannot exist.
	if got := r.startCount(); got != 0 {
		t.Errorf("stream started %d times for an unmappable key, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Event translation
// ---------------------------------------------------------------------------

func TestToKeyEventOnlyConvertsTransitions(t *testing.T) {
	for _, kind := range []uint8{
		hook.KeyHold, hook.MouseMove, hook.MouseDown, hook.MouseUp,
		hook.MouseWheel, hook.HookEnabled, hook.HookDisabled, hook.FakeEvent,
	} {
		if _, ok := toKeyEvent(hook.Event{Kind: kind}); ok {
			t.Errorf("event kind %d was converted to a key transition", kind)
		}
	}
	for kind, wantEdge := range map[uint8]keyEdge{hook.KeyDown: edgeDown, hook.KeyUp: edgeUp} {
		ke, ok := toKeyEvent(hook.Event{Kind: kind})
		if !ok {
			t.Fatalf("event kind %d was not converted", kind)
		}
		if ke.edge != wantEdge {
			t.Errorf("kind %d -> edge %v, want %v", kind, ke.edge, wantEdge)
		}
	}
}

func TestModsFromMask(t *testing.T) {
	cases := []struct {
		mask uint16
		want chord.Mod
	}{
		{0, 0},
		{1 << 0, chord.ModShift},
		{1 << 4, chord.ModShift},
		{1 << 1, chord.ModCtrl},
		{1 << 5, chord.ModCtrl},
		{1 << 2, chord.ModSuper},
		{1 << 6, chord.ModSuper},
		{1 << 3, chord.ModAlt},
		{1 << 7, chord.ModAlt},
		{1<<1 | 1<<3, chord.ModCtrl | chord.ModAlt},
		{1 << 8, 0},  // mouse button 1
		{1 << 13, 0}, // num lock
		{1 << 14, 0}, // caps lock
		{1 << 15, 0}, // scroll lock
	}
	for _, c := range cases {
		if got := modsFromMask(c.mask); got != c.want {
			t.Errorf("modsFromMask(%#x) = %d, want %d", c.mask, got, c.want)
		}
	}
}

// TestEventKeyCodePicksThePlatformField pins the per-backend field choice.
// Picking the wrong one fails silently -- chords simply never match -- which
// is exactly the class of bug this asserts away.
func TestEventKeyCodePicksThePlatformField(t *testing.T) {
	const keycode, rawcode = 111, 222
	got := eventKeyCode(keycode, rawcode)
	if runtime.GOOS == "linux" {
		if got != keycode {
			t.Errorf("on linux eventKeyCode must use Event.Keycode (the evdev code), got %d", got)
		}
		return
	}
	if got != rawcode {
		t.Errorf("on %s eventKeyCode must use Event.Rawcode, got %d", runtime.GOOS, got)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// assertNoGoroutineGrowth checks the goroutine count came back to where it
// started. goleak is not a dependency of this module, so this is the
// observable-state version of the same assertion; it polls because a
// goroutine that is exiting may not have been reaped yet.
func assertNoGoroutineGrowth(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		now := runtime.NumGoroutine()
		if now <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("goroutines grew from %d to %d", before, now)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// A dying stream must not strand a held hotkey (review finding I1)
// ---------------------------------------------------------------------------

// TestHookDisabledReleasesAHeldHotkey is a microphone-safety test.
//
// HookDisabled means the backend lost its listener and its loop goroutine has
// already returned, so NO KeyUp can ever follow. A hold binding latched at
// that instant would stay latched forever: Pressed was emitted, Released never
// would be, and the mic stays open until some later Apply happens to clear it.
func TestHookDisabledReleasesAHeldHotkey(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, down(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down")

	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)

	rec.want(t, "global.ptt:down", "global.ptt:up")
	if held := r.d.heldCount(); held != 0 {
		t.Errorf("%d actions still latched after the stream died, want 0", held)
	}
}

// TestClosedStreamReleasesAHeldHotkey is the same hazard reached the other
// way: the channel closing underneath us rather than an explicit
// HookDisabled. Both paths must release.
func TestClosedStreamReleasesAHeldHotkey(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, down(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down")

	src.End() // the stream goes away with the key still down

	waitFor(t, func() bool { return r.d.heldCount() == 0 })
	rec.want(t, "global.ptt:down", "global.ptt:up")
}

// TestDyingStreamKeepsRegistrations: releasing what is held must NOT discard
// the bindings. The stream can be restarted, and the user's keybinds have not
// changed -- only clear() (Suspend/Apply) empties the table.
func TestDyingStreamKeepsRegistrations(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, down(t, "F1", 0))
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)
	rec.want(t, "global.ptt:down", "global.ptt:up")

	// Recover, and the binding must still be live without re-registering it.
	r.UnregisterAll()
	mustRegister(t, r, "global.ptt", "F1", true, rec)
	src.send(t, hook.Event{Kind: hook.HookEnabled})
	src.send(t, down(t, "F1", 0))
	src.send(t, up(t, "F1", 0))
	src.flush(t)

	rec.want(t, "global.ptt:down", "global.ptt:up", "global.ptt:down", "global.ptt:up")
}

// TestDyingStreamDoesNotReleasePressOnlyBindings: a press-only action has no
// release semantics, so a dead stream must not invent one for it either.
func TestDyingStreamDoesNotReleasePressOnlyBindings(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.mute_toggle", "M", false, rec)

	src.send(t, down(t, "M", 0))
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)

	rec.want(t, "global.mute_toggle:down")
}

// TestReleaseOnStreamDeathIsIdempotent: several HookDisabled events, or a
// HookDisabled followed by the channel closing, must not produce a second
// Released for the same press.
func TestReleaseOnStreamDeathIsIdempotent(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, down(t, "F1", 0))
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)
	src.End()

	waitFor(t, func() bool { return r.d.heldCount() == 0 })
	rec.want(t, "global.ptt:down", "global.ptt:up")
}

// ---------------------------------------------------------------------------
// Teardown must not deadlock or hang the caller (review finding I2)
// ---------------------------------------------------------------------------

// TestRestartStopsTheReaderBeforeEndingTheStream pins the ORDER that makes the
// restart path safe.
//
// gohook's End() drains its channel with `for len(ev) != 0 { <-ev }` and then
// closes it. If our reader were still ranging over that same channel it would
// compete for those receives, and End() could pass its length check and then
// block forever on a receive no producer will satisfy. Signalling the reader
// first and waiting for it to acknowledge means End() drains unopposed.
func TestRestartStopsTheReaderBeforeEndingTheStream(t *testing.T) {
	src := newFakeSource()
	r := newHookRegistrar(src)
	src.probe = r.aliveReaders
	rec := &recorder{}
	t.Cleanup(func() { src.End() })

	mustRegister(t, r, "global.ptt", "F1", true, rec)
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)

	r.UnregisterAll()
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	if got := src.readersAliveAtEnd(); got != 0 {
		t.Errorf("End() ran with %d reader(s) still alive; it must be called only "+
			"after the reader has acknowledged quit, or its drain can deadlock", got)
	}
}

// TestTeardownDoesNotHoldTheLifecycleLock: a wedged teardown must not block
// unrelated lifecycle reads. ensureStream runs on the goroutine servicing a
// Wails call from the settings UI, so a lock held across End() is a frozen
// window, not merely a slow one.
func TestTeardownDoesNotHoldTheLifecycleLock(t *testing.T) {
	src := newFakeSource()
	release := make(chan struct{})
	src.blockEnd = release
	r := newHookRegistrar(src)
	rec := &recorder{}

	mustRegister(t, r, "global.ptt", "F1", true, rec)
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)
	r.UnregisterAll()

	// Drive a restart, whose End() is now stuck.
	registered := make(chan struct{})
	go func() {
		defer close(registered)
		c, _ := chord.Parse("F1")
		_ = r.Register("global.ptt", c, true, rec)
	}()

	// Wait until End() is genuinely in flight. Probing before the teardown
	// starts would find lifeMu free no matter how it is written, which is how
	// the first version of this test passed against a deliberately broken
	// implementation.
	select {
	case <-src.entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("src.End() was never reached; the restart did not happen")
	}

	// lifeMu must still be free while End() is blocked.
	lockFree := make(chan struct{})
	go func() {
		defer close(lockFree)
		_ = r.startCount()
	}()

	select {
	case <-lockFree:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("a blocked src.End() also blocked startCount(): the teardown is " +
			"holding lifeMu, which would freeze the settings UI")
	}

	close(release)
	select {
	case <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("Register did not return after End() unblocked")
	}
	src.End()
}

// TestWedgedReaderFailsInsteadOfHanging: if the reader does not acknowledge
// quit, registration must report a failure rather than block the UI, and must
// NOT start a second stream on top of the first.
func TestWedgedReaderFailsInsteadOfHanging(t *testing.T) {
	src := newFakeSource()
	r := newHookRegistrar(src)
	rec := &recorder{}
	t.Cleanup(func() { src.End() })

	mustRegister(t, r, "global.ptt", "F1", true, rec)
	src.send(t, hook.Event{Kind: hook.HookDisabled})
	src.flush(t)

	// Wedge the reader by hijacking its done channel: it will never close.
	r.lifeMu.Lock()
	r.readerDone = make(chan struct{})
	r.lifeMu.Unlock()

	r.UnregisterAll()
	c, _ := chord.Parse("F1")

	start := time.Now()
	err := r.Register("global.ptt", c, true, rec)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("Register over a wedged reader = %v, want ErrBackendUnavailable", err)
	}
	if elapsed > readerStopTimeout+2*time.Second {
		t.Errorf("Register blocked for %v; the wait must be bounded", elapsed)
	}
	if got := src.endCount(); got != 0 {
		t.Errorf("End() was called %d time(s) with the reader still alive; that is "+
			"exactly the condition that makes its drain unsafe", got)
	}
	if got := r.startCount(); got != 1 {
		t.Errorf("stream started %d times, want 1: a wedged teardown must not be "+
			"followed by a second stream", got)
	}
}

// ---------------------------------------------------------------------------
// Close (review minor 5)
// ---------------------------------------------------------------------------

// TestCloseReleasesHeldAndStopsTheStream: quitting mid-transmission must not
// leave Pressed unmatched, and the one-stream-per-process lifecycle must have
// a closing bracket.
func TestCloseReleasesHeldAndStopsTheStream(t *testing.T) {
	src := newFakeSource()
	r := newHookRegistrar(src)
	rec := &recorder{}
	before := runtime.NumGoroutine()

	mustRegister(t, r, "global.ptt", "F1", true, rec)
	src.send(t, down(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down")

	r.Close()

	rec.want(t, "global.ptt:down", "global.ptt:up")
	if got := src.endCount(); got != 1 {
		t.Errorf("End() called %d times on Close, want 1", got)
	}
	assertNoGoroutineGrowth(t, before)
}

// TestCloseIsIdempotentAndSafeWhenIdle: Close runs on a shutdown path, where
// the cheapest bug is a panic on a stream that never started.
func TestCloseIsIdempotentAndSafeWhenIdle(t *testing.T) {
	src := newFakeSource()
	r := newHookRegistrar(src)

	r.Close() // never started
	if got := src.endCount(); got != 0 {
		t.Errorf("End() called %d times on a stream that never started, want 0", got)
	}

	rec := &recorder{}
	mustRegister(t, r, "global.ptt", "F1", true, rec)
	r.Close()
	r.Close()
	if got := src.endCount(); got != 1 {
		t.Errorf("End() called %d times across two Closes, want 1", got)
	}
}

// TestManagerCloseReachesTheRegistrar wires the optional Closer seam: Manager
// must release registrations AND shut the OS layer down, without Registrar
// itself growing a third method that every fake would have to implement.
func TestManagerCloseReachesTheRegistrar(t *testing.T) {
	src := newFakeSource()
	r := newHookRegistrar(src)
	rec := &recorder{}
	m := New(r, rec)

	if err := m.Apply(map[string]Binding{"global.ptt": {mustChord(t, "F1"), true}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	src.send(t, down(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down")

	m.Close()

	rec.want(t, "global.ptt:down", "global.ptt:up")
	if got := src.endCount(); got != 1 {
		t.Errorf("Manager.Close reached End() %d times, want 1", got)
	}
}

// TestManagerCloseWorksWithAPlainRegistrar: the existing fakes implement only
// the two-method Registrar, which is the whole reason Closer is optional.
func TestManagerCloseWorksWithAPlainRegistrar(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{"a": {mustChord(t, "F1"), false}})

	m.Close() // must not panic on a Registrar with no Close

	if f.count() != 0 {
		t.Error("Close should have released every registration")
	}
}

// ---------------------------------------------------------------------------
// Replacing a held binding (review minor 1)
// ---------------------------------------------------------------------------

// TestReplacingAHeldBindingReleasesIt: add() drops the latch because the new
// binding's key may be a different one, and a latch that vanishes without its
// Released is an open microphone. Unreachable through Manager today, which is
// why the invariant belongs where the latch is removed.
func TestReplacingAHeldBindingReleasesIt(t *testing.T) {
	r, src, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)

	src.send(t, down(t, "F1", 0))
	src.flush(t)
	rec.want(t, "global.ptt:down")

	// Re-register the SAME action on a different key, without UnregisterAll.
	mustRegister(t, r, "global.ptt", "F2", true, rec)

	rec.want(t, "global.ptt:down", "global.ptt:up")
	if held := r.d.heldCount(); held != 0 {
		t.Errorf("%d actions still latched after rebinding, want 0", held)
	}
}

// TestReplacingAnUnheldBindingReleasesNothing guards the other direction: the
// release must be conditional on the latch, not on the replacement.
func TestReplacingAnUnheldBindingReleasesNothing(t *testing.T) {
	r, _, rec := newTestRegistrar(t)
	mustRegister(t, r, "global.ptt", "F1", true, rec)
	mustRegister(t, r, "global.ptt", "F2", true, rec)
	rec.want(t)
}

// waitFor polls until cond holds, for assertions driven by a goroutine rather
// than by a synchronous send.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
