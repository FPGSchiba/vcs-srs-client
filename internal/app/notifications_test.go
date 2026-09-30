package app

import (
	"reflect"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

func TestGetNotificationsWithNoNotifierIsEmpty(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)

	got := a.GetNotifications()
	if len(got.Items) != 0 {
		t.Fatalf("Items = %d, want 0", len(got.Items))
	}
	if got.Items == nil {
		t.Fatal("Items is nil; must be [] so the frontend's list type holds")
	}
}

func TestFocusMainWindowWithNoWailsAppDoesNotPanic(t *testing.T) {
	// Every test builds an App with no Wails application. The main window
	// is not a Registry entry -- it is a Wails window resolved by name --
	// so this binding must tolerate that.
	a := NewForTest(state.New(), nil, nil)
	a.FocusMainWindow()
}

func TestNotificationBindingsWithNoNotifierDoNotPanic(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)

	// Every binding must tolerate a nil notifier, the same discipline
	// settings.audio and settings.joy already follow: nil in tests and in
	// any build where wiring failed.
	a.MarkNotificationRead("n1")
	a.MarkAllNotificationsRead()
	a.DismissNotification("n1")
	a.ClearNotifications()
}

func TestGetNotificationsReflectsTheNotifier(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	n := notify.New(notify.Options{})
	a.setNotifier(n)

	n.Post(notify.Item{Title: "hello", Severity: notify.SeverityWarn})

	got := a.GetNotifications()
	if len(got.Items) != 1 {
		t.Fatalf("Items = %d, want 1", len(got.Items))
	}
	if got.Items[0].Title != "hello" {
		t.Fatalf("Title = %q, want \"hello\"", got.Items[0].Title)
	}
	if got.Unread != 1 {
		t.Fatalf("Unread = %d, want 1", got.Unread)
	}
}

func TestMarkAllNotificationsReadReachesTheNotifier(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	n.Post(notify.Item{Title: "x", Severity: notify.SeverityError})

	a.MarkAllNotificationsRead()

	if got := a.GetNotifications().Unread; got != 0 {
		t.Fatalf("Unread = %d, want 0", got)
	}
}

func TestDismissNotificationReachesTheNotifier(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	id := n.Post(notify.Item{Title: "x", Severity: notify.SeverityWarn})

	a.DismissNotification(id)

	if got := len(a.GetNotifications().Items); got != 0 {
		t.Fatalf("Items = %d, want 0", got)
	}
}

func TestClearNotificationsReachesTheNotifier(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	n.Post(notify.Item{Title: "x", Severity: notify.SeverityWarn})
	n.Post(notify.Item{Title: "y", Severity: notify.SeverityWarn})

	a.ClearNotifications()

	if got := len(a.GetNotifications().Items); got != 0 {
		t.Fatalf("Items = %d, want 0", got)
	}
}

// TestNotificationSeamsAreNotOnTheExportedMethodSet asserts the M-9 / fix
// wave 6 invariant DIRECTLY, rather than by reading main.go.
//
// main.go registers gui with application.NewService, which binds every
// exported METHOD on *App into the webview. Four notification seams must
// therefore stay off that set:
//
//	notifyAudioState / playNotificationSFX -- bound, they would let the
//	    renderer fabricate an error-severity "Microphone unavailable" item
//	    with arbitrary body text, or silently RESOLVE a genuine microphone
//	    fault out of the user's list by passing a clean DTO (ruling R13).
//	setNotifier -- strictly worse: SetNotifier(null) sets a.notif to nil and
//	    every binding and adapter here then early-returns, so the whole
//	    channel goes silently dead for the session -- no badge, no bell, no
//	    toast, no sound.
//	Notifier -- an accessor that never had a caller. Deleted in fix wave 6;
//	    named here so re-adding it fails rather than quietly re-widening the
//	    surface.
//
// main_wiring_test.go's TestNotificationAdaptersStayOffTheBoundServiceSurface
// is the other half of the pair: it greps main.go's CALL SITES, which is the
// only way to see a caller reverting to the method form. It cannot see a
// method being re-added to *App with no main.go caller, and this cannot see
// a call site. Reflection over the real method set is what makes the
// invariant an invariant rather than a text convention.
func TestNotificationSeamsAreNotOnTheExportedMethodSet(t *testing.T) {
	banned := map[string]string{
		"NotifyAudioState":    "the renderer could fabricate or silently resolve an audio fault; use the package-level app.NotifyAudioState(a, dto) seam",
		"PlayNotificationSFX": "the renderer could play the notification alert at will; use the package-level app.PlayNotificationSFX(a, severity) seam",
		"SetNotifier":         "the renderer could call SetNotifier(null) and kill the whole notification channel for the session; use the package-level app.SetNotifier(a, n) seam",
		"Notifier":            "an exported accessor hands the renderer the notifier itself; it was deleted in fix wave 6 and must not come back",
	}

	typ := reflect.TypeOf(&App{})
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if why, bad := banned[name]; bad {
			t.Errorf("(*App).%s is an EXPORTED method, so application.NewService(gui) binds it into the webview: %s", name, why)
		}
	}
}
