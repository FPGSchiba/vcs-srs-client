package joystick

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

const tdev = trigger.DeviceID("stick-c3")

func tbtn(b trigger.Button) trigger.JoyButton {
	return trigger.JoyButton{Device: tdev, Button: b}
}

type fakeSource struct {
	mu      sync.Mutex
	devices []Device
	held    map[trigger.JoyButton]struct{}
	conn    map[trigger.DeviceID]bool
	pollErr error
	closed  int
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		devices: []Device{{ID: tdev, Name: "Test Stick", Buttons: 32, Hats: 1}},
		held:    map[trigger.JoyButton]struct{}{},
		conn:    map[trigger.DeviceID]bool{tdev: true},
	}
}

func (f *fakeSource) hold(b trigger.JoyButton) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.held[b] = struct{}{}
}

func (f *fakeSource) release(b trigger.JoyButton) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.held, b)
}

// unplug models the device vanishing mid-hold: nothing is connected and
// nothing reads as held any more.
func (f *fakeSource) unplug() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = nil
	f.held = map[trigger.JoyButton]struct{}{}
	f.conn = map[trigger.DeviceID]bool{}
}

func (f *fakeSource) Devices() ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Device(nil), f.devices...), nil
}

func (f *fakeSource) Poll() (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pollErr != nil {
		return State{}, f.pollErr
	}
	held := make(map[trigger.JoyButton]struct{}, len(f.held))
	for k := range f.held {
		held[k] = struct{}{}
	}
	conn := make(map[trigger.DeviceID]bool, len(f.conn))
	for k, v := range f.conn {
		conn[k] = v
	}
	return State{Connected: conn, Held: held}, nil
}

func (f *fakeSource) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
}

type recorder struct {
	mu   sync.Mutex
	logs []string
}

func (r *recorder) Pressed(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, "down:"+id)
}

func (r *recorder) Released(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, "up:"+id)
}

func (r *recorder) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.logs...)
}

func testManager(t *testing.T) (*Manager, *fakeSource, *recorder) {
	t.Helper()
	src := newFakeSource()
	rec := &recorder{}
	m := New(src, rec, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return m, src, rec
}

func holdBind(b trigger.Button) Binding {
	return Binding{Joy: trigger.JoyBinding{Device: tdev, Button: b}, Hold: true}
}

func pressBind(b trigger.Button) Binding {
	return Binding{Joy: trigger.JoyBinding{Device: tdev, Button: b}, Hold: false}
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestPressAndReleaseEdges(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})

	m.tick()
	eq(t, rec.events(), nil)

	src.hold(tbtn(3))
	m.tick()
	m.tick() // held across two polls must NOT re-fire
	eq(t, rec.events(), []string{"down:global.ptt"})

	src.release(tbtn(3))
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
}

func TestPressKindNeverEmitsReleased(t *testing.T) {
	// Matches internal/hotkeys: only hold bindings owe a Released. Task 10's
	// refcount depends on this, so it is pinned here.
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.mute_toggle": {pressBind(4)}})

	src.hold(tbtn(4))
	m.tick()
	src.release(tbtn(4))
	m.tick()
	eq(t, rec.events(), []string{"down:global.mute_toggle"})
}

func TestDeviceVanishingWhileHeldReleases(t *testing.T) {
	// This is why the manager needs no stale-latch watchdog: polling makes
	// the failure self-healing, but only if we actually emit the edge.
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})

	src.hold(tbtn(3))
	m.tick()
	src.unplug()
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
}

func TestSuspendReleasesHeldActions(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})

	src.hold(tbtn(3))
	m.tick()
	m.Suspend()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})

	// While suspended, polling must do nothing at all.
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})

	// Resuming with the button still held re-presses it.
	m.Resume()
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt", "down:global.ptt"})
}

func TestApplyReleasesActionsItDropped(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	src.hold(tbtn(3))
	m.tick()

	// Rebinding the action away from the held button must release it, or the
	// refcount in Task 10 leaks and the action is dead for the session.
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(9)}})
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
}

func TestCloseReleasesHeldActions(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	src.hold(tbtn(3))
	m.tick()

	m.Close()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
	if src.closed != 1 {
		t.Errorf("source Close called %d times, want 1", src.closed)
	}
	m.Close() // must be safe twice
	if src.closed != 1 {
		t.Errorf("second Close reached the source (%d calls)", src.closed)
	}
}

func TestPollErrorIsRecordedNotPanicked(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	src.hold(tbtn(3))
	m.tick()

	src.mu.Lock()
	src.pollErr = errors.New("device read failed")
	src.mu.Unlock()
	m.tick()

	// A failing poll must release what it was holding: we can no longer
	// prove the button is down, and a stuck-open microphone is the worst
	// possible failure here.
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
	if m.LastErr() == nil {
		t.Error("LastErr = nil after a failing poll")
	}
}

func TestUnsupportedSourceReportsNotSupported(t *testing.T) {
	src := &unsupportedSource{}
	m := New(src, &recorder{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if m.Supported() {
		t.Error("Supported() = true for an unsupported source")
	}
	m.tick() // must not panic
}

type unsupportedSource struct{}

func (unsupportedSource) Devices() ([]Device, error) { return nil, ErrUnsupported }
func (unsupportedSource) Poll() (State, error)       { return State{}, ErrUnsupported }
func (unsupportedSource) Close()                     {}
