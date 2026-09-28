package app

import "github.com/FPGSchiba/vcs-srs-client/internal/notify"

// SetNotifier wires the notification channel. A nil notifier is legal and
// leaves every binding below inert, so a build whose wiring failed degrades
// to "no notifications" rather than crashing.
func (a *App) SetNotifier(n *notify.Notifier) { a.notif = n }

// Notifier returns the wired channel, or nil. Used by main.go's audio
// adapter, which sits outside internal/app because audio has two emit sites
// and only one of them is a Manager callback.
func (a *App) Notifier() *notify.Notifier { return a.notif }

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
