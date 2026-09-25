package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// newTestAppWithConfig is newTestAppWithPath but takes an explicit cfg,
// instead of always building config.Default() -- so a test can simulate a
// fresh launch reading back whatever an earlier App instance persisted to
// cfgPath (newTestAppWithPath ignores the file's contents entirely, since
// it only threads cfgPath through for the SAVE side).
func newTestAppWithConfig(t *testing.T, cfg *config.Config, cfgPath string) *App {
	t.Helper()
	a := NewForTest(state.New(), nil, nil)
	kb := keybinds.New()
	kb.Load(map[string][]string{})
	hk := hotkeys.New(&countingRegistrar{}, a)
	a.SetSettingsBackend(cfg, cfgPath, kb, hk, &recordingEmitter{})
	return a
}

// fakeControlSession is a minimal sessionAPI double recording every
// UpdateRadioInfo push, so the C1/I1 write-through and re-push tests can
// assert against exactly what reached the "server" without opening a
// socket.
type fakeControlSession struct {
	mu      sync.Mutex
	updates []*srspb.RadioInfo
}

func (f *fakeControlSession) Connect(context.Context, string, string, string, string) error {
	return nil
}
func (f *fakeControlSession) Disconnect(context.Context) error { return nil }
func (f *fakeControlSession) Reconnect(context.Context) error  { return nil }

func (f *fakeControlSession) UpdateRadioInfo(_ context.Context, info *srspb.RadioInfo) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, info)
	return nil
}

func (f *fakeControlSession) updateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.updates)
}

func (f *fakeControlSession) lastUpdate() *srspb.RadioInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updates) == 0 {
		return nil
	}
	return f.updates[len(f.updates)-1]
}

// fakeVoiceSession is a voiceSessionAPI recording every call, so the TX
// routing tests can assert against the exact frequency lists the App layer
// handed the session -- without ever opening a socket (this repo's sandbox
// blocks bind(2), which is what a real voice.Dial requires).
type fakeVoiceSession struct {
	mu sync.Mutex

	txCalls    [][]voice.TXTarget
	endCalls   int
	rxCalls    []rxCall
	effectsSet []effectsCall
	closed     int
}

type rxCall struct{ accept, global, test []voice.KHz }
type effectsCall struct{ voiceEffect, clippingEffect string }

func (f *fakeVoiceSession) SetTXFrequencies(targets []voice.TXTarget) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]voice.TXTarget(nil), targets...)
	f.txCalls = append(f.txCalls, cp)
}

func (f *fakeVoiceSession) EndTransmission() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.endCalls++
}

func (f *fakeVoiceSession) SetRXContext(accept, global, testFreqs []voice.KHz) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rxCalls = append(f.rxCalls, rxCall{accept: accept, global: global, test: testFreqs})
}

func (f *fakeVoiceSession) SetEffects(voiceEffect, clippingEffect string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.effectsSet = append(f.effectsSet, effectsCall{voiceEffect: voiceEffect, clippingEffect: clippingEffect})
}

func (f *fakeVoiceSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

func (f *fakeVoiceSession) lastTX() []voice.TXTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.txCalls) == 0 {
		return nil
	}
	return f.txCalls[len(f.txCalls)-1]
}

func (f *fakeVoiceSession) txCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.txCalls)
}

func (f *fakeVoiceSession) endCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.endCalls
}

func (f *fakeVoiceSession) lastRX() rxCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rxCalls) == 0 {
		return rxCall{}
	}
	return f.rxCalls[len(f.rxCalls)-1]
}

func (f *fakeVoiceSession) rxCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rxCalls)
}

func (f *fakeVoiceSession) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// hasFreq reports whether targets contains freq, regardless of order --
// txTargetsLocked ranges a map, so callers must never depend on ordering.
func hasFreq(targets []voice.TXTarget, freq voice.KHz) bool {
	for _, t := range targets {
		if t.Freq == freq {
			return true
		}
	}
	return false
}

