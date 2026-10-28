package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunRefusesNonLoopbackBeforeDial(t *testing.T) {
	var out, errw bytes.Buffer
	start := time.Now()
	code := run(context.Background(), []string{
		"run", "--control", "10.0.0.1:14446", "--voice", "127.0.0.1:5002",
		"--allow", "127.0.0.1:5002", "--scenario", "one-big-net",
	}, strings.NewReader(""), &out, &errw)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%q", code, errw.String())
	}
	if !strings.Contains(errw.String(), "not loopback") {
		t.Fatalf("stderr = %q, want it to contain %q", errw.String(), "not loopback")
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("took %v, a dial was probably attempted", d)
	}
}

func TestEverySubcommandChecksAddressesFirst(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--control", "10.0.0.1:1", "--allow", "127.0.0.1:1"},
		{"reflect", "--reflector", "10.0.0.1:1", "--allow", "127.0.0.1:1"},
		{"calibrate", "--voice", "10.0.0.1:1", "--allow", "127.0.0.1:1"},
	} {
		var out, errw bytes.Buffer
		if code := run(context.Background(), args, strings.NewReader(""), &out, &errw); code != 2 ||
			!strings.Contains(errw.String(), "not loopback") {
			t.Errorf("%v: exit=%d stderr=%q", args, code, errw.String())
		}
	}
}
