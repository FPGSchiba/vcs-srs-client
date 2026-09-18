package keybinds

import "testing"

func TestIsPerRadioID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"radio.1.ptt", true},
		{"radio.12.select", true},
		{"radio.999.ptt", true},
		{"radio.ptt", false},    // no ID segment
		{"radio..ptt", false},   // empty ID segment
		{"global.ptt", false},   // wrong prefix
		{"radio.1.mute", false}, // wrong suffix
		{"radio.1.PTT", false},  // case-sensitive
		{"radio", false},        // too short
		{"radio.", false},       // incomplete
		{"radio.1", false},      // incomplete (missing suffix)
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := isPerRadioID(tt.id); got != tt.want {
				t.Errorf("isPerRadioID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
