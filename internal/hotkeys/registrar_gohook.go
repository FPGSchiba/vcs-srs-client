// Package hotkeys: this file is the OS seam, implementing Registrar over
// github.com/robotn/gohook's raw event stream.
//
// WHY gohook AND NOT golang.design/x/hotkey. x/hotkey implements global
// HOTKEYS -- "this key is mine now". Every backend takes the bound key
// exclusively: macOS returns NULL from an active CGEventTap, Windows calls
// RegisterHotKey, Linux calls XGrabKey. For a voice client running next to
// Star Citizen that is a product-breaking defect: a user who binds
// push-to-talk to a key the game also uses loses that key in the game. What
// we need is a global LISTENER -- "tell me when this key moves, and let it
// through" -- and on gohook's purego backends pass-through is guaranteed by
// the MECHANISM rather than by a flag we could regress:
//
//	darwin  kCGEventTapOptionListenOnly (darwin.go:312) -- the window server
//	        ignores a listen-only tap's return value; it structurally cannot
//	        consume, and it does not sit synchronously in the input path.
//	windows WH_KEYBOARD_LL + an unconditional CallNextHookEx
//	        (windows.go:348) -- the canonical pass-through mechanism.
//	linux   XRecord, a passive observer. libuiohook's own source says it:
//	        "There is no way to consume the XRecord event."
//
// See docs/superpowers/specs/2026-09-15-passive-hotkey-listening-spike.md for
// the full evidence trail, including why the permission model is unchanged.
//
// ─────────────────────────────────────────────────────────────────────────
// PRIVACY: THIS FILE SEES EVERY KEYSTROKE ON THE MACHINE.
//
// gohook subscribes to the whole input mask and offers no way to narrow it,
// so the stream below carries every key the user presses in every
// application -- passwords included -- plus mouse movement. That is
// mechanically a keylogger's data path, and the only thing that stops it
// being one is discipline at this boundary.
//
// NEVER LOG A KEY IDENTITY HERE. Not Keychar, not Keycode, not Rawcode, not
// a "just for debugging" dump of the event, not at Debug level, not behind a
// flag. There is no level at which a raw keystroke log is acceptable in this
// process, because the log file outlives the session and is not encrypted.
//
// What IS fine, and is what we do: translate the event into a keyCode +
// modifier set, compare it against the registered chords, drop it, and let
// internal/app log the ACTION ID of a matched hotkey. An action ID reveals
// that "push-to-talk fired"; it reveals nothing about what the user typed.
// ─────────────────────────────────────────────────────────────────────────
package hotkeys

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	hook "github.com/robotn/gohook"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// ErrBackendUnavailable is returned by Register when the OS event stream
// cannot run at all in this environment. The user-visible consequence is the
// "global hotkeys unavailable" banner; the wrapped cause says why.
var ErrBackendUnavailable = errors.New("hotkeys: OS backend unavailable")

// errHookDisabled is what the OS stream reported itself as dead. On macOS
// that is a denied Accessibility grant; on Linux it is an unreachable or
// unusable X display.
var errHookDisabled = fmt.Errorf("%w: the OS key listener reported itself disabled "+
	"(macOS: Accessibility not granted; Linux: no usable X11 display)", ErrBackendUnavailable)

// eventSource is a seam NARROWER than Registrar, and it exists because
// Registrar is not narrow enough to test this file.
//
// Registrar's own seam lets hotkeys.Manager be tested without an OS. But
// everything below Registrar -- chord matching, auto-repeat debounce, the
// press/release latch, the stream lifecycle -- is exactly the logic this
// change introduces, and it would be untested if the only fake were a fake
// Registrar. eventSource splits the one genuinely untestable thing (gohook
// talking to the window server) away from all of it, so the tests drive real
// hook.Event values through the real conversion, the real matcher and the
// real lifecycle code.
type eventSource interface {
	// Start opens the stream and returns its event channel. The channel is
	// closed when the stream ends.
	Start() <-chan hook.Event
	// End tears the stream down and closes the channel Start returned.
	End()
}

// gohookSource is the real eventSource.
//
// Note hook.Start/hook.End operate on PACKAGE-GLOBAL state inside gohook --
// there is one stream per process, not one per object. That is why
// hookRegistrar is careful never to have two running at once.
type gohookSource struct{}

