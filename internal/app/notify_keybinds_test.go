package app

import (
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// withNotifier builds an App with a live notifier and returns both.
func withNotifier(t *testing.T) (*App, *notify.Notifier) {
	t.Helper()
	a := NewForTest(state.New(), nil, nil)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	return a, n
}

// waitForNotify waits until the notifier's snapshot satisfies cond and
// returns that snapshot.
//
// Every notification test in this package that has to wait for a COALESCING
// WINDOW to close goes through here rather than through a time.Sleep sized
// against the injected window. The trailing edge is delivered by a
// time.AfterFunc goroutine (internal/notify/coalesce.go), so a sleep is a
// bet that the runtime scheduled that goroutine inside the chosen margin.
// The form this replaces slept 120 ms against a 30 ms window -- a 4x margin,
// which is exactly the shape of a test that passes locally a thousand times
// and fails on a loaded box, with no failure message that points at the
// scheduler. Waiting on the STATE removes the bet: the timeout below is 1s,
// roughly 30x the injected window, and is reached only when the state
// genuinely never arrives.
//
// cond is also where the old form could PANIC rather than fail: it indexed
// n.Snapshot().Items[0] unguarded, so a regression that left the list empty
// crashed the whole test binary and took every other test's result with it.
// Every cond passed here MUST check length before indexing; the timeout path
// below is a clean t.Fatalf naming what was awaited and printing the
// snapshot that never satisfied it.
func waitForNotify(t *testing.T, n *notify.Notifier, what string, cond func(notify.Snapshot) bool) notify.Snapshot {
	t.Helper()
	const timeout = time.Second
	deadline := time.Now().Add(timeout)
	for {
		s := n.Snapshot()
		if cond(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s; snapshot = %+v", timeout, what, s)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestGlobalHotkeyFailureRaisesOnceWithPermission(t *testing.T) {
	a, n := withNotifier(t)

	dto := HotkeyStateDTO{
		Registered: false,
		Error:      "no backend",
		Failed:     map[string]string{"global.ptt": "unsupported key"},
		Permission: "denied",
	}
	// Emitted repeatedly, as emitHotkeyState genuinely is.
	for i := 0; i < 10; i++ {
		a.notifyHotkeyState(dto)
	}

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1 -- a global failure notifies ONCE, and the per-binding rows must be suppressed while Registered is false", len(items))
	}
	it := items[0]
	if it.Key != "hotkeys.global" {
		t.Fatalf("Key = %q, want \"hotkeys.global\"", it.Key)
	}
	if it.Severity != notify.SeverityError {
		t.Fatalf("Severity = %q, want error", it.Severity)
	}
	if !strings.Contains(it.Body, "no backend") {
		t.Fatalf("Body = %q, want it to carry the error text", it.Body)
	}
	found := false
	for _, kv := range it.Context {
		if kv.Key == "PERMISSION" && kv.Value == "denied" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Context = %v, want a PERMISSION row carrying \"denied\"", it.Context)
	}
	if len(it.Actions) != 1 || it.Actions[0].Kind != "navigate" || it.Actions[0].Target != "settings" {
		t.Fatalf("Actions = %v, want one navigate->settings action", it.Actions)
	}
}

func TestPermissionChangeUpdatesInPlaceRatherThanStacking(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e", Permission: "denied"})
	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e", Permission: "granted"})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1 -- the permission state is inside the fingerprint, so this UPDATES", len(items))
	}
	if !strings.Contains(contextValue(items[0], "PERMISSION"), "granted") {
		t.Fatalf("PERMISSION = %q, want \"granted\"", contextValue(items[0], "PERMISSION"))
	}
}

func contextValue(it notify.Item, key string) string {
	for _, kv := range it.Context {
		if kv.Key == key {
			return kv.Value
		}
	}
	return ""
}

func TestPerBindingFailuresOnlyWhileRegistered(t *testing.T) {
	a, n := withNotifier(t)

	// Registered TRUE with a partial failure: nineteen bindings work, one
	// Numpad7 does not.
	a.notifyHotkeyState(HotkeyStateDTO{
		Registered: true,
		Failed:     map[string]string{"global.ptt": "Numpad7 cannot be registered"},
		Permission: "granted",
	})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	if items[0].Key != "hotkeys.binding.global.ptt" {
		t.Fatalf("Key = %q, want \"hotkeys.binding.global.ptt\"", items[0].Key)
	}
	if items[0].Severity != notify.SeverityWarn {
		t.Fatalf("Severity = %q, want warn", items[0].Severity)
	}
	// "Global PTT" is the actual registry label for "global.ptt" (see
	// keybinds.StaticActions) -- corrected from the brief's "Push-To-Talk",
	// which does not match any registered action label.
	if !strings.Contains(items[0].Title, "Global PTT") && !strings.Contains(items[0].Title, "global.ptt") {
		t.Fatalf("Title = %q, want it to NAME the action", items[0].Title)
	}
}

