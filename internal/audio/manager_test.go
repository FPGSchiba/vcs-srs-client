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
