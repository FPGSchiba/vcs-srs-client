package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// wailsFactory creates real frameless+transparent Wails windows.
type wailsFactory struct{ app *application.App }

// NewWailsFactory builds a WindowFactory over the Wails app.
func NewWailsFactory(app *application.App) WindowFactory { return &wailsFactory{app: app} }

type wailsHandle struct{ win *application.WebviewWindow }

func (f *wailsFactory) Create(id, url string, g windowstate.Geometry) WindowHandle {
	win := f.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "VCS · " + id,
		Width:            g.W,
		Height:           g.H,
		X:                g.X,
		Y:                g.Y,
		URL:              url,
		Frameless:        true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
	})
	return &wailsHandle{win: win}
}

func (h *wailsHandle) Focus() { h.win.Focus() }
func (h *wailsHandle) Close() { h.win.Close() }
func (h *wailsHandle) Bounds() windowstate.Geometry {
	w, ht := h.win.Size()
	x, y := h.win.Position()
	return windowstate.Geometry{X: x, Y: y, W: w, H: ht}
}
func (h *wailsHandle) SetBounds(g windowstate.Geometry) {
	h.win.SetSize(g.W, g.H)
	h.win.SetPosition(g.X, g.Y)
}
