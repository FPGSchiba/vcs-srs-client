package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice/testdata"
)

// TestInterfaceV3BotLinesRoundTrip checks the v3 fixture lines whose types
// live in package main (features, calibration) against the bot's own Go type
// with DisallowUnknownFields. The loadgen-owned lines are checked in
// internal/loadgen.
func TestInterfaceV3BotLinesRoundTrip(t *testing.T) {
	for _, c := range []struct {
		file string
		dst  any
	}{
		{"v3-features.json", &featuresLine{}},
		{"v3-calibration.json", &calibrationLine{}},
	} {
		lines := ifaceLines(t, c.file)
		if len(lines) != 1 {
			t.Fatalf("%s: %d lines, want 1", c.file, len(lines))
		}
		roundTrip(t, c.file, lines[0], c.dst)
	}
}

// keyPaths flattens a JSON line to the sorted set of its key paths. Objects
// are descended into (except the data-keyed updates map); arrays,
// scalars and null are leaves, so an empty
// failed/updates/violations list compares equal to a filled one.
func keyPaths(t *testing.T, line string) []string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("line %q: %v", line, err)
	}
	var out []string
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			p := prefix + k
			// updates is keyed by update type, so its keys are data, not schema.
			if sub, ok := v.(map[string]any); ok && len(sub) > 0 && k != "updates" {
				walk(p+".", sub)
				continue
			}
			out = append(out, p)
		}
	}
	walk("", m)
	sort.Strings(out)
	return out
}

// requireSameKeys compares key sets only, never values.
func requireSameKeys(t *testing.T, what, got, want string) {
	t.Helper()
	g, w := keyPaths(t, got), keyPaths(t, want)
	if strings.Join(g, ",") != strings.Join(w, ",") {
		t.Errorf("%s: key set differs from the fixture\n got  %v\n want %v\n line %s", what, g, w, got)
	}
}

func lineType(t *testing.T, line string) string {
	t.Helper()
	var h struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		t.Fatalf("line %q: %v", line, err)
	}
	return h.Type
}

// TestCLIV3KeySets runs a real `run` against a headless server and a real
// `calibrate` against a real reflector, and requires every line of a type the
// v3 fixtures cover to carry exactly the fixture's keys.
func TestCLIV3KeySets(t *testing.T) {
	t.Run("run", func(t *testing.T) {
		srv := testdata.Start(t)
		voiceAddr := fmt.Sprintf("%s:%d", srv.VoiceHost, srv.VoicePort)
		pw := filepath.Join(t.TempDir(), "pw.json")
		b, _ := json.Marshal(map[string]string{srv.Coalition: srv.Password})
		if err := os.WriteFile(pw, b, 0o600); err != nil {
			t.Fatal(err)
		}
		in := strings.NewReader(`{"set_n":4}` + "\n" + `{"burst":{"add":2,"in_flight":2,"talker":"live"}}` + "\n" +
			`{"hold":1}` + "\n" + `{"report":"v3keys"}` + "\n" + `{"quit":true}` + "\n")
		var out, errw bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		code := run(ctx, []string{
			"run", "--scenario", "one-big-net", "--arm", "S",
			"--control", srv.ControlAddr, "--voice", voiceAddr,
			"--allow", srv.ControlAddr + "," + voiceAddr,
			"--passwords", pw, "--seed", "1",
		}, in, &out, &errw)
		if code != 0 {
			t.Fatalf("exit %d; stderr=%q stdout=%q", code, errw.String(), out.String())
		}
		ramp := ifaceLines(t, "v3-joins-ramp.json")[0]
		burst := ifaceLines(t, "v3-joins-burst.json")[0]
		step := ifaceLines(t, "v3-step.json")[0]
		progress := ifaceLines(t, "v3-progress.ndjson")
		var steps, joins int
		var sawBurst bool
		for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			switch lt := lineType(t, l); lt {
			case "step":
				steps++
				requireSameKeys(t, "step", l, step)
			case "joins":
				joins++
				var j struct {
					Mode string `json:"mode"`
				}
				_ = json.Unmarshal([]byte(l), &j)
				if j.Mode == "burst" {
					sawBurst = true
					requireSameKeys(t, "joins(burst)", l, burst)
				} else {
					requireSameKeys(t, "joins(ramp)", l, ramp)
				}
			case "progress":
				// Timer-driven, so seen only when a ramp outlasts the tick. A
				// zero join_failures key is absent in the ramp fixture, so
				// compare against the line of the same phase.
				var p struct {
					Phase string `json:"phase"`
				}
				_ = json.Unmarshal([]byte(l), &p)
				want := progress[0]
				if p.Phase == "burst" {
					want = progress[1]
				}
				var gm map[string]any
				_ = json.Unmarshal([]byte(l), &gm)
				if _, ok := gm["join_failures"]; ok && p.Phase == "ramp" {
					want = progress[1] // carries join_failures; phase differs, key set is the same shape
				}
				g, w := keyPaths(t, l), keyPaths(t, want)
				if strings.Join(g, ",") != strings.Join(w, ",") {
					t.Errorf("progress(%s): got %v want %v", p.Phase, g, w)
				}
			}
		}
		if steps != 1 || joins != 2 || !sawBurst {
			t.Fatalf("steps=%d joins=%d burst=%v; stdout=%s", steps, joins, sawBurst, out.String())
		}
	})

	t.Run("calibrate", func(t *testing.T) {
		refl, stopRefl := startReflectCLI(t)
		var out, errw bytes.Buffer
		code := run(context.Background(), []string{"calibrate", "--reflector", refl, "--allow", refl, "--steps", "4", "--hold", "1"},
			strings.NewReader(""), &out, &errw)
		stats := stopRefl()
		if code != 0 {
			t.Fatalf("exit %d; stderr=%q", code, errw.String())
		}
		cal := ifaceLines(t, "v3-calibration.json")[0]
		var n int
		for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if lineType(t, l) == "calibration" {
				n++
				requireSameKeys(t, "calibration", l, cal)
			}
		}
		if n == 0 {
			t.Fatalf("no calibration line in:\n%s", out.String())
		}
		rs := ifaceLines(t, "v2-reflector-stats.json")[0]
		requireSameKeys(t, "reflector_stats", stats, rs)
	})
}
