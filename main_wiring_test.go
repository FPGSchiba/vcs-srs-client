package main

import (
	"os"
	"strings"
	"testing"
)

// TestJoystickBackendIsWired guards the failure mode Phase 3 nearly shipped:
// a complete, tested subsystem that nothing ever constructs, so the feature
// is inert in the built binary while every unit test passes.
func TestJoystickBackendIsWired(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)
	for _, want := range []string{
		"joystick.NewOSSource",
		"joystick.New(",
		"SetJoystickBackend",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("main.go does not call %s -- the joystick subsystem would be inert", want)
		}
	}
}
