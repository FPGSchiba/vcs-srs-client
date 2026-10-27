package app

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/joystick"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// boundSurface is every exported method *App may have. main.go registers
// *App with application.NewService, which binds EVERY exported method into
// the webview, so this list IS the frontend API: the methods
// frontend/src/shared/api/client.ts calls, plus DismissNotification
// (deliberate groundwork for per-item dismiss, ROADMAP 7.2) and the two Wails
// lifecycle hooks.
//
// Main-only wiring lives in package-level functions taking *App (SetApp,
// SetSettingsBackend, HotkeyHandler, ...), not methods, so it stays off the
// webview. Adding a bound method means adding its name here deliberately.
var boundSurface = []string{
	"AddTrigger", "BeginCapture", "BrowseProfilesDir", "CancelConnect", "ClearHistory", "ClearKeybind",
	"ClearNotifications", "CloseWindow", "Connect", "DeleteProfile", "Disconnect",
	"EndCapture", "ExportHistoryCSV", "ExportProfile", "FocusMainWindow", "GetAudioDevices",
	"GetAudioEffectPresets", "GetAudioState", "GetBuildInfo", "GetClientState", "GetCommsLayout",
	"GetConnectionState", "GetHistory", "GetHotkeyState", "GetJoystickState", "GetKeybinds", "GetLastServerAddress",
	"GetNotifications", "GetOpenWindows", "GetProfileState", "GetSettings", "ImportProfile",
	"ListProfiles", "LoadProfile", "MarkAllNotificationsRead", "MarkNotificationRead",
	"OpenHotkeyPermissionSettings", "OpenProfilesDir", "OpenWindow", "PreviewEffect",
	"RecheckHotkeyPermission", "Reconnect", "ReconnectVoice", "RemoveTrigger", "RenameProfile",
	"RequestHotkeyPermission", "ResetLayout", "RevertProfile", "SaveProfile", "SaveProfileAs",
	"SelectRadio", "SetCommsLayout", "SetSettings", "StartMicTest", "StopMicTest", "ToggleWindow",
	"UpdateRadioInfo", "VoiceState",
	// Not called by client.ts today; kept on purpose.
	"DismissNotification",
	// Wails lifecycle hooks (not emitted into the bindings).
	"ServiceStartup", "ServiceShutdown",
}

func TestBoundSurfaceIsTheFrontendAPI(t *testing.T) {
	want := map[string]bool{}
	for _, n := range boundSurface {
		want[n] = true
	}
	typ := reflect.TypeOf(&App{})
	got := map[string]bool{}
	for i := 0; i < typ.NumMethod(); i++ {
		got[typ.Method(i).Name] = true
	}
	var extra, missing []string
	for n := range got {
		if !want[n] {
			extra = append(extra, n)
		}
	}
	for n := range want {
		if !got[n] {
			missing = append(missing, n)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	if len(extra) > 0 {
		t.Errorf("%d exported *App methods are not in boundSurface, so they are bound into the webview: %v\n"+
			"Main-only wiring belongs in a package-level func taking *App. A method the frontend "+
			"really calls must be added to boundSurface deliberately.", len(extra), extra)
	}
	if len(missing) > 0 {
		t.Errorf("boundSurface names %d methods *App no longer has (the frontend would lose them): %v", len(missing), missing)
	}
}

var (
	_ hotkeys.Handler       = (*hotkeyHandler)(nil)
	_ hotkeys.StaleReleaser = (*hotkeyHandler)(nil)
	_ joystick.Handler      = (*hotkeyHandler)(nil)
)

// The adapter must reach the same refcounted press path the App methods
// used to: one press and one release through it make one hotkey:pressed and
// one hotkey:released.
func TestHotkeyHandlerSatisfiesBothHandlerInterfaces(t *testing.T) {
	a, em := newTestAppOn(t, state.New(), &countingRegistrar{})
	h := HotkeyHandler(a)
	h.Pressed("global.ptt")
	h.Released("global.ptt")
	waitUntil(t, time.Second, func() bool {
		return em.count("hotkey:pressed") == 1 && em.count("hotkey:released") == 1
	})
}
