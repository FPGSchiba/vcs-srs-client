package app

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// TestXrunCountersProduceNoNotification is the HEALTHY-DTO half of the xrun
// guard, and on its own it is much weaker than it looks -- it was once the
// only guard on DoD 9 and it could not see the bug DoD 9 exists to prevent.
//
// Nothing here carries a fault, so all four raiseOrResolve calls in
// notifyAudioState take the RESOLVE branch and no notify.Item is ever
// constructed. The projection's CONTENTS therefore go unevaluated: folding
// dto.Overruns straight into the item's Context -- exactly the mistake the
// projection exists to prevent -- left this test passing.
//
// Keep it; "a healthy engine that is merely glitching says nothing" is a
// real assertion. But the fingerprint claim is carried by its two siblings
// below, which hold a fault OPEN while only the counters move.
func TestXrunCountersProduceNoNotification(t *testing.T) {
	a, n := withNotifier(t)

	// emitStateIfChanged compares the whole State struct, xrun counters
	// included, and runs every 2s poll tick -- so a glitching engine emits
	// ~1800 times an hour with nothing the user can see having changed. The
	// adapter's projection is what makes including audio safe at all.
	base := AudioStateDTO{Running: true, InputDevice: "mic-1", OutputDevice: "spk-1"}
	for i := 0; i < 1800; i++ {
		st := base
		st.Overruns = uint64(i)
		st.Underruns = uint64(i * 2)
		a.notifyAudioState(st)
	}

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- xrun counters must never produce a notification", got)
	}
}

// withCountingNotifier builds an App whose notifier reports how often it
// actually published and how often it asked for a sound. Counting is the
// only way to see churn: the notification LIST is idempotent under a
// re-raise of the same key, so a snapshot cannot distinguish "raised once"
// from "raised, re-raised and re-sounded 1800 times".
//
// The counters are mutex-guarded because a trailing coalescing timer can
// deliver on its own goroutine, not only on the caller's.
type notifyCounts struct {
	mu      sync.Mutex
	changes int
	sounds  int
}

func (c *notifyCounts) read() (changes, sounds int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.changes, c.sounds
}

func withCountingNotifier(t *testing.T) (*App, *notify.Notifier, *notifyCounts) {
	t.Helper()
	c := &notifyCounts{}
	a := NewForTest(state.New(), nil, nil)
	n := notify.New(notify.Options{
		// Neither callback re-enters the Notifier -- see notify.Options.
		OnChange: func(notify.Snapshot) {
			c.mu.Lock()
			c.changes++
			c.mu.Unlock()
		},
		OnSound: func(notify.Item) {
			c.mu.Lock()
			c.sounds++
			c.mu.Unlock()
		},
	})
	a.SetNotifier(n)
	t.Cleanup(n.StopTimers)
	return a, n, c
}

// TestPersistentFaultWithMovingXrunsDoesNotChurn is the real guard on DoD 9's
// "Overruns/Underruns movement provably produces NO notification", and the
// one that evaluates the projection's contents.
//
// The mic is dead and STAYS dead, so every poll constructs the
// "Microphone unavailable" item for real; the only thing that moves between
// polls is the xrun counters. Folding those into the item -- Context,
// Body, anywhere notify.fingerprint reaches -- makes every poll a
// content-change, which republishes the Snapshot and re-fires OnSound.
//
// A short injected window rather than the real 10s notify.WindowAudio, so
// each poll is a leading edge that applies synchronously instead of being
// swallowed by the coalescing window. Coalescing is NOT what is under test
// here -- it would mask the bug rather than expose it, which is precisely
// why the counters have to be checked against un-coalesced polls.
func TestPersistentFaultWithMovingXrunsDoesNotChurn(t *testing.T) {
	a, n, counts := withCountingNotifier(t)
	a.setNotifyWindows(0, 5*time.Millisecond)

	base := AudioStateDTO{
		Running:      true,
		InputDevice:  "mic-1",
		OutputDevice: "spk-1",
		InputError:   "device not found", // the fault is PERSISTENT
	}
	for i := 0; i < 20; i++ {
		st := base
		st.Overruns = uint64(i)
		st.Underruns = uint64(i * 2)
		a.notifyAudioState(st)
		time.Sleep(10 * time.Millisecond) // outlast the window: next poll is a leading edge
	}

	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items = %d, want 1 -- one persistent fault is one item", got)
	}
	changes, sounds := counts.read()
	if changes != 1 {
		t.Fatalf("OnChange fired %d times, want 1 -- a fault that has not changed must be published once, however far the xrun counters have moved", changes)
	}
	if sounds != 1 {
		t.Fatalf("OnSound fired %d times, want 1 -- the alert must not re-sound every poll tick of a glitching engine", sounds)
	}
}

