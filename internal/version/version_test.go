package version

import (
	"runtime/debug"
	"testing"
)

func TestBuildFromSettings(t *testing.T) {
	full := "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name string
		in   []debug.BuildSetting
		want string
	}{
		{"full sha truncated", []debug.BuildSetting{{Key: "vcs.revision", Value: full}}, "0123456"},
		{"short kept", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}, "abc"},
		{"nil", nil, "dev"},
		{"no revision key", []debug.BuildSetting{{Key: "vcs.time", Value: "2026-10-27T00:00:00Z"}}, "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildFrom(c.in); got != c.want {
				t.Errorf("buildFrom(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
