package connhealth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
)

// recorder collects every Snapshot the Monitor publishes.
type recorder struct {
	mu    sync.Mutex
	snaps []connhealth.Snapshot
}

func (r *recorder) add(s connhealth.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snaps = append(r.snaps, s)
}

func (r *recorder) last() (connhealth.Snapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.snaps) == 0 {
		return connhealth.Snapshot{}, false
	}
	return r.snaps[len(r.snaps)-1], true
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.snaps)
}

func TestNew_StartsDisconnectedWithUnknownRTT(t *testing.T) {
	m := connhealth.New(connhealth.Options{})
	got := m.Snapshot()

	if got.Control.State != connhealth.StateDisconnected {
		t.Errorf("Control.State = %q, want %q", got.Control.State, connhealth.StateDisconnected)
	}
	if got.Control.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Control.RTTMs = %d, want %d", got.Control.RTTMs, connhealth.RTTUnknown)
	}
	// Healthy is optimistic-false until a probe has actually answered:
	// claiming health nothing has confirmed is the failure mode
	// useSettingsSync's getHotkeyState comment already calls out.
	if got.Control.Healthy {
		t.Error("Control.Healthy = true on a fresh Monitor, want false")
	}
	if got.Voice.State != connhealth.StateUnavailable {
		t.Errorf("Voice.State = %q, want %q", got.Voice.State, connhealth.StateUnavailable)
	}
	if got.Voice.Available {
		t.Error("Voice.Available = true on a fresh Monitor, want false")
	}
}

func TestSetControlState_PublishesAndResetsProbeState(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{OnChange: rec.add})

	m.SetControlState(connhealth.StateConnected)

	got, ok := rec.last()
	if !ok {
		t.Fatal("SetControlState published nothing")
	}
	if got.Control.State != connhealth.StateConnected {
		t.Errorf("Control.State = %q, want %q", got.Control.State, connhealth.StateConnected)
	}
	// A fresh connection has measured nothing yet -- carrying the previous
	// connection's RTT forward would show a healthy ping for a link that has
	// not answered once. Same reasoning as voice.Session.resetBindingLocked.
	if got.Control.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Control.RTTMs = %d after reconnect, want %d", got.Control.RTTMs, connhealth.RTTUnknown)
	}
}

func TestSetVoiceState_CarriesErrorAndAvailability(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{OnChange: rec.add})

	m.SetVoiceState("retrying", "voice: no HELLO_ACK after 5 attempts", true)

	got, _ := rec.last()
	if got.Voice.State != "retrying" {
		t.Errorf("Voice.State = %q, want %q", got.Voice.State, "retrying")
	}
	if got.Voice.Error != "voice: no HELLO_ACK after 5 attempts" {
		t.Errorf("Voice.Error = %q, want the transition error", got.Voice.Error)
	}
	if !got.Voice.Available {
		t.Error("Voice.Available = false, want true")
	}
}

func TestSetVoiceState_UnavailableForcesUnknownRTT(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{
		OnChange: rec.add,
		VoiceRTT: func() time.Duration { return 42 * time.Millisecond },
	})

	m.SetVoiceState(connhealth.StateConnected, "", true)
	m.SetVoiceState(connhealth.StateUnavailable, "", false)

	got, _ := rec.last()
	if got.Voice.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Voice.RTTMs = %d while unavailable, want %d", got.Voice.RTTMs, connhealth.RTTUnknown)
	}
	if got.Voice.Healthy {
		t.Error("Voice.Healthy = true while unavailable, want false")
	}
}

func TestSetServer_Publishes(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{OnChange: rec.add})

	m.SetServer("127.0.0.1:5002")

	got, _ := rec.last()
	if got.Server != "127.0.0.1:5002" {
		t.Errorf("Server = %q, want %q", got.Server, "127.0.0.1:5002")
	}
}