// TestXrunMovementDoesNotResurrectADismissedFault is the same defect seen
// from the user's side, and it runs at the REAL notify.WindowAudio because
// this is exactly what production does.
//
// The mic is dead, the user dismisses the notification, and the mic stays
// dead. Dismiss retires the key's coalescing window (cancelPendingLocked),
// so the very next poll is a leading edge again. If the xrun counters are in
// the fingerprint, raiseLocked sees a fingerprint differing from the one the
// item was dismissed at, drops the suppression, re-inserts the item unread
// and re-lights the badge, the bell and the sound -- the R20
// dismissal-resurrection bug, on the most severe audio key there is.
func TestXrunMovementDoesNotResurrectADismissedFault(t *testing.T) {
	a, n, counts := withCountingNotifier(t)

	base := AudioStateDTO{Running: true, InputDevice: "mic-1", InputError: "device not found"}
	a.notifyAudioState(base)
	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	n.Dismiss(items[0].ID)
	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d after Dismiss, want 0", got)
	}
	_, soundsAtDismiss := counts.read()

	// The poll continues. The mic is still dead; only the counters move.
	for i := 1; i < 10; i++ {
		st := base
		st.Overruns = uint64(i)
		st.Underruns = uint64(i * 2)
		a.notifyAudioState(st)
	}

	snap := n.Snapshot()
	if len(snap.Items) != 0 {
		t.Fatalf("Items = %d, want 0 -- a dismissed fault must not come back because the xrun counters moved; the fault is unchanged", len(snap.Items))
	}
	if snap.Unread != 0 {
		t.Fatalf("Unread = %d, want 0", snap.Unread)
	}
	if _, sounds := counts.read(); sounds != soundsAtDismiss {
		t.Fatalf("OnSound fired %d more times after the dismissal, want 0", sounds-soundsAtDismiss)
	}
}

func TestRunningAndStartingAreNotFaults(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyAudioState(AudioStateDTO{Running: false, Starting: true})
	a.notifyAudioState(AudioStateDTO{Running: true, Starting: false})

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- lifecycle is not a fault", got)
	}
}

func TestInputErrorRaisesAnErrorNotification(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyAudioState(AudioStateDTO{InputError: "device not found"})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	it := items[0]
	if it.Key != "audio.input" {
		t.Fatalf("Key = %q, want \"audio.input\"", it.Key)
	}
	if it.Severity != notify.SeverityError {
		t.Fatalf("Severity = %q, want error -- a dead microphone in a voice-comms client is critical", it.Severity)
	}
	if it.Body != "device not found" {
		t.Fatalf("Body = %q, want the backend's error text", it.Body)
	}
}

func TestInputAndOutputAreIndependentKeys(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyAudioState(AudioStateDTO{InputError: "mic gone", OutputSubstituted: true, OutputDevice: "spk-default"})

	items := n.Snapshot().Items
	if len(items) != 2 {
		t.Fatalf("Items = %d, want 2 -- a failed input and a substituted output are separate items", len(items))
	}
	keys := map[string]bool{}
	for _, it := range items {
		keys[it.Key] = true
	}
	if !keys["audio.input"] || !keys["audio.output.substituted"] {
		t.Fatalf("keys = %v, want audio.input and audio.output.substituted", keys)
	}
}

