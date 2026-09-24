package audio

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestManager(t *testing.T, b *FakeBackend) *Manager {
	t.Helper()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)
	m := NewManager(b, ManagerOptions{
		PollInterval: 10 * time.Millisecond,
		VUInterval:   10 * time.Millisecond,
	})
	t.Cleanup(m.Stop)
	return m
}

func TestManagerStartOpensBothDevices(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st := m.State(); !st.Running {
		t.Fatalf("State().Running = false after Start: %+v", st)
	}
	// Running alone doesn't prove Start actually opened anything -- it's
	// set unconditionally. Assert the backend genuinely received both open
	// calls, for the default devices resolveDevice should have chosen.
	if calls := b.CaptureOpens(); len(calls) != 1 || calls[0] != "mic-1" {
		t.Fatalf("backend.OpenCapture calls = %v, want exactly one call for %q", calls, "mic-1")
	}
	if calls := b.PlaybackOpens(); len(calls) != 1 || calls[0] != "out-1" {
		t.Fatalf("backend.OpenPlayback calls = %v, want exactly one call for %q", calls, "out-1")
	}
}

func TestManagerCapturedAudioReachesTheSinkOnlyWhenGateIsOpen(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	sink := &recordingSink{}
	m.AddSink(sink)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	// Gate shut: PTT not held, VOX off.
	for i := 0; i < 5; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() == 0 }, "sink received audio with the gate shut")

	m.SetPTT(true)
	for i := 0; i < 5; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() > 0 }, "sink received nothing with PTT held")
}

func TestManagerMuteSilencesTheSink(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	sink := &recordingSink{}
	m.AddSink(sink)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	m.SetPTT(true)
	m.SetMuted(true)
	for i := 0; i < 10; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() == 0 }, "muted mic still reached the sink")
}

// TestToggleMutedFlipsAndReportsTheNewValue proves the basic contract:
// ToggleMuted flips Muted() and returns exactly the value it flipped to.
func TestToggleMutedFlipsAndReportsTheNewValue(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)

	if got := m.ToggleMuted(); !got || !m.Muted() {
		t.Fatalf("ToggleMuted() = %v, Muted() = %v, want true/true from unmuted", got, m.Muted())
	}
	if got := m.ToggleMuted(); got || m.Muted() {
		t.Fatalf("ToggleMuted() = %v, Muted() = %v, want false/false from muted", got, m.Muted())
	}
}

// TestConcurrentToggleMutedNeverDropsAToggle is the race regression guard
// for Fix 2: two independent sources bound to global.mute_toggle (a
// keyboard chord and a joystick button) call ToggleMuted from separate
// goroutines with no coordination above Manager. A plain
// SetMuted(!Muted()) read-modify-write lets both goroutines read the same
// starting value before either stores, so both flip to the same target and
// one toggle is lost. N concurrent toggles from a known-false start must
// leave Muted() == (N odd), proving every single toggle was actually
// applied rather than merged with another.
func TestConcurrentToggleMutedNeverDropsAToggle(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			m.ToggleMuted()
		}()
	}
	wg.Wait()

	if want := n%2 == 1; m.Muted() != want {
		t.Fatalf("after %d concurrent toggles from false, Muted() = %v, want %v (a toggle was dropped)", n, m.Muted(), want)
	}
}

func TestManagerHotPlugEmitsDeviceChange(t *testing.T) {
	b := NewFakeBackend()
	var mu sync.Mutex
	changed := 0
	b.SetDevices([]DeviceInfo{{ID: "mic-1", Name: "Mic One"}}, []DeviceInfo{{ID: "out-1", Name: "Out One"}})
	m := NewManager(b, ManagerOptions{
		PollInterval: 5 * time.Millisecond,
		VUInterval:   time.Hour,
		OnDevices: func(_, _ []DeviceInfo) {
			mu.Lock()
			changed++
			mu.Unlock()
		},
	})
	t.Cleanup(m.Stop)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One"}, {ID: "mic-2", Name: "Mic Two"}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One"}},
	)
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return changed > 0
	}, "hot-plug produced no OnDevices callback")
}

func TestManagerFallsBackToDefaultWhenSavedDeviceIsGone(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	m := NewManager(b, ManagerOptions{PollInterval: 5 * time.Millisecond, VUInterval: time.Hour})
	t.Cleanup(m.Stop)
	m.SetConfig(Config{InputDevice: "mic-vanished", OutputDevice: ""})
	if err := m.Start(); err != nil {
		t.Fatalf("Start with an absent saved device must fall back, got: %v", err)
	}
	st := m.State()
	if !st.Running {
		t.Fatalf("not running after fallback: %+v", st)
	}
	// Running alone is also true if the fallback never actually opened
	// anything (the resulting open failure would just land in InputError
	// while Running stayed true). Prove the fallback opened the default
	// device for real, and that the substitution was recorded in State.
	if st.InputError != "" {
		t.Fatalf("InputError = %q, want empty: falling back to the default device should open successfully: %+v", st.InputError, st)
	}
	if st.InputDevice != "mic-1" {
		t.Fatalf("InputDevice = %q, want the resolved default %q: %+v", st.InputDevice, "mic-1", st)
	}
	if !st.InputSubstituted {
		t.Fatalf("InputSubstituted = false, want true: a saved device that no longer enumerates must record the substitution: %+v", st)
	}
	if calls := b.CaptureOpens(); len(calls) != 1 || calls[0] != "mic-1" {
		t.Fatalf("backend.OpenCapture calls = %v, want exactly one call for the resolved default %q", calls, "mic-1")
	}
}

