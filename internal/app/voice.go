// Package app: the voice session's App-level wiring -- TX routing, selected
// radio, session lifecycle and the Sink/Source bridge registered with the
// audio Manager.
//
// Two traps this file exists to close, both confirmed by probe against
// internal/audio.Manager and documented at length on Manager.SetSource:
//
//  1. Registering a Sink with the Manager wires TRANSMIT only. Receive comes
//     back through a SEPARATE call, Manager.SetSource, feeding the mixer's
//     fourth bus via ReadInto. Task 9's own tests exercise the RX path
//     directly rather than through the Manager, so a Sink-only registration
//     leaves every test green while receive is completely inaudible.
//  2. Manager.SetSource's guard is `if s == nil`, which does NOT catch a nil
//     *voice.Session boxed in a non-nil Source interface -- `var sess
//     *voice.Session; SetSource(sess)` skips the guard and stores a non-nil
//     interface wrapping a nil pointer, and dspLoop's next ReadInto call
//     nil-panics on the DSP goroutine. The fix is that Manager NEVER sees a
//     *voice.Session at all: it is handed sessionBridge, a wrapper VALUE
//     (*sessionBridge) that is itself never nil, constructed once by
//     initVoice and never replaced. What CAN go nil is bridge.sess, an
//     atomic.Pointer[rtSession] -- a pointer, not an interface -- so
//     `!= nil` on the load is a real, safe pointer check with no boxing
//     involved. setVoiceSession applies the identical discipline one layer
//     up, at App.voice.sess (a voiceSessionAPI interface field): assigning a
//     typed-nil *voice.Session straight into it would reproduce the exact
//     same trap, so it stores an explicit nil interface instead.
package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// voiceSessionAPI is the subset of *voice.Session the App layer drives. Kept
// deliberately small (and an interface, not the concrete type) so
// voice_test.go can assert TX routing and RX-context wiring against a fake
// that never opens a socket -- this repo's sandbox blocks bind(2) outright,
// and a real *voice.Session can only be produced by voice.Dial.
type voiceSessionAPI interface {
	SetTXFrequencies(targets []voice.TXTarget)
	EndTransmission()
	SetRXContext(accept, global, testFreqs []voice.KHz)
	SetEffects(voiceEffect, clippingEffect string)
	// State reports the session's current lifecycle state (I2 fix).
	// VoiceState's Connected field derives from this rather than from mere
	// non-nil-ness, which used to report "connected" for a session that had
	// dialed a socket but never completed a HELLO and would never carry
	// audio.
	State() voice.State
	Close() error
}

var _ voiceSessionAPI = (*voice.Session)(nil)

// rtSession is the realtime subset sessionBridge forwards to: WriteFrame
// (audio.Sink's half) and ReadInto (audio.Source's half). *voice.Session
// satisfies it directly.
//
// It exists as its own tiny interface -- rather than sessionBridge holding
// a *voice.Session concretely -- so a test can install a double that proves
// calls made through a REAL audio.Manager actually reach it, without ever
// opening the socket voice.Dial requires (this repo's sandbox blocks
// bind(2)). This does not reopen the typed-nil trap: the value Manager
// itself holds is always *sessionBridge, never nil and never this
// interface, and bridge.sess below is a pointer (atomic.Pointer[rtSession]),
// not an interface value -- so the nil check on Load() is an ordinary,
// correct pointer comparison.
type rtSession interface {
	WriteFrame(frame []float32)
	ReadInto(buf []float32)
}

var _ rtSession = (*voice.Session)(nil)

// sessionBridge is the single Sink/Source main.go registers with the audio
// Manager exactly once, at process startup, for the Manager's whole
// lifetime (design decision D5): Phase 4's Manager has no RemoveSink and
// never calls Sink.Close(), so a per-connection registration would leak a
// Sink on every reconnect.
//
// The bridge itself is NEVER nil once constructed by initVoice -- only
// bridge.sess goes nil across a disconnect. See this file's package doc for
// why that split is what keeps Manager.SetSource's typed-nil trap
// unreachable.
type sessionBridge struct {
	sess atomic.Pointer[rtSession]
}

// WriteFrame implements audio.Sink. Runs on the DSP goroutine inside its
// realtime budget -- an atomic load and, when connected, a call straight
// through to the live session's WriteFrame, which carries the identical
// discipline itself. A nil load (disconnected) is a silent no-op: there is
// nowhere to send captured audio, which is the correct behaviour, not an
// error.
func (b *sessionBridge) WriteFrame(frame []float32) {
	if p := b.sess.Load(); p != nil {
		(*p).WriteFrame(frame)
	}
}

// Close satisfies audio.Sink. The Manager never calls it (D5's whole
// point), so this only needs to exist, not do anything.
func (b *sessionBridge) Close() error { return nil }

// ReadInto implements audio.Source. Same realtime discipline as WriteFrame.
// It must OVERWRITE buf (audio.Source's contract): when disconnected there
// is no received audio, and the caller's scratch buffer holds last tick's
// data, so the nil branch clears it explicitly rather than leaving it
// untouched.
func (b *sessionBridge) ReadInto(buf []float32) {
	if p := b.sess.Load(); p != nil {
		(*p).ReadInto(buf)
		return
	}
	clear(buf)
}