func (gohookSource) Start() <-chan hook.Event { return hook.Start() }
func (gohookSource) End()                     { hook.End() }

// hookRegistrar implements Registrar over a single, long-lived gohook stream.
//
// STREAM LIFECYCLE -- the decision, and why.
//
// x/hotkey's shape was N OS registrations for N hotkeys, so UnregisterAll
// genuinely meant "tell the OS to forget them". gohook's shape is ONE global
// stream and N chords we match in Go. Those are not the same object, and
// conflating them would be a bug:
//
//   - The stream is started LAZILY on the first Register and then KEPT ALIVE
//     across UnregisterAll, Suspend, Resume and every Apply. UnregisterAll
//     empties the chord table (releasing anything held) and nothing more.
//     Manager calls UnregisterAll immediately before re-registering on every
//     single rebind, so tearing the stream down there would mean a stop/start
//     cycle per keystroke typed into the capture UI. gohook's End() sleeps,
//     drains and CLOSES a package-global channel that Start() then
//     reallocates (darwin.go:248-278) -- restarting it is both slow and racy,
//     and doing it on a hot path is asking for the race to land.
//
//   - The one exception is a stream that has reported itself DEAD. gohook's
//     loop goroutine returns after emitting HookDisabled and never recovers,
//     so on macOS a user who launches VCS before granting Accessibility would
//     otherwise need to restart the app even after granting it. When the
//     reader has seen HookDisabled, the next registration cycle restarts the
//     stream once. It is armed by UnregisterAll and consumed by the restart,
//     so one Apply can trigger at most one restart no matter how many
//     bindings it registers -- otherwise nineteen bindings against a denied
//     permission would mean nineteen End/Start cycles.
//
//   - A stream that goes away RELEASES whatever it was holding, on every
//     path: HookDisabled, the channel closing underneath us, a deliberate
//     teardown, and Close. Once the stream is gone no KeyUp can arrive, so a
//     push-to-talk latched at that instant would stay latched forever --
//     Pressed emitted, Released never, microphone open with nothing able to
//     close it. See streamDied and dispatcher.releaseHeld.
//
// Consequences worth stating: there is exactly one reader goroutine for the
// life of the process (a restart ends the old one before starting the new
// one), repeated Apply/Suspend/Resume cycles allocate nothing and start
// nothing, and the hook can never be double-started.
//
// One failure the design does NOT cover, because nothing at this layer can
// observe it: gohook's X11 backend can die SILENTLY. If the RECORD data
// connection drops, x11ReadLoop returns on the read error and x11Loop
// (x11.go:227) returns without sending HookDisabled and without closing the
// channel. Our reader simply blocks forever, disabled stays false, and this
// registrar reports healthy. See the migration report for why a liveness
// watchdog is not the answer and what is.
type hookRegistrar struct {
	d   *dispatcher
	src eventSource

	// disabled is set by the reader goroutine when the stream reports itself
	// dead. Atomic rather than under lifeMu because the reader must be able
	// to set it while ensureStream holds that lock.
	disabled atomic.Bool

	// readersAlive counts reader goroutines that have been committed to and
	// have not yet returned. It exists to make ONE invariant checkable rather
	// than merely asserted in a comment: src.End() must never be called while
	// a reader is alive, because gohook's End() drains the same channel the
	// reader is receiving from and can block forever competing with it.
	readersAlive atomic.Int32

	// startMu serialises a whole start/restart operation. lifeMu alone is not
	// enough: a restart releases lifeMu to do its slow work (see teardown),
	// and without this a concurrent registration could start a second stream
	// on top of one still being torn down. Manager already serialises
	// Register, so this is belt and braces -- but it is the difference
	// between "cannot happen today" and "cannot happen".
	startMu sync.Mutex

	// lifeMu guards the fields below and NOTHING slow. It is never held
	// across src.End(), across a channel wait, or across a call into the
	// dispatcher or a Handler.
	lifeMu     sync.Mutex
	running    bool
	retryArmed bool
	quit       chan struct{}
	readerDone chan struct{}
	starts     int // how many times the stream has been started; tests assert on it
}