func TestManagerSecondStartWhileFirstIsInProgressReturnsError(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	unblock := b.BlockEnumerate()
	m := NewManager(b, ManagerOptions{PollInterval: time.Hour, VUInterval: time.Hour})
	t.Cleanup(func() {
		unblock() // in case a failure below leaves the first Start() parked
		m.Stop()
	})

	firstDone := make(chan error, 1)
	go func() { firstDone <- m.Start() }()

	// Deterministically wait for the first call to have claimed `starting`
	// and be parked inside the blocked Enumerate -- not a sleep, an
	// observable state transition (Fix B's State().Starting).
	waitFor(t, func() bool { return m.State().Starting }, "first Start() never reached the in-progress state")

	if err := m.Start(); !errors.Is(err, ErrStartInProgress) {
		t.Fatalf("second concurrent Start() = %v, want ErrStartInProgress", err)
	}
	// The in-progress rejection must not have disturbed the first call's
	// claim, and must not itself be mistaken for a completed start.
	if st := m.State(); st.Running {
		t.Fatalf("Running = true after only the rejected second Start(): %+v", st)
	}

	unblock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Start() (unblocked) returned an error: %v", err)
	}
	if st := m.State(); !st.Running || st.Starting {
		t.Fatalf("State() after the first Start() completed = %+v, want Running=true Starting=false", st)
	}
}

// TestManagerAbandonedDSPLoopCannotTouchTheNextGenerationsRings exercises
// Fix A's scenario end to end: it forces Stop() to abandon a genuinely
// parked dspLoop goroutine (via a blockingSink and a shortened
// stopJoinTimeout), starts a fresh generation, drives it, and then releases
// the abandoned goroutine.
//
// What this test DOES reliably prove, deterministically: generation 2's
// captureRing overflow count is fixed the instant the overflowing pushes
// happen (Ring.Write drops synchronously, in the caller's own goroutine --
// see ring.go -- so it does not depend on dspLoop's ticker winning or
// losing any race), and it must stay exactly there whether or not
// generation 1's parked goroutine has been released. If Fix A regressed
// (dspLoop reading m.captureRing/m.playbackRing live instead of via
// parameters), generation 1's resumed goroutine would become a SECOND
// reader of generation 2's captureRing and a SECOND writer of its
// playbackRing -- but neither of Manager's public State() fields
// (Overruns = captureRing.Dropped(), Underruns = playbackRing.Underruns())
// is guaranteed to move in a predictable direction from that kind of
// interference, and the interference itself is exactly as timing-dependent
// as ring.go's own doc says a same-shaped bug already is ("the overlap
// window is narrow and timing-dependent enough that -race does not
// reliably catch it"). So: this test is real evidence for the specific,
// deterministic slice it covers (Overruns fixed at push time), but it is
// NOT a reliable catch-all for a reintroduced Fix A regression, and I'm
// not presenting it as one. The load-bearing argument is structural --
// dspLoop/pollLoop take the rings as parameters, so an abandoned instance
// has no path to reach a later generation's fields at all, independent of
// timing -- see the report.
func TestManagerAbandonedDSPLoopCannotTouchTheNextGenerationsRings(t *testing.T) {
	origTimeout := stopJoinTimeout
	stopJoinTimeout = 20 * time.Millisecond
	t.Cleanup(func() { stopJoinTimeout = origTimeout })

	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})

	block := newBlockingSink()
	m := NewManager(b, ManagerOptions{PollInterval: time.Hour, VUInterval: time.Hour})
	m.AddSink(block)
	m.SetPTT(true) // open the gate so generation 1's dspLoop reaches the sink
	if err := m.Start(); err != nil {
		t.Fatalf("generation 1 Start: %v", err)
	}

	// Drive generation 1's dspLoop into the blocking sink and confirm it's
	// actually parked there before touching Stop().
	b.PushFrame(sine(0.5))
	waitFor(t, block.wasEntered, "generation 1's dspLoop never reached the blocking sink")

	// Stop() cannot join a goroutine parked in a Sink call; with
	// stopJoinTimeout shortened above, it abandons it (Fix 7) instead of
	// hanging the test. Generation 1's dspLoop is now a leaked goroutine,
	// still holding generation 1's rings as PARAMETERS (Fix A) -- not as
	// live Manager fields.
	stopReturned := make(chan struct{})
	go func() { m.Stop(); close(stopReturned) }()
	select {
	case <-stopReturned:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not return within 1s -- it should have abandoned the parked goroutine after stopJoinTimeout, not hung on it")
	}

	// Generation 2 must not inherit the open gate: if it did, its OWN
	// dspLoop would also call the still-blocked sink and this test would
	// hang on the wrong goroutine.
	m.SetPTT(false)
	if err := m.Start(); err != nil {
		t.Fatalf("generation 2 Start: %v", err)
	}
	t.Cleanup(func() {
		block.release() // let generation 1's parked goroutine finish
		m.Stop()
	})

	// Overflow generation 2's OWN captureRing deterministically: Write
	// drops synchronously in the pushing goroutine (ring.go), so this does
	// not race dspLoop's ticker at all -- by the time the loop below
	// returns, the drop count is fixed for good.
	for i := 0; i < 40; i++ {
		b.PushFrame(sine(0.9))
	}
	baseline := m.State().Overruns
	if baseline == 0 {
		t.Fatalf("pushing 40 frames into a %d-frame ring produced no Overruns -- test setup assumption is wrong", ringCapacityFrames)
	}

	// NOW let generation 1's parked goroutine resume. Give it a window to
	// run its post-resume steps before checking generation 2 again.
	block.release()
	time.Sleep(50 * time.Millisecond)

	if got := m.State().Overruns; got != baseline {
		t.Fatalf("generation 2 Overruns changed from %d to %d after releasing generation 1's parked goroutine -- generation 1 wrote into generation 2's captureRing", baseline, got)
	}

	// Weaker, indirect signal for the OTHER half of the bug (a rogue
	// reader stealing generation 2's captured frames rather than writing
	// extra ones): if generation 1's zombie had been consuming from
	// generation 2's captureRing concurrently, its rd index would have
	// advanced further than generation 2's own dspLoop alone would drive
	// it, which skews the ring's free-space accounting and would show up
	// as a SMALLER-than-expected overflow on a fresh push. It is not
	// proof -- a lost update on the ring's atomic indices is exactly the
	// kind of timing-dependent corruption ring.go's own doc says -race
	// does not reliably catch either -- but a grossly reduced increment
	// here would still be a red flag worth investigating.
	for i := 0; i < 40; i++ {
		b.PushFrame(sine(0.9))
	}
	if got := m.State().Overruns - baseline; got == 0 {
		t.Fatalf("generation 2's captureRing accepted 40 more frames with zero new Overruns after generation 1 resumed -- its free-space accounting looks disturbed")
	}
}