// voiceState is the App-level voice wiring: the live session, the
// refcounted TX set and the Sink/Source bridge.
type voiceState struct {
	mu sync.Mutex

	// sess is the live voice session, nil while disconnected. An interface
	// field (voiceSessionAPI), not sessionBridge's concrete
	// atomic.Pointer[voice.Session] -- this one is read under mu by the
	// control-plane calls in this file (SetTXFrequencies, SetRXContext,
	// ...), none of which run on the DSP goroutine, so a mutex costs
	// nothing here. setVoiceSession keeps this and bridge.sess in lock
	// step.
	sess voiceSessionAPI

	// tx is the refcounted active-TX set: one entry per currently-HELD PTT
	// ACTION (global.ptt, or one radio.<n>.ptt per held radio), not one
	// entry per radio. That is what makes releasing one action leave
	// another action's frequency alone (TestReleasingOneKeepsTheOther):
	// each action owns exactly its own slot regardless of what any other
	// action is doing.
	//
	// The value is a *voice.TXTarget rather than a value or bool so a HELD
	// action that fails to resolve to a real frequency (global.ptt with
	// nothing selected, or a radio.<n>.ptt for an id that vanished from the
	// persisted set) still occupies a slot -- nil, but present. That is
	// what lets the gate (len(tx) > 0) keep meaning EXACTLY what it meant
	// before Task 11: "at least one PTT-class action is held", independent
	// of whether anything is actually routable (TestGateStaysOpenWhileAnyTargetIsHeld).
	// The frequency LIST handed to SetTXFrequencies is a narrower view that
	// drops the nil entries.
	tx map[string]*voice.TXTarget

	// gen is the voice-session epoch. It is bumped on every teardown
	// (voiceSessionSwap's nil branch) and at the start of every new-session
	// attempt, BEFORE the dial (nextVoiceGeneration, called from
	// startVoiceSession). A dial that is still in flight when a later
	// teardown or a later, overlapping start has already run captured a
	// generation that no longer matches by the time it completes --
	// voiceSessionSwap compares against the live value immediately before
	// installing and rejects (closes, does not install) a stale dial. This
	// is what closes the race where handleVoiceRedirect dials a
	// replacement session, unlocked, while a concurrent Disconnect or
	// ServiceShutdown tears the live one down: see Priority 1 of
	// task-11b's review.
	gen uint64

	bridge *sessionBridge

	// clearTimer is the pending post-release TX-target clear, armed by
	// scheduleTXTargetClear once the active-TX set empties and fired after
	// ptt_release_delay_ms (I3 fix). Non-nil only while a clear is pending;
	// any press cancels it (see txPress) so it can never wipe out a
	// transmission that has resumed before it fires.
	clearTimer *time.Timer

	// dial is voice.Dial by default (set in initVoice); startVoiceSession
	// calls THROUGH this indirection rather than voice.Dial directly, so a
	// test can install a slow or instrumented fake without ever opening a
	// real socket -- this repo's sandbox blocks bind(2), which voice.Dial
	// requires. See TestHandleVoiceRedirectDoesNotBlockOnASlowDial (M5),
	// the regression for handleVoiceRedirect running the dial on its own
	// goroutine instead of the state.Store observer's.
	dial func(src voice.Sources, self uuid.UUID, secret string, opt voice.Options) (*voice.Session, error)
}

// cancelPendingTXClearLocked stops any TX-target clear scheduled by
// scheduleTXTargetClear. Caller holds voiceState.mu. A no-op when nothing is
// pending.
func (v *voiceState) cancelPendingTXClearLocked() {
	if v.clearTimer != nil {
		v.clearTimer.Stop()
		v.clearTimer = nil
	}
}

// initVoice wires the voice subsystem's zero state. Called from NewApp and
// NewForTest so every App, test or production, has a non-nil bridge and its
// store observers registered before anything else can run.
func (a *App) initVoice() {
	a.voice.bridge = &sessionBridge{}
	a.voice.tx = map[string]*voice.TXTarget{}
	a.voice.dial = voice.Dial
	// OnRadiosChanged already exists for the per-radio keybind refresh
	// (app.go); it fires on SetRadios/SetSelf/ClearSelf/RemoveClient, which
	// covers a CLIENT_RADIO_UPDATE echo of our own UpdateRadioInfo push at
	// connect. OnVoiceCredentialsChanged is the redirect path this file's
	// handleVoiceRedirect implements. OnSettingsChanged (task-11b Priority
	// 2) covers a server-side change to the global/test frequency lists
	// mid-session -- see refreshRXContext's doc.
	a.st.OnRadiosChanged(a.refreshRXContext)
	a.st.OnVoiceCredentialsChanged(a.handleVoiceRedirect)
	a.st.OnSettingsChanged(a.refreshRXContext)
}

// VoiceBridge returns the Sink/Source bridge main.go must register with the
// audio Manager exactly once, at startup (AddSink AND SetSource -- see this
// file's package doc for why both calls are required).
func (a *App) VoiceBridge() *sessionBridge { return a.voice.bridge }

// voiceSession reads the live session under lock (nil while disconnected).
func (a *App) voiceSession() voiceSessionAPI {
	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	return a.voice.sess
}

