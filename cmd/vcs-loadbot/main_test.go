package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen"
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

func TestFeaturesSubcommand(t *testing.T) {
	var out, errw bytes.Buffer
	code := run(context.Background(), []string{"features"}, strings.NewReader(""), &out, &errw)
	if code != 0 || errw.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", code, errw.String())
	}
	if n := strings.Count(out.String(), "\n"); n != 1 || !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("want exactly one line, got %q", out.String())
	}
	want, err := os.ReadFile("../../internal/loadgen/testdata/interface/v3-features.json")
	if err != nil {
		t.Fatal(err)
	}
	var g, w map[string]any
	if err := json.Unmarshal(out.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("features line\n got %v\nwant %v", g, w)
	}
	fs, _ := g["features"].([]any)
	strs := make([]string, len(fs))
	for i, f := range fs {
		strs[i], _ = f.(string)
	}
	if !sort.StringsAreSorted(strs) {
		t.Fatalf("features not sorted: %v", strs)
	}
}

func TestFeaturesDefaultsMatchFlags(t *testing.T) {
	var out, errw bytes.Buffer
	if code := run(context.Background(), []string{"features"}, strings.NewReader(""), &out, &errw); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	var l featuresLine
	if err := json.Unmarshal(out.Bytes(), &l); err != nil {
		t.Fatal(err)
	}
	// Flag defaults, read back from the real flag set via -h output.
	var herr bytes.Buffer
	run(context.Background(), []string{"run", "-h"}, strings.NewReader(""), io.Discard, &herr)
	help := herr.String()
	d := l.Defaults
	for _, c := range []struct{ flag, def string }{
		{"ping-every", fmt.Sprintf("(default %v)", defaultPingEvery)},
		{"join-rate-max", fmt.Sprintf("(default %v)", d.JoinRateMax)},
		{"join-rate-scale", fmt.Sprintf("(default %v)", d.JoinRateScale)},
		{"max-in-flight", fmt.Sprintf("(default %d)", d.MaxInFlight)},
		{"ramp-talker", fmt.Sprintf("(default %q)", d.RampTalker)},
	} {
		i := strings.Index(help, "-"+c.flag+" ")
		if i < 0 {
			t.Fatalf("flag %s missing from help:\n%s", c.flag, help)
		}
		rest := help[i:]
		if j := strings.Index(rest[1:], "\n  -"); j >= 0 {
			rest = rest[:j+1]
		}
		if !strings.Contains(rest, c.def) {
			t.Errorf("flag %s: help %q lacks %s", c.flag, rest, c.def)
		}
	}
	if d.PingEveryS != 5 || d.JoinRateMax != loadgen.DefaultJoinRateMax || d.JoinRateScale != loadgen.DefaultJoinRateScale ||
		d.MaxInFlight != loadgen.DefaultMaxInFlight || d.RampTalker != loadgen.RampTalkerSilent {
		t.Errorf("defaults = %+v", d)
	}
	b := loadgen.DefaultBounds
	if d.GCPauseP99US != b.GCPauseP99.Microseconds() || d.GCPauseWarnP99US != b.GCPauseWarnP99.Microseconds() {
		t.Errorf("gc defaults %+v vs bounds %v/%v", d, b.GCPauseP99, b.GCPauseWarnP99)
	}
}