// TestPerBindingFailuresRaiseInSortedOrder pins the sort in the per-binding
// raise loop. `Failed` is a MAP, so its range order is randomised by the
// runtime on every pass; each raise PREPENDS (notify.insertLocked), so the
// raise order is exactly the reverse of the resulting list order. Without the
// sort the popout's rows would shuffle between otherwise identical emits, and
// with five failing bindings that is a 1-in-120 chance of looking stable.
func TestPerBindingFailuresRaiseInSortedOrder(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyHotkeyState(HotkeyStateDTO{
		Registered: true,
		Permission: "granted",
		Failed: map[string]string{
			"channel.fleet": "a",
			"channel.ship":  "b",
			"global.ptt":    "c",
			"status.afk":    "d",
			"status.combat": "e",
		},
	})

	// Ascending by action id on the way in, so newest-first on the way out.
	want := []string{
		"hotkeys.binding.status.combat",
		"hotkeys.binding.status.afk",
		"hotkeys.binding.global.ptt",
		"hotkeys.binding.channel.ship",
		"hotkeys.binding.channel.fleet",
	}
	items := n.Snapshot().Items
	if len(items) != len(want) {
		t.Fatalf("Items = %d, want %d: %+v", len(items), len(want), items)
	}
	for i, k := range want {
		if items[i].Key != k {
			got := make([]string, len(items))
			for j, it := range items {
				got[j] = it.Key
			}
			t.Fatalf("item %d Key = %q, want %q; full order = %v", i, items[i].Key, k, got)
		}
	}
}

func TestPerBindingFailureResolvesWhenItLeavesFailed(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyHotkeyState(HotkeyStateDTO{
		Registered: true,
		Failed:     map[string]string{"global.ptt": "bad key"},
		Permission: "granted",
	})
	a.notifyHotkeyState(HotkeyStateDTO{Registered: true, Failed: map[string]string{}, Permission: "granted"})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1 (retained, resolved)", len(items))
	}
	if !items[0].Resolved {
		t.Fatal("the per-binding item did not resolve when the action left Failed")
	}
}

func TestGlobalSuccessResolvesEverything(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e", Permission: "denied"})
	a.notifyHotkeyState(HotkeyStateDTO{Registered: true, Failed: map[string]string{}, Permission: "granted"})

	for _, it := range n.Snapshot().Items {
		if !it.Resolved {
			t.Fatalf("item %q is still unresolved after a successful registration", it.Key)
		}
	}
	if got := n.Snapshot().Unread; got != 0 {
		t.Fatalf("Unread = %d, want 0", got)
	}
}

func TestJoystickUnsupportedIsInfoAndNeverAnError(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyJoystickState(JoystickStateDTO{Supported: false, Devices: []JoystickDeviceDTO{}})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	it := items[0]
	if it.Severity != notify.SeverityInfo {
		t.Fatalf("Severity = %q, want info -- \"unsupported\" must NEVER render as a failure", it.Severity)
	}
	if it.Unread {
		t.Fatal("the unsupported notice is unread; info is raised already-read so it never reaches the badge, bell or toast")
	}
	if len(it.Actions) != 0 {
		t.Fatalf("Actions = %v, want none -- there is nothing for the user to grant", it.Actions)
	}
	if got := n.Snapshot().Unread; got != 0 {
		t.Fatalf("Unread = %d, want 0", got)
	}
}

func TestJoystickErrorIsWarnAndResolves(t *testing.T) {
	a, n := withNotifier(t)

	// A short injected window, not the real 2s notify.WindowJoystick: see
	// setNotifyWindows. What is under test is that the item resolves once
	// the trailing timer fires, not how long the window is.
	a.setNotifyWindows(30*time.Millisecond, 0)

	a.notifyJoystickState(JoystickStateDTO{Supported: true, Error: "permission denied on /dev/input", Devices: []JoystickDeviceDTO{}})
	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Severity != notify.SeverityWarn {
		t.Fatalf("items = %v, want one warn item", items)
	}

	a.notifyJoystickState(JoystickStateDTO{Supported: true, Error: "", Devices: []JoystickDeviceDTO{}})
	// The Resolve lands inside the still-open coalescing window (see
	// internal/notify/coalesce.go), so it is deferred to the trailing timer
	// rather than applied synchronously -- the same behaviour
	// internal/notify/coalesce_test.go's TestFlapThatStopsStillEmitsItsSettledState
	// exercises. Waited on as a STATE, not as a duration: see waitForNotify.
	waitForNotify(t, n, "the joystick item to resolve once the trailing timer fires",
		func(s notify.Snapshot) bool { return len(s.Items) == 1 && s.Items[0].Resolved })
}

