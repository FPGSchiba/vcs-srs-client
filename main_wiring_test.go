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

// deferOffset returns the offset of a `defer` statement in main.go, and
// FAILS if the literal does not appear exactly once.
//
// The count is the load-bearing half. strings.Index returns the FIRST
// occurrence, so a comment in main.go that merely SPELLS OUT one of these
// statements above the real one would make an ordering assertion compare the
// wrong offset -- and for TestNotifierStopTimersIsRegisteredBeforeTheSources
// that yields a false PASS on a genuinely inverted shutdown order, which is
// the one direction these grep-style tests must never fail in. (Everything
// else about the pattern degrades to a false FAILURE, which is noisy but
// safe.) Measured: a decoy comment plus a real registration moved after
// `defer am.Stop()` passed the unguarded test.
//
// A duplicate is therefore an explicit failure rather than a silently wrong
// answer -- which also enforces main.go's standing rule that its prose must
// not spell out these three literals.
func deferOffset(t *testing.T, text, lit string) int {
	t.Helper()
	switch n := strings.Count(text, lit); {
	case n == 0:
		t.Fatalf("main.go no longer contains `%s`; update this test deliberately, not reflexively", lit)
	case n > 1:
		t.Fatalf("main.go contains `%s` %d times, want exactly 1 -- an ordering assertion indexes the FIRST occurrence, so a second one (a comment spelling out the statement, say) would silently compare the wrong offset; keep main.go's prose from naming these literals", lit, n)
	}
	return strings.Index(text, lit)
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
	stopIdx := deferOffset(t, text, "defer am.Stop()")
	closeIdx := deferOffset(t, text, "defer backend.Close()")
	// defers run LIFO, so the one registered FIRST runs LAST.
	// backend.Close() must run last, so it must be registered first.
	if closeIdx > stopIdx {
		t.Fatalf("`defer backend.Close()` (offset %d) must be registered BEFORE `defer am.Stop()` (offset %d) so it runs after it; see Stop()'s doc on bounded joins", closeIdx, stopIdx)
	}
}

