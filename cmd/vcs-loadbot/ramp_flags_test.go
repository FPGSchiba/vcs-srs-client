package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsBadRampFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--max-in-flight", "0"},
		{"--max-in-flight", "17"},
		{"--ramp-talker", "loud"},
		{"--join-rate-max", "-1"},
		{"--join-rate-scale", "-1"},
	} {
		var out, errw bytes.Buffer
		a := append([]string{"run", "--allow", "127.0.0.1:1", "--scenario", "one-big-net"}, args...)
		if code := run(context.Background(), a, strings.NewReader(""), &out, &errw); code != 2 {
			t.Errorf("%v: exit = %d, want 2; stderr=%q", args, code, errw.String())
		}
	}
}

// TestRampFlagDefaultsMatchI8 pins the I-8 defaults through the flag usage.
func TestRampFlagDefaultsMatchI8(t *testing.T) {
	var out, errw bytes.Buffer
	run(context.Background(), []string{"run", "-h"}, strings.NewReader(""), &out, &errw)
	u := errw.String()
	for _, want := range []string{
		"-join-rate-max float\n", "(default 12)",
		"-join-rate-scale float\n", "(default 5000)",
		"-max-in-flight int\n", "(default 8)",
		`-ramp-talker string`, `(default "silent")`,
	} {
		if !strings.Contains(u, want) {
			t.Errorf("usage lacks %q:\n%s", want, u)
		}
	}
}
