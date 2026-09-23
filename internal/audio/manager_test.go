package audio

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestManager(t *testing.T, b *FakeBackend) *Manager {
	t.Helper()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)
	m := NewManager(b, ManagerOptions{
		PollInterval: 10 * time.Millisecond,
		VUInterval:   10 * time.Millisecond,
	})
	t.Cleanup(m.Stop)
	return m
}

func TestManagerStartOpensBothDevices(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st := m.State(); !st.Running {
		t.Fatalf("State().Running = false after Start: %+v", st)
	}
}

func TestManagerCapturedAudioReachesTheSinkOnlyWhenGateIsOpen(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	sink := &recordingSink{}
	m.AddSink(sink)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	// Gate shut: PTT not held, VOX off.
	for i := 0; i < 5; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() == 0 }, "sink received audio with the gate shut")

	m.SetPTT(true)
	for i := 0; i < 5; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() > 0 }, "sink received nothing with PTT held")
}

func TestManagerMuteSilencesTheSink(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	sink := &recordingSink{}
	m.AddSink(sink)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	m.SetPTT(true)
	m.SetMuted(true)
	for i := 0; i < 10; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() == 0 }, "muted mic still reached the sink")
}

func TestManagerHotPlugEmitsDeviceChange(t *testing.T) {
	b := NewFakeBackend()
	var mu sync.Mutex
	changed := 0
	b.SetDevices([]DeviceInfo{{ID: "mic-1", Name: "Mic One"}}, []DeviceInfo{{ID: "out-1", Name: "Out One"}})
	m := NewManager(b, ManagerOptions{
		PollInterval: 5 * time.Millisecond,
		VUInterval:   time.Hour,
		OnDevices: func(_, _ []DeviceInfo) {
			mu.Lock()
			changed++
			mu.Unlock()
		},
	})
	t.Cleanup(m.Stop)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One"}, {ID: "mic-2", Name: "Mic Two"}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One"}},
	)
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return changed > 0
	}, "hot-plug produced no OnDevices callback")
}

func TestManagerFallsBackToDefaultWhenSavedDeviceIsGone(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	m := NewManager(b, ManagerOptions{PollInterval: 5 * time.Millisecond, VUInterval: time.Hour})
	t.Cleanup(m.Stop)
	m.SetConfig(Config{InputDevice: "mic-vanished", OutputDevice: ""})
	if err := m.Start(); err != nil {
		t.Fatalf("Start with an absent saved device must fall back, got: %v", err)
	}
	if st := m.State(); !st.Running {
		t.Fatalf("not running after fallback: %+v", st)
	}
}

func TestManagerReportsInputOpenFailure(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	b.FailNextOpen(errors.New("device busy"))
	m := NewManager(b, ManagerOptions{PollInterval: time.Hour, VUInterval: time.Hour})
	t.Cleanup(m.Stop)
	_ = m.Start()
	if st := m.State(); st.InputError == "" {
		t.Fatalf("State().InputError empty after a failed open: %+v", st)
	}
}

// recordingSink counts frames written to it.
type recordingSink struct {
	mu sync.Mutex
	n  int
}

func (s *recordingSink) WriteFrame(f []float32) {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
}
func (s *recordingSink) Close() error { return nil }
func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// waitFor polls cond for up to a second. The DSP goroutine is asynchronous,
// so tests synchronise on observable state rather than on sleeps.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(msg)
}