// setVoiceSession installs sess as the live session (nil to clear it on
// disconnect), keeping App.voice.sess and the bridge's atomic pointer in
// lock step, and closes whatever session was live before.
//
// gen is the generation nextVoiceGeneration returned to the caller BEFORE
// it dialed sess (meaningless, and ignored, when sess is nil -- a teardown
// always proceeds). voiceSessionSwap compares gen against the live
// generation immediately before installing; if a teardown or a newer,
// overlapping start already ran while this dial was in flight, sess is
// closed instead of installed and old/installed report that nothing
// changed. This is the fix for Priority 1 of task-11b's review: without
// it, a redirect's dial completing after a concurrent Disconnect or
// ServiceShutdown would install a live session after teardown, silently
// re-arming transmit.
//
// sess == nil is handled explicitly rather than falling into the ordinary
// interface assignment: `a.voice.sess = sess` with a nil *voice.Session
// would store a NON-NIL interface wrapping a nil pointer -- the identical
// trap Manager.SetSource's doc warns about, one layer up. Every caller in
// this file that needs "no session" MUST go through here rather than
// assigning a.voice.sess directly.
func (a *App) setVoiceSession(sess *voice.Session, gen uint64) {
	// api is left as the nil voiceSessionAPI interface value when sess is
	// nil -- assigning sess directly (`api = sess`) unconditionally would
	// reopen the exact typed-nil trap this function's doc warns about, one
	// layer up in voiceSessionSwap/a.voice.sess.
	var api voiceSessionAPI
	if sess != nil {
		api = sess
	}

	old, installed := a.voiceSessionSwap(api, gen)
	if !installed {
		// Stale generation: voiceSessionSwap already closed sess. It must
		// never reach the bridge or a.voice.sess.
		return
	}

	// bridge.sess is a POINTER (atomic.Pointer[rtSession]), not an
	// interface value, so Store(nil) here is a genuine nil pointer -- never
	// a typed-nil boxed inside a non-nil interface. The one place a value
	// gets boxed into the rtSession interface is the `var rt rtSession =
	// sess` line below, and it only ever runs when sess is already known
	// non-nil, so that box is never a typed nil either.
	if sess == nil {
		a.voice.bridge.sess.Store(nil)
	} else {
		var rt rtSession = sess
		a.voice.bridge.sess.Store(&rt)
	}

	if old != nil {
		if oc, ok := old.(*voice.Session); ok && oc != nil {
			oc.Close()
		}
	}
}

// voiceSessionSwap is setVoiceSession's generation-gated core. It works
// against voiceSessionAPI, not *voice.Session, specifically so a test can
// drive the exact install/reject decision with a fake -- no real dial (this
// sandbox blocks bind(2)) and no realtime bridge involved.
//
// sess == nil is a teardown: it always proceeds, unconditionally bumping
// the generation so it -- not gen, which is meaningless here -- invalidates
// any dial already in flight. sess != nil is a new-session install: it
// proceeds only if gen still matches the live generation; otherwise sess is
// closed (outside a.voice.mu, since Close can block on network I/O) and
// nothing is installed.
//
// Returns the session that was live immediately before a successful
// install (nil if none, or if sess was rejected) for the caller to Close
// AFTER releasing a.voice.mu, and whether sess was installed.
func (a *App) voiceSessionSwap(sess voiceSessionAPI, gen uint64) (old voiceSessionAPI, installed bool) {
	a.voice.mu.Lock()
	if sess != nil && gen != a.voice.gen {
		a.voice.mu.Unlock()
		sess.Close()
		return nil, false
	}
	if sess == nil {
		a.voice.gen++
	}
	old = a.voice.sess
	a.voice.sess = sess
	a.voice.mu.Unlock()
	return old, true
}

// nextVoiceGeneration bumps and returns the voice-session epoch. Called by
// startVoiceSession and handleVoiceRedirect BEFORE the dial -- and, for
// handleVoiceRedirect, before the dial is even scheduled onto its own
// goroutine (see that function's doc for why the ordering matters) -- so
// the returned value can be handed to startVoiceSessionWithGen and, from
// there, setVoiceSession/voiceSessionSwap, and compared against whatever
// the live generation has become by the time the dial completes.
func (a *App) nextVoiceGeneration() uint64 {
	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	a.voice.gen++
	return a.voice.gen
}

