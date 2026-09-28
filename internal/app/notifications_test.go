package app

import (
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
	a.SetNotifier(n)

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
	a.SetNotifier(n)
	n.Post(notify.Item{Title: "x", Severity: notify.SeverityError})

	a.MarkAllNotificationsRead()

	if got := a.GetNotifications().Unread; got != 0 {
		t.Fatalf("Unread = %d, want 0", got)
	}
}

func TestDismissNotificationReachesTheNotifier(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	n := notify.New(notify.Options{})
	a.SetNotifier(n)
	id := n.Post(notify.Item{Title: "x", Severity: notify.SeverityWarn})

	a.DismissNotification(id)

	if got := len(a.GetNotifications().Items); got != 0 {
		t.Fatalf("Items = %d, want 0", got)
	}
}

func TestClearNotificationsReachesTheNotifier(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	n := notify.New(notify.Options{})
	a.SetNotifier(n)
	n.Post(notify.Item{Title: "x", Severity: notify.SeverityWarn})
	n.Post(notify.Item{Title: "y", Severity: notify.SeverityWarn})

	a.ClearNotifications()

	if got := len(a.GetNotifications().Items); got != 0 {
		t.Fatalf("Items = %d, want 0", got)
	}
}