// wireTestVoice sets radios into a's persisted config and a fake session
// into a's live voice slot, entirely in-process -- no socket, no real
// *voice.Session. Returns the fake so the test can inspect its calls.
func wireTestVoice(t *testing.T, a *App, radios []config.Radio) *fakeVoiceSession {
	t.Helper()
	a.settings.mu.Lock()
	a.settings.cfg.Radios = radios
	a.settings.mu.Unlock()

	sess := &fakeVoiceSession{}
	a.voice.mu.Lock()
	a.voice.sess = sess
	a.voice.mu.Unlock()
	return sess
}

// TestPTTRoutesToTheSelectedRadio pins that global.ptt transmits on the
// SELECTED radio, which is what its own action description has claimed since
// Phase 3 while nothing implemented it.
func TestPTTRoutesToTheSelectedRadio(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)
	sess := wireTestVoice(t, a, []config.Radio{
		{ID: 1, Name: "Radio 1", FrequencyKHz: 30000, Enabled: true},
		{ID: 3, Name: "Radio 3", FrequencyKHz: 251000, Enabled: true},
	})
	a.st.SetSelectedRadio(3)

	a.Pressed("global.ptt")

	if n := sess.txCallCount(); n != 1 {
		t.Fatalf("SetTXFrequencies called %d times, want 1", n)
	}
	got := sess.lastTX()
	if len(got) != 1 || got[0].Freq != voice.KHz(251000) {
		t.Fatalf("targets = %+v, want [{251000 false}]", got)
	}
	if !m.PTT() {
		t.Error("PTT gate did not open")
	}
}

// TestPerRadioPTTRoutesToThatRadio pins that radio.N.ptt ignores the
// selection.
func TestPerRadioPTTRoutesToThatRadio(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)
	sess := wireTestVoice(t, a, []config.Radio{
		{ID: 1, Name: "Radio 1", FrequencyKHz: 30000, Enabled: true},
		{ID: 7, Name: "Radio 7", FrequencyKHz: 141500, Enabled: true},
	})
	// Selection deliberately points elsewhere: radio.7.ptt must not care.
	a.st.SetSelectedRadio(1)

	a.Pressed("radio.7.ptt")

	got := sess.lastTX()
	if len(got) != 1 || got[0].Freq != voice.KHz(141500) {
		t.Fatalf("targets = %+v, want [{141500 false}]", got)
	}
}

// TestHoldingBothTransmitsOnBoth pins the set union. Phase 3.5's refcount
// already lets two sources hold one action; this is two DIFFERENT actions
// held at once, which must widen the frequency set rather than replace it.
func TestHoldingBothTransmitsOnBoth(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)
	sess := wireTestVoice(t, a, []config.Radio{
		{ID: 3, Name: "Radio 3", FrequencyKHz: 251000, Enabled: true},
		{ID: 7, Name: "Radio 7", FrequencyKHz: 141500, Enabled: true},
	})
	a.st.SetSelectedRadio(3)

	a.Pressed("global.ptt")
	a.Pressed("radio.7.ptt")

	got := sess.lastTX()
	if len(got) != 2 {
		t.Fatalf("targets = %+v, want 2 entries", got)
	}
	if !hasFreq(got, 251000) || !hasFreq(got, 141500) {
		t.Fatalf("targets = %+v, want both 251000 and 141500", got)
	}
	if !m.PTT() {
		t.Error("PTT gate did not open")
	}
}

// TestReleasingOneKeepsTheOther pins that releasing one PTT does not cut
// transmission on the other -- the same class of bug Phase 3.5's press
// refcount exists to prevent, one level up.
func TestReleasingOneKeepsTheOther(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)
	sess := wireTestVoice(t, a, []config.Radio{
		{ID: 3, Name: "Radio 3", FrequencyKHz: 251000, Enabled: true},
		{ID: 7, Name: "Radio 7", FrequencyKHz: 141500, Enabled: true},
	})
	a.st.SetSelectedRadio(3)

	a.Pressed("global.ptt")
	a.Pressed("radio.7.ptt")
	a.Released("radio.7.ptt")

	if sess.endCallCount() != 0 {
		t.Fatalf("EndTransmission called %d times, want 0 -- global.ptt is still held", sess.endCallCount())
	}
	got := sess.lastTX()
	if len(got) != 1 || got[0].Freq != voice.KHz(251000) {
		t.Fatalf("targets after releasing radio.7.ptt = %+v, want only [{251000 false}]", got)
	}
	if !m.PTT() {
		t.Error("PTT gate closed even though global.ptt is still held")
	}
}

