package loadgen

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen/lgtest"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice/testdata"
)

// syncBuf is a bytes.Buffer safe for the Serve goroutine to write while the
// test reads.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitUntil(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A control connection reset in the middle of a run is data, not a crash: the
// hold must not fail, one control_lost line is written, and the next step
// reports the loss. This is the failure the N=1336 bench legs ended in:
// "loadbot: hold: bot N died: ... connection reset by peer".
func TestServeHoldSurvivesControlReset(t *testing.T) {
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
		Unit: "LDBT", Seed: 1, Bounds: DefaultBounds,
	}, InsecureDialer(proxy.Addr()))
	if err != nil {
		t.Fatal(err)
	}

	pr, pw := io.Pipe()
	out := &syncBuf{}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Serve(ctx, pr, out) }()

	feed := func(s string) {
		if _, err := io.WriteString(pw, s+"\n"); err != nil {
			t.Fatalf("feed %s: %v", s, err)
		}
	}
	feed(`{"set_n":4}`)
	feed(`{"hold":1}`)
	feed(`{"report":"a"}`)
	waitUntil(t, "step a", 60*time.Second, func() bool { return strings.Contains(out.String(), `"step":"a"`) })

	if err := proxy.Reset(2); err != nil {
		t.Fatalf("reset: %v (accepted %d)", err, proxy.Count())
	}
	waitUntil(t, "a bot to die", 10*time.Second, func() bool {
		for _, b := range r.bots {
			if b.ctl.Err() != nil {
				return true
			}
		}
		return false
	})

	// Written from a goroutine: if Serve has already failed it stops reading
	// stdin, and a blocked write must not hide that error.
	go func() {
		for _, c := range []string{`{"hold":1}`, `{"report":"b"}`, `{"quit":true}`} {
			if _, err := io.WriteString(pw, c+"\n"); err != nil {
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != nil {
			srv.DumpLog(t)
			t.Fatalf("Serve returned %v, want nil\nstdout:\n%s", err, out.String())
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Serve did not return")
	}
	_ = pw.Close()

	var lost []map[string]any
	var stepB map[string]any
	sc2 := bufio.NewScanner(strings.NewReader(out.String()))
	for sc2.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc2.Bytes(), &m); err != nil {
			t.Fatalf("non-JSON stdout line %q: %v", sc2.Text(), err)
		}
		switch m["type"] {
		case "control_lost":
			lost = append(lost, m)
		case "step":
			if m["step"] == "b" {
				stepB = m
			}
		}
	}
	if len(lost) != 1 {
		t.Fatalf("control_lost lines = %d, want 1\nstdout:\n%s", len(lost), out.String())
	}
	if lost[0]["code"] != "Unavailable" || lost[0]["cause"] != "stream" {
		t.Errorf("control_lost = %v, want code Unavailable cause stream", lost[0])
	}
	cl, _ := stepB["control_lost"].(map[string]any)
	if n, _ := cl["dead_total"].(float64); n != 1 {
		t.Errorf("step b control_lost = %v, want dead_total 1", cl)
	}
}