// DefaultStaleLatchTimeout is how long a hold binding may stay latched with
// no key-up before the watchdog force-releases it (see
// dispatcher.forceRelease).
//
// 120 seconds is a product decision, not a technical one, and it is a
// two-sided trade. Too short and the watchdog cuts off a user mid-sentence on
// a legitimately long transmission -- a new failure on the COMMON path, to fix
// a rare one. Too long and a stranded microphone stays open. 120 s sits above
// any plausible single push-to-talk transmission while still bounding the
// damage when a release is genuinely lost.
const DefaultStaleLatchTimeout = 120 * time.Second

// readerStopTimeout bounds how long a teardown waits for the reader goroutine
// to acknowledge its quit signal.
//
// The reader exits within one select iteration, so this should never be
// reached -- unless a Handler blocks, since dispatcher.handle calls out to
// application code. Bounded anyway: ensureStream runs on the goroutine
// servicing a Wails call from the settings UI, and an unbounded wait there is
// a hung window rather than a slow one.
const readerStopTimeout = 2 * time.Second

// errStreamWedged is returned when a previous stream's reader did not stop.
// Registration fails rather than starting a second stream on top of it.
var errStreamWedged = fmt.Errorf("%w: the previous OS key listener did not shut down", ErrBackendUnavailable)

// NewOSRegistrar returns a Registrar backed by the real OS key listener.
func NewOSRegistrar() Registrar { return newHookRegistrar(gohookSource{}) }

// newHookRegistrar builds a registrar over an injected event source.
func newHookRegistrar(src eventSource) *hookRegistrar {
	return &hookRegistrar{
		d:          newDispatcher(DefaultStaleLatchTimeout),
		src:        src,
		retryArmed: true,
	}
}

// setStaleLatchTimeout overrides the stale-latch watchdog deadline.
//
// For tests only, and UNEXPORTED for that reason -- a test that genuinely
// waits two minutes is a test nobody runs, but a timing knob reachable from
// outside the package is one somebody eventually sets to something absurd.
// A non-positive value disables the watchdog.
//
// Only safe before the first Register: it does not re-arm timers that are
// already running, which is exactly what a test wants and not what a live
// caller would expect.
func (r *hookRegistrar) setStaleLatchTimeout(d time.Duration) {
	r.d.mu.Lock()
	r.d.staleAfter = d
	r.d.mu.Unlock()
}

// Register records a chord to match and makes sure the OS stream is running.
//
// Unlike the x/hotkey implementation this replaces, nothing here talks to the
// OS per hotkey: there is no per-key registration to fail and no per-key
// goroutine. The failures it can report are (a) a key this platform cannot
// express, and (b) the stream not being usable at all. Both surface per
// action through Manager.Failed(), which is what the UI needs to tell the
// user WHICH binding did not take effect.
func (r *hookRegistrar) Register(actionID string, c chord.Chord, hold bool, h Handler) error {
	code, ok := osKeyCode(c.Key)
	if !ok {
		if !supportedPlatform() {
			return fmt.Errorf("hotkeys: %s: %w: no key table for this platform", actionID, ErrBackendUnavailable)
		}
		return fmt.Errorf("hotkeys: %s: no OS key mapping for %q on this platform", actionID, c.Key)
	}

	if err := r.ensureStream(); err != nil {
		return fmt.Errorf("hotkeys: %s (%s): %w", actionID, c, err)
	}

	r.d.add(actionID, boundAction{code: code, mods: c.Mods, hold: hold, h: h})
	return nil
}

// UnregisterAll empties the chord table. It does NOT stop the OS stream; see
// the type comment for why. Anything currently held is released first, so a
// suspension mid-PTT cannot strand an open microphone.
func (r *hookRegistrar) UnregisterAll() {
	r.lifeMu.Lock()
	r.retryArmed = true
	r.lifeMu.Unlock()

	r.d.clear()
}

// streamPlan is what ensureStream decided to do, computed under lifeMu so the
// slow part of a restart can happen with the lock released.
type streamPlan int

const (
	planStart   streamPlan = iota // nothing running; start one
	planHealthy                   // running and alive; nothing to do
	planDead                      // running, dead, and the retry is spent
	planRestart                   // running, dead, retry claimed; tear down then start
)