// blockingSink parks its first WriteFrame call until release is called, and
// reports (via wasEntered) whether that call has arrived -- so a test can
// deterministically wait for a dspLoop to be parked inside it before acting,
// rather than sleeping and hoping.
type blockingSink struct {
	entered     chan struct{}
	enteredOnce sync.Once
	releaseCh   chan struct{}
	releaseOnce sync.Once
}

func newBlockingSink() *blockingSink {
	return &blockingSink{entered: make(chan struct{}), releaseCh: make(chan struct{})}
}

func (s *blockingSink) WriteFrame(f []float32) {
	s.enteredOnce.Do(func() { close(s.entered) })
	<-s.releaseCh
}
func (s *blockingSink) Close() error { return nil }
func (s *blockingSink) release()     { s.releaseOnce.Do(func() { close(s.releaseCh) }) }
func (s *blockingSink) wasEntered() bool {
	select {
	case <-s.entered:
		return true
	default:
		return false
	}
}

// spyStream records whether Stop was called on it -- used to confirm a
// discarded reopen result is properly stopped rather than leaked.
type spyStream struct {
	mu      sync.Mutex
	stopped bool
}

func (s *spyStream) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	return nil
}
func (s *spyStream) wasStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// TestManagerDiscardsLateReopenFromAStaleGeneration is Fix A round 3's
// proof, exercised as a white-box test directly against maybeReopenCapture
// (this file is in package audio, so that's available) instead of through
// the full Start/Stop lifecycle. An end-to-end version turned out to also
// be achievable deterministically (see
// TestManagerStopAloneInvalidatesAnAbandonedReopen /
// TestManagerStartAfterStopInvalidatesAnAbandonedReopen, added in round 4)
// -- Start resets inputRetryAt to the zero time, so the FIRST reopen
// attempt needs no backoff wait, and stopJoinTimeout is test-injectable.
// This white-box version is kept alongside those, not instead of them: it
// pins down maybeReopenCapture's contract in isolation (construct an
// in-flight reopen against a stamped epoch, promote a new generation while
// it's parked, assert the discard), independent of Start/Stop/pollLoop's
// surrounding machinery, so a future change to any of THAT machinery can't
// accidentally stop exercising this specific function's guarantee.
//
// This complements, rather than duplicates,
// TestManagerAbandonedDSPLoopCannotTouchTheNextGenerationsRings: that test
// proves dspLoop/pollLoop's READ side can't cross generations; this one
// proves the reopen paths' WRITE-BACK side can't either. Both are needed --
// round 2 fixed the former and missed the latter.
func TestManagerDiscardsLateReopenFromAStaleGeneration(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	unblockOpen := b.BlockNextOpenCapture()
	m := NewManager(b, ManagerOptions{PollInterval: time.Hour, VUInterval: time.Hour})
	t.Cleanup(unblockOpen)

	// Set up "generation 1": captureStream down, epoch stamped, a reopen
	// about to start -- exactly maybeReopenCapture's own precondition,
	// constructed directly rather than via Start()/device-loss/backoff.
	m.mu.Lock()
	genEpoch := m.epoch + 1
	m.epoch = genEpoch
	m.mu.Unlock()

	captureRing := NewRing(ringCapacityFrames)
	reopenDone := make(chan struct{})
	go func() {
		m.maybeReopenCapture(Config{}, []DeviceInfo{{ID: "mic-1", IsDefault: true}}, time.Time{}, captureRing, genEpoch)
		close(reopenDone)
	}()

	// Wait for the reopen to have genuinely reached (and be blocked inside)
	// OpenCapture -- CaptureOpens() records the call before the block, so
	// this is an observable state transition, not a guess.
	waitFor(t, func() bool { return len(b.CaptureOpens()) > 0 }, "maybeReopenCapture never reached OpenCapture")

	// "Generation 2" takes over WHILE the reopen is in flight: bump the
	// epoch and publish its own real stream, exactly as Start() would
	// under mu (constructed directly here, again to avoid a slow, only-
	// probabilistic end-to-end reproduction).
	gen2Stream := &spyStream{}
	m.mu.Lock()
	m.epoch++
	m.captureStream = gen2Stream
	m.mu.Unlock()

	// Let the stale reopen's OpenCapture call finally return.
	unblockOpen()
	<-reopenDone

	m.mu.Lock()
	got := m.captureStream
	m.mu.Unlock()
	if got != gen2Stream {
		t.Fatalf("m.captureStream = %v, want generation 2's stream unchanged -- a stale reopen overwrote it", got)
	}
	if b.CaptureCallbackActive() {
		t.Fatal("the stale reopen's phantom stream is still registered with the backend -- it was discarded but not stopped (leaked)")
	}
}