func TestSubstitutionIsWarnNotError(t *testing.T) {
	a, n := withNotifier(t)
	a.settings = &settingsBackend{cfg: &config.Config{Audio: config.Audio{InputDevice: "mic-a"}}}

	a.notifyAudioState(AudioStateDTO{InputSubstituted: true, InputDevice: "mic-default"})

	it := n.Snapshot().Items[0]
	if it.Severity != notify.SeverityWarn {
		t.Fatalf("Severity = %q, want warn -- audio still works, just not on the chosen device", it.Severity)
	}
	if it.Key != "audio.input.substituted" {
		t.Fatalf("Key = %q, want \"audio.input.substituted\"", it.Key)
	}
	// The CONFIGURED device, not the substituted-in one -- see R20 and
	// TestSubstitutionDismissalSurvivesAHotPlugReshuffle below.
	if got := contextValue(it, "CONFIGURED"); got != "mic-a" {
		t.Fatalf("CONFIGURED = %q, want \"mic-a\"", got)
	}
	if contextValue(it, "IN USE") != "" {
		t.Fatal("the substitution item still carries an IN USE context row -- the resolved id is volatile and must not be in the fingerprint")
	}
	// Spec 5.4: the body names the configured device id that could not be
	// opened. It shipped as generic prose naming no device at all.
	if !strings.Contains(it.Body, "mic-a") {
		t.Fatalf("Body = %q, want it to name the configured device id", it.Body)
	}
}

// TestSubstitutionDismissalSurvivesAHotPlugReshuffle is R20's regression
// test, and the reason the substitution items are keyed on the CONFIGURED
// device rather than the resolved one.
//
// With the configured mic missing, the user dismisses the substitution
// warning. The OS default then changes underneath them -- a headset is
// plugged in -- while the configured mic is STILL missing. Nothing about the
// fault has changed, so nothing must be re-announced. While the resolved id
// rode in Context (which notify.fingerprint hashes) the differing
// fingerprint made raiseLocked delete the suppression and the dismissed
// warning came straight back; without a dismissal it re-marked the item
// unread and re-lit the launcher badge and the status-bar bell.
func TestSubstitutionDismissalSurvivesAHotPlugReshuffle(t *testing.T) {
	a, n := withNotifier(t)
	a.settings = &settingsBackend{cfg: &config.Config{Audio: config.Audio{InputDevice: "mic-a"}}}

	a.notifyAudioState(AudioStateDTO{InputSubstituted: true, InputDevice: "os-default-1"})
	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	n.Dismiss(items[0].ID)
	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d after Dismiss, want 0", got)
	}

	// The OS default changes; the configured device is still missing.
	a.notifyAudioState(AudioStateDTO{InputSubstituted: true, InputDevice: "os-default-2"})

	snap := n.Snapshot()
	if len(snap.Items) != 0 {
		t.Fatalf("Items = %d, want 0 -- a dismissed substitution warning must not come back because the OS default moved; the fault is unchanged", len(snap.Items))
	}
	if snap.Unread != 0 {
		t.Fatalf("Unread = %d, want 0", snap.Unread)
	}
}

// TestSubstitutionWithNoSettingsBackendDoesNotPanic pins the documented
// empty-id fallback: a nil settings backend is a supported state everywhere
// else in App, and reading the configured id must follow the same rule.
func TestSubstitutionWithNoSettingsBackendDoesNotPanic(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyAudioState(AudioStateDTO{OutputSubstituted: true, OutputDevice: "spk-default"})

	it := n.Snapshot().Items[0]
	if contextValue(it, "CONFIGURED") != "" {
		t.Fatalf("CONFIGURED = %q, want empty with no settings backend", contextValue(it, "CONFIGURED"))
	}
	if strings.Contains(it.Body, "()") {
		t.Fatalf("Body = %q, want the unnamed form rather than an empty parenthetical", it.Body)
	}
}

func TestEachAudioKeyResolvesIndependently(t *testing.T) {
	a, n := withNotifier(t)

	// A short injected window, not the real 10s notify.WindowAudio: the
	// second call's Resolve("audio.output") lands inside the still-open
	// coalescing window opened by the first call's Raise (see
	// internal/notify/coalesce.go), so it is deferred to the trailing timer
	// rather than applied synchronously. What is under test here is that the
	// four keys resolve INDEPENDENTLY, not how long the window is -- see
	// setNotifyWindows.
	a.setNotifyWindows(0, 30*time.Millisecond)

	a.notifyAudioState(AudioStateDTO{InputError: "mic gone", OutputError: "spk gone"})
	// The output recovers; the input does not.
	a.notifyAudioState(AudioStateDTO{InputError: "mic gone"})

	time.Sleep(120 * time.Millisecond)

	var input, output notify.Item
	for _, it := range n.Snapshot().Items {
		switch it.Key {
		case "audio.input":
			input = it
		case "audio.output":
			output = it
		}
	}
	if input.Resolved {
		t.Fatal("the input item resolved while its error was still present")
	}
	if !output.Resolved {
		t.Fatal("the output item did not resolve when its error cleared")
	}
}

