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

// TestSlogDefaultIsInstalled guards a fix that verifiably did not work while
// its own unit test said it did.
//
// internal/keybinds/store.go drops a keyboard chord when a config lists more
// than one for an action, and its doc comment justifies that destruction on
// the grounds that the drop "has to be diagnosable" -- it emits a Warn through
// slog.Default() naming the action and the dropped chord. But slog.SetDefault
// was never called in production: it appeared only in store_test.go, where the
// test installs a handler of its own and then proves the warning reached it.
// That cannot distinguish "wired correctly" from "wired to nowhere". In the
// shipped binary the warning went to stderr alone, and a Wails GUI build on
// Windows has no console, so it was discarded outright.
//
// A grep-style assertion is the established pattern here for exactly this
// class of defect -- see TestJoystickBackendIsWired -- because no unit test
// inside a package can observe whether main.go wires it up.
func TestSlogDefaultIsInstalled(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)
	if !strings.Contains(text, "slog.SetDefault(appLog)") {
		t.Error("main.go does not call slog.SetDefault(appLog) -- every slog.Default() " +
			"log line in the app (keybinds' dropped-chord Warn among them) would go to " +
			"stderr only, which a Wails GUI build on Windows discards")
	}
	// The default must be the rotating FILE logger, not some other logger
	// constructed nearby: the whole point is that the line reaches the log
	// file a user can send us.
	setIdx := strings.Index(text, "slog.SetDefault(appLog)")
	newIdx := strings.Index(text, "appLog := logger.New(")
	if newIdx < 0 || setIdx < 0 || setIdx < newIdx {
		t.Error("slog.SetDefault(appLog) must come after appLog := logger.New(...)")
	}
}
