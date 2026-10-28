package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func TestRunRejectsPositionalArgs(t *testing.T) {
	var out, errw bytes.Buffer
	code := run(context.Background(), []string{"run", "--allow", "127.0.0.1:1", "10.0.0.1:1"},
		strings.NewReader(""), &out, &errw)
	if code != 2 || !strings.Contains(errw.String(), "unexpected argument") {
		t.Fatalf("exit=%d stderr=%q, want 2 and \"unexpected argument\"", code, errw.String())
	}
}

func TestReflectThenCalibrateCLI(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var errw bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run(context.Background(), []string{"reflect", "--listen", "127.0.0.1:0", "--senders", "2", "--allow", "127.0.0.1:1"}, inR, outW, &errw)
		outW.Close()
	}()
	sc := bufio.NewScanner(outR)
	if !sc.Scan() {
		t.Fatalf("no ready line; stderr=%q", errw.String())
	}
	var ready struct {
		Type string `json:"type"`
		Addr string `json:"addr"`
	}
	if err := json.Unmarshal(sc.Bytes(), &ready); err != nil || ready.Type != "ready" || ready.Addr == "" {
		t.Fatalf("ready line %q: %v", sc.Text(), err)
	}

	statsCh := make(chan string, 1)
	go func() {
		last := ""
		for sc.Scan() {
			last = sc.Text()
		}
		statsCh <- last
	}()
	var out, cerr bytes.Buffer
	code := run(context.Background(), []string{"calibrate", "--reflector", ready.Addr, "--allow", ready.Addr, "--steps", "4,8", "--hold", "1"},
		strings.NewReader(""), &out, &cerr)
	if code != 0 {
		t.Fatalf("calibrate exit %d: %s", code, cerr.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", len(lines), out.String())
	}
	for i, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if m["label"] == nil {
			t.Errorf("line %d has no label", i)
		}
	}
	if !strings.Contains(lines[2], `"type":"receive_capacity"`) {
		t.Errorf("last line = %s", lines[2])
	}

	fmt.Fprintln(inW, `{"quit":true}`)
	if c := <-done; c != 0 {
		t.Fatalf("reflect exit %d: %s", c, errw.String())
	}
	if st := <-statsCh; !strings.Contains(st, `"type":"reflector_stats"`) {
		t.Errorf("last reflect line = %q", st)
	}
}

func TestReflectRefusesNonLoopbackListen(t *testing.T) {
	var out, errw bytes.Buffer
	code := run(context.Background(), []string{"reflect", "--listen", "0.0.0.0:0", "--senders", "2"},
		strings.NewReader(""), &out, &errw)
	if code != 2 || !strings.Contains(errw.String(), "not loopback") {
		t.Fatalf("exit=%d stderr=%q", code, errw.String())
	}
}

func TestParseSteps(t *testing.T) {
	got, err := parseSteps("64, 128,256")
	if err != nil || len(got) != 3 || got[1] != 128 {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []string{"", "1", "a,4", "8,4"} {
		if _, err := parseSteps(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