// startManagerWithAbandonedCaptureReopen drives the ACTUAL end-to-end
// lifecycle into the round 3/4 scenario, deterministically:
//
//  1. FailNextOpen makes Start's own OpenCapture fail, leaving
//     m.captureStream nil and m.inputRetryAt at the zero time.
//  2. Because inputRetryAt is the zero time, maybeReopenCapture's very
//     FIRST attempt (on the poll goroutine's first tick) needs no backoff
//     wait at all -- !now.Before(zeroTime) is immediately true. There is
//     no multi-second timer to race here; only PollInterval (short, set
//     by the caller) gates when that first tick fires.
//  3. BlockNextOpenCapture arms the backend so that reopen attempt blocks
//     genuinely inside OpenCapture, and the test waits for CaptureOpens()
//     to record it (an observable state transition, not a guess) before
//     proceeding.
//  4. stopJoinTimeout is shortened (test-injectable, already used by
//     TestManagerAbandonedDSPLoopCannotTouchTheNextGenerationsRings) so
//     Stop() abandons the parked poll goroutine instead of hanging on it.
//
// The caller decides what happens next -- release the block with no
// further Start() (the Stop()-alone gap), or Start() again first (the
// Start()-races-Start() case already covered in round 3) -- via the
// returned unblock func.
func startManagerWithAbandonedCaptureReopen(t *testing.T, pollInterval time.Duration) (m *Manager, b *FakeBackend, unblockOpen func()) {
	t.Helper()
	origTimeout := stopJoinTimeout
	stopJoinTimeout = 20 * time.Millisecond
	t.Cleanup(func() { stopJoinTimeout = origTimeout })

	b = NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	b.FailNextOpen(errors.New("device busy"))

	m = NewManager(b, ManagerOptions{PollInterval: pollInterval, VUInterval: time.Hour})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st := m.State(); st.InputError == "" {
		t.Fatalf("Start()'s own OpenCapture was supposed to fail (FailNextOpen); State() = %+v", st)
	}

	unblockOpen = b.BlockNextOpenCapture()

	// The poll goroutine's reopen is the SECOND OpenCapture call (the
	// first was Start's own, which failed via FailNextOpen).
	waitFor(t, func() bool { return len(b.CaptureOpens()) >= 2 }, "the poll goroutine's reopen never reached OpenCapture")

	stopReturned := make(chan struct{})
	go func() { m.Stop(); close(stopReturned) }()
	select {
	case <-stopReturned:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not return within 1s -- it should have abandoned the parked reopen goroutine, not hung on it")
	}

	return m, b, unblockOpen
}

// TestManagerStopAloneInvalidatesAnAbandonedReopen is Fix A round 4's
// proof for the terminal case round 3 missed: Stop() with NO subsequent
// Start(). Before round 4, epoch was bumped only in Start, so a reopen
// Stop() abandoned (still parked in OpenCapture) would, on finally
// returning, read its stamped epoch as still equal to m.epoch (unchanged
// since Stop never touched it) and publish -- a live, backend-registered
// capture stream landing in a Manager that had already reported itself
// stopped, with no future Stop() able to reach it.
func TestManagerStopAloneInvalidatesAnAbandonedReopen(t *testing.T) {
	m, b, unblockOpen := startManagerWithAbandonedCaptureReopen(t, 5*time.Millisecond)
	t.Cleanup(unblockOpen)

	if st := m.State(); st.Running {
		t.Fatalf("State().Running = true after Stop(), want false: %+v", st)
	}

	// Release the parked reopen NOW, with no subsequent Start(). If Stop()
	// didn't invalidate this generation, the reopen's OpenCapture call
	// (which completes successfully once unblocked -- FailNextOpen was
	// one-shot and already consumed by Start's own attempt) would publish
	// a phantom stream into a manager that believes itself stopped.
	//
	// Note this is a settle-then-check, not a waitFor on
	// CaptureCallbackActive(): that flag is ALREADY false right now (the
	// blocked call hasn't reached `b.onFrame = onFrame` yet), so a waitFor
	// keyed on "eventually false" would pass trivially without the
	// released goroutine having done anything at all. There is no
	// externally-observable transition here to key a waitFor off of in
	// either direction (the correct outcome leaves everything exactly as
	// Stop() already left it), so -- as with the same-shaped check in
	// TestManagerAbandonedDSPLoopCannotTouchTheNextGenerationsRings -- a
	// bounded settle window is the honest tool, not a polling condition
	// dressed up to look more precise than it is.
	unblockOpen()
	time.Sleep(50 * time.Millisecond)

	if b.CaptureCallbackActive() {
		t.Fatal("the abandoned reopen's phantom stream is still registered with the backend after Stop() alone -- Stop() did not invalidate its own generation")
	}
	m.mu.Lock()
	got := m.captureStream
	m.mu.Unlock()
	if got != nil {
		t.Fatalf("m.captureStream = %v after Stop() with no subsequent Start(), want nil", got)
	}
}

