package keybinds_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func key(t *testing.T, s string) trigger.Trigger {
	t.Helper()
	c, err := chord.Parse(s)
	if err != nil {
		t.Fatalf("chord.Parse(%q): %v", s, err)
	}
	return trigger.Key(c)
}

func joy(dev string, btn trigger.Button) trigger.Trigger {
	return trigger.Joy(trigger.JoyBinding{Device: trigger.DeviceID(dev), Button: btn})
}

func TestAddAccumulatesTriggers(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", joy("stick-c3", 11))

	got, ok := s.Get("global.ptt")
	if !ok || len(got) != 2 {
		t.Fatalf("Get after two Adds = %v (ok=%v), want 2 triggers", got, ok)
	}
	if !got[0].Equal(key(t, "F1")) || !got[1].Equal(joy("stick-c3", 11)) {
		t.Errorf("triggers out of order or wrong: %+v", got)
	}
}

func TestAddIsIdempotentForTheSameTrigger(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	if stolen := s.Add("global.ptt", key(t, "F1")); stolen != nil {
		t.Errorf("re-adding an action's own trigger reported a steal: %+v", stolen)
	}
	if got, _ := s.Get("global.ptt"); len(got) != 1 {
		t.Errorf("re-adding duplicated the trigger: %+v", got)
	}
}

func TestAddStealsFromAnotherAction(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	stolen := s.Add("channel.intercom", key(t, "F1"))
	if stolen == nil {
		t.Fatal("Add over another action's trigger reported no steal")
	}
	if stolen.ActionID != "global.ptt" {
		t.Errorf("stolen from %q, want global.ptt", stolen.ActionID)
	}
	if got, _ := s.Get("global.ptt"); len(got) != 0 {
		t.Errorf("victim kept the stolen trigger: %+v", got)
	}
}

func TestStealTakesOnlyTheConflictingTrigger(t *testing.T) {
	// The victim's OTHER bindings must survive -- this is the whole point of
	// the additive model.
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", joy("stick-c3", 11))
	s.Add("channel.intercom", key(t, "F1"))

	got, _ := s.Get("global.ptt")
	if len(got) != 1 || !got[0].Equal(joy("stick-c3", 11)) {
		t.Errorf("steal took more than the conflicting trigger: %+v", got)
	}
}

func TestKeyboardNeverStealsFromJoystick(t *testing.T) {
	// Separate conflict namespaces (spec section 10). This works because
	// Trigger.Equal compares Kind first.
	s := keybinds.New()
	s.Add("global.ptt", joy("stick-c3", 11))
	if stolen := s.Add("channel.intercom", key(t, "F1")); stolen != nil {
		t.Errorf("keyboard trigger stole from a joystick binding: %+v", stolen)
	}
}

func TestSecondKeyboardTriggerReplacesTheFirst(t *testing.T) {
	// internal/hotkeys registers one chord per action ID, so a second
	// keyboard chord would save, display, and never fire. Replace instead.
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", key(t, "F2"))

	got, _ := s.Get("global.ptt")
	if len(got) != 1 {
		t.Fatalf("Get = %+v, want exactly one keyboard trigger", got)
	}
	if !got[0].Equal(key(t, "F2")) {
		t.Errorf("kept %+v, want the newer chord F2", got[0])
	}
}

func TestKeyboardReplacementKeepsJoystickTriggers(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", joy("stick-c3", 11))
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", key(t, "F2"))

	got, _ := s.Get("global.ptt")
	if len(got) != 2 {
		t.Fatalf("Get = %+v, want the joystick trigger plus one chord", got)
	}
	var joys, keysN int
	for _, tr := range got {
		if tr.Kind == trigger.KindJoy {
			joys++
		} else {
			keysN++
		}
	}
	if joys != 1 || keysN != 1 {
		t.Errorf("got %d joystick and %d keyboard triggers, want 1 and 1", joys, keysN)
	}
}

func TestSeveralJoystickTriggersAccumulate(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", joy("stick-c3", 11))
	s.Add("global.ptt", joy("throttle-a1", 6))
	if got, _ := s.Get("global.ptt"); len(got) != 2 {
		t.Errorf("Get = %+v, want both joystick triggers", got)
	}
}

func TestRemoveAt(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", joy("stick-c3", 11))

	if err := s.RemoveAt("global.ptt", 0); err != nil {
		t.Fatalf("RemoveAt(0): %v", err)
	}
	got, _ := s.Get("global.ptt")
	if len(got) != 1 || !got[0].Equal(joy("stick-c3", 11)) {
		t.Errorf("after RemoveAt(0) = %+v, want the joystick trigger only", got)
	}
	for _, bad := range []int{-1, 1, 99} {
		if err := s.RemoveAt("global.ptt", bad); err == nil {
			t.Errorf("RemoveAt(%d) = nil error, want out-of-range failure", bad)
		}
	}
}

func TestLoadSnapshotRoundTrip(t *testing.T) {
	s := keybinds.New()
	s.Load(map[string][]string{
		"global.ptt":       {"F1", "joy:stick-c3:btn12"},
		"channel.intercom": {"joy:throttle-a1:btn7+stick-c3:btn3"},
		"future.action":    {"Ctrl+Q"}, // unknown id: must survive verbatim
	})
	snap := s.Snapshot()
	if len(snap["global.ptt"]) != 2 ||
		snap["global.ptt"][0] != "F1" || snap["global.ptt"][1] != "joy:stick-c3:btn12" {
		t.Errorf("global.ptt round trip = %v", snap["global.ptt"])
	}
	if got := snap["future.action"]; len(got) != 1 || got[0] != "Ctrl+Q" {
		t.Errorf("unknown action id lost: %v", got)
	}
}

