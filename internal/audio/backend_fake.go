package audio

import "sync"

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

func (b *FakeBackend) Enumerate() ([]DeviceInfo, []DeviceInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.enumErr != nil {
		return nil, nil, b.enumErr
	}
	return append([]DeviceInfo(nil), b.inputs...), append([]DeviceInfo(nil), b.outputs...), nil
}

func (b *FakeBackend) OpenCapture(id string, onFrame func([]float32)) (Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.takeFailure(); err != nil {
		return nil, err
	}
	b.onFrame = onFrame
	return &fakeStream{b: b, capture: true}, nil
}

func (b *FakeBackend) OpenPlayback(id string, fill func([]float32)) (Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.takeFailure(); err != nil {
		return nil, err
	}
	b.fill = fill
	return &fakeStream{b: b}, nil
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

func (b *FakeBackend) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
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