// TestManagerStartAfterStopInvalidatesAnAbandonedReopen is round 3's
// scenario (a later Start() supersedes an abandoned reopen), driven this
// time through the real Stop()-then-Start() lifecycle rather than a
// white-box call, to confirm the epoch invalidation still holds when a
// goroutine's parked span crosses an ACTUAL Stop()/Start() boundary rather
// than a hand-constructed one.
func TestManagerStartAfterStopInvalidatesAnAbandonedReopen(t *testing.T) {
	m, b, unblockOpen := startManagerWithAbandonedCaptureReopen(t, 5*time.Millisecond)

	// The backend now opens cleanly (FailNextOpen was one-shot, already
	// consumed). Start a real generation 2 while generation 1's reopen is
	// still parked inside the blocked OpenCapture.
	if err := m.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	t.Cleanup(func() {
		unblockOpen()
		m.Stop()
	})

	m.mu.Lock()
	gen2Stream := m.captureStream
	m.mu.Unlock()
	if gen2Stream == nil {
		t.Fatal("generation 2's Start() did not open a capture stream")
	}
	if calls := b.CaptureOpens(); len(calls) != 3 {
		t.Fatalf("CaptureOpens() = %v, want 3 (gen 1's failed attempt, gen 1's blocked reopen, gen 2's Start())", calls)
	}

	// Release generation 1's parked reopen now that generation 2 is live.
	// There's no further CaptureOpens()/State() transition to key a waitFor
	// off of here -- the stale reopen's own OpenCapture call was already
	// recorded before it blocked, and discarding it changes nothing
	// externally observable except (if the bug were present) overwriting
	// m.captureStream. A short settle window, exactly as
	// TestManagerAbandonedDSPLoopCannotTouchTheNextGenerationsRings already
	// uses for the same reason, gives the released goroutine time to reach
	// its write-or-discard decision before we check.
	unblockOpen()
	time.Sleep(50 * time.Millisecond)

	m.mu.Lock()
	got := m.captureStream
	m.mu.Unlock()
	if got != gen2Stream {
		t.Fatalf("m.captureStream changed from generation 2's real stream to %v after generation 1's abandoned reopen resumed -- it was not discarded", got)
	}
}

func TestManagerReportsInputOpenFailure(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	b.FailNextOpen(errors.New("device busy"))
	m := NewManager(b, ManagerOptions{PollInterval: time.Hour, VUInterval: time.Hour})
	t.Cleanup(m.Stop)
	_ = m.Start()
	if st := m.State(); st.InputError == "" {
		t.Fatalf("State().InputError empty after a failed open: %+v", st)
	}
}

// recordingSink counts frames written to it.
type recordingSink struct {
	mu sync.Mutex
	n  int
}

func (s *recordingSink) WriteFrame(f []float32) {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
}
func (s *recordingSink) Close() error { return nil }
func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// waitFor polls cond for up to a second. The DSP goroutine is asynchronous,
// so tests synchronise on observable state rather than on sleeps.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(msg)
}

// TestManagerSuppressesUnchangedVUButEmitsSilenceTransition proves both
// halves of EventAudioVU's documented contract (events.go's EventAudioVU
// doc, and the suppression this implements in dspLoop): a sustained,
// unchanged level produces exactly one emission (the baseline), and an
// actual level change -- INCLUDING a drop back to zero -- always produces
// a fresh one. Without this, an idle/muted mic would push one identical
// payload to OnVU on every VUInterval tick forever.
func TestManagerSuppressesUnchangedVUButEmitsSilenceTransition(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", IsDefault: true}},
	)

	var mu sync.Mutex
	var received []VU
	m := NewManager(b, ManagerOptions{
		PollInterval: time.Hour,
		VUInterval:   15 * time.Millisecond,
		OnVU: func(v VU) {
			mu.Lock()
			received = append(received, v)
			mu.Unlock()
		},
	})
	t.Cleanup(m.Stop)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	countAndLast := func() (int, VU) {
		mu.Lock()
		defer mu.Unlock()
		n := len(received)
		if n == 0 {
			return 0, VU{}
		}
		return n, received[n-1]
	}

	// A feeder goroutine keeps the capture ring continuously fed at
	// roughly half FrameDuration cadence -- fast enough that dspLoop's
	// ticker never has to fall back to reading an empty ring (which would
	// itself look like silence and confound the phases below). amp is
	// changed by the test between phases via a mutex, not an atomic
	// field on the Manager -- this is test-side synchronisation, not
	// anything dspLoop itself needs to worry about.
	var feedMu sync.Mutex
	amp := float32(0)
	stopFeed := make(chan struct{})
	var feedWG sync.WaitGroup
	feedWG.Add(1)
	go func() {
		defer feedWG.Done()
		ticker := time.NewTicker(FrameDuration / 2)
		defer ticker.Stop()
		for {
			select {
			case <-stopFeed:
				return
			case <-ticker.C:
				feedMu.Lock()
				a := amp
				feedMu.Unlock()
				b.PushFrame(sine(a))
			}
		}
	}()
	t.Cleanup(func() {
		close(stopFeed)
		feedWG.Wait()
	})
	setAmp := func(a float32) {
		feedMu.Lock()
		amp = a
		feedMu.Unlock()
	}

	// Phase 1: silence. Wait for the baseline emission (the very first
	// tick always emits, quantizeVUSegment's sentinel), then hold silence
	// across several more VUInterval windows and confirm no further
	// emission arrives -- the repeated-identical-value half.
	waitFor(t, func() bool { n, _ := countAndLast(); return n >= 1 }, "no baseline VU emission for silence")
	time.Sleep(150 * time.Millisecond)
	if n, last := countAndLast(); n != 1 {
		t.Fatalf("sustained silence produced %d VU emissions, want exactly 1 (baseline only): last=%+v", n, last)
	}

	// Phase 2: a loud, unmistakably different signal. This MUST produce a
	// new emission -- the changed-value half.
	setAmp(0.9)
	waitFor(t, func() bool { n, _ := countAndLast(); return n >= 2 }, "a genuine level change produced no additional VU emission")
	if _, last := countAndLast(); last.Input == 0 {
		t.Fatalf("VU emitted after raising the level still reports zero input: %+v", last)
	}
	// Hold the loud signal steady too, proving suppression isn't just a
	// silence special case.
	time.Sleep(150 * time.Millisecond)
	if n, last := countAndLast(); n != 2 {
		t.Fatalf("sustained loud signal produced %d VU emissions, want exactly 2 (one change, then suppressed): last=%+v", n, last)
	}

	// Phase 3: back to silence. "Suppress unchanged" must not mean "never
	// emit zero" -- the meter must be able to show the user stopped
	// talking.
	setAmp(0)
	waitFor(t, func() bool { n, last := countAndLast(); return n >= 3 && last.Input == 0 }, "transition back to silence was never emitted")
}