// TestGateStaysOpenWhileAnyTargetIsHeld pins that Manager.SetPTT keeps its
// exact current meaning: the TX set being non-empty.
func TestGateStaysOpenWhileAnyTargetIsHeld(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)
	sess := wireTestVoice(t, a, []config.Radio{
		{ID: 3, Name: "Radio 3", FrequencyKHz: 251000, Enabled: true},
		{ID: 7, Name: "Radio 7", FrequencyKHz: 141500, Enabled: true},
	})
	a.st.SetSelectedRadio(3)

	a.Pressed("global.ptt")
	a.Pressed("radio.7.ptt")
	a.Released("global.ptt")
	if !m.PTT() {
		t.Fatal("PTT gate closed after releasing only ONE of two held actions")
	}
	if sess.endCallCount() != 0 {
		t.Fatalf("EndTransmission called %d times, want 0", sess.endCallCount())
	}

	a.Released("radio.7.ptt")
	if m.PTT() {
		t.Fatal("PTT gate stayed open after releasing the LAST held action")
	}
	if sess.endCallCount() != 1 {
		t.Fatalf("EndTransmission called %d times, want 1", sess.endCallCount())
	}
}

// TestPTTWithNoSelectionStillOpensTheGateButRoutesNowhere pins the "held but
// unresolved" case: global.ptt pressed with nothing selected must still open
// the local mic gate (unchanged pre-Task-11 behaviour) but must not hand the
// session a bogus target.
func TestPTTWithNoSelectionStillOpensTheGateButRoutesNowhere(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)
	sess := wireTestVoice(t, a, nil)

	a.Pressed("global.ptt")

	if !m.PTT() {
		t.Fatal("PTT gate did not open even though global.ptt is held")
	}
	if got := sess.lastTX(); len(got) != 0 {
		t.Fatalf("targets = %+v, want empty (nothing selected)", got)
	}

	a.Released("global.ptt")
	if m.PTT() {
		t.Error("PTT gate stayed open after releasing the only held action")
	}
	if sess.endCallCount() != 1 {
		t.Fatalf("EndTransmission called %d times, want 1", sess.endCallCount())
	}
}

// TestHoldingBothOnTheSameFrequencyDeduplicates pins that global.ptt
// selected onto a radio also held directly via radio.N.ptt transmits ONE
// copy of the frame on that frequency, not two.
func TestHoldingBothOnTheSameFrequencyDeduplicates(t *testing.T) {
	a, _, _ := newTestApp(t)
	m := newTestAudioManager(t)
	a.SetAudioBackend(m)
	sess := wireTestVoice(t, a, []config.Radio{
		{ID: 3, Name: "Radio 3", FrequencyKHz: 251000, Enabled: true},
	})
	a.st.SetSelectedRadio(3)

	a.Pressed("global.ptt")
	a.Pressed("radio.3.ptt")

	got := sess.lastTX()
	if len(got) != 1 || got[0].Freq != voice.KHz(251000) {
		t.Fatalf("targets = %+v, want exactly one entry for 251000", got)
	}
}

// TestSelectRadioBindingSetsTheStore proves the Wails binding reaches
// state.Store.SelectedRadio.
func TestSelectRadioBindingSetsTheStore(t *testing.T) {
	a, _, _ := newTestApp(t)
	if err := a.SelectRadio(5); err != nil {
		t.Fatalf("SelectRadio: %v", err)
	}
	if got := a.st.SelectedRadio(); got != 5 {
		t.Fatalf("SelectedRadio() = %d, want 5", got)
	}
}