func TestSetControlState_NoOpWhenUnchanged(t *testing.T) {
	// Re-asserting the state the Monitor is already in must not publish: the
	// control path calls SetControlState on every emission, including the
	// redundant reconnecting -> reconnecting a failed Reconnect produces.
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{OnChange: rec.add})

	m.SetControlState(connhealth.StateConnected)
	before := rec.count()
	m.SetControlState(connhealth.StateConnected)

	if rec.count() != before {
		t.Errorf("published %d times for a repeated state, want %d", rec.count(), before)
	}
}

// tickFor drives one probe cycle synchronously. Tick is exported for exactly
// this: the loop is a thin wrapper around it, so tests never sleep.
func TestTick_SuccessRecordsRTTAndHealth(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{
		OnChange: rec.add,
		Ping: func(_ context.Context, _ int64) (int64, error) {
			return 8, nil
		},
	})
	m.SetControlState(connhealth.StateConnected)

	m.Tick(context.Background())

	got, _ := rec.last()
	if got.Control.RTTMs != 8 {
		t.Errorf("Control.RTTMs = %d, want 8", got.Control.RTTMs)
	}
	if !got.Control.Healthy {
		t.Error("Control.Healthy = false after a successful probe, want true")
	}
}

func TestTick_EchoesPreviousRTTToPing(t *testing.T) {
	// The echo is the sole input to the server's per-client latency map
	// (srs/srs_service.go Ping writes client.LatencyToControlMs = LastRttMs),
	// so dropping it leaves that map at zero for every VCS client forever.
	//
	// F2 (Phase 6 whole-branch review): the FIRST echo of every connection
	// must be 0, not connhealth.RTTUnknown (-1). RTTUnknown is this
	// package's internal sentinel for "nothing measured" -- echoing it
	// verbatim put -1 straight onto the wire via control.PingRequest and
	// into every peer's roster as this client's LatencyToControlMs.
	var seen []int64
	m := connhealth.New(connhealth.Options{
		Ping: func(_ context.Context, last int64) (int64, error) {
			seen = append(seen, last)
			return 12, nil
		},
	})
	m.SetControlState(connhealth.StateConnected)

	m.Tick(context.Background())
	m.Tick(context.Background())

	if len(seen) != 2 {
		t.Fatalf("Ping called %d times, want 2", len(seen))
	}
	if seen[0] != 0 {
		t.Errorf("first echo = %d, want 0 (RTTUnknown clamped, not the raw sentinel %d)",
			seen[0], connhealth.RTTUnknown)
	}
	if seen[1] != 12 {
		t.Errorf("second echo = %d, want 12 (a real measurement, unchanged by the clamp)", seen[1])
	}
}

func TestTick_FailuresBelowThresholdOnlyClearHealth(t *testing.T) {
	rec := &recorder{}
	lost := 0
	m := connhealth.New(connhealth.Options{
		FailureThreshold: 3,
		OnChange:         rec.add,
		OnLoss:           func() { lost++ },
		Ping: func(_ context.Context, _ int64) (int64, error) {
			return 0, errors.New("deadline exceeded")
		},
	})
	m.SetControlState(connhealth.StateConnected)

	m.Tick(context.Background())
	m.Tick(context.Background())

	got, _ := rec.last()
	if got.Control.State != connhealth.StateConnected {
		t.Errorf("Control.State = %q after 2 failures, want it unchanged at %q",
			got.Control.State, connhealth.StateConnected)
	}
	if got.Control.Healthy {
		t.Error("Control.Healthy = true after a failed probe, want false")
	}
	if lost != 0 {
		t.Errorf("OnLoss fired %d times below the threshold, want 0", lost)
	}
}

