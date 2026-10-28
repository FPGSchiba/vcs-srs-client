package loadgen

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen/lgtest"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice/testdata"
)

// A voice-handshake failure for one bot must be that ramp's recorded outcome,
// not a fatal error: Serve keeps going, reports the failure in a joins line
// and the step carries the shortfall.
func TestServeSetNContinuesPastHelloFailure(t *testing.T) {
	srv := testdata.Start(t)
	sc, err := LoadScenario("one-big-net")
	if err != nil {
		t.Fatal(err)
	}
	upstream := fmt.Sprintf("%s:%d", srv.VoiceHost, srv.VoicePort)
	proxy := lgtest.HelloDropProxy(t, upstream, func(i int) bool { return i == 2 })
	allow, err := ParseAllow(srv.ControlAddr + "," + proxy)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(RunConfig{
		Arm: "S", Scenario: sc, ControlAddr: srv.ControlAddr, VoiceAddr: proxy,
		Allow: allow, Passwords: map[string]string{srv.Coalition: srv.Password},
		Unit: "LDBT", Seed: 1, Bounds: DefaultBounds,
	}, DialInsecure(srv.ControlAddr))
	if err != nil {
		t.Fatal(err)
	}

	in := strings.NewReader(`{"set_n":4}` + "\n" + `{"hold":1}` + "\n" + `{"report":"a"}` + "\n" + `{"quit":true}` + "\n")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := r.Serve(ctx, in, &out); err != nil {
		srv.DumpLog(t)
		t.Fatalf("Serve returned %v, want nil\nstdout:\n%s", err, out.String())
	}

	var joins, step map[string]any
	sc2 := bufio.NewScanner(&out)
	for sc2.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc2.Bytes(), &m); err != nil {
			t.Fatalf("non-JSON stdout line %q: %v", sc2.Text(), err)
		}
		switch m["type"] {
		case "joins":
			joins = m
		case "step":
			step = m
		case "error":
			t.Fatalf("error line on stdout: %v", m)
		}
	}
	if joins == nil || step == nil {
		t.Fatalf("missing joins (%v) or step (%v) line", joins != nil, step != nil)
	}
	num := func(m map[string]any, k string) int { f, _ := m[k].(float64); return int(f) }
	for k, want := range map[string]int{"requested": 4, "had": 0, "n": 3, "joined": 3, "join_failures": 1} {
		if got := num(joins, k); got != want {
			t.Errorf("joins.%s = %d, want %d", k, got, want)
		}
	}
	by, _ := joins["failures_by_stage"].(map[string]any)
	if num(by, "hello") != 1 {
		t.Errorf("failures_by_stage = %v, want hello 1", by)
	}
	failed, _ := joins["failed"].([]any)
	if len(failed) != 1 {
		t.Fatalf("failed = %v, want 1 entry", failed)
	}
	f0, _ := failed[0].(map[string]any)
	if f0["stage"] != "hello" || num(f0, "tries") != 5 {
		t.Errorf("failed[0] = %v, want stage hello tries 5", f0)
	}
	if step["step"] != "a" {
		t.Errorf("step name = %v", step["step"])
	}
	for k, want := range map[string]int{"n": 3, "listeners": 2, "n_requested": 4, "join_failures_total": 1} {
		if got := num(step, k); got != want {
			t.Errorf("step.%s = %d, want %d", k, got, want)
		}
	}
}


// newJoinFailRunner builds a Runner against a real headless server whose
// voice endpoint is a HELLO-dropping proxy.
func newJoinFailRunner(t *testing.T, drop func(int) bool) *Runner {
	t.Helper()
	srv := testdata.Start(t)
	sc, err := LoadScenario("one-big-net")
	if err != nil {
		t.Fatal(err)
	}
	upstream := fmt.Sprintf("%s:%d", srv.VoiceHost, srv.VoicePort)
	proxy := lgtest.HelloDropProxy(t, upstream, drop)
	allow, err := ParseAllow(srv.ControlAddr + "," + proxy)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(RunConfig{
		Arm: "S", Scenario: sc, ControlAddr: srv.ControlAddr, VoiceAddr: proxy,
		Allow: allow, Passwords: map[string]string{srv.Coalition: srv.Password},
		Unit: "LDBT", Seed: 1, Bounds: DefaultBounds,
	}, DialInsecure(srv.ControlAddr))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func serveLines(t *testing.T, r *Runner, cmds string, timeout time.Duration) ([]map[string]any, error) {
	t.Helper()
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := r.Serve(ctx, strings.NewReader(cmds), &out)
	var lines []map[string]any
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		var m map[string]any
		if jerr := json.Unmarshal(sc.Bytes(), &m); jerr != nil {
			t.Fatalf("non-JSON stdout line %q: %v", sc.Text(), jerr)
		}
		lines = append(lines, m)
	}
	return lines, err
}

func ofType(lines []map[string]any, typ string) []map[string]any {
	var o []map[string]any
	for _, m := range lines {
		if m["type"] == typ {
			o = append(o, m)
		}
	}
	return o
}

// Bot 0 is the talker: without it there is nothing to measure, so its HELLO
// failure stays fatal and no joins line is written.
func TestServeTalkerHelloFailureStaysFatal(t *testing.T) {
	r := newJoinFailRunner(t, func(i int) bool { return i == 0 })
	lines, err := serveLines(t, r, `{"set_n":3}`+"\n", 60*time.Second)
	if err == nil {
		t.Fatal("Serve returned nil, want the talker's HELLO error")
	}
	if len(ofType(lines, "error")) != 1 {
		t.Errorf("want one error line, got %v", lines)
	}
	if len(ofType(lines, "joins")) != 0 {
		t.Errorf("a joins line was written for a fatal talker failure: %v", lines)
	}
}

// A no-op set_n still writes its joins line, with zeros.
func TestServeNoopSetNWritesJoinsLine(t *testing.T) {
	r := newJoinFailRunner(t, func(int) bool { return false })
	lines, err := serveLines(t, r, `{"set_n":2}`+"\n"+`{"set_n":2}`+"\n"+`{"quit":true}`+"\n", 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	js := ofType(lines, "joins")
	if len(js) != 2 {
		t.Fatalf("want 2 joins lines, got %d", len(js))
	}
	num := func(m map[string]any, k string) int { f, _ := m[k].(float64); return int(f) }
	j := js[1]
	if num(j, "had") != 2 || num(j, "requested") != 2 || num(j, "joined") != 0 || num(j, "n") != 2 {
		t.Errorf("noop joins = %v", j)
	}
}

// A slow ramp (one bot burns its 7.5 s ladder) writes progress lines, before
// the joins line, and every line is valid JSON.
func TestServeProgressLines(t *testing.T) {
	r := newJoinFailRunner(t, func(i int) bool { return i == 2 })
	r.progressEvery = 100 * time.Millisecond
	lines, err := serveLines(t, r, `{"set_n":4}`+"\n"+`{"quit":true}`+"\n", 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pi, ji := -1, -1
	for i, m := range lines {
		switch m["type"] {
		case "progress":
			if pi < 0 {
				pi = i
			}
			if m["phase"] != "ramp" {
				t.Errorf("phase = %v", m["phase"])
			}
		case "joins":
			ji = i
		}
	}
	if pi < 0 || ji < 0 || pi > ji {
		t.Fatalf("progress index %d, joins index %d; want progress before joins", pi, ji)
	}
}
