package audio

import (
	"errors"
	"fmt"
	"sync"
)

// ErrFakeBackendClosed is what every FakeBackend call returns once Close()
// has been called.
//
// This exists because the fake being MORE FORGIVING than reality hid a
// deterministic restart panic for the whole of Phase 4: Manager.Stop() used
// to call Backend.Close(), the malgo backend's Close Uninits/Frees the
// miniaudio context and nils it (backend_malgo.go), and a subsequent
// Start() -> Enumerate() would have nil-dereferenced on real hardware --
// while this fake happily kept serving calls after Close() and every test
// went green. A fake that accepts calls the real backend would crash on
// cannot prove anything about the real backend's lifecycle, so it refuses
// them instead. (Same asymmetry checkDeviceID below already closes for
// device ids.)
var ErrFakeBackendClosed = errors.New("audio: fake backend is closed")

// FakeBackend is an in-memory Backend for tests. It is compiled into the
// package (not a _test.go file) so other packages' tests can build a manager
// without hardware.
type FakeBackend struct {
	mu sync.Mutex

	inputs  []DeviceInfo
	outputs []DeviceInfo
	enumErr error

	onFrame  func([]float32)
	fill     func([]float32)
	recorded [][]float32
	failNext error
	closed   bool

	// captureOpens/playbackOpens record every id passed to OpenCapture /
	// OpenPlayback, in call order, whether or not the call succeeded -- so
	// a test can assert the backend genuinely received an open call
	// (rather than merely that Start() reported Running).
	captureOpens  []string
	playbackOpens []string

	// enumerateGate, when set (via BlockEnumerate), makes Enumerate block
	// until it's closed. Used to deterministically hold a Manager.Start()
	// call inside its "starting" window for tests (Fix B).
	enumerateGate chan struct{}

	// openCaptureGate, when set (via BlockNextOpenCapture), makes exactly
	// the next OpenCapture call block until it's closed -- AFTER already
	// recording itself in captureOpens, so CaptureOpens() reflects the
	// call as having genuinely started. Used to deterministically hold a
	// reopen (maybeReopenCapture) in flight for tests (Fix A round 3).
	openCaptureGate chan struct{}
}

func NewFakeBackend() *FakeBackend { return &FakeBackend{} }

// SetDevices replaces the enumeration result, simulating hot-plug.
func (b *FakeBackend) SetDevices(inputs, outputs []DeviceInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inputs, b.outputs = inputs, outputs
}

// FailEnumerate makes the next and subsequent Enumerate calls fail.
func (b *FakeBackend) FailEnumerate(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enumErr = err
}

// FailNextOpen makes exactly the next open call fail, so retry paths can be
// exercised without wedging the backend permanently.
func (b *FakeBackend) FailNextOpen(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failNext = err
}

// BlockEnumerate makes every subsequent Enumerate call block until the
// returned unblock func is called. Safe to call unblock more than once.
func (b *FakeBackend) BlockEnumerate() (unblock func()) {
	gate := make(chan struct{})
	b.mu.Lock()
	b.enumerateGate = gate
	b.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

func (b *FakeBackend) Enumerate() ([]DeviceInfo, []DeviceInfo, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, nil, ErrFakeBackendClosed
	}
	gate := b.enumerateGate
	if b.enumErr != nil {
		err := b.enumErr
		b.mu.Unlock()
		return nil, nil, err
	}
	inputs := append([]DeviceInfo(nil), b.inputs...)
	outputs := append([]DeviceInfo(nil), b.outputs...)
	b.mu.Unlock()

	if gate != nil {
		<-gate
	}
	return inputs, outputs, nil
}