// lastOpen returns the final entry of a CaptureOpens/PlaybackOpens log, or
// "" when nothing was ever opened.
func lastOpen(calls []string) string {
	if len(calls) == 0 {
		return ""
	}
	return calls[len(calls)-1]
}

// TestManagerDeviceSelectionChangeTakesEffectWithoutRestart is the C1
// regression: design DoD 18.2 says a user selecting a device "takes effect
// without restarting the app". Start() used to be the ONLY place
// cfg.InputDevice/OutputDevice were resolved into an open stream, and the
// bounded-backoff reopen pair only fires on a NIL stream -- so SetConfig
// stored a new id and absolutely nothing reopened. Changing the microphone
// in Settings did nothing, ever.
//
// Asserting State().InputDevice alone would not prove it: the assertion
// that matters is that the BACKEND received an open call for the new id
// (State could in principle be written without anything being opened), and
// that no error was recorded for it.
func TestManagerDeviceSelectionChangeTakesEffectWithoutRestart(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}, {ID: "mic-2", Name: "Mic Two"}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}, {ID: "out-2", Name: "Out Two"}},
	)
	var stMu sync.Mutex
	var states []State
	m := NewManager(b, ManagerOptions{
		PollInterval: 5 * time.Millisecond,
		VUInterval:   time.Hour,
		OnState: func(s State) {
			stMu.Lock()
			states = append(states, s)
			stMu.Unlock()
		},
	})
	t.Cleanup(m.Stop)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st := m.State(); st.InputDevice != "mic-1" || st.OutputDevice != "out-1" {
		t.Fatalf("Start resolved %q/%q, want the defaults mic-1/out-1: %+v", st.InputDevice, st.OutputDevice, st)
	}

	// The user picks a different microphone AND a different output in
	// Settings; internal/app pushes the whole config through SetConfig.
	m.SetConfig(Config{InputDevice: "mic-2", OutputDevice: "out-2"})

	waitFor(t, func() bool {
		return lastOpen(b.CaptureOpens()) == "mic-2" && lastOpen(b.PlaybackOpens()) == "out-2"
	}, "changing the configured devices never reached the backend: nothing reopened")

	st := m.State()
	if st.InputDevice != "mic-2" || st.OutputDevice != "out-2" {
		t.Fatalf("State reports %q/%q after the selection change, want mic-2/out-2: %+v", st.InputDevice, st.OutputDevice, st)
	}
	if st.InputError != "" || st.OutputError != "" {
		t.Fatalf("reopening onto the newly selected devices recorded an error: %+v", st)
	}
	if !b.CaptureCallbackActive() {
		t.Fatal("no capture callback is registered after the switch: the new stream never came up")
	}
	// I2: the health surface must actually move. A reopen that nothing
	// emits is invisible to the frontend.
	stMu.Lock()
	defer stMu.Unlock()
	sawNew := false
	for _, s := range states {
		if s.InputDevice == "mic-2" && s.OutputDevice == "out-2" {
			sawNew = true
		}
	}
	if !sawNew {
		t.Fatalf("OnState never carried the new device selection; emissions = %+v", states)
	}
}

// TestManagerRecoversWhenTheOpenDeviceDisappears is the I1 regression: the
// poll loop diffed the enumeration and emitted audio:devices_changed, and
// that was all. Nothing observed that the currently-OPEN device had
// vanished from the list, so an unplug produced no fallback, no reopen and
// no error -- the mic just went silent (design spec 8/13, DoD 18.3).
//
// Note this is NOT the same event as the selection change above: the config
// is untouched here, only the enumeration changes.
func TestManagerRecoversWhenTheOpenDeviceDisappears(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}, {ID: "mic-2", Name: "Mic Two"}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)
	m := NewManager(b, ManagerOptions{PollInterval: 5 * time.Millisecond, VUInterval: time.Hour})
	t.Cleanup(m.Stop)
	m.SetConfig(Config{InputDevice: "mic-2"})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st := m.State(); st.InputDevice != "mic-2" || st.InputSubstituted {
		t.Fatalf("Start did not open the saved device mic-2 unsubstituted: %+v", st)
	}

	// Unplug mic-2 while it is open.
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)

	waitFor(t, func() bool {
		st := m.State()
		return st.InputDevice == "mic-1" && st.InputSubstituted
	}, "unplugging the open capture device produced no fallback: State still reports the vanished device")

	if st := m.State(); st.InputError != "" {
		t.Fatalf("falling back to the surviving default recorded an error: %+v", st)
	}
	if lastOpen(b.CaptureOpens()) != "mic-1" {
		t.Fatalf("backend.OpenCapture calls = %v, want a fallback open for mic-1", b.CaptureOpens())
	}
	if !b.CaptureCallbackActive() {
		t.Fatal("no capture callback registered after the fallback: nothing actually reopened")
	}
	// Playback was untouched by the unplug and must NOT have been churned.
	if calls := b.PlaybackOpens(); len(calls) != 1 {
		t.Fatalf("backend.OpenPlayback calls = %v, want exactly the one from Start: an unrelated direction was reopened", calls)
	}
}