// TestRadioSelectActionSetsTheSelectedRadio proves the hotkey action
// "radio.<n>.select" -- unconsumed since Phase 3 -- now drives the same
// store field as the SelectRadio binding, and does so WITHOUT requiring an
// audio backend to be wired.
func TestRadioSelectActionSetsTheSelectedRadio(t *testing.T) {
	a, _, _ := newTestApp(t) // no SetAudioBackend call at all
	a.Pressed("radio.9.select")
	if got := a.st.SelectedRadio(); got != 9 {
		t.Fatalf("SelectedRadio() = %d, want 9", got)
	}
}

// TestVoiceStateReportsSelectionAndConnection proves the VoiceState binding
// surfaces both the selected radio and whether a voice session is live.
func TestVoiceStateReportsSelectionAndConnection(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.st.SetSelectedRadio(4)

	got := a.VoiceState()
	if got.SelectedRadio != 4 {
		t.Errorf("SelectedRadio = %d, want 4", got.SelectedRadio)
	}
	if got.Connected {
		t.Error("Connected = true with no session wired")
	}

	a.voice.mu.Lock()
	a.voice.sess = &fakeVoiceSession{}
	a.voice.mu.Unlock()

	if !a.VoiceState().Connected {
		t.Error("Connected = false with a session wired")
	}
}

// TestVoiceRedirectIsANoOpWhileDisconnected proves handleVoiceRedirect --
// the state.Store voice-credentials observer that would redial a live
// session after a VOICE_ADDRESS_UPDATE -- never dials while there is no
// live session (in particular: SyncClient's OWN initial SetVoiceCredentials
// call at connect must not trip it before startVoiceSession has run).
func TestVoiceRedirectIsANoOpWhileDisconnected(t *testing.T) {
	a, _, _ := newTestApp(t)
	// No voice session wired. Firing the credentials-changed path must not
	// panic or attempt a real dial (which would need a socket this sandbox
	// blocks).
	a.st.SetVoiceCredentials("secret", "10.0.0.9:5002", "")
	if a.voiceSession() != nil {
		t.Fatal("a live session appeared from nowhere")
	}
}

// --- Generation-gated install/teardown race (task-11b Priority 1) ---------
//
// handleVoiceRedirect dials a replacement session with a.voice.mu released
// (the dial is network I/O); a concurrent Disconnect or ServiceShutdown can
// tear the live session down before that dial completes. Without a
// generation check, setVoiceSession would install the stale dial's result
// unconditionally afterward -- a live session reappearing, transmit-capable,
// after the UI already reports disconnected. voiceSessionSwap is the seam
// that decision lives in; it is exercised directly here with fakes, exactly
// as the review asked for, since a real dial cannot run in this sandbox
// (bind(2) is blocked).

// TestVoiceSessionSwapRejectsAStaleGeneration proves a dial that captured
// an old generation -- because a teardown or a newer start ran while it was
// in flight -- is closed instead of installed, and the currently-live
// session is left untouched.
func TestVoiceSessionSwapRejectsAStaleGeneration(t *testing.T) {
	a, _, _ := newTestApp(t)

	live := &fakeVoiceSession{}
	a.voice.mu.Lock()
	a.voice.sess = live
	a.voice.gen = 5 // a teardown/newer start advanced past what the stale dial captured
	a.voice.mu.Unlock()

	stale := &fakeVoiceSession{}
	old, installed := a.voiceSessionSwap(stale, 4)

	if installed {
		t.Fatal("installed = true, want false for a stale generation")
	}
	if old != nil {
		t.Fatalf("old = %v, want nil when the dial is rejected", old)
	}
	if got := stale.closedCount(); got != 1 {
		t.Fatalf("stale.closedCount() = %d, want 1 -- a rejected dial must be closed, not leaked", got)
	}
	if got := a.voiceSession(); got != live {
		t.Fatal("the live session was replaced by a stale dial")
	}
}

