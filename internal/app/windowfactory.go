package app

import "github.com/FPGSchiba/vcs-srs-client/internal/windowstate"

// WindowHandle is the minimal control surface over one OS window.
type WindowHandle interface {
	Focus()
	Close()
	Bounds() windowstate.Geometry
	SetBounds(windowstate.Geometry)
}

// WindowFactory creates real windows. The Wails-backed implementation lives in
// windows_wails.go; tests pass a fake.
type WindowFactory interface {
	Create(id string, url string, g windowstate.Geometry) WindowHandle
}

// defaultGeometry returns the design's default geometry per window id.
func defaultGeometry(id string) windowstate.Geometry {
	switch id {
	case "comms":
		return windowstate.Geometry{X: 1190, Y: 70, W: 540, H: 720}
	default:
		return windowstate.Geometry{X: 160, Y: 90, W: 1440, H: 900}
	}
}

// windowURL maps a window id to its frontend entry.
func windowURL(id string) string {
	switch id {
	case "comms":
		return "/comms.html"
	default:
		return "/main.html"
	}
}
