package app

// hotkeyHandler is the OS hotkey and joystick layers' view of App. It is a
// separate, unregistered type so that Pressed/Released/ForceReleased are NOT
// on the webview-bound App service: bound, they would let any script in a
// window key PTT. Implements hotkeys.Handler, hotkeys.StaleReleaser and
// joystick.Handler.
type hotkeyHandler struct{ a *App }

// HotkeyHandler returns the handler main.go passes to hotkeys.New and
// joystick.New.
func HotkeyHandler(a *App) *hotkeyHandler { return &hotkeyHandler{a: a} }

// Pressed implements hotkeys.Handler and joystick.Handler.
func (h *hotkeyHandler) Pressed(actionID string) { h.a.pressed(actionID) }

// Released implements hotkeys.Handler and joystick.Handler.
func (h *hotkeyHandler) Released(actionID string) { h.a.released(actionID) }

// ForceReleased implements hotkeys.StaleReleaser.
func (h *hotkeyHandler) ForceReleased(actionID string) { h.a.forceReleased(actionID) }