// TestManagerRestartAfterStopReopensDevices is the C3 regression: Stop()
// used to call Backend.Close(), which on the malgo backend Uninits and Frees
// the miniaudio context and nils it -- so a later Start() -> Enumerate()
// nil-dereferenced. A deterministic restart panic, in a type whose entire
// epoch/generation design treats Stop/Start as a supported cycle.
//
// This test can only fail because FakeBackend now REJECTS calls after
// Close() (ErrFakeBackendClosed). The old, forgiving fake served calls
// happily after Close while the real backend would have crashed, which is
// exactly why every existing test stayed green over the bug.
func TestManagerRestartAfterStopReopensDevices(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	if err := m.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	m.Stop()

	if b.Closed() {
		t.Fatal("Stop() closed a Backend the Manager does not own; Close is terminal, so no later Start could ever succeed")
	}

	if err := m.Start(); err != nil {
		t.Fatalf("second Start after Stop: %v", err)
	}
	st := m.State()
	if !st.Running {
		t.Fatalf("not running after restart: %+v", st)
	}
	if st.InputError != "" || st.OutputError != "" {
		t.Fatalf("restart left a device error, i.e. the backend could not serve the second Start: %+v", st)
	}
	if calls := b.CaptureOpens(); len(calls) != 2 || calls[1] != "mic-1" {
		t.Fatalf("backend.OpenCapture calls = %v, want a second open for mic-1 after the restart", calls)
	}
	if calls := b.PlaybackOpens(); len(calls) != 2 || calls[1] != "out-1" {
		t.Fatalf("backend.OpenPlayback calls = %v, want a second open for out-1 after the restart", calls)
	}
}

// TestEmitStateIfChangedHonoursEpoch pins that an abandoned generation
// cannot publish a stale state over the current one. pollOnce can outlive
// the generation that launched it (Stop()'s bounded joins), and without an
// epoch check its emitState would overwrite m.lastState and fire OnState
// with a snapshot belonging to a dead generation.
func TestEmitStateIfChangedHonoursEpoch(t *testing.T) {
	var mu sync.Mutex
	var seen []State
	m := NewManager(NewFakeBackend(), ManagerOptions{
		OnState: func(st State) {
			mu.Lock()
			seen = append(seen, st)
			mu.Unlock()
		},
		VUInterval: time.Hour,
	})

	m.mu.Lock()
	current := m.epoch
	m.mu.Unlock()

	// An emit tagged with a stale epoch must be discarded entirely.
	m.emitStateIfChanged(current - 1)

	mu.Lock()
	n := len(seen)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("stale-epoch emit published %d state(s); expected 0", n)
	}

	// The current epoch still emits.
	m.emitStateIfChanged(current)
	mu.Lock()
	n = len(seen)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("current-epoch emit published %d state(s); expected 1", n)
	}
}

// pollOnceForTest drives exactly one poll tick synchronously, with this
// generation's rings and epoch -- the same arguments pollLoop would pass.
// It exists so a test can step the device-supersede / bounded-backoff
// reopen machinery deterministically instead of sleeping on PollInterval
// and hoping the right number of ticks landed.
func (m *Manager) pollOnceForTest() {
	m.mu.Lock()
	captureRing, playbackRing, epoch := m.captureRing, m.playbackRing, m.epoch
	m.mu.Unlock()
	m.pollOnce(captureRing, playbackRing, epoch)
}

