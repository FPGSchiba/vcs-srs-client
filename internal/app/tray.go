package app

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// mainWindowName is the Name given to the main window's WebviewWindowOptions
// in main.go, so it can be resolved back out of the Wails window manager.
const mainWindowName = "main"

// SetupTray creates the system tray icon and menu. Call after the main window
// exists. If tray creation fails, a.tray is left nil so TrayAvailable
// reports false and close-to-tray is force-disabled (spec R13) -- otherwise
// a silent tray failure would leave the user with a hidden window and no way
// to bring it back.
func (a *App) SetupTray(icon []byte) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Error("system tray setup failed; close-to-tray disabled", "panic", r)
			a.tray = nil
		}
	}()

	tray := a.wailsApp.SystemTray.New()
	tray.SetTemplateIcon(icon)
	tray.SetTooltip("Vanguard Communications System")

	menu := application.NewMenu()
	menu.Add("Show VCS").OnClick(func(*application.Context) { a.showMainWindow() })
	menu.Add("Settings").OnClick(func(*application.Context) { a.showMainWindow() })
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { a.quit() })
	tray.SetMenu(menu)
	tray.OnClick(func() { a.toggleMainWindow() })

	a.tray = tray
}

// TrayAvailable reports whether the system tray was created successfully.
// Used to gate close-to-tray: a failed tray must never trap the user with a
// hidden window and no way back (spec R13).
func (a *App) TrayAvailable() bool {
	return a.tray != nil
}

// showMainWindow reveals and focuses the main window (tray menu -> Show VCS
// / Settings, and after a hotkey or dock-icon reactivation).
func (a *App) showMainWindow() {
	win, ok := a.wailsApp.Window.GetByName(mainWindowName)
	if !ok {
		return
	}
	win.Show()
	win.Focus()
}

// toggleMainWindow shows the main window if it is hidden, or hides it if it
// is currently visible. Wired to the tray icon's click handler.
func (a *App) toggleMainWindow() {
	win, ok := a.wailsApp.Window.GetByName(mainWindowName)
	if !ok {
		return
	}
	if win.IsVisible() {
		win.Hide()
		return
	}
	win.Show()
	win.Focus()
}

// OnMainWindowClose is registered as the main window's close interceptor. It
// returns true (prevent the close, hide instead) when MinimizeToTray is on
// AND the tray was created successfully -- if the tray failed, closing must
// behave normally or the user would be stuck with no way to bring the window
// back (spec R13).
func (a *App) OnMainWindowClose() (preventClose bool) {
	return a.GetSettings().MinimizeToTray && a.TrayAvailable()
}

// quit disconnects the control session first, so the server sees a clean
// leave rather than a dropped stream, then terminates the app. Wired to the
// tray menu's Quit item -- it must always exit regardless of close-to-tray.
func (a *App) quit() {
	if a.sess != nil {
		_ = a.sess.Disconnect(context.Background())
	}
	a.wailsApp.Quit()
}
