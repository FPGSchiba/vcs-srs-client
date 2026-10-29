package loadgen

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// rigProgressEvery is the progress period a rig's Runner uses; zero keeps
// the 5 s default.
var rigProgressEvery time.Duration

func joinsLines(g *serveRig, t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range g.lines(t) {
		if m["type"] == "joins" {
			out = append(out, m)
		}
	}
	return out
}

// sampleRamp polls r.nextSeq while the runner is in the ramp phase and
// returns the first and last sequence seen and the sample count.
func sampleRamp(g *serveRig, t *testing.T, until func() bool) (first, last uint32, n int) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && !until() {
		if Phase(g.r.phase.Load()) == PhaseRamp {
			s := g.r.nextSeq.Load()
			if n == 0 {
				first = s
			}
			last = s
			n++
		}
		time.Sleep(2 * time.Millisecond)
	}
	return
}

func TestSetNSilentTalker(t *testing.T) {
	g := newServeRigCfg(t, 0, func(c *RunConfig) {
		c.JoinRateMax, c.MaxInFlight = 20, 2
	})
	g.feed(t, `{"set_n":2}`, `{"set_n":12}`)
	// The first set_n has already created the talker; sample the second ramp.
	waitUntil(t, "first ramp", 30*time.Second, func() bool { return len(joinsLines(g, t)) >= 1 })
	first, last, n := sampleRamp(g, t, func() bool { return len(joinsLines(g, t)) >= 2 })
	if n == 0 {
		t.Fatal("never observed the second ramp")
	}
	if first != last {
		t.Fatalf("talker sent during a silent ramp: nextSeq %d -> %d over %d samples", first, last, n)
	}
	// After the ramp the talker is resumed.
	waitUntil(t, "talker resumed", 5*time.Second, func() bool { return g.r.nextSeq.Load() > last })
	js := joinsLines(g, t)
	if js[0]["talker"] != "silent" || js[1]["talker"] != "silent" {
		t.Fatalf("joins talker = %v, %v; want silent", js[0]["talker"], js[1]["talker"])
	}
	if err := g.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestRampTalkerLiveFlag(t *testing.T) {
	g := newServeRigCfg(t, 0, func(c *RunConfig) {
		c.JoinRateMax, c.MaxInFlight, c.RampTalker = 20, 2, "live"
	})
	g.feed(t, `{"set_n":2}`, `{"set_n":12}`)
	waitUntil(t, "first ramp", 30*time.Second, func() bool { return len(joinsLines(g, t)) >= 1 })
	first, last, n := sampleRamp(g, t, func() bool { return len(joinsLines(g, t)) >= 2 })
	if n == 0 {
		t.Fatal("never observed the second ramp")
	}
	if last <= first+5 {
		t.Fatalf("live talker did not send during the ramp: %d -> %d", first, last)
	}
	js := joinsLines(g, t)
	if js[1]["talker"] != "live" {
		t.Fatalf("joins talker = %v, want live", js[1]["talker"])
	}
	if err := g.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestSetNRespectsRateAndInFlight(t *testing.T) {
	old := rigProgressEvery
	rigProgressEvery = 100 * time.Millisecond
	defer func() { rigProgressEvery = old }()
	g := newServeRigCfg(t, 0, func(c *RunConfig) {
		c.JoinRateMax, c.JoinRateScale, c.MaxInFlight = 20, 0, 2
	})
	g.feed(t, `{"set_n":22}`)
	waitUntil(t, "joins line", 60*time.Second, func() bool { return len(joinsLines(g, t)) == 1 })
	j := joinsLines(g, t)[0]
	if rs, _ := j["ramp_s"].(float64); rs < 0.95 {
		t.Fatalf("ramp_s = %v, want >= 0.95 (20 joins at 20/s)", j["ramp_s"])
	}
	if p := g.r.peakInFlight.Load(); p > 2 {
		t.Fatalf("in-flight gauge peaked at %d, cap 2", p)
	}
	if j["in_flight_max"] != float64(2) {
		t.Fatalf("in_flight_max = %v", j["in_flight_max"])
	}
	rl, ok := j["rate_limit"].(map[string]any)
	if !ok || rl["max_per_s"] != float64(20) || rl["scale"] != float64(0) {
		t.Fatalf("rate_limit = %v", j["rate_limit"])
	}
	if jps, _ := j["joins_per_s"].(float64); jps <= 0 || jps > 25 {
		t.Fatalf("joins_per_s = %v", j["joins_per_s"])
	}
	if p, _ := j["join_p99_us"].(float64); p <= 0 {
		t.Fatalf("join_p99_us = %v", j["join_p99_us"])
	}
	if _, ok := j["hello_retried"]; !ok {
		t.Fatal("no hello_retried")
	}
	if j["mode"] != "ramp" {
		t.Fatalf("mode = %v", j["mode"])
	}
	var progress int
	for _, m := range g.lines(t) {
		if m["type"] == "progress" && m["phase"] == "ramp" {
			progress++
			if m["talker"] != "silent" {
				t.Fatalf("progress talker = %v", m["talker"])
			}
			if r, _ := m["rate_per_s"].(float64); r != 20 {
				t.Fatalf("progress rate_per_s = %v, want 20", m["rate_per_s"])
			}
		}
	}
	if progress == 0 {
		t.Fatal("no progress line in a ~1 s ramp at 100 ms period")
	}
	if err := g.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestProgressCarriesRateAndTalker(t *testing.T) {
	l := &RateLimiter{Max: 12, Scale: 5000}
	tl := newJoinTally(100, 0)
	tl.configure(RampMode, "silent", 8, l)
	tl.begin()
	tl.ok(1, time.Millisecond)
	b := mustMarshal(t, tl.progress())
	m := fixtureMap(t, b)
	if m["talker"] != "silent" || m["rate_per_s"] != float64(12) {
		t.Fatalf("progress line %s", b)
	}
	// An unbounded ramp reports 0, and the key is still present.
	tu := newJoinTally(10, 0)
	tu.configure(RampMode, "live", 8, &RateLimiter{})
	m = fixtureMap(t, mustMarshal(t, tu.progress()))
	if v, ok := m["rate_per_s"]; !ok || v != float64(0) || m["talker"] != "live" {
		t.Fatalf("unbounded progress line %v", m)
	}
	// The calibrate phase line is unchanged.
	m = fixtureMap(t, mustMarshal(t, ProgressLine{Type: "progress", Phase: "calibrate_ramp"}))
	if _, ok := m["rate_per_s"]; ok || strings.Contains(string(mustMarshal(t, m)), "talker") {
		t.Fatalf("calibrate_ramp line gained v3 keys: %v", m)
	}
}

func TestNewRunnerRejectsBadRampConfig(t *testing.T) {
	for _, c := range []RunConfig{
		{RampTalker: "loud"}, {JoinRateMax: -1}, {JoinRateScale: -1}, {MaxInFlight: 17},
	} {
		if _, err := NewRunner(c, nil); err == nil {
			t.Errorf("NewRunner(%+v) accepted", c)
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