// TestDeviceChangeDuringBackoffIsNotDelayed pins that selecting a different
// device clears a backoff accumulated against the PREVIOUS device. The
// reset used to live only in the branch that requires an open stream, so
// the one case that needed it -- no stream, because opening kept failing --
// was the one case it never ran for, stranding the user's new selection
// behind up to 30 seconds of backoff for a device they are no longer asking
// for.
func TestDeviceChangeDuringBackoffIsNotDelayed(t *testing.T) {
	be := NewFakeBackend()
	be.SetDevices(
		[]DeviceInfo{{ID: "bad", Name: "Bad"}, {ID: "good", Name: "Good", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)
	be.FailCaptureFor("bad", errors.New("device is wedged"))

	// PollInterval is deliberately far longer than the test: every tick
	// that matters is driven by hand through pollOnceForTest, so the real
	// poll goroutine must not slip an extra one in behind our back.
	m := NewManager(be, ManagerOptions{VUInterval: time.Hour, PollInterval: time.Hour})
	m.SetConfig(Config{InputDevice: "bad"})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	// Drive polls until a backoff has accumulated against "bad".
	for i := 0; i < 3; i++ {
		m.pollOnceForTest()
	}
	m.mu.Lock()
	backoff := m.inputBackoff
	m.mu.Unlock()
	if backoff == 0 {
		t.Fatal("expected a non-zero input backoff after repeated open failures")
	}

	// The user picks a device that works.
	m.SetConfig(Config{InputDevice: "good"})
	m.pollOnceForTest()

	m.mu.Lock()
	stream, id, resetBackoff := m.captureStream, m.inputID, m.inputBackoff
	m.mu.Unlock()
	if stream == nil {
		t.Fatalf("capture did not open on the newly selected device; backoff is still %v", resetBackoff)
	}
	if id != "good" {
		t.Fatalf("opened device %q, want \"good\"", id)
	}
}

// TestBackoffIsHeldWhileTheTargetIsUnchanged is the other half of
// TestDeviceChangeDuringBackoffIsNotDelayed: the nil-stream reset must fire
// when the target MOVES and stay quiet when it does not.
//
// It is what makes maybeReopenCapture's inputTargetID stamp load-bearing.
// Without that stamp the target would freeze at whatever Start resolved, so
// a FALLBACK device that keeps failing (the device Start opened is gone, so
// resolveDevice now names a different one) would look like a brand-new
// target on every single poll tick. The reset would then clear the backoff
// each time and bounded-backoff would silently degrade into a retry on
// every tick -- which is precisely the unbounded per-open C-allocation path
// reopenBackoffMax exists to prevent (see its doc).
func TestBackoffIsHeldWhileTheTargetIsUnchanged(t *testing.T) {
	be := NewFakeBackend()
	be.SetDevices(
		[]DeviceInfo{{ID: "a", Name: "A", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)
	m := NewManager(be, ManagerOptions{VUInterval: time.Hour, PollInterval: time.Hour})
	// Empty id == "follow the system default", so the resolved device
	// changes under us when the default changes -- without any SetConfig.
	m.SetConfig(Config{})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	// "a" unplugs; the default becomes "b", which is wedged.
	be.SetDevices(
		[]DeviceInfo{{ID: "b", Name: "B", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)
	be.FailCaptureFor("b", errors.New("device is wedged"))

	m.pollOnceForTest() // supersedes "a", attempts "b", starts the backoff
	afterFirst := len(be.CaptureOpens())
	if afterFirst != 2 {
		t.Fatalf("expected exactly 2 capture opens (Start's \"a\" and the poll's \"b\"), got %v", be.CaptureOpens())
	}

	// Neither of these ticks may attempt anything: the target has not
	// moved, so the backoff from "b" is still the right answer.
	m.pollOnceForTest()
	m.pollOnceForTest()

	if opens := be.CaptureOpens(); len(opens) != afterFirst {
		t.Fatalf("backoff was not honoured: capture opens grew to %v across two polls inside the backoff window", opens)
	}
	m.mu.Lock()
	backoff, target := m.inputBackoff, m.inputTargetID
	m.mu.Unlock()
	if backoff == 0 {
		t.Fatal("backoff was reset even though the target device did not change")
	}
	if target != "b" {
		t.Fatalf("inputTargetID = %q, want \"b\" -- the reopen attempt must stamp the device it aimed at", target)
	}
}

// markedSink counts frames, separating the ones carrying real (non-silent)
// capture data from the zero-filled frames a ring underrun produces. The
// distinction is what lets TestDSPLoopCatchesUpAfterAStall tell "the loop
// absorbed the backlog" from "the loop ticked again and read nothing".
type markedSink struct {
	mu            sync.Mutex
	marked, total int
}

func (s *markedSink) WriteFrame(f []float32) {
	s.mu.Lock()
	s.total++
	if f[0] != 0 {
		s.marked++
	}
	s.mu.Unlock()
}
func (s *markedSink) Close() error { return nil }

// TestDSPLoopCatchesUpAfterAStall pins that a capture backlog decays instead
// of becoming permanent latency. A time.Ticker coalesces missed ticks, so
// one-frame-per-tick could never drain a backlog it did not cause: every
// frame the loop failed to read stayed in the ring forever as added latency,
// until Drain() eventually dumped ~160 ms of it in one audible jump.
//
// It equally pins the OTHER half of the fix, which is the easier one to get
// wrong and the harder one to hear in a unit test: the playback side must
// still run EXACTLY ONCE per tick. Playback is paced by the output device,
// not by capture backlog, so writing one playback frame per absorbed capture
// frame would make playback run fast. The playbackRing depth assertion below
// is what holds that line -- nothing reads that ring in this test, so its
// depth is exactly the number of mixer passes the loop performed.
//
// The tick source is hand-driven (Manager.dspTick) rather than the real
// FrameDuration ticker: the assertions are about what ONE tick does, and a
// sleep-based version would be a race between the test and the scheduler on
// the one path in this package where a flaky test is worse than none.
func TestDSPLoopCatchesUpAfterAStall(t *testing.T) {
	be := NewFakeBackend()
	be.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)
	m := NewManager(be, ManagerOptions{VUInterval: time.Hour, PollInterval: time.Hour})
	tick := make(chan time.Time)
	m.dspTick = tick
	sink := &markedSink{}
	m.AddSink(sink)
	m.SetPTT(true) // gate open with no start delay, so every frame reaches the sink

	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	// The stall: four frames arrive from the OS capture callback before the
	// DSP loop gets a single tick. A real time.Ticker would have coalesced
	// those missed ticks into exactly one, which is what this hand-driven
	// channel models.
	frame := make([]float32, FrameSamples)
	for i := range frame {
		frame[i] = 0.5
	}
	for i := 0; i < 4; i++ {
		be.PushFrame(frame)
	}

	tick <- time.Now() // the catch-up tick
	// A send completes exactly when dspLoop receives, so this second send
	// returning proves the first iteration finished. Stop() then joins the
	// goroutine, which both ends the second iteration and gives us a
	// happens-before edge to read everything below race-free.
	tick <- time.Now()
	m.Stop()

	sink.mu.Lock()
	marked, total := sink.marked, sink.total
	sink.mu.Unlock()

	if marked <= 1 {
		t.Fatalf("sink saw %d frame(s) of real capture data across 2 ticks; a 4-frame backlog "+
			"was not caught up -- it is now permanent latency", marked)
	}
	if marked > 4 {
		t.Fatalf("sink saw %d frames of real capture data but only 4 were ever written", marked)
	}
	if marked != 4 {
		t.Fatalf("sink saw %d of the 4 backlogged frames; one tick should absorb up to "+
			"1+maxCatchUpFrames = 4", marked)
	}
	// Tick 2 finds an empty ring and still runs the chain once on a
	// zero-filled frame, exactly as the pre-fix loop did on an underrun.
	if total != 5 {
		t.Fatalf("sink saw %d frames total, want 5 (4 real on the catch-up tick, 1 silent on the next)", total)
	}

	// THE REGRESSION THAT WOULD BE INAUDIBLE IN A TEST AND OBVIOUS ON
	// HARDWARE: playback must advance one frame per TICK, never one per
	// absorbed capture frame.
	m.mu.Lock()
	pb := m.playbackRing
	m.mu.Unlock()
	if got := pb.Available() / FrameSamples; got != 2 {
		t.Fatalf("playbackRing holds %d frames after 2 ticks, want exactly 2 -- the playback "+
			"side must run once per tick, not once per caught-up capture frame, or playback runs fast", got)
	}
}
