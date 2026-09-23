//go:build darwin

package joystick

import "log/slog"

// macOS has no backend, by decision rather than omission.
//
// Star Citizen has no macOS build and none is planned, so there is no game to
// talk over. Reading HID on macOS would also need IOHIDManager, which is
// gated behind the Input Monitoring TCC permission -- a SECOND, different
// grant from the Accessibility permission the keyboard hotkey path already
// has to negotiate. Spending a second permission prompt on a platform that
// cannot run the game is poor value.
//
// This reports UNSUPPORTED, never DENIED. The distinction matters to the UI:
// denied earns a grant affordance, unsupported must not, because there is
// nothing the user can do about it.
// The logger is accepted and ignored: the signature is shared across every
// platform file so main.go has one call, and there is nothing here to report.
func NewOSSource(*slog.Logger) (Source, error) { return unsupportedSrc{}, nil }

type unsupportedSrc struct{}

func (unsupportedSrc) Devices() ([]Device, error) { return nil, ErrUnsupported }
func (unsupportedSrc) Poll() (State, error)       { return State{}, ErrUnsupported }
func (unsupportedSrc) Close()                     {}
