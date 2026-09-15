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

	hook "github.com/robotn/gohook"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// SupportsRelease reports whether the backend can detect key release, which
// hold-to-talk PTT bindings require.
//
// True on every platform this builds for, verified per backend in gohook
// v1.0.0-beta1: darwin emits KeyUp from kCGEventKeyUp and synthesises one for
// modifier-only transitions out of kCGEventFlagsChanged (darwin.go:401-404,
// :425-455); windows emits it from WM_KEYUP/WM_SYSKEYUP (windows.go:342-344,
// :381-391); linux/X11 emits it from xproto.KeyRelease (x11.go:335, :381).
//
// This is also an upgrade over the previous implementation rather than parity
// with it: x/hotkey's Windows backend had no delivered key-up at all and
// polled GetAsyncKeyState every 10 ms, so a release could be reported up to a
// tick late and a tap shorter than a tick could be missed entirely.
func SupportsRelease() bool { return true }

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
// Consequences worth stating: there is exactly one reader goroutine for the
// life of the process (a restart ends the old one before starting the new
// one), repeated Apply/Suspend/Resume cycles allocate nothing and start
// nothing, and the hook can never be double-started.
type hookRegistrar struct {
	d   *dispatcher
	src eventSource

	// disabled is set by the reader goroutine when the stream reports itself
	// dead. Atomic rather than under lifeMu because the reader must be able
	// to set it while ensureStream holds that lock.
	disabled atomic.Bool

	// lifeMu guards everything below: the stream's running state. It is never
	// held while calling into the dispatcher or a Handler.
	lifeMu     sync.Mutex
	running    bool
	retryArmed bool
	readerDone chan struct{}
	starts     int // how many times the stream has been started; tests assert on it
}

// NewOSRegistrar returns a Registrar backed by the real OS key listener.
func NewOSRegistrar() Registrar { return newHookRegistrar(gohookSource{}) }

// newHookRegistrar builds a registrar over an injected event source.
func newHookRegistrar(src eventSource) *hookRegistrar {
	return &hookRegistrar{d: newDispatcher(), src: src, retryArmed: true}
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

// ensureStream starts the OS stream if it is not running, or restarts it once
// if it has reported itself dead and a retry is armed.
func (r *hookRegistrar) ensureStream() error {
	if err := sessionSupported(); err != nil {
		return err
	}

	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()

	switch {
	case !r.running:
		// First registration, or a restart after a completed teardown.
	case !r.disabled.Load():
		return nil // healthy and already running
	case !r.retryArmed:
		return errHookDisabled
	default:
		// Dead, and we are allowed one restart. Tear the old stream down and
		// wait for its reader to finish before starting a new one, so there
		// is never more than one reader alive.
		r.retryArmed = false
		done := r.readerDone
		r.src.End()
		if done != nil {
			<-done
		}
		r.running = false
	}

	r.disabled.Store(false)
	done := make(chan struct{})
	ch := r.src.Start()
	r.readerDone = done
	r.running = true
	r.starts++
	go r.read(ch, done)
	return nil
}

// read pumps the OS event stream until it closes.
//
// This is the only place raw OS key data exists in this program, and it lives
// for the duration of one loop iteration. See the file comment: nothing here
// may log, buffer, or forward a key identity.
func (r *hookRegistrar) read(ch <-chan hook.Event, done chan struct{}) {
	defer close(done)

	for e := range ch {
		switch e.Kind {
		case hook.HookEnabled:
			r.disabled.Store(false)
		case hook.HookDisabled:
			// The backend could not install its listener, or lost it. Its
			// loop goroutine has already returned; only a restart recovers.
			r.disabled.Store(true)
		default:
			if ke, ok := toKeyEvent(e); ok {
				r.d.handle(ke)
			}
		}
	}

	// A closed channel means the stream is gone. Mark it dead so the next
	// registration cycle restarts it rather than silently matching nothing.
	r.disabled.Store(true)
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
