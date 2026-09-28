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
	a.SetNotifier(n)
	return a, n
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

	a.notifyJoystickState(JoystickStateDTO{Supported: true, Error: "permission denied on /dev/input", Devices: []JoystickDeviceDTO{}})
	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Severity != notify.SeverityWarn {
		t.Fatalf("items = %v, want one warn item", items)
	}

	a.notifyJoystickState(JoystickStateDTO{Supported: true, Error: "", Devices: []JoystickDeviceDTO{}})
	// The Resolve lands inside WindowJoystick's still-open 2s coalescing
	// window (see internal/notify/coalesce.go), so it is deferred to the
	// trailing timer rather than applied synchronously -- the same behaviour
	// internal/notify/coalesce_test.go's TestFlapThatStopsStillEmitsItsSettledState
	// exercises. withNotifier wires no OnChange channel to wait on, so this
	// waits out the real window instead of asserting synchronously.
	time.Sleep(notify.WindowJoystick + 200*time.Millisecond)
	if !n.Snapshot().Items[0].Resolved {
		t.Fatal("the joystick item did not resolve when the error cleared")
	}
}

func TestNotifyWithNoNotifierDoesNotPanic(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	a.notifyHotkeyState(HotkeyStateDTO{Registered: false, Error: "e"})
	a.notifyJoystickState(JoystickStateDTO{Supported: false})
}