// radioActionID parses "radio.<n>.<suffix>" and returns n. Shared by the
// per-radio PTT and select actions, which differ only in suffix.
func radioActionID(actionID, suffix string) (uint32, bool) {
	const prefix = "radio."
	if !strings.HasPrefix(actionID, prefix) || !strings.HasSuffix(actionID, suffix) {
		return 0, false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(actionID, prefix), suffix)
	if mid == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(mid, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// perRadioPTTID parses "radio.<n>.ptt".
func perRadioPTTID(actionID string) (uint32, bool) { return radioActionID(actionID, ".ptt") }

// radioSelectID parses "radio.<n>.select".
func radioSelectID(actionID string) (uint32, bool) { return radioActionID(actionID, ".select") }

// resolveTXTarget maps a PTT-class action id to the frequency it should
// transmit on right now, or nil if it cannot be resolved (global.ptt with
// nothing selected, or an id that names no persisted, enabled radio).
//
// global.ptt resolves against the SELECTED radio at the moment of THIS
// call -- i.e. at press time, not re-resolved for the life of the hold.
// Retargeting an already-transmitting press because the user changed the
// selection mid-sentence would be a far stranger radio than one that keeps
// transmitting on whatever was selected when the key went down.
//
// Only ENABLED radios resolve: a disabled radio is the user's own "this one
// is off" -- the same set SetRXContext's accept list honours -- so transmit
// and receive stay consistent about which radios are live.
func (a *App) resolveTXTarget(actionID string) *voice.TXTarget {
	var radioID uint32
	switch {
	case actionID == "global.ptt":
		radioID = a.st.SelectedRadio()
		if radioID == 0 {
			return nil
		}
	default:
		id, ok := perRadioPTTID(actionID)
		if !ok {
			return nil
		}
		radioID = id
	}

	sb := a.settings
	if sb == nil {
		return nil
	}
	sb.mu.Lock()
	radios := sb.cfg.Radios
	sb.mu.Unlock()
	for _, r := range radios {
		if r.ID == radioID && r.Enabled {
			t := voice.TXTarget{Freq: voice.KHz(r.FrequencyKHz), Intercom: r.IsIntercom}
			return &t
		}
	}
	return nil
}

// txTargetsLocked renders tx's resolved entries as the frequency list
// SetTXFrequencies expects, deduplicated by frequency (two held actions
// that resolve to the SAME radio -- e.g. global.ptt selected onto radio 3
// while radio.3.ptt is also held -- must not transmit the frame twice on
// one frequency). Caller holds voiceState.mu.
func txTargetsLocked(tx map[string]*voice.TXTarget) []voice.TXTarget {
	dedup := make(map[voice.KHz]voice.TXTarget, len(tx))
	for _, t := range tx {
		if t != nil {
			dedup[t.Freq] = *t
		}
	}
	out := make([]voice.TXTarget, 0, len(dedup))
	for _, t := range dedup {
		out = append(out, t)
	}
	return out
}

// txPress adds actionID to the active-TX set (resolved target, or nil if it
// cannot be resolved -- see resolveTXTarget) and, ATOMICALLY with that
// mutation -- under voiceState.mu, in the SAME critical section -- installs
// the resulting frequency list on the live session. The set is guaranteed
// non-empty after this call, which is why dispatchAudioPressed does not need
// a "gate open" return: a press can never be the transition that closes it.
//
// The session update must happen inside the same lock as the map mutation,
// not after it. Before this fix, the caller read the map under the lock,
// released it, and only THEN called SetTXFrequencies -- which left a window
// where clearTXTargetsIfStillIdle's own (equally unlocked) store could land
// AFTER this press's store, silently wiping the brand-new targets while the
// gate stayed open (hotkey:pressed already fired, PTT lit) and every frame
// was dropped as DroppedNoTarget: a fully silent transmission with a green
// light on it. Holding voiceState.mu across both this store and the clear's
// (see clearTXTargetsIfStillIdle) makes that interleaving impossible --
// whichever one starts its critical section first finishes it before the
// other can begin. See TestClearCannotRaceANewPress for the direct,
// deterministic reproduction.
//
// A press also cancels any TX-target clear scheduled by an earlier
// release's tail (see scheduleTXTargetClear, I3 fix): that clear must never
// fire after a transmission has already resumed and wipe out its targets.
// This is belt-and-suspenders with the atomicity above: cancelling here
// closes the ordinary case (the timer has not fired yet), the shared lock
// closes the rarer one (the timer's callback is already running).
func (a *App) txPress(actionID string) {
	target := a.resolveTXTarget(actionID)

	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	if a.voice.tx == nil {
		a.voice.tx = map[string]*voice.TXTarget{}
	}
	freshStart := len(a.voice.tx) == 0
	a.voice.cancelPendingTXClearLocked()
	a.voice.tx[actionID] = target

	if sess := a.voice.sess; sess != nil {
		// Bump the accumulator generation on the PRESS edge that STARTS a
		// transmission, not on release (M1 fix) -- see the original comment
		// this carried in dispatchAudioPressed before the fix wave that
		// moved the session calls in here.
		if freshStart {
			sess.EndTransmission()
		}
		sess.SetTXFrequencies(txTargetsLocked(a.voice.tx))
	}
}

// txRelease removes actionID from the active-TX set and, when any OTHER
// action is still held, installs the shrunk frequency list on the live
// session -- atomically with the map mutation, under voiceState.mu, for the
// identical reason txPress's session call moved in-lock (see its doc).
//
// held reports whether the set is still non-empty after the removal --
// exactly Manager.SetPTT's gate condition, independent of whether either
// action ever resolved to a real frequency. dispatchAudioReleased arms the
// post-release clear (scheduleTXTargetClear) itself when held is false;
// that call is deliberately OUTSIDE this method; scheduleTXTargetClear takes
// voiceState.mu itself and calling it while still holding the lock here
// would deadlock.
func (a *App) txRelease(actionID string) (held bool) {
	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	if a.voice.tx != nil {
		delete(a.voice.tx, actionID)
	}
	held = len(a.voice.tx) > 0
	if held {
		if sess := a.voice.sess; sess != nil {
			sess.SetTXFrequencies(txTargetsLocked(a.voice.tx))
		}
	}
	return held
}

// scheduleTXTargetClear arms a timer to clear the live voice session's TX
// target set once the PTT release tail (ptt_release_delay_ms) has actually
// finished (I3 fix). Called from dispatchAudioReleased exactly when the
// active-TX set has just emptied.
//
// It cannot clear immediately: internal/audio's gate deliberately stays
// open for the configured release delay after the key comes up (manual
// check 8 expects that tail on the wire), and WriteFrame keeps running with
// the OLD targets for exactly that long. Clearing here at the release edge
// would silence the tail instead of merely ending it honestly.
//
// Left un-cleared at all (the bug this fixes), the session's TX target set
// -- voice.Session.tx.targets, set only by SetTXFrequencies and never by
// EndTransmission -- keeps holding the last-pressed frequency forever.
// internal/audio's VOX path calls Sink.WriteFrame independently of PTT
// (gate.go: `pttOpen || voxOpen`), so a LATER VOX trigger with no key held
// at all would key the radio on that stale frequency with no transmit
// indicator lit -- an open mic the user cannot see. VOX does not transmit
// in Phase 5 by design (see docs/superpowers/plans/2026-09-24-phase-5-
// manual-verification.md's known-gaps section), but the stale target must
// still not exist for the day it does.
//
// Any timer already pending is replaced (there is at most one outstanding
// transmission's tail to clear), and a subsequent press cancels it outright
// (txPress) so a transmission that resumes before the clear fires is never
// wiped out from under it.
//
// The scheduled delay adds a small margin -- clearTailMargin, two DSP frames
// -- on top of the configured wall-clock ptt_release_delay_ms. The gate
// (internal/audio/gate.go) counts its own tail in DSP FRAMES, not wall-clock
// time: g.tail is seeded from the SAME PTTReleaseDelayMS, converted via
// msToFrames, at the moment the key goes up. If the DSP loop is running
// behind when that happens -- a device reopen, scheduler pressure -- the
// frame-counted tail finishes later, in wall-clock terms, than this
// millisecond timer does. Without the margin, this clear could fire and
// wipe the session's TX targets while the gate is still counting down its
// last frame or two, truncating exactly the tail manual check 8 expects on
// the wire. The margin costs nothing when the DSP loop is on schedule (the
// clear was always going to fire strictly after the gate closes, since the
// gate's tail and this delay are seeded from the same config value); it
// only matters when the loop has fallen behind.
func (a *App) scheduleTXTargetClear() {
	delayMS := 0
	if sb := a.settings; sb != nil {
		sb.mu.Lock()
		delayMS = sb.cfg.Audio.PTTReleaseDelayMS
		sb.mu.Unlock()
	}
	delay := time.Duration(delayMS)*time.Millisecond + clearTailMargin

	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	a.voice.cancelPendingTXClearLocked()
	a.voice.clearTimer = time.AfterFunc(delay, a.clearTXTargetsIfStillIdle)
}

// clearTailMargin is added on top of ptt_release_delay_ms by
// scheduleTXTargetClear so a DSP loop that is running slightly behind still
// finishes emitting its frame-counted tail before the millisecond timer
// clears the session's TX targets out from under it. Two frames (20 ms at
// the fixed 10 ms cadence, audio.FrameDuration) -- enough slack for an
// ordinary scheduling hiccup without meaningfully delaying VOX's stale-
// target cleanup (I3).
const clearTailMargin = 2 * audio.FrameDuration

// clearTXTargetsIfStillIdle is scheduleTXTargetClear's deferred half. It
// clears the live session's TX targets only if the active-TX set is STILL
// empty by the time it runs -- a press that raced the timer and landed a
// moment before this fired must not have its brand-new targets wiped from
// under it (txPress's own cancel closes the more common ordering; this
// double-check closes the one where the timer had already started running
// when the press's cancel landed).
//
// The idle check and the SetTXFrequencies(nil) store below run inside the
// SAME voiceState.mu critical section as txPress's mutation-plus-store (see
// its doc). Before this fix they did not: the old version read stillIdle,
// UNLOCKED, and only then called SetTXFrequencies(nil), leaving a window
// where a press landing in that gap would win the idle check (which ran a
// moment before) and still lose its own store to this one landing after it
// -- gate open, transmit indicator lit, every frame silently dropped. This
// closes that window: while this function holds voiceState.mu, txPress
// cannot even begin building its own targets, so the two stores can no
// longer interleave in either order.
func (a *App) clearTXTargetsIfStillIdle() {
	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	a.voice.clearTimer = nil
	if len(a.voice.tx) != 0 {
		return
	}
	if sess := a.voice.sess; sess != nil {
		sess.SetTXFrequencies(nil)
	}
}

// SelectRadio is the Wails binding backing radio selection from the UI
// (clicking a radio tile). It is independent of the audio backend and of
// PTT state -- selecting a radio while nothing is held merely changes what
// the NEXT global.ptt press targets.
//
// It also persists the selection to local config (M4 fix): without this,
// state.Store.selectedRadio was in-memory only, so the tuned frequency
// survived a restart but which radio was selected did not -- global.ptt
// resolved to nothing on every fresh launch until the user clicked a radio
// card again.
func (a *App) SelectRadio(id uint32) error {
	a.st.SetSelectedRadio(id)
	return a.persistSelectedRadio(id)
}

// VoiceState is the Wails binding for the voice session's current snapshot.
// See dto.go for VoiceStateDTO's shape.
//
// Connected derives from the session's actual lifecycle state (I2 fix), not
// from mere non-nil-ness: a session that has dialed a socket but never
// completed a HELLO -- a wrong or missing voice secret, e.g. -- used to
// report Connected == true here from the instant voice.Dial returned, which
// is exactly the honest-DoD-4 distinction VoiceStateDTO.Connected's own doc
// says it exists to make.
func (a *App) VoiceState() VoiceStateDTO {
	sess := a.voiceSession()
	return VoiceStateDTO{
		SelectedRadio: a.st.SelectedRadio(),
		Connected:     sess != nil && sess.State() == voice.StateConnected,
	}
}

// voiceDialInputs gathers what voice.Dial needs from the store and the
// persisted config, reporting ok=false (and logging why) when a voice
// session cannot be started yet -- e.g. immediately after Connect returns,
// before SyncClient's voice_secret has reached the store, or a self GUID
// that failed to parse as a UUID. Either failure leaves the control
// connection fully usable; only voice is unavailable.
func (a *App) voiceDialInputs() (self uuid.UUID, secret string, src voice.Sources, ok bool) {
	secret, coalitionAddr, _ := a.st.VoiceCredentials()
	if secret == "" {
		a.logger.Warn("voice: no voice secret available; voice session not started")
		return uuid.UUID{}, "", voice.Sources{}, false
	}

	selfGUID := a.st.Snapshot().SelfGUID
	self, err := uuid.Parse(selfGUID)
	if err != nil {
		a.logger.Warn("voice: self GUID did not parse as a UUID; voice session not started", "err", err)
		return uuid.UUID{}, "", voice.Sources{}, false
	}

	var cfgHost, serverURL string
	var cfgPort int
	if sb := a.settings; sb != nil {
		sb.mu.Lock()
		cfgHost = sb.cfg.Voice.Host
		cfgPort = sb.cfg.Voice.Port
		serverURL = sb.cfg.ServerURL
		sb.mu.Unlock()
	}
	// Update and Sync both carry the SAME stored coalition address: the
	// store does not (and need not) distinguish "this came from
	// SyncClient" from "this came from a later VOICE_ADDRESS_UPDATE" --
	// voice.Sources' precedence between them only matters when they
	// DISAGREE, and here they never do.
	src = voice.Sources{
		Update:     coalitionAddr,
		Sync:       coalitionAddr,
		ConfigHost: cfgHost,
		ConfigPort: cfgPort,
		ServerURL:  serverURL,
	}
	return self, secret, src, true
}

// voiceDialOptions builds voice.Options from the persisted [voice] config.
//
// OnState is wired here (I2 fix): before this, nothing in the shipped
// binary ever observed voice.Session's state machine at all -- the whole
// deliverLoop/cbQueue delivery mechanism had zero production consumers, so
// a wrong or missing voice secret failed the handshake silently as far as
// the UI was concerned. It emits events.EventVoiceState, mirroring
// audio:state/hotkeys:state (main.go's OnState wiring for the audio
// Manager). err.Error() is safe to emit as-is: voice.Session never embeds
// the secret in any error string it produces (a wrong-length secret is
// reported by byte count, not value -- see ErrSecretLength's wrapping in
// voice.Dial), so nothing here needs to scrub it.
func (a *App) voiceDialOptions() voice.Options {
	jitterMS := 0
	maxBufferMS := 0
	var em *events.Tagged
	if sb := a.settings; sb != nil {
		sb.mu.Lock()
		jitterMS = sb.cfg.Voice.JitterBufferMS
		maxBufferMS = sb.cfg.Voice.MaxBufferMS
		em = sb.em
		sb.mu.Unlock()
	}
	return voice.Options{
		Log:         a.logger,
		JitterMS:    jitterMS,
		MaxBufferMS: maxBufferMS,
		OnState: func(st voice.State, err error) {
			if em == nil {
				return
			}
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			em.VoiceState(st.String(), msg)
		},
	}
}

// startVoiceSession dials a fresh voice session against the store's current
// credentials/addresses and installs it. It captures the generation
// (nextVoiceGeneration) itself and delegates to startVoiceSessionWithGen --
// for Connect and Reconnect, the only remaining direct callers, there is no
// ordered-goroutine race to preserve, so capturing here is equivalent to
// capturing at the top of startVoiceSessionWithGen.
//
// handleVoiceRedirect does NOT call this: it captures its own generation
// and calls startVoiceSessionWithGen directly. See that function's doc.
func (a *App) startVoiceSession() {
	a.startVoiceSessionWithGen(a.nextVoiceGeneration())
}

// startVoiceSessionWithGen dials a fresh voice session against the store's
// current credentials/addresses and installs it under the given generation,
// refreshing the RX context immediately afterward so a session that starts
// with radios already known (a reconnect, e.g.) does not sit fail-closed
// (SetRXContext's default) until the next unrelated radios-changed
// notification.
//
// gen must have been captured (nextVoiceGeneration) BEFORE this function
// was scheduled to run -- NOT inside it. startVoiceSession (Connect/
// Reconnect) captures it immediately before calling in, which is
// equivalent since those callers run inline with nothing else able to
// interleave. handleVoiceRedirect is different: it runs this on its own
// goroutine (see its doc for why), but captures gen BEFORE spawning that
// goroutine, on the ordered state.Store observer goroutine, so that two
// redirects' generations are ordered the same way the redirects themselves
// were delivered -- regardless of which goroutine's dial happens to finish
// first. Capturing gen in here instead (i.e. after the scheduling hop)
// would let generation order follow dial-completion order instead, which
// can let a stale-address redirect win over a newer one, or let a
// redirect's generation land after a concurrent Disconnect/ServiceShutdown
// bump, installing a live session behind a teardown. voiceSessionSwap still
// rejects a stale gen either way; only the ordering of what counts as
// "stale" depends on where gen was captured.
//
// A dial failure is logged and swallowed, exactly like every other optional
// subsystem in this app (audio, joystick): the control connection must stay
// usable with no voice at all, the same discipline SetAudioBackend's
// caller in main.go already follows for a missing sound card.
func (a *App) startVoiceSessionWithGen(gen uint64) {
	self, secret, src, ok := a.voiceDialInputs()
	if !ok {
		return
	}
	// Through a.voice.dial, not voice.Dial directly -- see voiceState.dial's
	// doc for why (test seam; defaults to voice.Dial in initVoice). Read
	// under the lock: dial itself is set once, at construction, and never
	// mutated in production, but a test installing a fake races this read
	// against that assignment absent the lock, and the race detector
	// (mandatory in this repo's CI) would flag it.
	a.voice.mu.Lock()
	dial := a.voice.dial
	a.voice.mu.Unlock()
	sess, err := dial(src, self, secret, a.voiceDialOptions())
	if err != nil {
		a.logger.Warn("voice: dial failed; voice is unavailable for this session", "err", err)
		return
	}
	a.setVoiceSession(sess, gen)
	a.refreshRXContext()
}

// stopVoiceSession closes the live session (if any) and clears it, via
// setVoiceSession(nil, ...) -- see that method for why nil must be routed
// through it rather than assigned directly. The generation argument is
// ignored for a teardown (voiceSessionSwap's nil branch always proceeds and
// bumps the generation itself), so 0 is passed.
func (a *App) stopVoiceSession() { a.setVoiceSession(nil, 0) }

// handleVoiceRedirect re-points the voice session at a fresh socket when a
// VOICE_ADDRESS_UPDATE redirect changes the store's coalition/global
// addresses. voice.Session has no live "change endpoint" call -- Resolve
// runs once, inside Dial -- so following a redirect means dialing a NEW
// session against the now-current Sources and swapping it in;
// setVoiceSession closes the superseded one only AFTER the swap, so the
// gap between "new socket open" and "old socket closed" is this
// goroutine's own work, not a network round trip.
//
// It is registered as a state.Store voice-credentials observer (see
// initVoice), which also fires for the VERY FIRST SetVoiceCredentials call
// SyncClient makes at connect -- before startVoiceSession has dialed
// anything. That call is a no-op here (live is false), which is why the
// dial in startVoiceSession, not this observer, owns bringing a session up
// for the first time.
//
// The dial itself -- specifically voice.Dial inside startVoiceSessionWithGen
// -- runs on its OWN goroutine, not this one. state.Store's observer
// contract (see OnVoiceCredentialsChanged's doc) requires an observer not
// block: this is called synchronously from route() on internal/control's
// ConsumeUpdates goroutine, the same one that delivers every OTHER control
// update (client list, radio echoes, connection state). voice.Dial can
// block up to resolveTimeout (10s) resolving a blackholed hostname; running
// it inline here used to stall that whole goroutine -- and therefore every
// other control update -- for up to 10s on every redirect.
//
// The generation, however, is captured RIGHT HERE -- on this goroutine,
// before the `go` below schedules the dial -- not inside
// startVoiceSessionWithGen. That is deliberate and load-bearing: this
// function runs on the single ordered ConsumeUpdates goroutine, so two
// redirects' calls here are always ordered the same way the redirects
// themselves arrived. If gen were captured after the scheduling hop
// instead (i.e. inside the spawned goroutine), two racing redirects could
// take their generations in whichever order their goroutines happen to be
// scheduled, not the order the redirects were delivered in -- letting a
// stale-address redirect's session win over a newer one, or letting a
// redirect's generation land after a concurrent Disconnect/
// ServiceShutdown's teardown bump, installing a live session behind a
// teardown (exactly the class voiceSessionSwap exists to prevent). Taking
// gen here restores that ordering exactly as it was before the dial moved
// off this goroutine, while still keeping the dial itself off it.
func (a *App) handleVoiceRedirect() {
	a.voice.mu.Lock()
	live := a.voice.sess != nil
	a.voice.mu.Unlock()
	if !live {
		return
	}
	gen := a.nextVoiceGeneration()
	go a.startVoiceSessionWithGen(gen)
}

// refreshRXContext recomputes the RX frequency filter -- the local client's
// enabled radios plus the server's global/test frequency lists -- and the
// RX radio-effect selection, pushing both to the live session. It is a
// no-op while disconnected.
//
// Registered as an OnRadiosChanged observer (fires on a CLIENT_RADIO_UPDATE
// echo of our own UpdateRadioInfo push, among other radio-set mutations),
// an OnSettingsChanged observer (fires on SetSettings -- a server-side
// change to the global/test frequency lists, task-11b Priority 2), and
// called directly right after a session dials.
func (a *App) refreshRXContext() {
	sess := a.voiceSession()
	if sess == nil {
		return
	}

	var radios []config.Radio
	var voiceEffect, clippingEffect string
	if sb := a.settings; sb != nil {
		sb.mu.Lock()
		radios = sb.cfg.Radios
		voiceEffect = sb.cfg.Audio.VoiceEffect
		clippingEffect = sb.cfg.Audio.ClippingEffect
		sb.mu.Unlock()
	}

	accept := make([]voice.KHz, 0, len(radios))
	for _, r := range radios {
		if r.Enabled {
			accept = append(accept, voice.KHz(r.FrequencyKHz))
		}
	}

	var global, test []voice.KHz
	if settings := a.st.Settings(); settings != nil {
		for _, f := range settings.GetGlobalFrequencies() {
			if khz := voice.KHzFromMHz32(f); khz != 0 {
				global = append(global, khz)
			}
		}
		for _, f := range settings.GetTestFrequencies() {
			if khz := voice.KHzFromMHz32(f); khz != 0 {
				test = append(test, khz)
			}
		}
	}

	sess.SetRXContext(accept, global, test)
	sess.SetEffects(voiceEffect, clippingEffect)
}

// pushPersistedRadios re-pushes the locally persisted radio set to the
// server via UpdateRadioInfo, on every single connect. The server creates
// every client with ZERO radios (state.AddClient) and that is per-session
// state with nothing server-side to restore it from -- config.Radio's own
// doc explains why the client is the one durable copy. a.sess (not
// a.settings) is nil-guarded here because pushPersistedRadios runs from
// Connect, strictly after a.sess.Connect has already succeeded, so a nil
// a.sess only happens in a test that built an App with no session at all.
func (a *App) pushPersistedRadios(ctx context.Context) {
	sb := a.settings
	if sb == nil || a.sess == nil {
		return
	}
	sb.mu.Lock()
	radios := append([]config.Radio(nil), sb.cfg.Radios...)
	sb.mu.Unlock()

	info := &srspb.RadioInfo{}
	for _, r := range radios {
		info.Radios = append(info.Radios, &srspb.Radio{
			Id:   r.ID,
			Name: r.Name,
			// FrequencyKHz is the canonical integer; MHz32 is the exact
			// same expression the server's own comparison uses (see
			// voice.KHz.MHz32's doc), so this round-trips without a
			// rounding difference silently dropping the radio out of range.
			Frequency:  voice.KHz(r.FrequencyKHz).MHz32(),
			Enabled:    r.Enabled,
			IsIntercom: r.IsIntercom,
		})
	}
	if err := a.sess.UpdateRadioInfo(ctx, info); err != nil {
		a.logger.Warn("voice: failed to push persisted radios on connect", "err", err)
	}
}

// persistRadios writes the DTO radios the frontend just sent through
// UpdateRadioInfo into local config as the new, authoritative radio set,
// converting each RadioDTO.Frequency (float32 MHz, the wire/UI form) back to
// canonical kHz -- config.Radio's own doc explains why kHz, not MHz, is the
// only form this client stores.
//
// This is the write-through half of the C1 fix: resolveTXTarget and
// refreshRXContext both read sb.cfg.Radios directly, and UpdateRadioInfo
// used to only push to the server -- so after a UI tune, this client kept
// transmitting on and accepting the OLD frequency forever, no matter how
// many times the server echoed the new one back (the echo just re-read the
// same stale config). It must run BEFORE the server push: a failed local
// persist must not be masked by a successful server round trip that this
// client itself cannot then act on correctly.
//
// A no-op (nil error) when a.settings is nil, the same discipline every
// other settings-writing method in this file/package follows so tests that
// build an App with no settings backend keep working.
func (a *App) persistRadios(dtos []RadioDTO) error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	radios := make([]config.Radio, 0, len(dtos))
	for _, d := range dtos {
		radios = append(radios, config.Radio{
			ID:           d.ID,
			Name:         d.Name,
			FrequencyKHz: uint32(voice.KHzFromMHz32(d.Frequency)),
			Enabled:      d.Enabled,
			IsIntercom:   d.IsIntercom,
		})
	}

	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = radios
	var saveErr error
	if sb.cfgPath != "" {
		saveErr = config.Save(sb.cfgPath, &next)
	}
	if saveErr == nil {
		sb.cfg = &next
	}
	sb.mu.Unlock()
	if saveErr != nil {
		return fmt.Errorf("save radios: %w", saveErr)
	}
	return nil
}

// persistSelectedRadio writes the newly selected radio id to local config
// (M4 fix), SYNCHRONOUSLY on the caller's goroutine. Used by SelectRadio
// (the UI binding), which already runs on its own Wails-dispatched
// goroutine, is never called from gohook's event-reader goroutine, and
// returns this error straight to its own caller -- none of which apply to
// the hotkey path (radio.<n>.select), which uses
// queueSelectedRadioPersist instead. See that method's doc for why the two
// paths differ and how they share one ordering guard.
func (a *App) persistSelectedRadio(id uint32) error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	return a.persistSelectedRadioSeq(id, a.nextRadioSelectSeq())
}

