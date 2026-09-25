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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
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
}

// initVoice wires the voice subsystem's zero state. Called from NewApp and
// NewForTest so every App, test or production, has a non-nil bridge and its
// store observers registered before anything else can run.
func (a *App) initVoice() {
	a.voice.bridge = &sessionBridge{}
	a.voice.tx = map[string]*voice.TXTarget{}
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
// startVoiceSession BEFORE it dials, so the returned value can be handed to
// setVoiceSession/voiceSessionSwap afterward and compared against whatever
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
// cannot be resolved -- see resolveTXTarget) and returns the frequency list
// to hand to SetTXFrequencies. The set is guaranteed non-empty after this
// call, which is why dispatchAudioPressed does not need a "gate open"
// return: a press can never be the transition that closes it.
func (a *App) txPress(actionID string) []voice.TXTarget {
	target := a.resolveTXTarget(actionID)

	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	if a.voice.tx == nil {
		a.voice.tx = map[string]*voice.TXTarget{}
	}
	a.voice.tx[actionID] = target
	return txTargetsLocked(a.voice.tx)
}

// txRelease removes actionID from the active-TX set and reports whether any
// OTHER action is still held (held == len(set) > 0 after the removal) --
// exactly Manager.SetPTT's gate condition, independent of whether either
// action ever resolved to a real frequency.
func (a *App) txRelease(actionID string) (targets []voice.TXTarget, held bool) {
	a.voice.mu.Lock()
	defer a.voice.mu.Unlock()
	if a.voice.tx != nil {
		delete(a.voice.tx, actionID)
	}
	return txTargetsLocked(a.voice.tx), len(a.voice.tx) > 0
}

// SelectRadio is the Wails binding backing radio selection from the UI
// (clicking a radio tile). It is independent of the audio backend and of
// PTT state -- selecting a radio while nothing is held merely changes what
// the NEXT global.ptt press targets.
func (a *App) SelectRadio(id uint32) error {
	a.st.SetSelectedRadio(id)
	return nil
}

// VoiceState is the Wails binding for the voice session's current snapshot.
// See dto.go for VoiceStateDTO's shape.
func (a *App) VoiceState() VoiceStateDTO {
	return VoiceStateDTO{
		SelectedRadio: a.st.SelectedRadio(),
		Connected:     a.voiceSession() != nil,
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
func (a *App) voiceDialOptions() voice.Options {
	jitterMS := 0
	if sb := a.settings; sb != nil {
		sb.mu.Lock()
		jitterMS = sb.cfg.Voice.JitterBufferMS
		sb.mu.Unlock()
	}
	return voice.Options{Log: a.logger, JitterMS: jitterMS}
}

// startVoiceSession dials a fresh voice session against the store's current
// credentials/addresses and installs it, refreshing the RX context
// immediately afterward so a session that starts with radios already known
// (a reconnect, e.g.) does not sit fail-closed (SetRXContext's default)
// until the next unrelated radios-changed notification.
//
// The generation is captured (nextVoiceGeneration) BEFORE the dial, which
// is network I/O outside any lock: if a concurrent Disconnect or
// ServiceShutdown (or another, overlapping startVoiceSession -- e.g. two
// redirects racing) runs while this dial is in flight, setVoiceSession
// rejects the now-stale result instead of installing a session behind a
// teardown's back. See voiceSessionSwap.
//
// A dial failure is logged and swallowed, exactly like every other optional
// subsystem in this app (audio, joystick): the control connection must stay
// usable with no voice at all, the same discipline SetAudioBackend's
// caller in main.go already follows for a missing sound card.
func (a *App) startVoiceSession() {
	self, secret, src, ok := a.voiceDialInputs()
	if !ok {
		return
	}
	gen := a.nextVoiceGeneration()
	sess, err := voice.Dial(src, self, secret, a.voiceDialOptions())
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
func (a *App) handleVoiceRedirect() {
	a.voice.mu.Lock()
	live := a.voice.sess != nil
	a.voice.mu.Unlock()
	if !live {
		return
	}
	a.startVoiceSession()
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