// ensureStream starts the OS stream if it is not running, or restarts it once
// if it has reported itself dead and a retry is armed.
//
// The work is split deliberately: the DECISION happens under lifeMu, the
// teardown does not. gohook's End() drains and closes a package-global
// channel and sleeps while doing it, so holding a lock across it risks
// blocking every later Register -- and ensureStream runs on the goroutine
// servicing a Wails call, so that reads to the user as a frozen settings
// window. startMu keeps the whole operation serialised regardless.
func (r *hookRegistrar) ensureStream() error {
	if err := sessionSupported(); err != nil {
		return err
	}

	r.startMu.Lock()
	defer r.startMu.Unlock()

	quit, done, plan := r.planStream()
	switch plan {
	case planHealthy:
		return nil
	case planDead:
		return errHookDisabled
	case planRestart:
		if err := r.teardown(quit, done); err != nil {
			return err
		}
	}

	r.startStream()
	return nil
}

// planStream decides what ensureStream should do and, for a restart, CLAIMS
// it: the retry is spent and the stream is marked not-running before the lock
// is released, so nothing else can restart it concurrently. Returns the old
// stream's channels for teardown.
func (r *hookRegistrar) planStream() (quit, done chan struct{}, plan streamPlan) {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()

	switch {
	case !r.running:
		return nil, nil, planStart
	case !r.disabled.Load():
		return nil, nil, planHealthy
	case !r.retryArmed:
		return nil, nil, planDead
	}

	r.retryArmed = false
	r.running = false
	quit, done = r.quit, r.readerDone
	r.quit, r.readerDone = nil, nil
	return quit, done, planRestart
}

// teardown stops the reader, then the stream. Called with startMu held and
// lifeMu NOT held.
//
// The ORDER is the fix for a real deadlock. gohook's End() drains its channel
// with `for len(ev) != 0 { <-ev }` and then closes it (darwin.go:268-272,
// x11.go:142-146). If our reader were still ranging over that same channel it
// would compete for those receives, and End() could take the last element's
// length check and then block forever on a receive no producer will satisfy --
// asyncon is already false by then. Mouse-move traffic keeps that buffer
// non-empty, so the window is small but real, and the restart path is exactly
// the macOS "user just granted Accessibility" recovery.
//
// Signalling our reader first and waiting for it to acknowledge means End()
// drains against no competitor at all. On timeout we do NOT call End(): a
// still-live reader is precisely the condition that makes it unsafe, and
// leaking one blocked goroutine beats wedging the UI.
func (r *hookRegistrar) teardown(quit chan struct{}, done <-chan struct{}) error {
	if quit != nil {
		close(quit)
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(readerStopTimeout):
			return errStreamWedged
		}
	}
	r.src.End()
	return nil
}

// startStream opens a new stream and its reader. Called with startMu held.
func (r *hookRegistrar) startStream() {
	quit := make(chan struct{})
	done := make(chan struct{})

	r.disabled.Store(false)
	ch := r.src.Start()

	r.lifeMu.Lock()
	r.quit, r.readerDone = quit, done
	r.running = true
	r.starts++
	r.lifeMu.Unlock()

	// Counted BEFORE the goroutine is scheduled: from here on a reader exists
	// as far as teardown is concerned, even if it has not run its first
	// instruction yet.
	r.readersAlive.Add(1)
	go r.read(ch, quit, done)
}

// aliveReaders reports how many reader goroutines are live. Tests assert it
// is zero at the moment src.End() runs; see readersAlive.
func (r *hookRegistrar) aliveReaders() int32 { return r.readersAlive.Load() }

// Close stops the OS stream for good. It releases anything held first, so a
// quit mid-transmission cannot leave the server believing we are still
// talking.
//
// This is the closing bracket on "one stream per process". At process exit
// the OS reclaims the tap regardless, so this is tidiness rather than a leak
// fix -- but a lifecycle with a start and no stop is one somebody eventually
// gets wrong, and it gives a test a way to prove the teardown path works.
// Safe to call when nothing is running, and safe to call twice.
func (r *hookRegistrar) Close() {
	r.startMu.Lock()
	defer r.startMu.Unlock()

	r.lifeMu.Lock()
	quit, done := r.quit, r.readerDone
	r.quit, r.readerDone = nil, nil
	running := r.running
	r.running = false
	r.lifeMu.Unlock()

	r.d.clear()
	if running {
		_ = r.teardown(quit, done)
	}
}

