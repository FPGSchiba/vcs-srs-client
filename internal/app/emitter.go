package app

import "github.com/wailsapp/wails/v3/pkg/application"

// wailsEmitter adapts the Wails application event system to events.Emitter.
type wailsEmitter struct{ app *application.App }

// NewWailsEmitter builds an events.Emitter backed by the Wails app.
func NewWailsEmitter(app *application.App) *wailsEmitter { return &wailsEmitter{app: app} }

// Emit broadcasts a custom event to all windows.
func (w *wailsEmitter) Emit(name string, payload any) {
	w.app.Event.Emit(name, payload)
}
