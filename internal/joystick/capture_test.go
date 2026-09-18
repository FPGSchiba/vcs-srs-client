package joystick

import (
	"testing"

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
