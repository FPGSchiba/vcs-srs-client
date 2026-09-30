package app

import "github.com/FPGSchiba/vcs-srs-client/internal/windowstate"

// WindowHandle is the minimal control surface over one OS window.
type WindowHandle interface {
	Focus()
	Close()
	Bounds() windowstate.Geometry
	SetBounds(windowstate.Geometry)
}

// boundsNotifier is implemented by handles that can report a user move or
// resize. Optional (a type assertion in Registry.Open) so fakes that never
// resize need not implement it. The callback runs on the window system's
// event thread and must not block or call back into the window.
type boundsNotifier interface {
	OnBoundsChanged(fn func())
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
	case "notifications":
		// Narrower and shorter than Comms: this is a reading list, not a
		// live control surface, and it is opened transiently.
		return windowstate.Geometry{X: 1150, Y: 120, W: 520, H: 640}
	default:
		return windowstate.Geometry{X: 160, Y: 90, W: 1440, H: 900}
	}
}

// windowURL maps a window id to its frontend entry.
func windowURL(id string) string {
	switch id {
	case "comms":
		return "/comms.html"
	case "notifications":
		return "/notifications.html"
	default:
		return "/main.html"
	}
}