func TestTick_ThresholdFiresLossExactlyOnce(t *testing.T) {
	lost := 0
	m := connhealth.New(connhealth.Options{
		FailureThreshold: 3,
		OnLoss:           func() { lost++ },
		Ping: func(_ context.Context, _ int64) (int64, error) {
			return 0, errors.New("deadline exceeded")
		},
	})
	m.SetControlState(connhealth.StateConnected)

	for i := 0; i < 6; i++ {
		m.Tick(context.Background())
	}

	if lost != 1 {
		t.Errorf("OnLoss fired %d times, want exactly 1", lost)
	}
}

// Review Focus #1. A link that drops one probe every so often is not a link
// that is down. Without a reset on success, failures accumulate across the
// whole session and eventually declare loss on a healthy connection.
func TestTick_SuccessResetsTheFailureCounter(t *testing.T) {
	lost := 0
	fail := true
	m := connhealth.New(connhealth.Options{
		FailureThreshold: 3,
		OnLoss:           func() { lost++ },
		Ping: func(_ context.Context, _ int64) (int64, error) {
			if fail {
				return 0, errors.New("deadline exceeded")
			}
			return 7, nil
		},
	})
	m.SetControlState(connhealth.StateConnected)

	// Two failures, one success, two failures. Five ticks, never three in a
	// row, so loss must never be declared.
	m.Tick(context.Background())
	m.Tick(context.Background())
	fail = false
	m.Tick(context.Background())
	fail = true
	m.Tick(context.Background())
	m.Tick(context.Background())

	if lost != 0 {
		t.Errorf("OnLoss fired %d times across a flapping link, want 0", lost)
	}
}

func TestTick_DoesNotProbeWhileDisconnected(t *testing.T) {
	// Hammering a server we already know is gone is pure noise, and a probe
	// against a nil control client can only ever return an error, which
	// would re-fire loss on a link already reported lost.
	calls := 0
	m := connhealth.New(connhealth.Options{
		Ping: func(_ context.Context, _ int64) (int64, error) {
			calls++
			return 1, nil
		},
	})
	// Never set connected: the Monitor starts disconnected.

	m.Tick(context.Background())
	m.Tick(context.Background())

	if calls != 0 {
		t.Errorf("Ping called %d times while disconnected, want 0", calls)
	}
}

// Review Focus #4. voice.Session.RTT() returns 0 before the first answered
// keepalive and after every rebind (resetBindingLocked clears it precisely
// so the UI never shows a healthy ping for a broken session). Passed through
// as a number that is a plausible-looking "0ms".
func TestTick_VoiceZeroRTTIsUnknownNotZero(t *testing.T) {
	rec := &recorder{}
	rtt := time.Duration(0)
	m := connhealth.New(connhealth.Options{
		OnChange: rec.add,
		Ping:     func(_ context.Context, _ int64) (int64, error) { return 3, nil },
		VoiceRTT: func() time.Duration { return rtt },
	})
	m.SetControlState(connhealth.StateConnected)
	m.SetVoiceState(connhealth.StateConnected, "", true)

	m.Tick(context.Background())
	got, _ := rec.last()
	if got.Voice.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Voice.RTTMs = %d for a zero RTT, want %d", got.Voice.RTTMs, connhealth.RTTUnknown)
	}

	rtt = 24 * time.Millisecond
	m.Tick(context.Background())
	got, _ = rec.last()
	if got.Voice.RTTMs != 24 {
		t.Errorf("Voice.RTTMs = %d, want 24", got.Voice.RTTMs)
	}
}

func TestTick_VoiceRTTNotReadWhileUnavailable(t *testing.T) {
	calls := 0
	m := connhealth.New(connhealth.Options{
		Ping:     func(_ context.Context, _ int64) (int64, error) { return 3, nil },
		VoiceRTT: func() time.Duration { calls++; return 5 * time.Millisecond },
	})
	m.SetControlState(connhealth.StateConnected)
	m.SetVoiceState(connhealth.StateUnavailable, "", false)

	m.Tick(context.Background())

	if calls != 0 {
		t.Errorf("VoiceRTT called %d times while unavailable, want 0", calls)
	}
}

