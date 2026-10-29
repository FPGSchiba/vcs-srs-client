package loadgen

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// v3Lines returns the non-empty lines of a shared W6 fix 2 fixture (I-17).
func v3Lines(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "interface", name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(b, []byte("\n")) || bytes.HasSuffix(b, []byte("\n\n")) {
		t.Fatalf("%s: want exactly one trailing newline", name)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// v3RoundTrip decodes line into dst with DisallowUnknownFields, re-marshals it
// and requires the same map as the line: the bot emits exactly the agreed keys
// and types, no more and no fewer.
func v3RoundTrip(t *testing.T, name, line string, dst any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		t.Fatalf("%s: decode into %T: %v\nline: %s", name, dst, err, line)
	}
	out, err := json.Marshal(dst)
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	if err := json.Unmarshal([]byte(line), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("%s: round trip differs\nwant %v\ngot  %v\nout  %s", name, want, got, out)
	}
}

// TestInterfaceV3FixturesRoundTrip checks every v3 fixture line whose type the
// loadgen package owns (step, control_lost, joins, progress). The calibration
// and features lines belong to cmd/vcs-loadbot and are checked there.
func TestInterfaceV3FixturesRoundTrip(t *testing.T) {
	seen := map[string]int{}
	for _, f := range []string{
		"v3-step.json", "v3-control-lost.json", "v3-joins-ramp.json",
		"v3-joins-burst.json", "v3-progress.ndjson",
	} {
		for _, line := range v3Lines(t, f) {
			var head struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(line), &head); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			var dst any
			switch head.Type {
			case "step":
				dst = &StepReport{}
			case "control_lost":
				dst = &DeathLine{}
			case "joins":
				dst = &JoinReport{}
			case "progress":
				dst = &ProgressLine{}
			default:
				t.Fatalf("%s: unexpected type %q", f, head.Type)
			}
			seen[head.Type]++
			v3RoundTrip(t, f, line, dst)
		}
	}
	for typ, n := range map[string]int{"step": 1, "control_lost": 1, "joins": 2, "progress": 2} {
		if seen[typ] != n {
			t.Errorf("type %s: checked %d lines, want %d", typ, seen[typ], n)
		}
	}
}

// TestV3JoinsRampFixtureHoldsI1Invariants applies the I-1 invariants to the
// ramp fixture. The burst fixture is exempt: it lists 1 of 30 failures on
// purpose.
func TestV3JoinsRampFixtureHoldsI1Invariants(t *testing.T) {
	var r JoinReport
	if err := json.Unmarshal([]byte(v3Lines(t, "v3-joins-ramp.json")[0]), &r); err != nil {
		t.Fatal(err)
	}
	if r.Joined+r.JoinFailures != r.Requested-r.Had {
		t.Errorf("joined+failures %d != requested-had %d", r.Joined+r.JoinFailures, r.Requested-r.Had)
	}
	if r.N != r.Had+r.Joined {
		t.Errorf("n %d != had+joined %d", r.N, r.Had+r.Joined)
	}
	if len(r.HelloTriesHist) != len(DefaultHelloLadder)+1 || r.HelloTriesHist[0] != 0 {
		t.Errorf("hist shape %v", r.HelloTriesHist)
	}
	sum, max, retried := 0, 0, 0
	for i, v := range r.HelloTriesHist {
		sum += v
		if v > 0 {
			max = i
		}
		if i >= 2 {
			retried += v
		}
	}
	if sum != r.Joined {
		t.Errorf("hist sums to %d, joined %d", sum, r.Joined)
	}
	if max != r.HelloTriesMax {
		t.Errorf("hello_tries_max %d, hist says %d", r.HelloTriesMax, max)
	}
	if retried != r.HelloRetried {
		t.Errorf("hello_retried %d, hist says %d", r.HelloRetried, retried)
	}
	fb := r.FailuresByStage
	if fb.Control+fb.Radio+fb.VoiceDial+fb.Hello != r.JoinFailures {
		t.Errorf("failures_by_stage %+v != join_failures %d", fb, r.JoinFailures)
	}
	if len(r.Failed) > 32 || (len(r.Failed) < r.JoinFailures) != r.FailedTruncated {
		t.Errorf("failed list %d, truncated %v, failures %d", len(r.Failed), r.FailedTruncated, r.JoinFailures)
	}
	if r.Label != ReportLabel || r.Schema != JoinsSchema || r.Mode != RampMode {
		t.Errorf("header %q %q %q", r.Label, r.Schema, r.Mode)
	}
}
