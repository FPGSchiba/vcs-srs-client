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

	"github.com/FPGSchiba/vcs-srs-client/internal/voice/testdata"
)

func TestLoadbotOneBigNetAgainstRealServer(t *testing.T) {
	srv := testdata.Start(t)

	sc, err := LoadScenario("one-big-net")
	if err != nil {
		t.Fatal(err)
	}
	if sc.Coalition != testdata.CoalitionName {
		t.Fatalf("scenario coalition %q != fixture %q", sc.Coalition, testdata.CoalitionName)
	}
	voiceAddr := fmt.Sprintf("%s:%d", srv.VoiceHost, srv.VoicePort)
	allow, err := ParseAllow(srv.ControlAddr + "," + voiceAddr)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(RunConfig{
		Arm: "S", Scenario: sc,
		ControlAddr: srv.ControlAddr, VoiceAddr: voiceAddr,
		Allow:     allow,
		Passwords: map[string]string{srv.Coalition: srv.Password},
		Unit:      "LDBT", Seed: 1,
		Bounds: DefaultBounds,
	}, DialInsecure(srv.ControlAddr))
	if err != nil {
		t.Fatal(err)
	}

	in := strings.NewReader(`{"set_n":8}` + "\n" + `{"hold":2}` + "\n" + `{"report":"s1"}` + "\n" + `{"quit":true}` + "\n")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := r.Serve(ctx, in, &out); err != nil {
		srv.DumpLog(t)
		t.Fatalf("Serve: %v\nstdout:\n%s", err, out.String())
	}

	var steps []StepReport
	sc2 := bufio.NewScanner(&out)
	for sc2.Scan() {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(sc2.Bytes(), &probe); err != nil {
			t.Fatalf("non-JSON stdout line %q: %v", sc2.Text(), err)
		}
		if probe.Type != "step" {
			continue
		}
		var s StepReport
		if err := json.Unmarshal(sc2.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		steps = append(steps, s)
	}
	if len(steps) != 1 {
		t.Fatalf("want 1 step report, got %d", len(steps))
	}
	s := steps[0]
	t.Logf("report: %+v", s)
	// Loss-only: latency is never asserted (Windows clock, design 4.1).
	if s.N != 8 || s.Listeners != 7 || s.Talkers != 1 {
		t.Fatalf("N/Listeners/Talkers = %d/%d/%d", s.N, s.Listeners, s.Talkers)
	}
	if s.Expected < 7*90 {
		t.Fatalf("Expected = %d, want >= %d", s.Expected, 7*90)
	}
	if s.LossRatio > 0.01 {
		t.Fatalf("LossRatio = %v", s.LossRatio)
	}
	if s.Unique == 0 || s.Resyncs.Hold != 0 {
		t.Fatalf("Unique=%d Resyncs.Hold=%d", s.Unique, s.Resyncs.Hold)
	}
	if len(r.lastPerListener) != 7 {
		t.Fatalf("per-listener results = %d, want 7", len(r.lastPerListener))
	}
	for i, lr := range r.lastPerListener {
		if lr.Unique == 0 {
			t.Fatalf("listener %d received nothing", i)
		}
	}
	if s.Updates["CLIENT_JOINED"] < 1 {
		t.Fatalf("Updates = %v", s.Updates)
	}
	if s.Payload != "synthetic-not-opus" || s.Label == "" || s.Step != "s1" {
		t.Fatalf("Payload=%q Label=%q Step=%q", s.Payload, s.Label, s.Step)
	}
}

func TestEveryListenerReceives(t *testing.T) {
	// Guard against an aggregate that hides a silent listener.
	srv := testdata.Start(t)
	sc, _ := LoadScenario("one-big-net")
	voiceAddr := fmt.Sprintf("%s:%d", srv.VoiceHost, srv.VoicePort)
	allow, _ := ParseAllow(srv.ControlAddr + "," + voiceAddr)
	r, err := NewRunner(RunConfig{
		Arm: "S", Scenario: sc, ControlAddr: srv.ControlAddr, VoiceAddr: voiceAddr,
		Allow: allow, Passwords: map[string]string{srv.Coalition: srv.Password},
		Unit: "LDBT", Seed: 1, Bounds: DefaultBounds,
	}, DialInsecure(srv.ControlAddr))
	if err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(`{"set_n":4}` + "\n" + `{"hold":1}` + "\n" + `{"report":"x"}` + "\n" + `{"quit":true}` + "\n")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := r.Serve(ctx, in, &out); err != nil {
		t.Fatal(err)
	}
	if len(r.lastPerListener) != 3 {
		t.Fatalf("per-listener results = %d, want 3", len(r.lastPerListener))
	}
	for i, lr := range r.lastPerListener {
		if lr.Unique == 0 {
			t.Fatalf("listener %d received nothing", i)
		}
	}
}