func TestStartStop_IsIdempotentAndJoins(t *testing.T) {
	m := connhealth.New(connhealth.Options{
		Interval: 5 * time.Millisecond,
		Ping:     func(_ context.Context, _ int64) (int64, error) { return 1, nil },
	})
	m.SetControlState(connhealth.StateConnected)

	m.Start()
	m.Start() // second call is a no-op, not a second goroutine

	// Give the loop a chance to run at least once. This is the only test in
	// the package that touches the real clock; everything else drives Tick.
	time.Sleep(30 * time.Millisecond)

	m.Stop()
	m.Stop() // second call must not panic on an already-closed channel

	if got := m.Snapshot(); got.Control.RTTMs != 1 {
		t.Errorf("Control.RTTMs = %d after the loop ran, want 1", got.Control.RTTMs)
	}
}

// Review Finding (a). publish's contract is that emitMu is acquired BEFORE
// mu and held across both the mutation and the OnChange delivery. If a
// setter instead released mu before acquiring emitMu (the pre-fix code), a
// second, fully independent setter could commit its own mutation -- and
// even attempt delivery -- while the first delivery was still in flight,
// letting delivery order and commit order disagree.
//
// This proves the lock discipline directly, per the review's suggested
// form: while the first OnChange is blocked in flight, a second setter must
// not be able to reach its own mutation (observable here as Snapshot()
// still reading the first value), because it cannot acquire emitMu until
// the first call releases it.
func TestPublish_SecondSetterCannotCommitWhileFirstDeliveryInFlight(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once

	m := connhealth.New(connhealth.Options{
		OnChange: func(s connhealth.Snapshot) {
			if s.Server == "first" {
				enteredOnce.Do(func() { close(entered) })
				<-release
			}
		},
	})

	go m.SetServer("first")
	<-entered // OnChange("first") is in flight and blocked on release.

	secondDone := make(chan struct{})
	go func() {
		m.SetServer("second")
		close(secondDone)
	}()

	// Give the second call every opportunity to run. Under the pre-fix
	// locking (mu released before emitMu acquired), this is ample time for
	// its mutation to land even though delivery is still blocked.
	select {
	case <-secondDone:
		t.Fatal("second SetServer completed while the first OnChange was still in flight")
	case <-time.After(20 * time.Millisecond):
	}

	if got := m.Snapshot().Server; got != "first" {
		t.Fatalf("Snapshot().Server = %q while the first OnChange was still in flight, want %q -- "+
			"the second setter committed before the first commit-and-deliver sequence finished",
			got, "first")
	}

	close(release)
	<-secondDone

	if got := m.Snapshot().Server; got != "second" {
		t.Errorf("Snapshot().Server = %q after both setters ran, want %q", got, "second")
	}
}

// Review Finding (b). Ping runs with no lock held, so a concurrent
// SetControlState can land while it is in flight. Tick must re-check the
// control state after re-acquiring mu and discard a probe result that no
// longer belongs to the current connection -- not the RTT, not Healthy, not
// the failure count, not loss.
func TestTick_DiscardsStaleProbeAfterConcurrentDisconnect(t *testing.T) {
	rec := &recorder{}
	var m *connhealth.Monitor
	m = connhealth.New(connhealth.Options{
		OnChange: rec.add,
		Ping: func(_ context.Context, _ int64) (int64, error) {
			// Simulate the control plane dropping while this probe is in
			// flight: the transition lands, and is delivered, before Ping
			// itself returns success.
			m.SetControlState(connhealth.StateDisconnected)
			return 9, nil
		},
	})
	m.SetControlState(connhealth.StateConnected)

	m.Tick(context.Background())

	got, ok := rec.last()
	if !ok {
		t.Fatal("Tick published nothing")
	}
	if got.Control.State != connhealth.StateDisconnected {
		t.Errorf("Control.State = %q, want %q", got.Control.State, connhealth.StateDisconnected)
	}
	if got.Control.Healthy {
		t.Error("Control.Healthy = true for a stale probe applied after a concurrent disconnect, want false")
	}
	if got.Control.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Control.RTTMs = %d for a stale probe applied after a concurrent disconnect, want %d",
			got.Control.RTTMs, connhealth.RTTUnknown)
	}
}

