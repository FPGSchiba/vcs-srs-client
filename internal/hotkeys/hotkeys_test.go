package hotkeys

import (
	"errors"
	"sync"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

type fakeRegistrar struct {
	mu          sync.Mutex
	registered  map[string]Binding
	unregisters int
	failOn      string
	registerErr error
}

func newFake() *fakeRegistrar {
	return &fakeRegistrar{registered: map[string]Binding{}}
}

func (f *fakeRegistrar) Register(id string, c chord.Chord, hold bool, _ Handler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn == id {
		return f.registerErr
	}
	f.registered[id] = Binding{Chord: c, Hold: hold}
	return nil
}

func (f *fakeRegistrar) UnregisterAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = map[string]Binding{}
	f.unregisters++
}

func (f *fakeRegistrar) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.registered)
}

type nopHandler struct{}

func (nopHandler) Pressed(string)  {}
func (nopHandler) Released(string) {}

func mustChord(t *testing.T, s string) chord.Chord {
	t.Helper()
	c, err := chord.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return c
}

func TestApplyRegistersAll(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	err := m.Apply(map[string]Binding{
		"global.ptt":         {mustChord(t, "F1"), true},
		"global.mute_toggle": {mustChord(t, "M"), false},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if f.count() != 2 {
		t.Errorf("registered %d, want 2", f.count())
	}
	if !m.Registered() {
		t.Error("Registered() should be true after a clean Apply")
	}
}

func TestApplyClearsPreviousRegistrations(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{"a": {mustChord(t, "F1"), false}})
	m.Apply(map[string]Binding{"b": {mustChord(t, "F2"), false}})
	if f.count() != 1 {
		t.Errorf("registered %d after re-Apply, want 1", f.count())
	}
	if _, stale := f.registered["a"]; stale {
		t.Error("previous registration was not cleared")
	}
}

func TestSuspendUnregistersAndResumeRestores(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	binds := map[string]Binding{"global.ptt": {mustChord(t, "F1"), true}}
	m.Apply(binds)

	m.Suspend()
	if f.count() != 0 {
		t.Errorf("after Suspend registered %d, want 0", f.count())
	}
	if m.Registered() {
		t.Error("Registered() should be false while suspended")
	}

	if err := m.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if f.count() != 1 {
		t.Errorf("after Resume registered %d, want 1", f.count())
	}
}

func TestApplyWhileSuspendedDefersRegistration(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Suspend()
	m.Apply(map[string]Binding{"global.ptt": {mustChord(t, "F1"), true}})
	if f.count() != 0 {
		t.Error("Apply during suspension must not register with the OS")
	}
	if err := m.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if f.count() != 1 {
		t.Error("Resume must register the bindings applied while suspended")
	}
}

func TestApplySkipsZeroChords(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{
		"bound":   {mustChord(t, "F1"), false},
		"unbound": {chord.Chord{}, false},
	})
	if f.count() != 1 {
		t.Errorf("registered %d, want 1 (unbound must be skipped)", f.count())
	}
}

func TestRegistrationFailureIsRecordedNotFatal(t *testing.T) {
	f := newFake()
	f.failOn = "global.ptt"
	f.registerErr = errors.New("permission denied")
	m := New(f, nopHandler{})

	err := m.Apply(map[string]Binding{
		"global.ptt":         {mustChord(t, "F1"), true},
		"global.mute_toggle": {mustChord(t, "M"), false},
	})
	if err == nil {
		t.Error("Apply should report the failure")
	}
	if m.LastError() == nil {
		t.Error("LastError should hold the failure")
	}
	if f.count() != 1 {
		t.Error("the binding that could register should still have registered")
	}
	if m.Registered() {
		t.Error("Registered() should be false when any registration failed")
	}
}

func TestSuspendIsIdempotent(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{"a": {mustChord(t, "F1"), false}})
	m.Suspend()
	m.Suspend()
	if err := m.Resume(); err != nil {
		t.Fatalf("Resume after double Suspend: %v", err)
	}
	if f.count() != 1 {
		t.Error("one Resume should restore after repeated Suspend")
	}
}
