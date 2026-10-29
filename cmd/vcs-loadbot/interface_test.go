package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen"
)

const ifaceDir = "../../internal/loadgen/testdata/interface"

// ifaceLines returns the non-empty lines of an I-7 fixture.
func ifaceLines(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ifaceDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(b, []byte("\n")) {
		t.Fatalf("%s: no trailing newline", name)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// roundTrip decodes line into dst with DisallowUnknownFields, re-marshals
// dst and requires the same map as the line itself: the bot emits exactly the
// agreed keys and types.
func roundTrip(t *testing.T, name, line string, dst any, drop ...string) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		t.Fatalf("%s: decode into %T: %v\nline: %s", name, dst, err, line)
	}
	var want, got map[string]any
	if err := json.Unmarshal([]byte(line), &want); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(dst)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range drop {
		// "a.b" drops key b of the object at key a.
		if parent, child, ok := strings.Cut(k, "."); ok {
			if m, isMap := got[parent].(map[string]any); isMap {
				delete(m, child)
			}
			continue
		}
		delete(got, k)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("%s: round trip differs\nwant %v\ngot  %v\nline %s\nout  %s", name, want, got, line, b)
	}
}

// TestInterfaceFixturesRoundTrip checks every I-7 fixture line the bot writes
// against the bot's own Go type.
func TestInterfaceFixturesRoundTrip(t *testing.T) {
	files := []string{
		"v2-joins.json", "v2-joins-noop.json", "v2-progress.ndjson",
		"v2-calibrate.ndjson", "v2-reflector-stats.json",
	}
	seen := map[string]int{}
	for _, f := range files {
		for _, line := range ifaceLines(t, f) {
			var head struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(line), &head); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			var dst any
			switch head.Type {
			case "joins":
				dst = &loadgen.JoinReport{}
			case "progress":
				dst = &loadgen.ProgressLine{}
			case "calibration":
				dst = &calibrationLine{}
			case "calibration_stopped":
				dst = &calibrationStoppedLine{}
			case "receive_capacity":
				dst = &receiveCapacityLine{}
			case "reflector_stats":
				dst = &reflectorStatsLine{}
			default:
				t.Fatalf("%s: fixture line of unexpected type %q", f, head.Type)
			}
			seen[head.Type]++
			// The v2 fixtures predate the v3 keys; C-T10 round-trips the v3 ones.
			roundTrip(t, f, line, dst, v3Keys[head.Type]...)
		}
	}
	for _, typ := range []string{"joins", "progress", "calibration", "calibration_stopped", "receive_capacity", "reflector_stats"} {
		if seen[typ] == 0 {
			t.Errorf("no fixture line of type %s was checked", typ)
		}
	}
}

// TestStepReportCarriesInterfaceAdditions pins interface I-3.
func TestStepReportCarriesInterfaceAdditions(t *testing.T) {
	b, err := json.Marshal(loadgen.StepReport{Type: "step", NRequested: 7, JoinFailuresTotal: 3})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["n_requested"] != float64(7) || m["join_failures_total"] != float64(3) {
		t.Fatalf("step line lacks n_requested/join_failures_total: %s", b)
	}
}

// v3Keys are the keys v3 added to v2 line types (I-12, I-14); the v2
// fixtures do not have them.
var v3Keys = map[string][]string{
	"joins":    {"mode", "talker", "joins_per_s", "join_p99_us", "hello_retried", "in_flight_max", "rate_limit"},
	"progress": {"rate_per_s", "talker"},
	// C-T7: soft GC bound, warnings and GC cycles.
	"calibration": {"warnings", "health.gc_cycles", "bounds.gc_pause_warn_p99_us"},
}