// queueSelectedRadioPersist persists id OFF the caller's goroutine (M6 fix).
// dispatchAudioPressed's radio.<n>.select handling used to call
// persistSelectedRadio synchronously -- a full config.Save under
// sb.writeMu -- on gohook's event-reader goroutine. internal/hotkeys/
// dispatch.go's own doc says the OS event stream "must not be able to
// stall behind" a Handler call, and that package documents a dropped KeyUp
// (gohook drops events from a full 1024-slot buffer rather than blocking
// the OS input path) as a cause of a STRANDED-OPEN MIC. writeMu is also
// held across SetSettings applies and hotkey re-registration, so the worst
// case here is well beyond a normal ~1ms save.
//
// The in-memory selection (state.Store.SetSelectedRadio) is applied by the
// caller BEFORE this is queued, so the only thing deferred is the disk
// write -- a crash between the two loses only the restart-survival
// guarantee for this one selection, not the selection itself, which is
// already live.
//
// ORDERING is preserved despite running on a fresh goroutine per call: the
// sequence number is taken synchronously, on the CALLER's goroutine,
// before the persist goroutine is even spawned, so seq order always
// matches request order -- even though the spawned goroutines' own
// scheduling (and therefore the order they actually reach sb.writeMu) is
// not guaranteed to preserve it. persistSelectedRadioSeq refuses to write
// a seq older than the newest one already persisted, so whichever
// goroutine's write actually lands last in real time always carries the
// newest requested selection: a late-scheduled goroutine for an OLDER
// press can never clobber a newer press's already-persisted value.
func (a *App) queueSelectedRadioPersist(id uint32) {
	sb := a.settings
	if sb == nil {
		return
	}
	seq := a.nextRadioSelectSeq()
	go func() {
		if err := a.persistSelectedRadioSeq(id, seq); err != nil {
			a.logger.Warn("voice: failed to persist selected radio", "err", err)
		}
	}()
}

