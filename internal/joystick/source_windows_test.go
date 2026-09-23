//go:build windows

package joystick

import (
	"errors"
	"io"
	"log/slog"
	"testing"
)

// TestWinInitFailedKeepsTheManagerAlive is the M1 guard.
//
// newHelperWindow and di8.Create used to return an error from NewOSSource,
// which main.go treats as "drop the manager": sb.joy stayed nil,
// GetJoystickState answered {Supported:false, Error:""} -- byte-identical to
// macOS's "no backend at all" -- so the Keybinds banner (gated on
// supported && error) never fired and the 3s rediscover retry never existed,
// because it lives inside a Manager that was never built.
//
// The contract this pins: the stub's error is REPORTED and is NOT
// ErrUnsupported, so Manager.New records it in discoverErr and leaves
// Supported() true.
func TestWinInitFailedKeepsTheManagerAlive(t *testing.T) {
	boom := errors.New("joystick: create helper window: out of resources")
	src := winInitFailed(slog.New(slog.NewTextHandler(io.Discard, nil)), boom)

	devs, err := src.Devices()
	if err == nil {
		t.Fatal("Devices() returned no error; the failure would be invisible to the UI")
	}
	if !errors.Is(err, boom) {
		t.Errorf("Devices() error = %v, want it to wrap %v", err, boom)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Error("Devices() reported ErrUnsupported; that flips Supported() to false and " +
			"collapses a retryable Windows failure into macOS's no-backend state")
	}
	if len(devs) != 0 {
		t.Errorf("Devices() = %v, want none", devs)
	}

	// Manager.New's probe is the consumer that matters: it must keep the
	// affordance visible and surface the message.
	m := New(src, &noopHandler{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !m.Supported() {
		t.Error("Supported() = false after an init failure; the UI would hide the " +
			"affordance and show no banner")
	}
	if got := m.LastErr(); got == nil || !errors.Is(got, boom) {
		t.Errorf("LastErr() = %v, want the init failure", got)
	}

	// Nothing is open, so nothing can be held -- and a poll error every 10ms
	// would win lastErrLocked's precedence and replace the message that names
	// the actual failed call.
	st, err := src.Poll()
	if err != nil {
		t.Errorf("Poll() error = %v, want nil", err)
	}
	if len(st.Held) != 0 {
		t.Errorf("Poll() held %v, want nothing", st.Held)
	}
	src.Close()
	src.Close() // Close is documented safe to call more than once
}

type noopHandler struct{}

func (noopHandler) Pressed(string)  {}
func (noopHandler) Released(string) {}
