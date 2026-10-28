package loadgen

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestJoinTallyReport(t *testing.T) {
	tl := newJoinTally(75+10, 10)
	// 40 successes: tries 1 (30), 2 (7), 5 (3).
	for i := 0; i < 30; i++ {
		tl.ok(1)
	}
	for i := 0; i < 7; i++ {
		tl.ok(2)
	}
	for i := 0; i < 3; i++ {
		tl.ok(5)
	}
	// 35 failures across all four stages, inserted in descending bot order.
	stages := []JoinStage{StageControl, StageRadio, StageVoiceDial, StageHello}
	want := map[JoinStage]int{}
	for i := 0; i < 35; i++ {
		st := stages[i%4]
		want[st]++
		tries := 0
		if st == StageHello {
			tries = 5
		}
		tl.fail(JoinFailure{Bot: 100 - i, GUID: "g", Stage: st, Tries: tries, Error: "x"})
	}
	rh := RampHealth{SchedP99US: 850, GCPauseP99US: 120}
	r := tl.report(85, 10, 2*time.Second, rh)

	if r.Type != "joins" || r.Schema != "loadbot-joins/1" || r.Label != ReportLabel {
		t.Fatalf("header: %+v", r)
	}
	if r.Requested != 85 || r.Had != 10 || r.Joined != 40 || r.N != 50 || r.JoinFailures != 35 {
		t.Fatalf("counts: %+v", r)
	}
	if len(r.HelloTriesHist) != len(DefaultHelloLadder)+1 {
		t.Fatalf("hist len %d", len(r.HelloTriesHist))
	}
	sum := 0
	for _, v := range r.HelloTriesHist {
		sum += v
	}
	if sum != r.Joined || r.HelloTriesHist[0] != 0 || r.HelloTriesHist[1] != 30 || r.HelloTriesHist[2] != 7 || r.HelloTriesHist[5] != 3 {
		t.Fatalf("hist %v", r.HelloTriesHist)
	}
	if r.HelloTriesMax != 5 {
		t.Fatalf("max %d", r.HelloTriesMax)
	}
	if len(r.Failed) != 32 || !r.FailedTruncated {
		t.Fatalf("failed %d truncated %v", len(r.Failed), r.FailedTruncated)
	}
	for i := 1; i < len(r.Failed); i++ {
		if r.Failed[i-1].Bot >= r.Failed[i].Bot {
			t.Fatalf("not sorted at %d", i)
		}
	}
	fb := r.FailuresByStage
	if fb.Control+fb.Radio+fb.VoiceDial+fb.Hello != 35 ||
		fb.Control != want[StageControl] || fb.Hello != want[StageHello] ||
		fb.Radio != want[StageRadio] || fb.VoiceDial != want[StageVoiceDial] {
		t.Fatalf("by stage %+v want %v", fb, want)
	}
	if r.RampS != 2 || r.RampHealth != rh {
		t.Fatalf("ramp %v %+v", r.RampS, r.RampHealth)
	}
	// hello_tries_max includes failed bots even when no success went that high.
	t2 := newJoinTally(2, 0)
	t2.ok(1)
	t2.fail(JoinFailure{Bot: 1, Stage: StageHello, Tries: 5})
	if m := t2.report(2, 0, 0, RampHealth{}).HelloTriesMax; m != 5 {
		t.Fatalf("failed-bot max %d", m)
	}
	if t2.report(2, 0, 0, RampHealth{}).Failed == nil {
		t.Fatal("failed must marshal as an array")
	}
	if p := tl.progress(); p.Done != 75 || p.Joined != 40 || p.HelloRetried != 10 || p.JoinFailures != 35 {
		t.Fatalf("progress %+v", p)
	}
}

func TestJoinTallyConcurrent(t *testing.T) {
	tl := newJoinTally(1600, 0)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				tl.begin()
				if i%10 == 0 {
					tl.fail(JoinFailure{Bot: g*100 + i, Stage: StageHello, Tries: 5})
				} else {
					tl.ok(1 + i%3)
				}
				_ = tl.progress()
			}
		}(g)
	}
	wg.Wait()
	r := tl.report(1600, 0, 0, RampHealth{})
	if r.Joined != 1440 || r.JoinFailures != 160 || r.FailuresByStage.Hello != 160 {
		t.Fatalf("%+v", r)
	}
	if p := tl.progress(); p.InFlight != 0 || p.Done != 1600 {
		t.Fatalf("progress %+v", p)
	}
}

func fixtureMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestJoinReportMatchesInterfaceFixture(t *testing.T) {
	g := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "interface", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	rep := JoinReport{
		Type: "joins", Schema: JoinsSchema, Requested: 2076, Had: 1038, N: 2074, Joined: 1036, JoinFailures: 2,
		FailuresByStage: StageCounts{Hello: 2},
		HelloTriesHist:  []int{0, 1006, 25, 4, 1, 0}, HelloTriesMax: 5,
		Failed: []JoinFailure{
			{Bot: 1553, GUID: "0b6f3c1e-8f0a-4c1d-9a51-1f2d3c4b5a69", Stage: StageHello, Tries: 5, Error: "no HELLO_ACK after 5 tries"},
			{Bot: 1669, GUID: "5a0e2d7c-3b44-4e8e-b1f2-7c9d0e1f2a3b", Stage: StageHello, Tries: 5, Error: "no HELLO_ACK after 5 tries"},
		},
		RampS: 38.4, RampHealth: RampHealth{SchedP99US: 850, GCPauseP99US: 120}, Label: ReportLabel,
	}
	noop := newJoinTally(2, 2).report(2, 2, 0, RampHealth{})
	for _, c := range []struct {
		file string
		rep  JoinReport
	}{{"v2-joins.json", rep}, {"v2-joins-noop.json", noop}} {
		b, err := json.Marshal(c.rep)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := fixtureMap(t, b), fixtureMap(t, g(c.file)); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s mismatch:\n got  %s\n want %s", c.file, b, g(c.file))
		}
	}
}

func TestProgressLineMatchesFixture(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "interface", "v2-progress.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(b)
	cases := []ProgressLine{
		{Type: "progress", Phase: "ramp", Requested: 2076, Had: 1038, Done: 512, Joined: 510, JoinFailures: 2, InFlight: 16, HelloRetried: 9, ElapsedS: 5.0},
		{Type: "progress", Phase: "calibrate_ramp", Target: 4096, Bots: 3000, HelloRetried: 12, ElapsedS: 5.0},
	}
	for i, c := range cases {
		out, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fixtureMap(t, out), fixtureMap(t, lines[i])) {
			t.Fatalf("line %d: got %s want %s", i, out, lines[i])
		}
	}
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// A tries value below 1 (no HELLO sent) must not break "index 0 is always 0"
// or "the hist sums to joined" (interface I-1).
func TestJoinTallyOkClampsTriesBelowOne(t *testing.T) {
	tl := newJoinTally(3, 0)
	for _, tries := range []int{0, -1, 1} {
		tl.begin()
		tl.ok(tries)
	}
	r := tl.report(3, 0, 0, RampHealth{})
	if r.HelloTriesHist[0] != 0 {
		t.Fatalf("hist[0] = %d, want 0 (hist %v)", r.HelloTriesHist[0], r.HelloTriesHist)
	}
	sum := 0
	for _, c := range r.HelloTriesHist {
		sum += c
	}
	if sum != r.Joined || r.Joined != 3 {
		t.Fatalf("hist sums to %d, joined %d, want 3", sum, r.Joined)
	}
}

func TestJoinErrorKeepsV1Text(t *testing.T) {
	base := errors.New("no HELLO_ACK after 5 tries")
	cases := []struct {
		name string
		err  *JoinError
		want string
	}{
		{"hello", &JoinError{Bot: 1669, Stage: StageHello, Tries: 5, Err: base}, "bot 1669: hello: no HELLO_ACK after 5 tries"},
		{"radio", &JoinError{Bot: 7, Stage: StageRadio, Err: base}, "bot 7: update radio: no HELLO_ACK after 5 tries"},
		{"voice_dial", &JoinError{Bot: 7, Stage: StageVoiceDial, Err: base}, "bot 7: dial voice: no HELLO_ACK after 5 tries"},
		{"control", &JoinError{Bot: 7, Stage: StageControl, Err: errors.New("join LDBT-0042: dial refused")}, "join LDBT-0042: dial refused"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("%s: Error() = %q, want %q", c.name, got, c.want)
		}
	}
	var wrapped error = fmt.Errorf("outer: %w", cases[0].err)
	var je *JoinError
	if !errors.As(wrapped, &je) || je.Bot != 1669 || je.Stage != StageHello || je.Tries != 5 {
		t.Fatalf("errors.As failed: %+v", je)
	}
	if !errors.Is(wrapped, base) {
		t.Fatal("errors.Is does not pass through Unwrap")
	}
}