// read pumps the OS event stream until it closes or quit is closed.
//
// This is the only place raw OS key data exists in this program, and it lives
// for the duration of one loop iteration. See the file comment: nothing here
// may log, buffer, or forward a key identity.
//
// quit exists so a teardown never has to rely on the stream's channel being
// closed to stop us -- see teardown for the deadlock that would otherwise be
// reachable.
func (r *hookRegistrar) read(ch <-chan hook.Event, quit <-chan struct{}, done chan struct{}) {
	defer close(done)
	defer r.readersAlive.Add(-1)

	for {
		select {
		case <-quit:
			// Deliberate teardown. The stream is going away, so no KeyUp can
			// follow; release anything held rather than stranding it.
			r.d.releaseHeld()
			return

		case e, ok := <-ch:
			if !ok {
				// The stream closed underneath us.
				r.streamDied()
				return
			}

			switch e.Kind {
			case hook.HookEnabled:
				r.disabled.Store(false)
			case hook.HookDisabled:
				// The backend could not install its listener, or lost it. Its
				// loop goroutine has already returned; only a restart
				// recovers. Keep reading: on darwin the channel is NOT closed
				// on this path, and quit is what stops us.
				r.streamDied()
			default:
				if ke, ok := toKeyEvent(e); ok {
					r.d.handle(ke)
				}
			}
		}
	}
}

// streamDied marks the stream dead and releases anything currently held.
//
// The release is the important half. Marking it dead only fixes the NEXT
// registration cycle; without the release, a hold binding latched at the
// moment the stream died stays latched forever, because the KeyUp that would
// have cleared it can no longer be delivered. Pressed was emitted, Released
// never would be, and the microphone stays open.
func (r *hookRegistrar) streamDied() {
	r.disabled.Store(true)
	r.d.releaseHeld()
}

// startCount reports how many times the OS stream has been started. Tests use
// it to prove Apply/Suspend/Resume cycles do not restart or double-start it.
func (r *hookRegistrar) startCount() int {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()
	return r.starts
}

// toKeyEvent reduces a gohook event to the three things the matcher needs,
// discarding everything else -- including Keychar, which is the actual
// character the user typed and must never travel further than this line.
//
// Only KeyDown and KeyUp are converted. KeyHold is deliberately dropped: on
// X11 it is the auto-repeat of a held key (x11.go:101-102) and on Windows it
// is a synthetic "typed character" event carrying Keychar and a zero Keycode
// (windows.go:369-380). Neither is a key transition, and treating either as
// one would re-fire push-to-talk while the key is held.
func toKeyEvent(e hook.Event) (keyEvent, bool) {
	var edge keyEdge
	switch e.Kind {
	case hook.KeyDown:
		edge = edgeDown
	case hook.KeyUp:
		edge = edgeUp
	default:
		return keyEvent{}, false
	}

	return keyEvent{
		edge: edge,
		code: eventKeyCode(e.Keycode, e.Rawcode),
		mods: modsFromMask(e.Mask),
	}, true
}

// gohook's modifier mask bits, mirroring libuiohook's MASK_* (iohook.h) and
// identical across all three purego backends (darwin.go:97-104,
// windows.go:102-111, x11.go:73-79).
//
// Left and right are separate bits and we accept either: the darwin and X11
// backends only ever set the LEFT bit regardless of which physical modifier
// was used, while Windows sets the correct side. Matching on the pair makes
// the three agree.
const (
	hookMaskShift uint16 = 1<<0 | 1<<4
	hookMaskCtrl  uint16 = 1<<1 | 1<<5
	hookMaskMeta  uint16 = 1<<2 | 1<<6
	hookMaskAlt   uint16 = 1<<3 | 1<<7
)

// modsFromMask translates gohook's modifier mask into chord's.
//
// The lock keys (NumLock bit 13, CapsLock bit 14, ScrollLock bit 15) and the
// mouse-button bits (8-12) are deliberately ignored: none of them is part of
// a chord, and letting CapsLock participate in the comparison would make
// every binding stop working the moment a user left CapsLock on.
func modsFromMask(m uint16) chord.Mod {
	var out chord.Mod
	if m&hookMaskCtrl != 0 {
		out |= chord.ModCtrl
	}
	if m&hookMaskAlt != 0 {
		out |= chord.ModAlt
	}
	if m&hookMaskShift != 0 {
		out |= chord.ModShift
	}
	if m&hookMaskMeta != 0 {
		out |= chord.ModSuper
	}
	return out
}