// Review Finding (Critical, round 2). Task 3's real owner reacts to OnLoss
// synchronously, on the SAME goroutine, by calling SetControlState -- that
// is the "single normal path" OnLoss's doc comment describes. If Tick still
// held emitMu when OnLoss fired, that call would block forever on a mutex
// its own goroutine already holds: sync.Mutex is not reentrant.
//
// Against the pre-fix code (emitMu held via defer across the whole rest of
// Tick, including the OnLoss call) this test does not fail an assertion --
// it hangs. It is written to survive that: the reproduction runs in its own
// goroutine, and the test fails on a bounded timeout instead of hanging the
// suite.
func TestTick_OnLossReentrantSetControlStateDoesNotDeadlock(t *testing.T) {
	rec := &recorder{}
	var m *connhealth.Monitor
	m = connhealth.New(connhealth.Options{
		FailureThreshold: 3,
		OnChange:         rec.add,
		Ping: func(_ context.Context, _ int64) (int64, error) {
			return 0, errors.New("deadline exceeded")
		},
		OnLoss: func() {
			// Exactly Task 3's chain: the owner's synchronous reaction to
			// loss is SetControlState, on the same goroutine Tick called
			// OnLoss from.
			m.SetControlState(connhealth.StateDisconnected)
		},
	})
	m.SetControlState(connhealth.StateConnected)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 3; i++ {
			m.Tick(context.Background())
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Tick did not return within 2s -- OnLoss's reentrant SetControlState deadlocked on emitMu")
	}

	got, ok := rec.last()
	if !ok {
		t.Fatal("published nothing")
	}
	if got.Control.State != connhealth.StateDisconnected {
		t.Errorf("Control.State = %q, want %q", got.Control.State, connhealth.StateDisconnected)
	}
}

// Review Finding (Important, round 3). Dropping Tick's top-level defer to
// get OnLoss outside the locked region also dropped the unwind guarantee: a
// panicking OnChange left emitMu locked forever, wedging every subsequent
// Tick, SetServer, SetControlState and SetVoiceState -- the same class of
// permanent deadlock as round 2's finding, just reached by a panic instead
// of a reentrant call.
//
// Recovered here (as a supervisor around the ticker goroutine would) so the
// test process itself survives; the assertion is that the Monitor's locks
// survive too. Guarded with a bounded timeout, same shape as the round-2
// reentrancy test, so a regression fails instead of hanging the suite.
func TestTick_PanickingOnChangeStillReleasesEmitMu(t *testing.T) {
	// panicOnChange is toggled from the test's own goroutine only, strictly
	// before each call whose OnChange delivery it controls: false while
	// establishing the connected state below (that publish must not panic,
	// or there is nothing left to Tick), true for the one Tick call this
	// test exists to break, then false again so the final SetControlState
	// can prove the lock survived rather than re-triggering the panic.
	panicOnChange := false
	m := connhealth.New(connhealth.Options{
		OnChange: func(connhealth.Snapshot) {
			if panicOnChange {
				panic("boom")
			}
		},
		Ping: func(_ context.Context, _ int64) (int64, error) {
			return 5, nil
		},
	})
	m.SetControlState(connhealth.StateConnected)

	panicOnChange = true
	func() {
		defer func() { _ = recover() }()
		m.Tick(context.Background())
	}()
	panicOnChange = false

	done := make(chan struct{})
	go func() {
		m.SetControlState(connhealth.StateDisconnected)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SetControlState did not return within 2s -- a panicking OnChange left emitMu locked")
	}
}