// TestVoiceSessionSwapInstallsOnTheCurrentGeneration is the happy-path
// counterpart: a dial whose captured generation still matches installs
// normally, is not closed, and reports the session it replaced.
func TestVoiceSessionSwapInstallsOnTheCurrentGeneration(t *testing.T) {
	a, _, _ := newTestApp(t)

	prior := &fakeVoiceSession{}
	a.voice.mu.Lock()
	a.voice.sess = prior
	a.voice.gen = 5
	a.voice.mu.Unlock()

	fresh := &fakeVoiceSession{}
	old, installed := a.voiceSessionSwap(fresh, 5)

	if !installed {
		t.Fatal("installed = false, want true for the current generation")
	}
	if old != voiceSessionAPI(prior) {
		t.Fatalf("old = %v, want the previously-live session", old)
	}
	if got := fresh.closedCount(); got != 0 {
		t.Fatalf("fresh.closedCount() = %d, want 0 -- an installed session must not be closed", got)
	}
	if got := a.voiceSession(); got != fresh {
		t.Fatal("the current-generation session was not installed")
	}
}

// TestStopVoiceSessionInvalidatesAnInFlightDial is the integration-shaped
// version of the same race: it drives the exact sequence startVoiceSession
// / stopVoiceSession / setVoiceSession would run across a real redirect,
// through the same generation seam, and confirms a dial that "completes"
// after a teardown never reinstalls a session.
func TestStopVoiceSessionInvalidatesAnInFlightDial(t *testing.T) {
	a, _, _ := newTestApp(t)

	live := &fakeVoiceSession{}
	a.voice.mu.Lock()
	a.voice.sess = live
	a.voice.mu.Unlock()

	// A redirect captures the generation before its dial begins.
	gen := a.nextVoiceGeneration()

	// The user disconnects (or the app quits) while that dial is still in
	// flight -- teardown bumps the generation and clears the live session.
	a.stopVoiceSession()
	if got := a.voiceSession(); got != nil {
		t.Fatal("stopVoiceSession did not clear the live session")
	}

	// The stale dial "completes" and tries to install.
	redialed := &fakeVoiceSession{}
	old, installed := a.voiceSessionSwap(redialed, gen)

	if installed {
		t.Fatal("a dial captured before a teardown must not install afterward")
	}
	if old != nil {
		t.Fatalf("old = %v, want nil", old)
	}
	if got := redialed.closedCount(); got != 1 {
		t.Fatalf("redialed.closedCount() = %d, want 1", got)
	}
	if got := a.voiceSession(); got != nil {
		t.Fatal("disconnected state must not have gained a live session")
	}
}

// --- Server-settings-driven RX refresh (task-11b Priority 2) --------------

// TestSettingsChangeRefreshesRXContext proves state.Store's new
// OnSettingsChanged observer is wired to refreshRXContext: a server-side
// change to the global/test frequency lists reaches a live session's RX
// filter immediately, without needing a radios change or a reconnect
// first (the gap Priority 2 of task-11b's review flagged).
func TestSettingsChangeRefreshesRXContext(t *testing.T) {
	a, _, _ := newTestApp(t)
	sess := wireTestVoice(t, a, []config.Radio{
		{ID: 1, Name: "Radio 1", FrequencyKHz: 30000, Enabled: true},
	})
	before := sess.rxCallCount()

	a.st.SetSettings(&srspb.ServerSettings{
		GlobalFrequencies: []float32{251.000},
		TestFrequencies:   []float32{100.500},
	})

	if got := sess.rxCallCount(); got != before+1 {
		t.Fatalf("SetRXContext called %d times after SetSettings, want %d", got, before+1)
	}
	last := sess.lastRX()
	if len(last.global) != 1 || last.global[0] != voice.KHz(251000) {
		t.Fatalf("global freqs = %v, want [251000]", last.global)
	}
	if len(last.test) != 1 || last.test[0] != voice.KHz(100500) {
		t.Fatalf("test freqs = %v, want [100500]", last.test)
	}
}

