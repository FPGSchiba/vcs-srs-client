package loadgen

import (
	"strings"
	"testing"
	"time"
)

func TestParseBurstCommand(t *testing.T) {
	c, err := ParseCommand([]byte(`{"burst":{"add":150,"in_flight":16,"talker":"live"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Burst == nil || c.Burst.Add != 150 || c.Burst.InFlight != 16 || c.Burst.Talker != "live" {
		t.Fatalf("burst = %+v", c.Burst)
	}
	if _, err := ParseCommand([]byte(`{"burst":{"add":1,"in_flight":1,"talker":"silent"}}`)); err != nil {
		t.Fatal(err)
	}
	for name, line := range map[string]string{
		"add 0":         `{"burst":{"add":0,"in_flight":4,"talker":"live"}}`,
		"in_flight 0":   `{"burst":{"add":5,"in_flight":0,"talker":"live"}}`,
		"in_flight 17":  `{"burst":{"add":5,"in_flight":17,"talker":"live"}}`,
		"talker x":      `{"burst":{"add":5,"in_flight":4,"talker":"x"}}`,
		"talker absent": `{"burst":{"add":5,"in_flight":4}}`,
		"unknown inner": `{"burst":{"add":5,"in_flight":4,"talker":"live","rate":3}}`,
		"with set_n":    `{"burst":{"add":5,"in_flight":4,"talker":"live"},"set_n":3}`,
	} {
		if _, err := ParseCommand([]byte(line)); err == nil {
			t.Errorf("%s: accepted %s", name, line)
		}
	}
}

// sampleBurst polls nextSeq while the runner is in the burst phase.
func sampleBurst(g *serveRig, t *testing.T, until func() bool) (first, last uint32, n int) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && !until() {
		if Phase(g.r.phase.Load()) == PhaseBurst {
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

func TestServeBurstWritesBurstJoins(t *testing.T) {
	g := newServeRigCfg(t, 0, nil)
	g.feed(t, `{"set_n":4}`, `{"hold":1}`, `{"burst":{"add":20,"in_flight":1,"talker":"live"}}`)
	first, last, n := sampleBurst(g, t, func() bool { return len(joinsLines(g, t)) >= 2 })
	js := joinsLines(g, t)
	if len(js) != 2 {
		t.Fatalf("want 2 joins lines, got %d", len(js))
	}
	j := js[1]
	if j["mode"] != "burst" || j["requested"] != float64(24) || j["had"] != float64(4) ||
		j["joined"] != float64(20) || j["talker"] != "live" || j["rate_limit"] != nil || j["in_flight_max"] != float64(1) {
		t.Fatalf("burst joins = %v", j)
	}
	if n == 0 || last <= first {
		t.Fatalf("live talker did not send during the burst: %d -> %d over %d samples", first, last, n)
	}
	if err := g.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestServeBurstConcurrentInFlight(t *testing.T) {
	g := newServeRigCfg(t, 0, nil)
	g.feed(t, `{"set_n":4}`, `{"hold":1}`, `{"burst":{"add":6,"in_flight":6,"talker":"live"}}`)
	sampleBurst(g, t, func() bool { return len(joinsLines(g, t)) >= 2 })
	js := joinsLines(g, t)
	if len(js) != 2 {
		t.Fatalf("want 2 joins lines, got %d", len(js))
	}
	j := js[1]
	if j["mode"] != "burst" || j["requested"] != float64(10) || j["had"] != float64(4) ||
		j["joined"] != float64(6) || j["join_failures"] != float64(0) ||
		j["in_flight_max"] != float64(6) || j["rate_limit"] != nil || j["talker"] != "live" {
		t.Fatalf("burst joins = %v", j)
	}
	if err := g.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestServeBurstSilent(t *testing.T) {
	g := newServeRigCfg(t, 0, nil)
	g.feed(t, `{"set_n":3}`, `{"burst":{"add":20,"in_flight":1,"talker":"silent"}}`)
	first, last, n := sampleBurst(g, t, func() bool { return len(joinsLines(g, t)) >= 2 })
	if n == 0 {
		t.Fatal("never observed the burst")
	}
	if first != last {
		t.Fatalf("talker sent during a silent burst: %d -> %d", first, last)
	}
	if js := joinsLines(g, t); js[1]["talker"] != "silent" || js[1]["mode"] != "burst" {
		t.Fatalf("joins = %v", js[1])
	}
	waitUntil(t, "talker resumed", 5*time.Second, func() bool { return g.r.nextSeq.Load() > last })
	if err := g.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestServeBurstWithFailuresIsData(t *testing.T) {
	// Arrival index 5 is one of the burst's sources (set_n 3 uses 0..2).
	r := newJoinFailRunner(t, func(i int) bool { return i == 5 })
	lines, err := serveLines(t, r,
		`{"set_n":3}`+"\n"+`{"burst":{"add":4,"in_flight":2,"talker":"live"}}`+"\n"+`{"quit":true}`+"\n", 90*time.Second)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	js := ofType(lines, "joins")
	if len(js) != 2 {
		t.Fatalf("want 2 joins lines, got %d", len(js))
	}
	j := js[1]
	if j["mode"] != "burst" || j["join_failures"] != float64(1) || j["joined"] != float64(3) {
		t.Fatalf("burst joins = %v", j)
	}
	if r.joinFailTotal.Load() != 1 {
		t.Fatalf("joinFailTotal = %d", r.joinFailTotal.Load())
	}
}

func TestServeBurstBeforeAnyBotIsFatal(t *testing.T) {
	r := newJoinFailRunner(t, func(int) bool { return false })
	lines, err := serveLines(t, r, `{"burst":{"add":3,"in_flight":2,"talker":"live"}}`+"\n", 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), "burst") {
		t.Fatalf("err = %v, want a burst error", err)
	}
	if len(ofType(lines, "error")) != 1 || len(ofType(lines, "joins")) != 0 {
		t.Fatalf("lines = %v", lines)
	}
}

func TestBurstProgressLinePhase(t *testing.T) {
	tl := newJoinTally(10, 4)
	tl.configure(BurstMode, "live", 4, nil)
	m := fixtureMap(t, mustMarshal(t, tl.progress()))
	if m["phase"] != "burst" || m["talker"] != "live" || m["rate_per_s"] != float64(0) {
		t.Fatalf("progress = %v", m)
	}
}