// TestNotifierStopTimersIsRegisteredBeforeTheSources pins the other half of
// main.go's shutdown ordering, the half notify.StopTimers' own doc used to
// get wrong. It claimed "StopTimers runs before the event bus is torn down";
// it does not -- every defer in main() runs only after wailsApp.Run() has
// returned.
//
// What IS true, and what actually makes "cancels every armed timer" a final
// statement rather than a racy one, is defer LIFO: `defer notifier.StopTimers()`
// is registered BEFORE `defer jm.Close()` and `defer am.Stop()`, so those two
// run FIRST and the joystick and audio poll goroutines -- the sources that
// raise into the notifier and therefore arm its windows -- are already
// stopped by the time StopTimers runs. Registered the other way round, a poll
// tick landing between StopTimers and the source's own shutdown would arm a
// fresh window that nothing would ever cancel.
//
// That ordering was load-bearing, undocumented and unguarded. This is the
// guard; see TestMainWiringClosesBackendAfterManagerStop, which does the same
// job for the backend/manager pair, for why it reads the source.
func TestNotifierStopTimersIsRegisteredBeforeTheSources(t *testing.T) {
	text := readMainGo(t)
	stopTimersIdx := deferOffset(t, text, "defer notifier.StopTimers()")
	// defers run LIFO, so the one registered FIRST runs LAST. StopTimers
	// must run last of the three, so it must be registered first.
	for _, source := range []string{"defer jm.Close()", "defer am.Stop()"} {
		idx := deferOffset(t, text, source)
		if idx < stopTimersIdx {
			t.Errorf("`%s` (offset %d) is registered BEFORE `defer notifier.StopTimers()` (offset %d), so LIFO runs StopTimers first -- a poll tick from that source could then arm a coalescing window nothing will ever cancel", source, idx, stopTimersIdx)
		}
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

// TestSFXDedup_SkipsExactRepeat is F4's regression test (Phase 6
// whole-branch review): monitor.SetControlState dedupes against the state it
// already holds, but gui.PlayConnectionSFX does not, and a known, accepted
// duplicate `disconnected` (Disconnect firing after the probe detector has
// already declared loss) used to reach PlayConnectionSFX twice.
func TestSFXDedup_SkipsExactRepeat(t *testing.T) {
	d := &sfxDedup{}

	if !d.shouldPlay("connected") {
		t.Error("shouldPlay(\"connected\") on a fresh gate = false, want true")
	}
	if d.shouldPlay("connected") {
		t.Error("shouldPlay(\"connected\") repeated = true, want false (this is the F4 bug)")
	}
	// A third repeat must stay suppressed -- shouldPlay must record the
	// state even on the call it rejects, not just the one it lets through.
	if d.shouldPlay("connected") {
		t.Error("shouldPlay(\"connected\") a third time = true, want false")
	}
}

// TestSFXDedup_PlaysEveryGenuineTransition guards against an over-broad fix:
// the gate must not suppress a genuinely different state, including a
// transition back to one already seen earlier in the sequence (a flap).
func TestSFXDedup_PlaysEveryGenuineTransition(t *testing.T) {
	d := &sfxDedup{}
	seq := []string{"reconnecting", "connected", "reconnecting", "disconnected", "connected"}
	for i, s := range seq {
		if !d.shouldPlay(s) {
			t.Errorf("shouldPlay(%q) at step %d = false, want true (genuine transition)", s, i)
		}
	}
}

// TestSFXGateIsWiredIntoTheObserver guards against the fix existing as dead
// code -- the exact failure mode TestJoystickBackendIsWired documents --
// main.go could define sfxDedup and never actually gate PlayConnectionSFX
// with it.
func TestSFXGateIsWiredIntoTheObserver(t *testing.T) {
	text := readMainGo(t)
	if !strings.Contains(text, "sfxGate.shouldPlay(") {
		t.Error("main.go does not call sfxGate.shouldPlay(...) -- PlayConnectionSFX would be ungated again")
	}
}

// TestNotificationChannelIsWired guards the same failure mode as its
// neighbours above -- TestJoystickBackendIsWired and TestAudioBackendIsWired
// in particular: a fully-tested notification store (internal/notify, Task 2)
// with a live App wiring point (app.SetNotifier, Task 6) that nothing in the
// shipped binary ever constructs, leaving the whole channel inert while
// every unit test in both packages passes.
//
// This is a source-text assertion, not a behavioural one, because no test
// inside internal/notify or internal/app can observe whether main.go
// actually builds a *notify.Notifier and hands it to the App -- exactly the
// reasoning documented on TestJoystickBackendIsWired and readMainGo above.
func TestNotificationChannelIsWired(t *testing.T) {
	text := readMainGo(t)
	for _, want := range []string{
		"notify.New(",
		"app.SetNotifier(gui, ",
		"notifEvents.Notifications(",
		"OnSound:",
		"defer notifier.StopTimers()",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("main.go does not call %s -- the notification channel would be inert", want)
		}
	}
}

// TestAudioNotificationAdapterIsWiredAtBothEmitSites guards DoD 9, which
// rests on the audio adapter being reached from BOTH of main.go's audio emit
// sites. app.NotifyAudioState -- a package-level function, deliberately NOT
// a method on *App, so it stays off the webview-bound service surface --
// exists for no other reason.
//
// The two sites are not interchangeable and neither is redundant:
//
//   - the NewMalgoBackend failure branch, where NO Manager exists at all.
//     This is the most severe audio failure there is and an adapter hung
//     only off OnState can never see it.
//   - the Manager's OnState callback, which carries every later fault: a
//     device that will not open, a substitution, a recovery.
//
// Deleting either call leaves every unit test in internal/app and
// internal/notify green -- the exact failure mode TestJoystickBackendIsWired
// and TestAudioBackendIsWired exist to catch -- so, like them, this is a
// source-text assertion.
func TestAudioNotificationAdapterIsWiredAtBothEmitSites(t *testing.T) {
	text := readMainGo(t)
	const call = "app.NotifyAudioState(gui, "
	if got := strings.Count(text, call); got != 2 {
		t.Fatalf("main.go contains %d %s calls, want exactly 2 -- one for the no-backend DTO and one inside the Manager's OnState callback", got, call)
	}

	mgrIdx := strings.Index(text, "audio.NewManager(")
	if mgrIdx < 0 {
		t.Fatal("main.go no longer calls audio.NewManager(; update this test deliberately, not reflexively")
	}
	onStateIdx := strings.Index(text, "OnState: func(st audio.State)")
	if onStateIdx < 0 {
		t.Fatal("main.go no longer registers OnState: func(st audio.State); update this test deliberately, not reflexively")
	}

	noBackend := strings.Index(text, call)
	onState := strings.Index(text[noBackend+len(call):], call) + noBackend + len(call)
	if noBackend > mgrIdx {
		t.Error("the first app.NotifyAudioState(gui, ...) call is not in the NewMalgoBackend failure branch -- " +
			"the no-Manager case, the most severe audio failure there is, would go unnotified")
	}
	if onState < onStateIdx {
		t.Error("the second app.NotifyAudioState(gui, ...) call is not inside the Manager's OnState callback -- " +
			"every audio fault after startup would go unnotified")
	}
}

// TestNotificationAdaptersStayOffTheBoundServiceSurface pins the M-9 fix.
//
// main.go registers gui with application.NewService, so EVERY exported
// method on *App is callable from the webview. The two notification adapters
// main.go drives -- the audio projection and the notification sound -- were
// exported only so main.go could reach them, which contradicted the branch's
// own rule (ruling R13, and setCaptureTimeout / setNotifyWindows' doc
// comments): "an exported method on the service is bound and reachable from
// the webview, and <this> is not the frontend's business".
//
// Concretely, a bound NotifyAudioState lets the renderer fabricate an
// error-severity "Microphone unavailable" notification with arbitrary body
// text, or -- by passing a clean DTO -- silently RESOLVE a genuine
// microphone fault out of the user's list. The renderer is first-party, so
// the impact is low; the inconsistency was the finding.
//
// Fix wave 6 extended the rule to SetNotifier, which was worse than either
// adapter: a bound SetNotifier(null) sets a.notif to nil and silences the
// ENTIRE channel for the session. The dead Notifier() accessor beside it was
// deleted outright.
//
// A source-text assertion for the reason all its siblings here are: the
// CALL SITES live in main.go, and no test inside internal/app can see
// whether one went back to the method form. The complementary invariant --
// that no such method exists on *App to call in the first place -- is
// asserted directly, by reflection over the exported method set, in
// internal/app's TestNotificationSeamsAreNotOnTheExportedMethodSet. Both are
// needed: this one catches a call site regressing, that one catches the
// method being re-added.
func TestNotificationAdaptersStayOffTheBoundServiceSurface(t *testing.T) {
	text := readMainGo(t)
	for _, banned := range []string{
		"gui.NotifyAudioState(",
		"gui.PlayNotificationSFX(",
		"gui.SetNotifier(",
		"gui.Notifier(",
	} {
		if strings.Contains(text, banned) {
			t.Errorf("main.go calls %s -- that method form is bound into the webview by "+
				"application.NewService(gui). Use the package-level app.NotifyAudioState / "+
				"app.PlayNotificationSFX seam instead", banned)
		}
	}
	if !strings.Contains(text, "app.PlayNotificationSFX(gui, ") {
		t.Error("main.go does not route notify.Options.OnSound through " +
			"app.PlayNotificationSFX(gui, ...) -- the notification sound would never play")
	}
}

// TestHistoryIsWired guards the transmission log's wiring: a complete,
// tested log that main.go never attaches makes every history binding
// early-return, so the Transmission Log stays empty with nothing logged to
// explain it. It also pins the two neighbouring wirings needed for anything
// to be recorded at all -- OnRX/HistoryIdleMS in the voice dial options (no
// RX row is ever logged without them) -- and that the builtin profile seed
// is called exactly once.
func TestHistoryIsWired(t *testing.T) {
	text := readMainGo(t)
	for _, want := range []string{
		"history.Load(",
		"histLog.SetPersist(",
		"app.SetHistory(gui, histLog)",
		"app.StartHistoryTicker(gui)",
		"app.NotifyHistoryFlush(gui, err)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("main.go does not contain %s -- the transmission log would be inert", want)
		}
	}
	if n := strings.Count(text, "gui.SeedBuiltinProfiles()"); n != 1 {
		t.Errorf("main.go calls gui.SeedBuiltinProfiles() %d times, want exactly 1 (a second call double-seeds)", n)
	}

	voiceSrc, err := os.ReadFile("internal/app/voice.go")
	if err != nil {
		t.Fatalf("read voice.go: %v", err)
	}
	vtext := string(voiceSrc)
	for _, want := range []string{"OnRX:", "HistoryIdleMS:"} {
		if n := strings.Count(vtext, want); n != 1 {
			t.Errorf("voice.go has %d occurrences of %s in voiceDialOptions, want exactly 1", n, want)
		}
	}
}