// --- C1: UpdateRadioInfo write-through --------------------------------------

// TestUpdateRadioInfo_WritesThroughLocalConfig is the C1 regression. Before
// this fix, UpdateRadioInfo only pushed to the server: resolveTXTarget and
// refreshRXContext both read sb.cfg.Radios directly and neither was ever
// written, so a UI tune from 251.000 to 130.000 left TX targeting 251000 kHz
// and the RX accept list at [251000] forever -- exactly the divergence the
// whole-branch review's reviewer captured empirically. This test asserts the
// RESOLVED values, not just that config was written, because a half-fix that
// persists to disk but leaves the in-memory sb.cfg pointer stale would still
// pass a config-file-only assertion.
func TestUpdateRadioInfo_WritesThroughLocalConfig(t *testing.T) {
	a, _, _ := newTestApp(t)
	fc := &fakeControlSession{}
	a.sess = fc
	sess := wireTestVoice(t, a, []config.Radio{
		{ID: 1, Name: "Radio 1", FrequencyKHz: 251000, Enabled: true},
	})
	a.st.SetSelectedRadio(1)

	// Confirm the PRE-tune baseline actually resolves the OLD frequency, so
	// this test cannot pass vacuously.
	if target := a.resolveTXTarget("radio.1.ptt"); target == nil || target.Freq != voice.KHz(251000) {
		t.Fatalf("pre-tune resolveTXTarget = %+v, want 251000 kHz", target)
	}

	if err := a.UpdateRadioInfo(RadioInfoDTO{Radios: []RadioDTO{
		{ID: 1, Name: "Radio 1", Frequency: 130.000, Enabled: true},
	}}); err != nil {
		t.Fatalf("UpdateRadioInfo: %v", err)
	}

	// TX: resolveTXTarget must resolve the NEW frequency immediately --
	// no server round trip, no OnRadiosChanged echo required.
	target := a.resolveTXTarget("radio.1.ptt")
	if target == nil || target.Freq != voice.KHz(130000) {
		t.Fatalf("resolveTXTarget after tune = %+v, want 130000 kHz", target)
	}

	// RX: refreshRXContext must produce the NEW accept list.
	a.refreshRXContext()
	last := sess.lastRX()
	if len(last.accept) != 1 || last.accept[0] != voice.KHz(130000) {
		t.Fatalf("refreshRXContext accept list = %v, want [130000]", last.accept)
	}

	// The server push still happens, unchanged from before this fix.
	if got := fc.updateCount(); got != 1 {
		t.Fatalf("UpdateRadioInfo pushed to the server %d times, want 1", got)
	}
	if got := fc.lastUpdate().GetRadios()[0].GetFrequency(); got != 130.000 {
		t.Fatalf("pushed frequency = %v, want 130.000", got)
	}
}

// TestUpdateRadioInfo_PersistsToDisk proves the write-through survives a
// fresh App built from the same config path -- i.e. it goes through
// config.Save, not just an in-memory sb.cfg swap -- matching the "persist
// locally" half of design §9.2 and DoD 7.
func TestUpdateRadioInfo_PersistsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	a, _, _ := newTestAppWithPath(t, path)
	a.sess = &fakeControlSession{}

	if err := a.UpdateRadioInfo(RadioInfoDTO{Radios: []RadioDTO{
		{ID: 7, Name: "Rescue", Frequency: 121.500, Enabled: true, IsIntercom: false},
	}}); err != nil {
		t.Fatalf("UpdateRadioInfo: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(loaded.Radios) != 1 || loaded.Radios[0].ID != 7 || loaded.Radios[0].FrequencyKHz != 121500 {
		t.Fatalf("loaded.Radios = %+v, want one radio at 121500 kHz", loaded.Radios)
	}
}

