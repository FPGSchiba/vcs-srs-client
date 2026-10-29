package loadgen

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen/lgtest"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice/testdata"
)

type serveRig struct {
	r     *Runner
	proxy *lgtest.ResetProxy
	out   *syncBuf
	pw    *io.PipeWriter
	done  chan error
	srv   *testdata.Server
}

func newServeRig(t *testing.T, ping time.Duration) *serveRig {
	t.Helper()
	srv := testdata.Start(t)
	sc, err := LoadScenario("one-big-net")
	if err != nil {
		t.Fatal(err)
	}
	proxy := lgtest.TCPResetProxy(t, srv.ControlAddr)
	voiceAddr := fmt.Sprintf("%s:%d", srv.VoiceHost, srv.VoicePort)
	allow, err := ParseAllow(proxy.Addr() + "," + voiceAddr)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(RunConfig{
		Arm: "S", Scenario: sc, ControlAddr: proxy.Addr(), VoiceAddr: voiceAddr,
		Allow: allow, Passwords: map[string]string{srv.Coalition: srv.Password},
		Unit: "LDBT", Seed: 1, Bounds: DefaultBounds, PingEvery: ping,
	}, InsecureDialer(proxy.Addr()))
	if err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	g := &serveRig{r: r, proxy: proxy, out: &syncBuf{}, pw: pw, done: make(chan error, 1), srv: srv}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(func() { cancel(); _ = pw.Close() })
	go func() { g.done <- r.Serve(ctx, pr, g.out) }()
	return g
}

func (g *serveRig) feed(t *testing.T, cmds ...string) {
	t.Helper()
	go func() {
		for _, c := range cmds {
			if _, err := io.WriteString(g.pw, c+"\n"); err != nil {
				return
			}
		}
	}()
}

func (g *serveRig) finish(t *testing.T) error {
	t.Helper()
	g.feed(t, `{"quit":true}`)
	select {
	case err := <-g.done:
		return err
	case <-time.After(60 * time.Second):
		t.Fatal("Serve did not return")
	}
	return nil
}

func (g *serveRig) lines(t *testing.T) []map[string]any {
	t.Helper()
	var ms []map[string]any
	sc := bufio.NewScanner(strings.NewReader(g.out.String()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("non-JSON line %q: %v", sc.Text(), err)
		}
		ms = append(ms, m)
	}
	return ms
}

func (g *serveRig) waitDead(t *testing.T) {
	t.Helper()
	waitUntil(t, "a bot to die", 10*time.Second, func() bool {
		g.r.dmu.Lock()
		defer g.r.dmu.Unlock()
		return g.r.deadTotal > 0
	})
}

