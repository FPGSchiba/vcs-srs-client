//go:build linux

package hotkeys

// sessionSupported gates registration on the Linux session being one global
// key listening can work in at all. See session.go.
func sessionSupported() error { return linuxSession() }
