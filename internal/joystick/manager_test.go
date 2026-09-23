package joystick

import (
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	pollErr error
	closed  int

	// pollDelay, inFlight and maxInFlight instrument concurrent-access
	// detection for TestBeginCaptureNeverCallsSourceWhileTheLoopIsRunning.
	// They are zero-value/no-op for every other test in this file.
	//
	// inFlight/maxInFlight are updated OUTSIDE mu, deliberately: mu would
	// itself serialise overlapping Poll() calls and hide the exact bug this
	// exists to catch (two goroutines both inside Poll() at once). A real
	// backend -- vendored DirectInput COM objects, evdev file handles -- has
	// no such internal mutex, so two concurrent callers there is undefined
	// behaviour, not just slow.
	pollDelay   time.Duration
	inFlight    int32
	maxInFlight int32
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		devices: []Device{{ID: tdev, Name: "Test Stick"}},
		held:    map[trigger.JoyButton]struct{}{},
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
}

func (f *fakeSource) Devices() ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Device(nil), f.devices...), nil
}

func (f *fakeSource) Poll() (State, error) {
	n := atomic.AddInt32(&f.inFlight, 1)
	defer atomic.AddInt32(&f.inFlight, -1)
	for {
		prev := atomic.LoadInt32(&f.maxInFlight)
		if n <= prev {
			break
		}
		if atomic.CompareAndSwapInt32(&f.maxInFlight, prev, n) {
			break
		}
	}
	if f.pollDelay > 0 {
		time.Sleep(f.pollDelay)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pollErr != nil {
		return State{}, f.pollErr
	}
	held := make(map[trigger.JoyButton]struct{}, len(f.held))
	for k := range f.held {
		held[k] = struct{}{}
	}
	return State{Held: held}, nil
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

// seqEvent is one Handler call, tagged with a monotonic sequence number so
// ordering can be asserted regardless of wall-clock resolution.
type seqEvent struct {
	seq  int64
	id   string
	edge string // "P" or "R"
}

// orderedRecorder is like recorder but tags every edge with a sequence
// number, so a test can assert ORDER (never Released before its matching
// Pressed) and not just counts. Its own mutex only protects the slice
// append; it makes no assumption about which goroutine calls it.
type orderedRecorder struct {
	mu     sync.Mutex
	seq    int64
	events []seqEvent
}

func (r *orderedRecorder) Pressed(id string)  { r.record(id, "P") }
func (r *orderedRecorder) Released(id string) { r.record(id, "R") }

func (r *orderedRecorder) record(id, edge string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	r.events = append(r.events, seqEvent{seq: r.seq, id: id, edge: edge})
}

func (r *orderedRecorder) snapshot() []seqEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]seqEvent(nil), r.events...)
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// assertNeverReleasedBeforePressed walks the recorded edges in sequence order
// and fails the instant any action shows a Released with no outstanding
// Pressed, or two Pressed in a row with no Released between -- the exact
// shape of the commit-then-notify race between tick() and
// Suspend()/Apply()/Close(): a concurrent Suspend can take an action out of
// the active set and emit its Released before tick's own Pressed call for the
// same transition actually runs.
func assertNeverReleasedBeforePressed(t *testing.T, events []seqEvent) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no press/release edges recorded -- test did not exercise anything")
	}
	held := map[string]bool{}
	for _, e := range events {
		switch e.edge {
		case "P":
			if held[e.id] {
				t.Fatalf("seq %d: Pressed(%s) observed while already pressed with no Released between", e.seq, e.id)
			}
			held[e.id] = true
		case "R":
			if !held[e.id] {
				t.Fatalf("seq %d: Released(%s) observed before its matching Pressed -- commit-then-notify race", e.seq, e.id)
			}
			held[e.id] = false
		}
	}
}

// TestConcurrentSuspendNeverReordersReleaseBeforePress runs the REAL poll
// loop via Start() -- every other test in this file drives tick()
// synchronously and cannot exhibit this by construction -- while hammering
// Suspend/Resume/Apply from other goroutines. This is exactly the shape
// Suspend() exists for: the UI opens a bind-capture while Start()'s loop
// keeps running.
//
// -race cannot see the bug this pins: every shared field is correctly
// mutex-protected. The bug is a LOGICAL ordering race between two
// independently-unlocked commit-then-notify sequences (tick() and
// Suspend()/Apply()/Close()), each internally race-free.
func TestConcurrentSuspendNeverReordersReleaseBeforePress(t *testing.T) {
	src := newFakeSource()
	rec := &orderedRecorder{}
	m := New(src, rec, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.PollInterval = 200 * time.Microsecond
	m.RediscoverInterval = time.Hour // keep rediscovery out of the way

	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	m.Start()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Goroutine 1: hammer the physical button up and down.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			src.hold(tbtn(3))
			src.release(tbtn(3))
		}
	}()

	// Goroutine 2: hammer Suspend/Resume -- the exact operation whose
	// takeActiveLocked() races tick()'s commit-then-notify window.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			m.Suspend()
			m.Resume()
		}
	}()

	// Goroutine 3: hammer Apply with the same binding table -- Apply()
	// releases-then-lets-the-next-tick-re-press exactly like Suspend/Resume,
	// so it races the same window.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
	m.Close()

	assertNeverReleasedBeforePressed(t, rec.snapshot())
}

// TestPressKindDoesNotReFireAcrossAnApplyWhileHeld is the I2 guard.
//
// takeActiveLocked used to empty ALL of m.active while returning only the
// hold actions that owe a Released. A press-kind action bound to a button
// that was still physically down therefore looked newly active on the very
// next tick and fired a SECOND Pressed with no Released between them.
//
// Apply is not a rare event: any server radio-list update reaches
// App.RefreshKeybinds -> applyHotkeys -> jm.Apply. So a mute-toggle on a
// HOTAS button, held while routine server traffic lands, toggled twice and
// did nothing -- "my mute button did nothing". The keyboard path cannot do
// this at all (it is event-driven and never re-presses), so it was a pure
// source asymmetry.
func TestPressKindDoesNotReFireAcrossAnApplyWhileHeld(t *testing.T) {
	m, src, rec := testManager(t)
	binds := map[string][]Binding{"global.mute_toggle": {pressBind(4)}}
	m.Apply(binds)

	src.hold(tbtn(4))
	m.tick()
	eq(t, rec.events(), []string{"down:global.mute_toggle"})

	// An unrelated reapply -- the server added a radio -- while the button is
	// still down.
	m.Apply(binds)
	m.tick()
	m.tick()
	eq(t, rec.events(), []string{"down:global.mute_toggle"})

	// The button is still genuinely latched: releasing and pressing again is
	// a new toggle, and must still fire.
	src.release(tbtn(4))
	m.tick()
	src.hold(tbtn(4))
	m.tick()
	eq(t, rec.events(), []string{"down:global.mute_toggle", "down:global.mute_toggle"})
}

// TestPressKindDoesNotReFireAcrossASuspendWhileHeld is the Suspend half of
// I2: Suspend shares takeActiveLocked with Apply, so a capture opened and
// closed while a press-kind button is held used to fire the action a second
// time on resume.
func TestPressKindDoesNotReFireAcrossASuspendWhileHeld(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.mute_toggle": {pressBind(4)}})

	src.hold(tbtn(4))
	m.tick()
	eq(t, rec.events(), []string{"down:global.mute_toggle"})

	m.Suspend()
	m.Resume()
	m.tick()
	eq(t, rec.events(), []string{"down:global.mute_toggle"})
}
