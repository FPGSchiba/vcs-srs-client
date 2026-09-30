package app

import "github.com/FPGSchiba/vcs-srs-client/internal/notify"

// setNotifier wires the notification channel. A nil notifier is legal and
// leaves every binding below inert, so a build whose wiring failed degrades
// to "no notifications" rather than crashing.
//
// UNEXPORTED, and reached from main.go through the package-level SetNotifier
// below. application.NewService(gui) binds every exported METHOD on *App
// into the webview, and a bound wiring setter is strictly worse than the two
// adapters ruling R13 and M-9 already moved off this surface: those let the
// renderer fabricate or resolve ONE item, whereas SetNotifier(null) sets
// a.notif to nil, after which every binding and every adapter in this
// package early-returns -- no badge, no bell, no toast, no sound, for the
// rest of the session, with nothing logged to explain it.
func (a *App) setNotifier(n *notify.Notifier) { a.notif = n }

// SetNotifier wires the notification channel into the App. main.go calls it
// once, at startup.
//
// A package-level FUNCTION taking the App, rather than a method on it, for
// the reason setNotifier documents: nothing package-level is bound by
// application.NewService, so main.go keeps its call and the renderer gains
// nothing. The same seam as NotifyAudioState and PlayNotificationSFX.
//
// There is deliberately no exported Notifier() accessor beside it. One
// existed until fix wave 6 and had never had a caller in any commit on this
// branch -- its doc named an audio adapter in main.go that ruling R24 had
// since moved INTO this package -- while being just as bound as this setter.
//
// SCOPE NOTE: the Set*-for-wiring pattern is older than this branch --
// SetApp, SetBackend, SetSettingsBackend and their siblings are all exported
// methods on the bound service and all carry the same exposure. They are
// deliberately left alone here; see the Phase 7.2 spec's note.
func SetNotifier(a *App, n *notify.Notifier) { a.setNotifier(n) }

// GetNotifications returns the current snapshot for a window hydrating on
// mount. Returns an empty, non-nil list when nothing is wired: the frontend
// types Items as an array, and a nil slice marshals to null.
func (a *App) GetNotifications() notify.Snapshot {
	if a.notif == nil {
		return notify.Snapshot{Items: []notify.Item{}}
	}
	return a.notif.Snapshot()
}

// MarkNotificationRead clears one item's unread flag.
func (a *App) MarkNotificationRead(id string) {
	if a.notif == nil {
		return
	}
	a.notif.MarkRead(id)
}

// MarkAllNotificationsRead clears every unread flag.
func (a *App) MarkAllNotificationsRead() {
	if a.notif == nil {
		return
	}
	a.notif.MarkAllRead()
}

// DismissNotification removes one item. For a keyed item this also suppresses
// an identical re-raise, so a condition that still holds cannot put the row
// straight back -- see notify.Notifier.Dismiss.
func (a *App) DismissNotification(id string) {
	if a.notif == nil {
		return
	}
	a.notif.Dismiss(id)
}

// ClearNotifications removes every item.
func (a *App) ClearNotifications() {
	if a.notif == nil {
		return
	}
	a.notif.Clear()
}

// FocusMainWindow shows and focuses the main client window.
//
// Needed because a notification's `navigate` action fires from the
// Notifications POPOUT, and the main window is NOT a Registry entry: it is
// created directly in main.go as a Wails window named MainWindowName, so
// windows.Open("main") would fall through windowURL's default and spawn a
// SECOND window at /main.html rather than focusing the existing one.
//
// Delegates to showMainWindow (internal/app/tray.go), which the tray menu
// and the dock-icon reactivation path already use -- one implementation of
// "reveal and focus the main window", not two.
//
// The nil check is this function's own: showMainWindow dereferences
// a.wailsApp unguarded, which is safe from the tray (it cannot exist without
// one) but not from a frontend binding, and every test builds an App with no
// Wails application at all.
func (a *App) FocusMainWindow() {
	if a.wailsApp == nil {
		return
	}
	a.showMainWindow()
}
