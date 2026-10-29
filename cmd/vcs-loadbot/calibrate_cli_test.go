package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen/lgtest"
)

// startReflectCLI runs `reflect` and returns its address and a stop func that
// returns the final reflector_stats line.
func startReflectCLI(t *testing.T) (addr string, stop func() string) {
	t.Helper()
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
		Addr string `json:"addr"`
	}
	if err := json.Unmarshal(sc.Bytes(), &ready); err != nil || ready.Addr == "" {
		t.Fatalf("ready line %q: %v", sc.Text(), err)
	}
	last := make(chan string, 1)
	go func() {
		l := ""
		for sc.Scan() {
			l = sc.Text()
		}
		last <- l
	}()
	return ready.Addr, func() string {
		fmt.Fprintln(inW, `{"quit":true}`)
		<-done
		return <-last
	}
}

func keysOf(t *testing.T, line string) []string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("line %q: %v", line, err)
	}
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// fixtureKeys returns the key set of the first I-7 fixture line of each type.
func fixtureKeys(t *testing.T) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "loadgen", "testdata", "interface", "v2-calibrate.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var h struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(l), &h); err != nil {
			t.Fatal(err)
		}
		if _, ok := out[h.Type]; !ok {
			out[h.Type] = keysOf(t, l)
		}
	}
	return out
}

func TestCalibrateCLIStopsAtLastGoodStep(t *testing.T) {
	refl, stopRefl := startReflectCLI(t)
	proxy := lgtest.HelloDropProxy(t, refl, func(i int) bool { return i >= 6 })

	var out, errw bytes.Buffer
	code := run(context.Background(), []string{"calibrate", "--reflector", proxy, "--allow", proxy, "--steps", "4,8", "--hold", "1"},
		strings.NewReader(""), &out, &errw)
	stats := stopRefl()
	if code != 0 {
		t.Fatalf("exit %d, want 0 with one completed step; stderr=%q", code, errw.String())
	}
	if !strings.Contains(stats, `"queue_full_drops"`) || !strings.Contains(stats, `"write_errors"`) {
		t.Errorf("reflector_stats = %q, want split counters", stats)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var types []string
	want := fixtureKeys(t)
	for _, l := range lines {
		var h struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(l), &h); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		if h.Type == "progress" {
			continue
		}
		types = append(types, h.Type)
		if !strings.HasPrefix(l, `{"type":"`+h.Type+`"`) {
			t.Errorf("%s line does not lead with type: %.60s", h.Type, l)
		}
		if got, w := keysOf(t, l), want[h.Type]; fmt.Sprint(got) != fmt.Sprint(w) {
			t.Errorf("%s keys = %v, want %v", h.Type, got, w)
		}
	}
	if fmt.Sprint(types) != "[calibration calibration_stopped receive_capacity]" {
		t.Fatalf("line types = %v, want [calibration calibration_stopped receive_capacity]\n%s", types, out.String())
	}
	var stopped struct {
		N, Bots int
		Stage   string
		Tries   int
		Reason  string
	}
	for _, l := range lines {
		if strings.Contains(l, `"calibration_stopped"`) {
			_ = json.Unmarshal([]byte(l), &stopped)
		}
	}
	if stopped.N != 8 || stopped.Bots != 6 || stopped.Stage != "hello" || stopped.Tries != 5 ||
		stopped.Reason != "bot 6: no HELLO_ACK after 5 tries" {
		t.Errorf("calibration_stopped = %+v", stopped)
	}
}

func TestCalibrateCLINoStepExits1(t *testing.T) {
	refl, stopRefl := startReflectCLI(t)
	proxy := lgtest.HelloDropProxy(t, refl, func(i int) bool { return i >= 1 })

	var out, errw bytes.Buffer
	code := run(context.Background(), []string{"calibrate", "--reflector", proxy, "--allow", proxy, "--steps", "4", "--hold", "1"},
		strings.NewReader(""), &out, &errw)
	stopRefl()
	if code != 1 {
		t.Fatalf("exit %d, want 1; stdout=%q", code, out.String())
	}
	s := out.String()
	if !strings.Contains(s, `"type":"calibration_stopped"`) {
		t.Errorf("stdout lacks calibration_stopped: %q", s)
	}
	if strings.Contains(s, `"type":"receive_capacity"`) || strings.Contains(s, `"type":"calibration"`) {
		t.Errorf("stdout has a result line though no step completed: %q", s)
	}
}

func TestReflectStatsLineHasSplitCounters(t *testing.T) {
	_, stopRefl := startReflectCLI(t)
	st := stopRefl()
	var m map[string]any
	if err := json.Unmarshal([]byte(st), &m); err != nil {
		t.Fatalf("%q: %v", st, err)
	}
	for _, k := range []string{"send_errors", "queue_full_drops", "write_errors", "senders"} {
		if _, ok := m[k]; !ok {
			t.Errorf("reflector_stats lacks %q: %s", k, st)
		}
	}
}
