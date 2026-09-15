package chord

import "testing"

func TestFromCode(t *testing.T) {
	tests := []struct {
		name                       string
		code                       string
		ctrl, alt, shift, super    bool
		want                       string
	}{
		{"letter", "KeyD", false, false, false, false, "D"},
		{"digit", "Digit1", false, false, false, false, "1"},
		{"alt digit", "Digit1", false, true, false, false, "Alt+1"},
		{"function key", "F1", false, false, false, false, "F1"},
		{"space", "Space", false, false, false, false, "Space"},
		{"ctrl letter", "KeyE", true, false, false, false, "Ctrl+E"},
		{"numpad", "Numpad7", false, false, false, false, "Numpad7"},
		{"arrow", "ArrowUp", false, false, false, false, "ArrowUp"},
		{"all modifiers", "KeyA", true, true, true, true, "Ctrl+Alt+Shift+Super+A"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := FromCode(tt.code, tt.ctrl, tt.alt, tt.shift, tt.super)
			if err != nil {
				t.Fatalf("FromCode error: %v", err)
			}
			if got := c.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromCodeRejectsModifierKeys(t *testing.T) {
	// Pressing a bare modifier must not produce a binding.
	for _, code := range []string{"ControlLeft", "AltRight", "ShiftLeft", "MetaLeft"} {
		t.Run(code, func(t *testing.T) {
			if _, err := FromCode(code, true, false, false, false); err == nil {
				t.Errorf("FromCode(%q) = nil error, want rejection", code)
			}
		})
	}
}

func TestFromCodeRejectsUnknown(t *testing.T) {
	if _, err := FromCode("NotARealCode", false, false, false, false); err == nil {
		t.Error("expected error for unknown code")
	}
}
