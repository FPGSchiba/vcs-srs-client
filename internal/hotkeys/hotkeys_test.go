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
	// failOn maps an action ID to the error Register should return for it.
	// IDs absent from this map register normally.
	failOn map[string]error
}

func newFake() *fakeRegistrar {
	return &fakeRegistrar{registered: map[string]Binding{}, failOn: map[string]error{}}
}

func (f *fakeRegistrar) Register(id string, c chord.Chord, hold bool, _ Handler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, fail := f.failOn[id]; fail {
		return err
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
	f.failOn["global.ptt"] = errors.New("permission denied")
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

func TestFailedRecordsEveryFailingAction(t *testing.T) {
	f := newFake()
	f.failOn["global.ptt"] = errors.New("no OS key mapping for \"Numpad7\"")
	f.failOn["global.mute_toggle"] = errors.New("no OS key mapping for \"PageUp\"")
	m := New(f, nopHandler{})

	err := m.Apply(map[string]Binding{
		"global.ptt":         {mustChord(t, "F1"), true},
		"global.mute_toggle": {mustChord(t, "M"), false},
		"global.deafen":      {mustChord(t, "F2"), false},
	})
	if err == nil {
		t.Fatal("Apply should report a failure")
	}

	failed := m.Failed()
	if len(failed) != 2 {
		t.Fatalf("Failed() has %d entries, want 2: %v", len(failed), failed)
	}
	if _, ok := failed["global.ptt"]; !ok {
		t.Error("Failed() missing global.ptt")
	}
	if _, ok := failed["global.mute_toggle"]; !ok {
		t.Error("Failed() missing global.mute_toggle")
	}
	if _, ok := failed["global.deafen"]; ok {
		t.Error("Failed() should not include the action that registered cleanly")
	}
	if f.count() != 1 {
		t.Errorf("registered %d, want 1 (only global.deafen)", f.count())
	}
}

func TestFailedEmptyAfterCleanApply(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	if err := m.Apply(map[string]Binding{
		"global.ptt": {mustChord(t, "F1"), true},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if failed := m.Failed(); len(failed) != 0 {
		t.Errorf("Failed() = %v, want empty after a clean Apply", failed)
	}
}

func TestFailedReturnsACopy(t *testing.T) {
	f := newFake()
	f.failOn["global.ptt"] = errors.New("permission denied")
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{"global.ptt": {mustChord(t, "F1"), true}})

	failed := m.Failed()
	failed["global.ptt"] = "tampered"
	failed["injected"] = "should not appear"

	again := m.Failed()
	if again["global.ptt"] == "tampered" {
		t.Error("mutating the returned map affected Manager's internal state")
	}
	if _, ok := again["injected"]; ok {
		t.Error("mutating the returned map injected a new entry into Manager's internal state")
	}
}

func TestFailedClearedByCleanApply(t *testing.T) {
	f := newFake()
	f.failOn["global.ptt"] = errors.New("permission denied")
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{"global.ptt": {mustChord(t, "F1"), true}})
	if len(m.Failed()) != 1 {
		t.Fatal("expected the initial failure to be recorded")
	}

	delete(f.failOn, "global.ptt")
	if err := m.Apply(map[string]Binding{"global.ptt": {mustChord(t, "F2"), true}}); err != nil {
		t.Fatalf("Apply after fixing the binding: %v", err)
	}
	if failed := m.Failed(); len(failed) != 0 {
		t.Errorf("Failed() = %v, want empty once the user fixes the binding", failed)
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