func TestNoBackendDTORaisesBothErrorKeys(t *testing.T) {
	a, n := withNotifier(t)

	// main.go:225's hand-pushed DTO when NewMalgoBackend fails: no Manager
	// exists, so this is the ONLY signal that audio is dead entirely. An
	// adapter hung only off the Manager's OnState would miss it.
	a.notifyAudioState(AudioStateDTO{
		InputError:  "malgo: no backend",
		OutputError: "malgo: no backend",
	})

	items := n.Snapshot().Items
	if len(items) != 2 {
		t.Fatalf("Items = %d, want 2 -- both directions genuinely are dead", len(items))
	}
	for _, it := range items {
		if it.Severity != notify.SeverityError {
			t.Fatalf("item %q severity = %q, want error", it.Key, it.Severity)
		}
	}
}

func TestNotifyWindowDefaultsAreUnoverridden(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)

	// No setNotifyWindows call: the seam must not leak into production
	// defaults. Without this test, someone could later default
	// notifWinJoystick/notifWinAudio to a test-friendly value and nothing
	// would notice.
	if got := a.joystickWindow(); got != notify.WindowJoystick {
		t.Fatalf("joystickWindow() = %v, want notify.WindowJoystick (%v)", got, notify.WindowJoystick)
	}
	if got := a.audioWindow(); got != notify.WindowAudio {
		t.Fatalf("audioWindow() = %v, want notify.WindowAudio (%v)", got, notify.WindowAudio)
	}
}

func TestAudioNotifyWithNoNotifierDoesNotPanic(t *testing.T) {
	// state.New(), not a nil store: NewForTest's initVoice unconditionally
	// calls store.OnRadiosChanged, which dereferences a nil *state.Store --
	// see notify_keybinds_test.go's sibling TestNotifyWithNoNotifierDoesNotPanic.
	// The point under test is "no notifier", not "no state store".
	a := NewForTest(state.New(), nil, nil)
	a.notifyAudioState(AudioStateDTO{InputError: "e"})
}

// TestAudioNotificationsUseTheAudioWindow pins that the adapter actually
// COALESCES on a.audioWindow(), not merely that the accessor returns the
// right constant.
//
// TestNotifyWindowDefaultsAreUnoverridden covers the accessor; nothing
// covered the call site, so replacing `window := a.audioWindow()` in
// NotifyAudioState with notify.WindowHotkeys -- i.e. deleting audio
// coalescing outright -- left the whole suite green. Audio is the source the
// 10s window exists for: audio:state fires every 2s for as long as the
// engine is glitching, and a device flapping at the poll rate would
// otherwise publish a Snapshot and re-sound the alert on every tick.
//
// A long injected window so the second poll is unambiguously INSIDE it; the
// notifier's timers are stopped by withCountingNotifier's cleanup.
func TestAudioNotificationsUseTheAudioWindow(t *testing.T) {
	a, n, counts := withCountingNotifier(t)
	a.setNotifyWindows(0, time.Hour)

	// Leading edge: opens the window on audio.input and publishes at once.
	a.notifyAudioState(AudioStateDTO{Running: true, InputError: "device not found"})
	if changes, _ := counts.read(); changes != 1 {
		t.Fatalf("OnChange fired %d times for the leading edge, want 1", changes)
	}

	// The very next poll carries a DIFFERENT error, so identity dedupe
	// cannot be what holds it back -- only the coalescing window can.
	a.notifyAudioState(AudioStateDTO{Running: true, InputError: "device is busy"})

	changes, _ := counts.read()
	if changes != 1 {
		t.Fatalf("OnChange fired %d times, want 1 -- the second poll was published "+
			"immediately, so notifyAudioState is not coalescing on a.audioWindow() at "+
			"all and a flapping device republishes the whole Snapshot every 2s", changes)
	}
	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Body != "device not found" {
		t.Fatalf("items = %+v, want the leading edge's body still committed -- the "+
			"changed fault belongs to the open window, not to the list", items)
	}
}
