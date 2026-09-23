package joystick

import (
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func captureManager(t *testing.T) (*Manager, *fakeSource, *recorder) {
	t.Helper()
	return testManager(t)
}

func TestCaptureBareButton(t *testing.T) {
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	src.hold(tbtn(11))
	m.tick()
	if got != nil {
		t.Fatal("capture completed on press; it must complete on release")
	}
	src.release(tbtn(11))
	m.tick()

	if got == nil {
		t.Fatal("capture did not complete on release")
	}
	if got.Binding.Device != tdev || got.Binding.Button != 11 {
		t.Errorf("captured %+v, want stick-c3 button 11", got.Binding)
	}
	if got.Binding.Modifier != nil {
		t.Errorf("bare capture produced a modifier: %+v", got.Binding.Modifier)
	}
}

func TestCaptureInfersModifierFromHoldOrder(t *testing.T) {
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	src.hold(tbtn(5)) // held FIRST -> modifier
	m.tick()
	src.hold(tbtn(3)) // pressed SECOND -> main
	m.tick()
	src.release(tbtn(3))
	src.release(tbtn(5))
	m.tick()

	if got == nil {
		t.Fatal("capture did not complete")
	}
	if got.Binding.Button != 3 {
		t.Errorf("main = %v, want button 3", got.Binding.Button)
	}
	if got.Binding.Modifier == nil || got.Binding.Modifier.Button != 5 {
		t.Errorf("modifier = %+v, want button 5", got.Binding.Modifier)
	}
}

func TestCaptureIgnoresButtonsAlreadyHeldAtBaseline(t *testing.T) {
	// A button down when capture starts must not register -- otherwise a
	// user holding their PTT while opening settings binds it instantly.
	m, src, _ := captureManager(t)
	src.hold(tbtn(9))

	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })
	m.tick()
	src.release(tbtn(9))
	m.tick()

	if got != nil {
		t.Errorf("baseline-held button was captured: %+v", got.Binding)
	}
}

func TestSameTickTieBreakIsDeterministic(t *testing.T) {
	// Both buttons first appear in the SAME sample, so hold order is
	// unknowable. Lower (DeviceID, Button) must win the modifier slot, or
	// capture depends on poll alignment.
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	src.hold(tbtn(7))
	src.hold(tbtn(2))
	m.tick()
	src.release(tbtn(7))
	src.release(tbtn(2))
	m.tick()

	if got == nil {
		t.Fatal("capture did not complete")
	}
	if got.Binding.Modifier == nil || got.Binding.Modifier.Button != 2 {
		t.Errorf("modifier = %+v, want the lower-sorted button 2", got.Binding.Modifier)
	}
	if got.Binding.Button != 7 {
		t.Errorf("main = %v, want button 7", got.Binding.Button)
	}
}

func TestCaptureSuppressesHandlerDispatch(t *testing.T) {
	// Pressing a button to bind it must never also fire the action it is
	// being bound to.
	m, src, rec := captureManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	m.BeginCapture(func(Captured) {})

	src.hold(tbtn(3))
	m.tick()
	src.release(tbtn(3))
	m.tick()

	if ev := rec.events(); len(ev) != 0 {
		t.Errorf("handler fired during capture: %v", ev)
	}
}

func TestCancelCaptureStopsCapturing(t *testing.T) {
	m, src, _ := captureManager(t)
	called := false
	m.BeginCapture(func(Captured) { called = true })
	m.CancelCapture()

	src.hold(tbtn(3))
	m.tick()
	src.release(tbtn(3))
	m.tick()

	if called {
		t.Error("capture completed after CancelCapture")
	}
}

func TestCaptureHatDirection(t *testing.T) {
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	hat := trigger.JoyButton{Device: tdev, Button: trigger.HatButton(0, 2)}
	src.hold(hat)
	m.tick()
	src.release(hat)
	m.tick()

	if got == nil || got.Binding.Button != trigger.HatButton(0, 2) {
		t.Errorf("hat capture = %+v, want hat 0 dir 2", got)
	}
}

// TestBeginCaptureNeverCallsSourceWhileTheLoopIsRunning runs the REAL poll
// loop via Start() -- every other test in this file drives tick() manually
// and cannot exercise this by construction -- while hammering
// BeginCapture/CancelCapture from another goroutine, exactly the shape a UI
// opening/closing a bind-capture dialog takes while the loop keeps polling.
//
// It pins down the two review findings against Task 7's first cut:
//
//  1. BeginCapture must NEVER call Source itself once the loop owns it --
//     the fake source's own internal mutex would silently serialise a
//     concurrent Poll() and hide the bug (as -race does, for the same
//     reason), so this counts actual concurrent ENTRIES into Poll() with an
//     instrumented counter that lives outside that mutex, matching how the
//     reviewer proved the original 401-overlap bug.
//  2. An action already active when BeginCapture is called must not have its
//     Released lost. The test deliberately leaves a capture armed (no
//     matching CancelCapture) before the physical release, which is exactly
//     the window Task 7's first cut lost the edge in: capturing() suppresses
//     tick()'s entire dispatch block, so nothing would otherwise reconcile
//     the action between BeginCapture and the eventual CancelCapture/release.
//
// This test FAILS against the pre-fix BeginCapture (direct, unguarded
// m.src.Poll() call; no force-release on arm) and PASSES against the fix --
// see task-7-report.md for the captured before/after output.
func TestBeginCaptureNeverCallsSourceWhileTheLoopIsRunning(t *testing.T) {
	src := newFakeSource()
	src.pollDelay = time.Millisecond // widen the window so a real overlap would be caught
	rec := &recorder{}
	m := New(src, rec, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.PollInterval = time.Millisecond
	m.RediscoverInterval = time.Hour

	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	src.hold(tbtn(3)) // held BEFORE the loop ever starts

	m.Start()
	defer m.Close()

	// Let the loop observe the held button and press global.ptt at least
	// once, so there is a genuinely active, HOLD-kind action in play before
	// any capture is armed.
	time.Sleep(20 * time.Millisecond)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				// Leave a capture armed on exit -- see the release below.
				m.BeginCapture(func(Captured) {})
				return
			default:
			}
			m.BeginCapture(func(Captured) {})
			m.CancelCapture()
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	// A capture is now armed (the goroutine's last act above). Release the
	// physical button while it still is -- the exact scenario that lost the
	// edge pre-fix, since an armed capture suppresses tick()'s dispatch
	// entirely and nothing else would ever reconcile m.active.
	src.release(tbtn(3))
	time.Sleep(20 * time.Millisecond)
	m.CancelCapture()

	if max := atomic.LoadInt32(&src.maxInFlight); max > 1 {
		t.Errorf("Source saw %d concurrent Poll() calls, want at most 1 -- "+
			"BeginCapture must never touch Source while the poll loop owns it", max)
	}

	released := false
	for _, e := range rec.events() {
		if e == "up:global.ptt" {
			released = true
		}
	}
	if !released {
		t.Error("global.ptt's Released was lost while a capture was armed")
	}
}