// nextRadioSelectSeq bumps and returns the monotonic ordering sequence
// persistSelectedRadioSeq uses to reject a stale, out-of-order write (see
// queueSelectedRadioPersist's doc). 0 is never a value a real call
// returns -- sb == nil returns it as a sentinel, and the first real call
// returns 1 -- which is what lets radioSelectPersistedSeq's zero value mean
// "nothing persisted yet" unambiguously.
func (a *App) nextRadioSelectSeq() uint64 {
	sb := a.settings
	if sb == nil {
		return 0
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.radioSelectSeq++
	return sb.radioSelectSeq
}

// persistSelectedRadioSeq is persistSelectedRadio's core, taking the
// ordering sequence number explicitly so both the synchronous
// (persistSelectedRadio/SelectRadio) and asynchronous
// (queueSelectedRadioPersist) callers share one stale-write guard: a seq
// older than the newest one already persisted is a no-op (nil error, not
// a failure -- the newer write already did this call's job, better).
// seq == 0 (the sb == nil sentinel from nextRadioSelectSeq; unreachable
// here since sb is already known non-nil by the time this runs, but kept
// as an explicit non-guarded fallback rather than an assumption) always
// proceeds.
func (a *App) persistSelectedRadioSeq(id uint32, seq uint64) error {
	sb := a.settings
	if sb == nil {
		return nil
	}

	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

	sb.mu.Lock()
	if seq != 0 && seq < sb.radioSelectPersistedSeq {
		sb.mu.Unlock()
		return nil
	}
	next := *sb.cfg
	next.SelectedRadioID = id
	var saveErr error
	if sb.cfgPath != "" {
		saveErr = config.Save(sb.cfgPath, &next)
	}
	if saveErr == nil {
		sb.cfg = &next
		if seq != 0 {
			sb.radioSelectPersistedSeq = seq
		}
	}
	sb.mu.Unlock()
	if saveErr != nil {
		return fmt.Errorf("save selected radio: %w", saveErr)
	}
	return nil
}