func TestServeControlLostLineShape(t *testing.T) {
	g := newServeRig(t, 0)
	g.feed(t, `{"set_n":4}`, `{"hold":1}`, `{"report":"a"}`)
	waitUntil(t, "step a", 60*time.Second, func() bool { return strings.Contains(g.out.String(), `"step":"a"`) })
	if err := g.proxy.Reset(2); err != nil {
		t.Fatal(err)
	}
	g.waitDead(t)
	if err := g.finish(t); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	raw, err := os.ReadFile("testdata/interface/v3-control-lost.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	for _, m := range g.lines(t) {
		if m["type"] == "control_lost" {
			got = m
		}
	}
	if got == nil {
		t.Fatalf("no control_lost line:\n%s", g.out.String())
	}
	for k := range fixture {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	for k := range got {
		if _, ok := fixture[k]; !ok {
			t.Errorf("unexpected key %q", k)
		}
	}
	if got["phase"] != "settle" {
		t.Errorf("phase = %v, want settle (reset between commands)", got["phase"])
	}
	known := false
	for _, a := range g.proxy.RemoteAddrs() {
		if a == got["local_addr"] {
			known = true
		}
	}
	if !known {
		t.Errorf("local_addr %v not among proxy remotes %v", got["local_addr"], g.proxy.RemoteAddrs())
	}
}

func TestServeControlLostCappedAt64(t *testing.T) {
	r := &Runner{}
	buf := &syncBuf{}
	r.out = &lineWriter{w: buf}
	r.phase.Store(int32(PhaseHold))
	for i := 0; i < 70; i++ {
		r.onDead(nil, DeathRecord{Name: fmt.Sprintf("LDBT-%04d", i), Cause: CauseStream, Code: "Unavailable"})
	}
	if n := strings.Count(buf.String(), "\n"); n != 64 {
		t.Fatalf("control_lost lines = %d, want 64", n)
	}
	if r.deadTotal != 70 || len(r.deadFirst) != 8 {
		t.Errorf("deadTotal %d first %d, want 70 and 8", r.deadTotal, len(r.deadFirst))
	}
}

func TestStepReportControlLostAndPing(t *testing.T) {
	g := newServeRig(t, 0)
	g.feed(t, `{"set_n":3}`, `{"hold":1}`, `{"report":"a"}`)
	waitUntil(t, "step a", 60*time.Second, func() bool { return strings.Contains(g.out.String(), `"step":"a"`) })
	if err := g.proxy.Reset(2); err != nil {
		t.Fatal(err)
	}
	g.waitDead(t)
	g.feed(t, `{"hold":1}`, `{"report":"b"}`)
	waitUntil(t, "step b", 60*time.Second, func() bool { return strings.Contains(g.out.String(), `"step":"b"`) })
	if err := g.finish(t); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	steps := map[string]map[string]any{}
	for _, m := range g.lines(t) {
		if m["type"] == "step" {
			steps[m["step"].(string)] = m
		}
	}
	a := steps["a"]["control_lost"].(map[string]any)
	b := steps["b"]["control_lost"].(map[string]any)
	if a["dead_total"].(float64) != 0 || a["alive"].(float64) != 3 {
		t.Errorf("step a control_lost = %v", a)
	}
	if b["dead_total"].(float64) != 1 || b["alive"].(float64) != 2 || b["dead_since_last_step"].(float64) != 1 {
		t.Errorf("step b control_lost = %v", b)
	}
	if b["dead_in_hold"].(float64) != 0 {
		t.Errorf("death before the hold must not count as dead_in_hold: %v", b)
	}
	if n := len(b["first"].([]any)); n != 1 {
		t.Errorf("first has %d entries, want 1", n)
	}
	ping := steps["b"]["ping"].(map[string]any)
	if ping["every_s"].(float64) != 0 || ping["sent"].(float64) != 0 || ping["failed"].(float64) != 0 || ping["rtt_p99_us"].(float64) != 0 {
		t.Errorf("ping block with --ping-every 0 = %v, want zeros", ping)
	}
	if w, ok := steps["b"]["warnings"].([]any); !ok || len(w) != 0 {
		t.Errorf("warnings = %v, want []", steps["b"]["warnings"])
	}
}

// The talker's control connection is not the voice plane: losing it is a
// control_lost line and the hold still completes.
func TestServeTalkerControlLossIsNotFatal(t *testing.T) {
	g := newServeRig(t, 0)
	g.feed(t, `{"set_n":3}`)
	waitUntil(t, "joins", 60*time.Second, func() bool { return strings.Contains(g.out.String(), `"type":"joins"`) })
	// Connection 0 is bot 0, the talker (it joins alone first).
	if err := g.proxy.Reset(0); err != nil {
		t.Fatal(err)
	}
	g.waitDead(t)
	g.feed(t, `{"hold":1}`, `{"report":"a"}`)
	waitUntil(t, "step a", 60*time.Second, func() bool { return strings.Contains(g.out.String(), `"step":"a"`) })
	if err := g.finish(t); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var lost int
	for _, m := range g.lines(t) {
		if m["type"] == "control_lost" {
			lost++
			if m["bot"].(float64) != 0 {
				t.Errorf("control_lost bot = %v, want 0", m["bot"])
			}
		}
	}
	if lost != 1 {
		t.Errorf("control_lost lines = %d, want 1", lost)
	}
}

func TestServeFatalErrorCarriesContext(t *testing.T) {
	r := &Runner{}
	r.started = time.Now().Add(-3 * time.Second)
	r.lastCmd = `{"set_n":2}`
	r.deadTotal = 1
	r.errCtx = r.snapshotContext()
	got := r.ErrContext()
	for _, want := range []string{`last command {"set_n":2}`, "bots 0, dead 1", "uptime 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("ErrContext %q missing %q", got, want)
		}
	}
}
