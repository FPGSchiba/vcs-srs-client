// Package version exposes the client version, the SRS wire-protocol version
// this client implements, and the build identifier stamped by the Go toolchain.
package version

import "runtime/debug"

const (
	// Client is the VCS client release version.
	Client = "0.1.0"
	// Protocol is the SRS wire-protocol version this client implements.
	Protocol = "1.0.0"
)

// Build returns the short VCS revision stamped into the binary by `go build`
// (Go records this automatically when building inside a git repo), or "dev"
// when no revision is available (e.g. `go run`).
func Build() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 7 {
				return s.Value[:7]
			}
			return s.Value
		}
	}
	return "dev"
}
