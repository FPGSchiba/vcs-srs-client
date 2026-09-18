package chord

import "testing"

func TestChordString(t *testing.T) {
	tests := []struct {
		name string
		in   Chord
		want string
	}{
		{"bare key", Chord{Key: "F1"}, "F1"},
		{"single modifier", Chord{Mods: ModCtrl, Key: "E"}, "Ctrl+E"},
		{"canonical order", Chord{Mods: ModSuper | ModShift | ModAlt | ModCtrl, Key: "A"}, "Ctrl+Alt+Shift+Super+A"},
		{"alt digit", Chord{Mods: ModAlt, Key: "1"}, "Alt+1"},
		{"zero", Chord{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRoundTrip(t *testing.T) {
	for _, s := range []string{"F1", "Ctrl+E", "Ctrl+Alt+Shift+Super+A", "Alt+1", "Space", "Ctrl+Alt+A"} {
		t.Run(s, func(t *testing.T) {
			c, err := Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", s, err)
			}
			if got := c.String(); got != s {
				t.Errorf("round trip = %q, want %q", got, s)
			}
		})
	}
}

func TestParseNormalisesModifierOrder(t *testing.T) {
	c, err := Parse("Shift+Ctrl+A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.String(); got != "Ctrl+Shift+A" {
		t.Errorf("got %q, want canonical %q", got, "Ctrl+Shift+A")
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name, in string
		wantErr  error
	}{
		{"empty", "", ErrEmpty},
		{"modifier only", "Ctrl", ErrModifierOnly},
		{"modifiers only", "Ctrl+Shift", ErrModifierOnly},
		{"unknown key", "Ctrl+NotAKey", ErrUnknownKey},
		{"duplicate ctrl", "Ctrl+Ctrl+A", ErrDuplicateModifier},
		{"duplicate shift", "Shift+Shift+F1", ErrDuplicateModifier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.in); err == nil {
				t.Fatalf("Parse(%q) = nil error, want %v", tt.in, tt.wantErr)
			}
		})
	}
}

func TestIsZero(t *testing.T) {
	if !(Chord{}).IsZero() {
		t.Error("empty chord should be zero")
	}
	if (Chord{Key: "A"}).IsZero() {
		t.Error("chord with key should not be zero")
	}
}
