//go:build !linux

package hotkeys

// sessionSupported is the non-Linux half of the session pre-flight. Its build
// tag is the exact complement of session_linux.go's, so the partition is
// exhaustive and disjoint by construction.
//
// macOS and Windows have one windowing system each and no equivalent of
// Linux's X11-vs-Wayland split, so there is nothing to detect. macOS's one
// gate -- the Accessibility grant -- is not a session property: it is owned
// by PermissionChecker (permission_darwin.go), and the stream additionally
// reports HookDisabled without it, which registrar_gohook.go turns into a
// registration failure.
func sessionSupported() error { return nil }
