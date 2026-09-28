package app

import (
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

func TestXrunCountersProduceNoNotification(t *testing.T) {
	a, n := withNotifier(t)

	// THE critical audio test. emitStateIfChanged compares the whole State
	// struct, xrun counters included, and runs every 2s poll tick -- so a
	// glitching engine emits ~1800 times an hour with nothing the user can
	// see having changed. The adapter's projection is what makes including
	// audio safe at all.
	base := AudioStateDTO{Running: true, InputDevice: "mic-1", OutputDevice: "spk-1"}
	for i := 0; i < 1800; i++ {
		st := base
		st.Overruns = uint64(i)
		st.Underruns = uint64(i * 2)
		a.NotifyAudioState(st)
	}

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- xrun counters must never produce a notification", got)
	}
}

func TestRunningAndStartingAreNotFaults(t *testing.T) {
	a, n := withNotifier(t)

	a.NotifyAudioState(AudioStateDTO{Running: false, Starting: true})
	a.NotifyAudioState(AudioStateDTO{Running: true, Starting: false})

	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d, want 0 -- lifecycle is not a fault", got)
	}
}

func TestInputErrorRaisesAnErrorNotification(t *testing.T) {
	a, n := withNotifier(t)

	a.NotifyAudioState(AudioStateDTO{InputError: "device not found"})

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

	a.NotifyAudioState(AudioStateDTO{InputError: "mic gone", OutputSubstituted: true, OutputDevice: "spk-default"})

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

	a.NotifyAudioState(AudioStateDTO{InputSubstituted: true, InputDevice: "mic-default"})

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

	a.NotifyAudioState(AudioStateDTO{InputSubstituted: true, InputDevice: "os-default-1"})
	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	n.Dismiss(items[0].ID)
	if got := len(n.Snapshot().Items); got != 0 {
		t.Fatalf("Items = %d after Dismiss, want 0", got)
	}

	// The OS default changes; the configured device is still missing.
	a.NotifyAudioState(AudioStateDTO{InputSubstituted: true, InputDevice: "os-default-2"})

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

	a.NotifyAudioState(AudioStateDTO{OutputSubstituted: true, OutputDevice: "spk-default"})

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

	a.NotifyAudioState(AudioStateDTO{InputError: "mic gone", OutputError: "spk gone"})
	// The output recovers; the input does not.
	a.NotifyAudioState(AudioStateDTO{InputError: "mic gone"})

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
	a.NotifyAudioState(AudioStateDTO{
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
	a.NotifyAudioState(AudioStateDTO{InputError: "e"})
}
