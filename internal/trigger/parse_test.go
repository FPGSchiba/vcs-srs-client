package trigger_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func mustChord(t *testing.T, s string) chord.Chord {
	t.Helper()
	c, err := chord.Parse(s)
	if err != nil {
		t.Fatalf("chord.Parse(%q): %v", s, err)
	}
	return c
}

func TestParseRoundTrip(t *testing.T) {
	mod := trigger.JoyButton{Device: "throttle-a1", Button: 6}
	cases := []struct {
		s    string
		want trigger.Trigger
	}{
		{"V", trigger.Key(mustChord(t, "V"))},
		{"Ctrl+Alt+F1", trigger.Key(mustChord(t, "Ctrl+Alt+F1"))},
		{"joy:stick-c3:btn1", trigger.Joy(trigger.JoyBinding{Device: "stick-c3", Button: 0})},
		{"joy:stick-c3:btn12", trigger.Joy(trigger.JoyBinding{Device: "stick-c3", Button: 11})},
		{"joy:stick-c3:btn128", trigger.Joy(trigger.JoyBinding{Device: "stick-c3", Button: 127})},
		{"joy:stick-c3:hat1.up", trigger.Joy(trigger.JoyBinding{
			Device: "stick-c3", Button: trigger.HatButton(0, 0)})},
		{"joy:stick-c3:hat4.up_left", trigger.Joy(trigger.JoyBinding{
			Device: "stick-c3", Button: trigger.HatButton(3, 7)})},
		// cross-device modifier: throttle button qualifies a stick button
		{"joy:throttle-a1:btn7+stick-c3:btn3", trigger.Joy(trigger.JoyBinding{
			Device: "stick-c3", Button: 2, Modifier: &mod})},
	}
	for _, c := range cases {
		got, err := trigger.Parse(c.s)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", c.s, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("Parse(%q) = %+v, want %+v", c.s, got, c.want)
		}
		if back := got.String(); back != c.s {
			t.Errorf("Parse(%q).String() = %q, want round trip", c.s, back)
		}
	}
}

func TestParseRejects(t *testing.T) {
	bad := []string{
		"",                           // empty
		"joy:",                       // no ref
		"joy:stick-c3",               // no input
		"joy:stick-c3:btn0",          // buttons are 1-indexed in text
		"joy:stick-c3:btn129",        // above 128
		"joy:stick-c3:btn",           // no number
		"joy:stick-c3:hat0.up",       // hats are 1-indexed in text
		"joy:stick-c3:hat5.up",       // above HatCount
		"joy:stick-c3:hat1.sideways", // not a direction
		"joy:stick c3:btn1",          // space is not a legal device id char
		"joy:a:btn1+b:btn2+c:btn3",   // only one modifier is supported
		"joy:stick-c3:wheel3",        // unknown input kind
	}
	for _, s := range bad {
		if got, err := trigger.Parse(s); err == nil {
			t.Errorf("Parse(%q) = %+v, want error", s, got)
		}
	}
}

func TestParseBareStringIsKeyboardChord(t *testing.T) {
	// Anything without the joy: prefix must go to chord.Parse, and its
	// failures must propagate rather than being swallowed into a joy binding.
	if _, err := trigger.Parse("NotAKey"); err == nil {
		t.Error("Parse(\"NotAKey\") = nil error, want chord parse failure")
	}
	got, err := trigger.Parse("Ctrl+E")
	if err != nil {
		t.Fatalf("Parse(\"Ctrl+E\"): %v", err)
	}
	if got.Kind != trigger.KindKey {
		t.Errorf("Parse(\"Ctrl+E\").Kind = %v, want KindKey", got.Kind)
	}
}

func TestStringOfZeroTriggerIsEmpty(t *testing.T) {
	if got := (trigger.Trigger{}).String(); got != "" {
		t.Errorf("zero Trigger.String() = %q, want \"\"", got)
	}
}
