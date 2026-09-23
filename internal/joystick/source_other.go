//go:build !windows && !linux && !darwin

package joystick

import "log/slog"

// No backend on this platform. Same contract as darwin: unsupported, not
// denied.
func NewOSSource(*slog.Logger) (Source, error) { return otherUnsupportedSrc{}, nil }

type otherUnsupportedSrc struct{}

func (otherUnsupportedSrc) Devices() ([]Device, error) { return nil, ErrUnsupported }
func (otherUnsupportedSrc) Poll() (State, error)       { return State{}, ErrUnsupported }
func (otherUnsupportedSrc) Close()                     {}