func TestNotifyWithNoNotifierDoesNotPanic(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e"})
	a.notifyJoystickState(JoystickStateDTO{Supported: false})
}

func TestPerBindingFailureReturnsAfterClearAll(t *testing.T) {
	a, n := withNotifier(t)

	failing := HotkeyStateDTO{
		Registered: true,
		Failed:     map[string]string{"global.ptt": "unsupported key"},
		Permission: "granted",
	}
	a.notifyHotkeyState(failing)
	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items = %d, want 1 before CLEAR ALL", got)
	}

	// The user presses CLEAR ALL. The chord is STILL unregisterable, so the
	// warning is expected back the next time the state is emitted -- once
	// the condition has been through a clear-and-recur cycle. Without the
	// adapter's own memory the resolve loop lost the key with the list, so
	// the dismissal suppression Clear installed could never be lifted and
	// the warning never returned, for the life of the process.
	n.Clear()

	// A rebind that fixes the chord, then breaks it again: exactly what the
	// user does next.
	a.notifyHotkeyState(HotkeyStateDTO{Registered: true, Failed: map[string]string{}, Permission: "granted"})
	a.notifyHotkeyState(failing)

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1 -- the per-binding warning never came back after CLEAR ALL", len(items))
	}
	if items[0].Resolved {
		t.Fatal("the returning warning is already resolved")
	}
	if items[0].Key != bindingKey("global.ptt") {
		t.Fatalf("Key = %q, want %q", items[0].Key, bindingKey("global.ptt"))
	}
}

func TestPerBindingResolveSurvivesClearAll(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyHotkeyState(HotkeyStateDTO{
		Registered: true,
		Failed:     map[string]string{"global.ptt": "unsupported key"},
		Permission: "granted",
	})
	n.Clear()

	// The action stops failing. The resolve must still fire even though the
	// list the old implementation read its previous state out of is empty --
	// a Resolve is the ONLY thing that lifts Clear's suppression, so losing
	// it is what made the mute permanent.
	a.notifyHotkeyState(HotkeyStateDTO{Registered: true, Failed: map[string]string{}, Permission: "granted"})

	// The same condition recurs, byte-identically.
	a.notifyHotkeyState(HotkeyStateDTO{
		Registered: true,
		Failed:     map[string]string{"global.ptt": "unsupported key"},
		Permission: "granted",
	})

	if got := len(n.Snapshot().Items); got != 1 {
		t.Fatalf("Items = %d, want 1 -- CLEAR ALL permanently disabled this binding's warning", got)
	}
}

// TestUnsupportedOutranksAnErrorOnTheJoystickState pins the PRECEDENCE of
// notifyJoystickState's `case !dto.Supported:` over its `case dto.Error !=
// "":` sibling -- the guard that keeps macOS's "unsupported" Informational
// even when an error string is also present.
//
// Narrowing the first case to `!dto.Supported && dto.Error == ""` survived
// the suite. In production the two are practically exclusive, because
// GetJoystickState only fills Error when err != nil && jm.Supported(); but
// the DTO is a plain struct that any future call site can populate, and the
// rule it encodes -- "unsupported is informational, NEVER a failure", spec
// 4.2 -- is the one trap this adapter documents at length. Info severity is
// raised already-read, so it reaches neither badge, bell nor toast; a Warn
// would reach the badge and the bell, telling a macOS user their joystick
// is broken when the platform simply has no joystick support to offer.
func TestUnsupportedOutranksAnErrorOnTheJoystickState(t *testing.T) {
	a, n := withNotifier(t)

	a.notifyJoystickState(JoystickStateDTO{Supported: false, Error: "enumerate: no such device"})

	items := n.Snapshot().Items
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	it := items[0]
	if it.Severity != notify.SeverityInfo {
		t.Fatalf("Severity = %q, want info -- an unsupported PLATFORM is not a failure, "+
			"whatever else the DTO carries; warn would light the badge and the bell on "+
			"a machine where there is nothing for the user to fix", it.Severity)
	}
	if !strings.Contains(it.Title, "unsupported") {
		t.Fatalf("Title = %q, want the unsupported-platform title, not the error one", it.Title)
	}
	if len(it.Actions) != 0 {
		t.Fatalf("Actions = %v, want none -- there is nothing for the user to grant or open", it.Actions)
	}
	if snap := n.Snapshot(); snap.Unread != 0 {
		t.Fatalf("Unread = %d, want 0 -- info is raised already-read", snap.Unread)
	}
}
