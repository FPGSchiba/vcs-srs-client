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
