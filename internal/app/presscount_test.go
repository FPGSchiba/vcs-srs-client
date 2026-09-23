package app

import (
	"sync"
	"testing"
)

func TestPressCountSingleSource(t *testing.T) {
	p := newPressCount()
	if !p.press("global.ptt") {
		t.Error("first press = false, want true (0->1 must emit)")
	}
	if !p.release("global.ptt") {
		t.Error("matching release = false, want true (1->0 must emit)")
	}
}

func TestPressCountTwoSourcesEmitOnce(t *testing.T) {
	// The defect this type exists to prevent: keyboard and joystick both
	// holding global.ptt must produce exactly one down and one up.
	p := newPressCount()

	if !p.press("global.ptt") {
		t.Fatal("keyboard press = false, want true")
	}
	if p.press("global.ptt") {
		t.Error("joystick press = true, want false: the action is already held")
	}
	if p.release("global.ptt") {
		t.Error("releasing ONE source emitted a release while the other still holds it")
	}
	if !p.release("global.ptt") {
		t.Error("releasing the last source = false, want true")
	}
}

func TestPressCountNeverGoesNegative(t *testing.T) {
	// An unbalanced Released (a manager bug, a forced release after the
	// action was already let go) must not make the NEXT press silent.
	p := newPressCount()
	if p.release("global.ptt") {
		t.Error("release with nothing held = true, want false")
	}
	if !p.press("global.ptt") {
		t.Error("press after a spurious release = false, want true")
	}
}

func TestPressCountActionsAreIndependent(t *testing.T) {
	p := newPressCount()
	p.press("global.ptt")
	if !p.press("radio.1.ptt") {
		t.Error("a different action was affected by the first action's count")
	}
}

func TestPressCountIsRaceFree(t *testing.T) {
	// Both managers call into this from their own goroutines.
	p := newPressCount()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				p.press("global.ptt")
				p.release("global.ptt")
			}
		}()
	}
	wg.Wait()
}