func TestLoadDropsOnlyTheUnparseableEntry(t *testing.T) {
	s := keybinds.New()
	s.Load(map[string][]string{
		"global.ptt": {"F1", "joy:!!!bad", "joy:stick-c3:btn12"},
	})
	got, _ := s.Get("global.ptt")
	if len(got) != 2 {
		t.Fatalf("Get = %+v, want the two parseable triggers", got)
	}
	if !got[0].Equal(key(t, "F1")) || !got[1].Equal(joy("stick-c3", 11)) {
		t.Errorf("wrong survivors: %+v", got)
	}
}

func TestLoadKeepsOnlyTheFirstKeyboardTrigger(t *testing.T) {
	s := keybinds.New()
	s.Load(map[string][]string{
		"global.ptt": {"F1", "joy:stick-c3:btn12", "F2"},
	})
	got, _ := s.Get("global.ptt")
	if len(got) != 2 {
		t.Fatalf("Get = %+v, want exactly two triggers", got)
	}
	if !got[0].Equal(key(t, "F1")) || !got[1].Equal(joy("stick-c3", 11)) {
		t.Errorf("wrong survivors: %+v, want F1 keyboard trigger and the joystick trigger (F2 dropped)", got)
	}
}

func TestClearRemovesEveryTrigger(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", joy("stick-c3", 11))
	s.Clear("global.ptt")
	if got, ok := s.Get("global.ptt"); ok && len(got) != 0 {
		t.Errorf("after Clear = %+v, want empty", got)
	}
}

// TestLoadWarnsAboutTheDroppedKeyboardChord pins the diagnosability half of
// the one-keyboard-trigger rule.
//
// The rule itself is right, but enforcing it silently means a hand-edited
// `"global.push_to_mute" = ["V", "Ctrl+B"]` loads as V and the very next
// Save rewrites that line as `"global.push_to_mute" = "V"` -- the user's
// second chord destroyed on disk with no trace anywhere. Dropping it is
// still better than a binding that is silently dead at dispatch, but the
// destruction has to be diagnosable, so Load names the action and the chord
// it threw away.
func TestLoadWarnsAboutTheDroppedKeyboardChord(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := keybinds.New()
	// The chord is listed AFTER a joystick trigger on purpose: the survivor
	// reported in the warning must be the kept CHORD, not merely the first
	// surviving trigger of any kind.
	s.Load(map[string][]string{
		"global.push_to_mute": {"joy:stick-c3:btn12", "V", "Ctrl+B"},
	})

	out := buf.String()
	if out == "" {
		t.Fatal("Load dropped a second keyboard chord silently; the next Save rewrites " +
			"the user's file without it and nothing anywhere records why")
	}
	if !strings.Contains(out, "global.push_to_mute") {
		t.Errorf("warning does not name the action: %q", out)
	}
	if !strings.Contains(out, "Ctrl+B") {
		t.Errorf("warning does not name the dropped chord: %q", out)
	}
	if !strings.Contains(out, "dropped=Ctrl+B") {
		t.Errorf("warning does not attribute Ctrl+B to the DROPPED slot: %q", out)
	}
	if !strings.Contains(out, "kept=V") {
		t.Errorf("warning does not name V as the survivor: %q", out)
	}
}

// TestLoadDoesNotWarnForAJoystickTriggerAlongsideAChord guards the other
// direction: joystick triggers are unlimited, so a key + several buttons is
// the ordinary case and must produce no warning at all.
func TestLoadDoesNotWarnForAJoystickTriggerAlongsideAChord(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := keybinds.New()
	s.Load(map[string][]string{
		"global.ptt": {"F1", "joy:stick-c3:btn12", "joy:stick-c3:btn3"},
	})
	if got := buf.String(); got != "" {
		t.Errorf("Load warned about a perfectly legal binding list: %q", got)
	}
	if got, _ := s.Get("global.ptt"); len(got) != 3 {
		t.Fatalf("Get = %+v, want all three triggers", got)
	}
}

// TestLoadWarnsAboutAnUnparseableTrigger pins the other destructive drop in
// Load, for the same reason as the dropped-chord warning above.
//
// A hand-edited `"global.ptt" = ["joy:stick-c3:btn3", "joy:stick-c3:btn!2"]`
// with a typo in the second entry loads as the first alone, and the very next
// Save rewrites that line without the typo'd entry -- the user's binding gone
// from config.toml for good, with no trace anywhere of what was rejected or
// why. The two paths destroy config by exactly the same mechanism, so they
// must be equally diagnosable.
func TestLoadWarnsAboutAnUnparseableTrigger(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := keybinds.New()
	s.Load(map[string][]string{
		"global.ptt": {"joy:stick-c3:btn3", "joy:stick-c3:btn!2"},
	})

	out := buf.String()
	if out == "" {
		t.Fatal("Load dropped an unparseable trigger silently; the next Save rewrites " +
			"the user's file without it and nothing anywhere records why")
	}
	if !strings.Contains(out, "global.ptt") {
		t.Errorf("warning does not name the action: %q", out)
	}
	if !strings.Contains(out, "joy:stick-c3:btn!2") {
		t.Errorf("warning does not name the rejected entry: %q", out)
	}
	// The surviving entry must not be reported as dropped.
	if strings.Contains(out, "dropped=joy:stick-c3:btn3 ") {
		t.Errorf("warning blames the surviving entry: %q", out)
	}
	if got, _ := s.Get("global.ptt"); len(got) != 1 {
		t.Fatalf("Get = %+v, want the one parseable trigger kept", got)
	}
}
