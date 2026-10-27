package app

import "sync"

// keybindRefresher runs keybind refreshes OFF the state store's mutating
// goroutine. The radios observer (onRadiosChanged) runs synchronously on
// whatever goroutine mutated the store -- in production the gRPC update
// stream -- and store.go forbids an observer from blocking, but a refresh
// needs writeMu, which a settings save or a capture can hold for a long
// time.
//
// The queue is FIFO with one job per local-radio signature change, NOT a
// coalescing mailbox (Phase 6.9 CL2, user decision D1): N signature changes
// still produce exactly N applyHotkeys -> N hotkeys:state and N
// keybinds:changed, in the same order as the old synchronous path. Dedupe
// happens only at enqueue (an unchanged signature enqueues nothing).
// Every job is identical (it re-reads current state), so the FIFO is a
// counter.
type keybindRefresher struct {
	run func()

	mu      sync.Mutex
	pending int
	running bool
	stopped bool

	wake     chan struct{} // cap 1
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newKeybindRefresher(run func()) *keybindRefresher {
	r := &keybindRefresher{
		run:  run,
		wake: make(chan struct{}, 1),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go r.loop()
	return r
}

// enqueue adds one job. Never blocks; a no-op after shutdown.
func (r *keybindRefresher) enqueue() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.pending++
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *keybindRefresher) loop() {
	defer close(r.done)
	for {
		select {
		case <-r.stop:
			return
		case <-r.wake:
		}
		for {
			r.mu.Lock()
			if r.stopped || r.pending == 0 {
				r.mu.Unlock()
				break
			}
			r.pending--
			r.running = true
			r.mu.Unlock()

			r.run()

			r.mu.Lock()
			r.running = false
			r.mu.Unlock()
		}
	}
}

// close lets an in-flight job finish, discards queued ones (the OS hotkey
// layer is closed right after, so applying them would only register keys
// to unregister them) and joins the goroutine. Idempotent.
func (r *keybindRefresher) close() {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.stopped = true
		r.pending = 0
		r.mu.Unlock()
		close(r.stop)
	})
	<-r.done
}

// idle reports whether nothing is queued or running.
func (r *keybindRefresher) idle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending == 0 && !r.running
}

// onRadiosChanged is the state store's radios observer. It compares the
// local radio signature under sb.mu only and, if it changed, enqueues one
// refresh. It never takes writeMu and never blocks.
//
// Per-radio actions are derived from the local client's radios, which do not
// exist at startup -- they arrive with SyncClient at connect time and change
// again on every server-side radio update. Without the refresh the per-radio
// panel stayed absent and per-radio hotkeys stayed unregistered until the
// user happened to change some unrelated keybind.
//
// Radio updates for OTHER clients flow through the same observer, so this
// short-circuits unless the LOCAL radio set actually changed; otherwise every
// remote radio tweak would re-emit the whole keybind list.
func (a *App) onRadiosChanged() {
	sb := a.settings
	if sb == nil {
		return // settings backend not wired (tests that only exercise session bindings)
	}
	sig := radioSignature(a.radioRefs())
	sb.mu.Lock()
	unchanged := sb.radioSig == sig
	sb.radioSig = sig
	r := sb.refresh
	sb.mu.Unlock()
	if unchanged || r == nil {
		return
	}
	r.enqueue()
}

// refreshKeybinds is one refresh job, run on the refresher's goroutine:
// re-apply OS hotkeys for the current radio set, then emit keybinds:changed.
func (a *App) refreshKeybinds() {
	sb := a.settings
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()
	a.applyHotkeys()
	sb.em.KeybindsChanged(a.GetKeybinds())
}

// stopKeybindRefresh stops and joins the refresh worker. Idempotent, and a
// no-op when the settings backend was never wired.
func (a *App) stopKeybindRefresh() {
	sb := a.settings
	if sb == nil {
		return
	}
	sb.mu.Lock()
	r := sb.refresh
	sb.mu.Unlock()
	if r != nil {
		r.close()
	}
}