// TestUpdateRadioInfo_NoSettingsBackendIsANoOp proves UpdateRadioInfo never
// panics on an App built with no settings backend (e.g. an early test
// double), matching persistRadios'/persistSelectedRadio's documented nil
// no-op discipline.
func TestUpdateRadioInfo_NoSettingsBackendIsANoOp(t *testing.T) {
	a := NewForTest(state.New(), &fakeControlSession{}, nil)
	if err := a.UpdateRadioInfo(RadioInfoDTO{Radios: []RadioDTO{
		{ID: 1, Name: "Radio 1", Frequency: 30.000, Enabled: true},
	}}); err != nil {
		t.Fatalf("UpdateRadioInfo: %v", err)
	}
}

// --- M4: selected-radio persistence -----------------------------------------

// TestSelectRadio_PersistsSelection is the M4 regression: SelectRadio (the
// UI binding, clicking a radio card) must persist the selection to local
// config, not just state.Store, or global.ptt resolves to nothing on every
// fresh launch until the user clicks a radio card again.
func TestSelectRadio_PersistsSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	a, _, _ := newTestAppWithPath(t, path)

	if err := a.SelectRadio(3); err != nil {
		t.Fatalf("SelectRadio: %v", err)
	}
	if got := a.st.SelectedRadio(); got != 3 {
		t.Fatalf("SelectedRadio() = %d, want 3", got)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.SelectedRadioID != 3 {
		t.Fatalf("loaded.SelectedRadioID = %d, want 3", loaded.SelectedRadioID)
	}

	// A fresh App built from what's now ON DISK must restore the selection
	// -- this is the actual "survives a restart" guarantee manual check 9
	// expects. main.go's own startup does exactly this: config.Load(path)
	// followed by SetSettingsBackend.
	fresh := newTestAppWithConfig(t, loaded, path)
	if got := fresh.st.SelectedRadio(); got != 3 {
		t.Fatalf("fresh App's SelectedRadio() = %d, want 3 (restored from config)", got)
	}
}

// TestRadioSelectAction_PersistsSelection proves the hotkey path
// (radio.<n>.select) persists exactly like the SelectRadio binding does.
func TestRadioSelectAction_PersistsSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	a, _, _ := newTestAppWithPath(t, path)

	a.Pressed("radio.9.select")
	if got := a.st.SelectedRadio(); got != 9 {
		t.Fatalf("SelectedRadio() = %d, want 9", got)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.SelectedRadioID != 9 {
		t.Fatalf("loaded.SelectedRadioID = %d, want 9", loaded.SelectedRadioID)
	}
}

// --- I1: Reconnect re-pushes persisted radios -------------------------------

// TestReconnect_RePushesPersistedRadios is the I1 regression. App.Connect
// re-pushes the locally persisted radio set after a.sess.Connect
// (pushPersistedRadios), because the server creates every client with zero
// radios; App.Reconnect used to skip that call entirely, so after a
// reconnect the server had nothing to relay to us and receive stayed dead
// even though the control connection (and the UI's banner) reported
// connected.
func TestReconnect_RePushesPersistedRadios(t *testing.T) {
	a, _, _ := newTestApp(t) // config.Default() seeds four radios
	fc := &fakeControlSession{}
	a.sess = fc

	if err := a.Reconnect(); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}

	if got := fc.updateCount(); got != 1 {
		t.Fatalf("UpdateRadioInfo pushed %d times on Reconnect, want 1", got)
	}
	if got := len(fc.lastUpdate().GetRadios()); got != 4 {
		t.Fatalf("pushed %d radios on Reconnect, want the 4 seeded by config.Default()", got)
	}
}

// TestReconnect_PropagatesSessionError proves a failed Reconnect still
// returns the error (and never pushes radios against a session that just
// failed to re-establish).
func TestReconnect_PropagatesSessionError(t *testing.T) {
	a, _, _ := newTestApp(t)
	fc := &fakeControlSession{}
	a.sess = &failingReconnectSession{fakeControlSession: fc}

	if err := a.Reconnect(); err == nil {
		t.Fatal("Reconnect() = nil error, want the session's error")
	}
	if got := fc.updateCount(); got != 0 {
		t.Fatalf("UpdateRadioInfo pushed %d times after a failed Reconnect, want 0", got)
	}
}

