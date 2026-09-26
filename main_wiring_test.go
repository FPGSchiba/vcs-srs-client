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
	newIdx := strings.Index(text, "appLog, closeLog := logger.New(")
	if newIdx < 0 || setIdx < 0 || setIdx < newIdx {
		t.Error("slog.SetDefault(appLog) must come after appLog, closeLog := logger.New(...)")
	}
}

// TestStdlogLevelIsPinnedBeforeSetDefault guards the fix that keeps log.Fatal
// alive once slog.SetDefault is installed.
//
// slog.SetDefault redirects the standard log package through a handlerWriter
// pinned at LevelInfo, which DROPS the record when the handler is not enabled
// at that level. With log_level = "WARN" or "ERROR" -- an ordinary setting for
// a user cutting noise -- the log.Fatal(err) at the bottom of main would then
// write nowhere at all: not the file, not stderr, where before SetDefault it
// at least reached stderr. The app would exit 1 in silence on the single most
// important message it can emit.
//
// slog.SetLogLoggerLevel(slog.LevelError) fixes that, and it appears exactly
// once, in main.go, so no unit test inside any package can observe it --
// delete the line and every other test in the repo stays green. Hence the
// same grep-style assertion as TestJoystickBackendIsWired and
// TestSlogDefaultIsInstalled. The ORDER matters too: SetLogLoggerLevel must
// run before SetDefault installs the bridge it configures.
func TestStdlogLevelIsPinnedBeforeSetDefault(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)
	pinIdx := strings.Index(text, "slog.SetLogLoggerLevel(slog.LevelError)")
	if pinIdx < 0 {
		t.Fatal("main.go does not call slog.SetLogLoggerLevel(slog.LevelError) -- " +
			"log.Fatal would be silently dropped at log_level = \"WARN\" or \"ERROR\", " +
			"and the app would exit 1 with no message anywhere")
	}
	setIdx := strings.Index(text, "slog.SetDefault(appLog)")
	if setIdx < 0 || pinIdx > setIdx {
		t.Error("slog.SetLogLoggerLevel(slog.LevelError) must come BEFORE " +
			"slog.SetDefault(appLog) -- it configures the stdlib bridge SetDefault installs")
	}
}

// readMainGo is a small helper shared by the audio-wiring tests below --
// unlike TestJoystickBackendIsWired and its siblings above, which each
// re-read the file inline, these all need the same source text more than
// once per test.
func readMainGo(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	return string(src)
}

// TestAudioBackendIsWired guards the exact failure mode
// TestJoystickBackendIsWired documents: Phase 4's audio engine, config
// schema, event channel and frontend bindings can all be complete and
// individually tested while main.go never actually constructs the malgo
// backend or hands a Manager to App -- leaving the whole subsystem inert in
// the shipped binary.
func TestAudioBackendIsWired(t *testing.T) {
	text := readMainGo(t)
	for _, want := range []string{
		"audio.NewMalgoBackend()",
		"audio.NewManager(",
		"gui.SetAudioBackend(",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("main.go does not call %s -- the audio subsystem would be inert", want)
		}
	}
}

// TestAudioBackendFailureDoesNotAbortStartup guards the rule that matters
// most for this wiring: NewMalgoBackend can fail for reasons that have
// nothing to do with whether the rest of the app should run -- no sound
// card, a denied OS permission, a broken driver -- exactly like
// joystick.NewOSSource failing on a machine with no joystick. The client
// must still launch, connect, and let the user use every non-audio feature,
// with the failure surfaced to the frontend rather than swallowed.
//
// The search window is bounded to just after the NewMalgoBackend() call
// rather than the whole file, because main.go legitimately calls
// log.Fatal(err) at the very bottom for wailsApp.Run() failing -- that is
// an unrelated, correct use of log.Fatal this test must not flag.
func TestAudioBackendFailureDoesNotAbortStartup(t *testing.T) {
	text := readMainGo(t)
	idx := strings.Index(text, "audio.NewMalgoBackend()")
	if idx < 0 {
		t.Fatal("main.go does not call audio.NewMalgoBackend()")
	}
	end := idx + 700
	if end > len(text) {
		end = len(text)
	}
	block := text[idx:end]

	if !strings.Contains(block, "Warn(") {
		t.Error("a failed audio.NewMalgoBackend() is not logged -- the failure would be silent")
	}
	if strings.Contains(block, "log.Fatal") {
		t.Error("a failed audio.NewMalgoBackend() must not abort startup via log.Fatal -- " +
			"the client is a voice-comms app first and must still run with no audio device")
	}
	if !strings.Contains(block, "AudioState(") {
		t.Error("a failed audio.NewMalgoBackend() must still emit an audio:state event -- " +
			"the frontend needs an honest answer, not silence, when there is no manager at all " +
			"to ask")
	}
}

// TestAudioManagerStopIsRegisteredForShutdown guards device cleanup: Stop()
// must run when the app exits so devices are released, mirroring
// `defer jm.Close()` for the joystick manager just above it in main.go.
func TestAudioManagerStopIsRegisteredForShutdown(t *testing.T) {
	text := readMainGo(t)
	if !strings.Contains(text, "defer am.Stop()") {
		t.Error("main.go does not defer the audio manager's Stop() -- devices would " +
			"never be released cleanly on shutdown")
	}
}

// TestMainWiringClosesBackendAfterManagerStop pins the shutdown ordering
// main.go depends on. Stop()'s joins are bounded, so dspLoop can still be
// running when Stop() returns; closing the backend before Stop() would let
// an abandoned dspLoop touch a freed malgo context. Phase 5's socket-owning
// sink is what makes a slow WriteFrame -- and therefore an abandoned
// dspLoop -- reachable in practice.
//
// This reads main.go's source rather than executing it, because the
// ordering being asserted is the order of two `defer` statements inside
// func main(), which no test can observe at runtime without launching the
// real GUI.
func TestMainWiringClosesBackendAfterManagerStop(t *testing.T) {
	text := readMainGo(t)
	stopIdx := strings.Index(text, "defer am.Stop()")
	closeIdx := strings.Index(text, "defer backend.Close()")
	if stopIdx < 0 {
		t.Fatal("main.go no longer contains `defer am.Stop()`; update this test deliberately, not reflexively")
	}
	if closeIdx < 0 {
		t.Fatal("main.go no longer contains `defer backend.Close()`; update this test deliberately, not reflexively")
	}
	// defers run LIFO, so the one registered FIRST runs LAST.
	// backend.Close() must run last, so it must be registered first.
	if closeIdx > stopIdx {
		t.Fatalf("`defer backend.Close()` (offset %d) must be registered BEFORE `defer am.Stop()` (offset %d) so it runs after it; see Stop()'s doc on bounded joins", closeIdx, stopIdx)
	}
}

// TestVoiceBridgeIsWiredAsBothSinkAndSource guards Task 11's own version of
// the TestJoystickBackendIsWired failure mode, made worse: registering ONLY
// AddSink wires transmit, and every test in the repo (including Task 9's own
// RX suite, which exercises internal/voice directly rather than through the
// Manager) stays green while received audio is silently discarded. There is
// no failing test anywhere else that would catch a missing SetSource call --
// this grep is it.
func TestVoiceBridgeIsWiredAsBothSinkAndSource(t *testing.T) {
	text := readMainGo(t)
	for _, want := range []string{
		"gui.VoiceBridge()",
		"am.AddSink(",
		"am.SetSource(",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("main.go does not call %s -- the voice Sink/Source bridge would be inert", want)
		}
	}
}
