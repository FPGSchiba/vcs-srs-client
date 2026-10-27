package app

import (
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

func radioSet(names ...string) *srspb.RadioInfo {
	out := &srspb.RadioInfo{}
	for i, n := range names {
		out.Radios = append(out.Radios, &srspb.Radio{Id: uint32(i + 1), Name: n})
	}
	return out
}

// The radios observer runs on the store's mutating goroutine -- in
// production the gRPC update stream. It must not wait for writeMu, which a
// settings save or a capture can hold for a long time.
func TestRadiosObserverDoesNotBlockOnWriteMu(t *testing.T) {
	st := state.New()
	a, _ := newTestAppOn(t, st, &countingRegistrar{})
	st.SetSelf("me", &srspb.ClientInfo{Name: "me"})
	waitKeybindRefreshIdle(t, a)

	a.settings.writeMu.Lock()
	done := make(chan struct{})
	go func() {
		st.SetRadios("me", radioSet("GUARD"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		a.settings.writeMu.Unlock()
		t.Fatal("st.SetRadios blocked on writeMu: the radios observer stalls the update stream")
	}
	a.settings.writeMu.Unlock()
	waitKeybindRefreshIdle(t, a)
}

// One refresh per local-radio signature change: N distinct changes still
// produce exactly N hotkeys:state and N keybinds:changed, in the same
// per-job order as the old synchronous path (user decision D1).
func TestRadiosObserverOneRefreshPerSignatureChange(t *testing.T) {
	st := state.New()
	a, em := newTestAppOn(t, st, &countingRegistrar{})
	st.SetSelf("me", &srspb.ClientInfo{Name: "me"})
	waitKeybindRefreshIdle(t, a)

	base := len(em.names())
	st.SetRadios("me", radioSet("A"))
	st.SetRadios("me", radioSet("A", "B"))
	st.SetRadios("me", radioSet("C"))
	waitKeybindRefreshIdle(t, a)

	var got []string
	for _, n := range em.names()[base:] {
		if n == events.EventHotkeysState || n == events.EventKeybindsChanged {
			got = append(got, n)
		}
	}
	want := []string{
		events.EventHotkeysState, events.EventKeybindsChanged,
		events.EventHotkeysState, events.EventKeybindsChanged,
		events.EventHotkeysState, events.EventKeybindsChanged,
	}
	if len(got) != len(want) {
		t.Fatalf("events after 3 signature changes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events after 3 signature changes = %v, want %v", got, want)
		}
	}

	// Another client's radios, and a local change that keeps the signature
	// (same ids and names, different frequency), refresh nothing.
	base = len(em.names())
	st.SetRadios("someone-else", radioSet("THEIRS"))
	same := radioSet("C")
	same.Radios[0].Frequency = 251.0
	st.SetRadios("me", same)
	waitKeybindRefreshIdle(t, a)
	for _, n := range em.names()[base:] {
		if n == events.EventHotkeysState || n == events.EventKeybindsChanged {
			t.Fatalf("unchanged local signature emitted %q (all: %v)", n, em.names()[base:])
		}
	}
}

// closeTrackingRegistrar records Register calls and whether any landed after
// the hotkey manager was closed.
type closeTrackingRegistrar struct {
	mu         sync.Mutex
	closed     bool
	regs       int
	afterClose int
}

func (r *closeTrackingRegistrar) Register(string, chord.Chord, bool, hotkeys.Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.regs++
	if r.closed {
		r.afterClose++
	}
	return nil
}
func (r *closeTrackingRegistrar) UnregisterAll() {}
func (r *closeTrackingRegistrar) Close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
}

// Quit while a keybind refresh is queued behind writeMu: shutdown must not
// hang, and no hotkey may be registered after hk.Close().
func TestKeybindRefreshWorkerStopsBeforeHotkeyClose(t *testing.T) {
	st := state.New()
	reg := &closeTrackingRegistrar{}
	a, _ := newTestAppOn(t, st, reg)
	st.SetSelf("me", &srspb.ClientInfo{Name: "me"})
	waitKeybindRefreshIdle(t, a)

	reg.mu.Lock()
	before := reg.regs
	reg.mu.Unlock()

	a.settings.writeMu.Lock()
	st.SetRadios("me", radioSet("GUARD")) // job queued, blocked on writeMu
	// Wait until the worker has dequeued it and is parked on writeMu, so it
	// is genuinely IN FLIGHT when shutdown starts (a job still queued at
	// that point is discarded by design, which is not what this pins).
	r := a.settings.refresh
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		inFlight := r.running
		r.mu.Unlock()
		if inFlight {
			break
		}
		if time.Now().After(deadline) {
			a.settings.writeMu.Unlock()
			t.Fatal("refresh job never started")
		}
		time.Sleep(time.Millisecond)
	}
	st.SetRadios("me", radioSet("GUARD", "FLEET")) // stays queued; discarded by shutdown

	shut := make(chan error, 1)
	go func() { shut <- a.ServiceShutdown() }()
	time.Sleep(20 * time.Millisecond) // let shutdown reach the worker stop
	a.settings.writeMu.Unlock()

	select {
	case err := <-shut:
		if err != nil {
			t.Fatalf("ServiceShutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ServiceShutdown hung with a keybind refresh queued")
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if !reg.closed {
		t.Fatal("hk.Close was never reached")
	}
	if reg.afterClose != 0 {
		t.Fatalf("%d hotkey registrations landed after hk.Close", reg.afterClose)
	}
	if reg.regs <= before {
		t.Fatal("the in-flight refresh never ran; it must finish before shutdown proceeds")
	}
}

// waitKeybindRefreshIdle waits until no keybind refresh is queued or
// running, for tests that assert on what a radio change produced.
func waitKeybindRefreshIdle(t *testing.T, a *App) {
	t.Helper()
	a.settings.mu.Lock()
	r := a.settings.refresh
	a.settings.mu.Unlock()
	if r == nil {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for !r.idle() {
		if time.Now().After(deadline) {
			t.Fatal("keybind refresh worker did not go idle within 2s")
		}
		time.Sleep(time.Millisecond)
	}
}

// close() must let the in-flight job finish and discard every queued one.
// Deterministic counterpart to TestKeybindRefreshWorkerStopsBeforeHotkeyClose,
// which cannot pin the discard without depending on scheduling.
func TestKeybindRefresherCloseDiscardsQueuedJobs(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	gate := make(chan struct{})
	r := newKeybindRefresher(func() {
		<-gate
		mu.Lock()
		runs++
		mu.Unlock()
	})

	r.enqueue()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		inFlight := r.running
		r.mu.Unlock()
		if inFlight {
			break
		}
		if time.Now().After(deadline) {
			close(gate)
			t.Fatal("first job never started")
		}
		time.Sleep(time.Millisecond)
	}
	r.enqueue()
	r.enqueue() // two jobs queued behind the in-flight one

	closed := make(chan struct{})
	go func() { r.close(); close(closed) }()
	// Wait until close() has marked the refresher stopped, so the queued
	// jobs are discarded before the in-flight one is released.
	deadline = time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		stopped := r.stopped
		r.mu.Unlock()
		if stopped {
			break
		}
		if time.Now().After(deadline) {
			close(gate)
			t.Fatal("close never marked the refresher stopped")
		}
		time.Sleep(time.Millisecond)
	}
	close(gate)

	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("close hung waiting for the in-flight job")
	}
	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Fatalf("refresh ran %d times, want exactly 1 (the in-flight job); queued jobs must be discarded", runs)
	}
	r.enqueue() // no-op after close; must not panic or block
}