// failingReconnectSession wraps fakeControlSession with a Reconnect that
// always fails, for TestReconnect_PropagatesSessionError.
type failingReconnectSession struct {
	*fakeControlSession
}

func (f *failingReconnectSession) Reconnect(context.Context) error {
	return errReconnectFailed
}

var errReconnectFailed = errors.New("reconnect failed")

// --- Sink/Source wiring test -----------------------------------------------

// wiringFakeSession is a minimal rtSession (WriteFrame + ReadInto) double,
// used ONLY by TestSinkAndSourceBothReachTheBridge to stand in for a live
// voice session without ever opening a socket. It is not a voiceSessionAPI
// (this test is about the REALTIME bridge, not the control-plane session
// interface the TX-routing tests above exercise).
type wiringFakeSession struct {
	mu      sync.Mutex
	written [][]float32
	reads   int
	rxValue float32 // ReadInto fills every sample with this constant
}

func (w *wiringFakeSession) WriteFrame(frame []float32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.written = append(w.written, append([]float32(nil), frame...))
}

func (w *wiringFakeSession) ReadInto(buf []float32) {
	w.mu.Lock()
	w.reads++
	v := w.rxValue
	w.mu.Unlock()
	for i := range buf {
		buf[i] = v
	}
}

func (w *wiringFakeSession) writeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.written)
}

func (w *wiringFakeSession) readCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reads
}

// TestSinkAndSourceBothReachTheBridge is the wiring test the brief demands:
// it proves BOTH halves of the audio<->voice seam are live through a REAL
// audio.Manager (fake backend, real ticker), not just that sessionBridge
// forwards calls in isolation.
//
// Registering only the Sink wires transmit; receive stays silent with
// every existing test in the repo still green, because Task 9's RX tests
// exercise internal/voice directly rather than through the Manager. This
// test fails red if SetSource (or AddSink) is ever forgotten, which
// nothing else in this repository would catch -- it is the reason
// wiringFakeSession exists at all rather than just re-using
// fakeVoiceSession: the bug this guards is specifically about the Manager
// reaching the bridge's realtime methods, not about anything the
// control-plane interface can observe.
func TestSinkAndSourceBothReachTheBridge(t *testing.T) {
	backend := audio.NewFakeBackend()
	m := audio.NewManager(backend, audio.ManagerOptions{
		PollInterval: time.Hour, VUInterval: time.Hour,
	})
	t.Cleanup(m.Stop)

	bridge := &sessionBridge{}
	m.AddSink(bridge)
	m.SetSource(bridge)

	fake := &wiringFakeSession{rxValue: 0.5}
	var rt rtSession = fake
	bridge.sess.Store(&rt)

	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Open the gate and push one captured frame through the fake backend's
	// capture callback -- this is what makes it through gate.Step into
	// dspLoop's sinks loop, exactly as a real PTT press would.
	m.SetPTT(true)
	backend.PushFrame(make([]float32, audio.FrameSamples))

	deadline := time.Now().Add(2 * time.Second)
	for fake.writeCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fake.writeCount() == 0 {
		t.Fatal("TRANSMIT wiring is broken: the Manager never called WriteFrame on the registered Source/Sink -- AddSink is missing or not reaching dspLoop")
	}

	// The playback callback pulls from playbackRing, which dspLoop fills
	// from mixer.Mix(monitor, rxBuf, sfx, notif) every tick -- and rxBuf is
	// populated ONLY by Source.ReadInto. If SetSource was never called (the
	// exact trap this test exists to catch), rxBuf stays permanently
	// cleared and fake.ReadInto is simply never invoked, no matter how long
	// this waits.
	deadline = time.Now().Add(2 * time.Second)
	for fake.readCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fake.readCount() == 0 {
		t.Fatal("RECEIVE wiring is broken: the Manager never called ReadInto on the registered Source -- SetSource is missing (Task 9's own tests would not catch this)")
	}
}