// BlockNextOpenCapture makes exactly the next OpenCapture call block until
// the returned unblock func is called. Safe to call unblock more than once.
func (b *FakeBackend) BlockNextOpenCapture() (unblock func()) {
	gate := make(chan struct{})
	b.mu.Lock()
	b.openCaptureGate = gate
	b.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

func (b *FakeBackend) OpenCapture(id string, onFrame func([]float32)) (Stream, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, ErrFakeBackendClosed
	}
	b.captureOpens = append(b.captureOpens, id)
	gate := b.openCaptureGate
	b.openCaptureGate = nil // one-shot: only the call that claimed it blocks
	b.mu.Unlock()

	if gate != nil {
		<-gate
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	// Re-checked after the gate: a Close() that lands while this call is
	// parked is precisely the use-after-free the real backend would suffer
	// (the context Freed out from under an in-flight ma_device_init).
	if b.closed {
		return nil, ErrFakeBackendClosed
	}
	if err := b.takeFailure(); err != nil {
		return nil, err
	}
	if err := checkDeviceID(id, b.inputs); err != nil {
		return nil, err
	}
	b.onFrame = onFrame
	return &fakeStream{b: b, capture: true}, nil
}

func (b *FakeBackend) OpenPlayback(id string, fill func([]float32)) (Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrFakeBackendClosed
	}
	b.playbackOpens = append(b.playbackOpens, id)
	if err := b.takeFailure(); err != nil {
		return nil, err
	}
	if err := checkDeviceID(id, b.outputs); err != nil {
		return nil, err
	}
	b.fill = fill
	return &fakeStream{b: b}, nil
}

// CaptureOpens returns every device id passed to OpenCapture, in call
// order, regardless of whether the call succeeded.
func (b *FakeBackend) CaptureOpens() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.captureOpens...)
}

// CaptureCallbackActive reports whether some opened capture Stream's OS
// callback is currently registered -- i.e. a Stream was returned by
// OpenCapture and has NOT had Stop() called on it since. Used to confirm a
// discarded (stale-generation) reopen result was properly stopped rather
// than leaked.
func (b *FakeBackend) CaptureCallbackActive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.onFrame != nil
}

// PlaybackOpens returns every device id passed to OpenPlayback, in call
// order, regardless of whether the call succeeded.
func (b *FakeBackend) PlaybackOpens() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.playbackOpens...)
}

// takeFailure consumes the one-shot open failure. Caller holds b.mu.
func (b *FakeBackend) takeFailure() error {
	if b.failNext != nil {
		err := b.failNext
		b.failNext = nil
		return err
	}
	return nil
}

// checkDeviceID mirrors malgoBackend.setDeviceID's contract so the fake
// cannot be more forgiving than reality: an empty id always means "follow
// the system default" and succeeds unconditionally (backend.go's
// DeviceInfo.ID doc), while a non-empty id must match something currently
// enumerable or the open fails, exactly as it would against real hardware
// when the persisted device has been unplugged. Without this check, a
// manager's "saved device is gone -> fall back to default" path could never
// be exercised against the fake and would only fail the first time it met
// real hardware.
func checkDeviceID(id string, list []DeviceInfo) error {
	if id == "" {
		return nil
	}
	for _, d := range list {
		if d.ID == id {
			return nil
		}
	}
	return fmt.Errorf("audio: device %q not found", id)
}

// Close marks the backend permanently unusable, mirroring the real
// backend's terminal Uninit/Free. Every subsequent Enumerate/OpenCapture/
// OpenPlayback returns ErrFakeBackendClosed -- see that var's doc.
func (b *FakeBackend) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}

// Closed reports whether Close has been called.
func (b *FakeBackend) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// PushFrame delivers one frame to the capture callback, as the OS would.
func (b *FakeBackend) PushFrame(frame []float32) {
	b.mu.Lock()
	cb := b.onFrame
	b.mu.Unlock()
	if cb != nil {
		cb(frame)
	}
}

// PullFrame asks the playback callback for one frame and records it.
func (b *FakeBackend) PullFrame() {
	b.mu.Lock()
	cb := b.fill
	b.mu.Unlock()
	if cb == nil {
		return
	}
	f := make([]float32, FrameSamples)
	cb(f)
	b.mu.Lock()
	b.recorded = append(b.recorded, f)
	b.mu.Unlock()
}

// Captured returns every frame the playback side produced.
func (b *FakeBackend) Captured() [][]float32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]float32(nil), b.recorded...)
}

type fakeStream struct {
	b       *FakeBackend
	capture bool
	stopped bool
}

func (s *fakeStream) Stop() error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopped = true
	if s.capture {
		s.b.onFrame = nil
	} else {
		s.b.fill = nil
	}
	return nil
}
