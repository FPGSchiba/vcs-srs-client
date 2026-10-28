package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/loadgen"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice/testdata"
)

func TestRunD2ArmIsRefused(t *testing.T) {
	var out, errw bytes.Buffer
	code := run(context.Background(), []string{"run", "--arm", "D2", "--allow", "127.0.0.1:1"},
		strings.NewReader(""), &out, &errw)
	if code != 2 || !strings.Contains(errw.String(), "D2 arrives in B-1 (W6 C2)") {
		t.Fatalf("exit=%d stderr=%q", code, errw.String())
	}
}

func TestRunRequiresControlAndPasswords(t *testing.T) {
	var out, errw bytes.Buffer
	code := run(context.Background(), []string{"run", "--allow", "127.0.0.1:1", "--scenario", "one-big-net"},
		strings.NewReader(""), &out, &errw)
	if code != 2 || !strings.Contains(errw.String(), "--control") {
		t.Fatalf("exit=%d stderr=%q", code, errw.String())
	}
}

func TestRunSubcommandEndToEnd(t *testing.T) {
	srv := testdata.Start(t)
	voiceAddr := fmt.Sprintf("%s:%d", srv.VoiceHost, srv.VoicePort)
	pw := filepath.Join(t.TempDir(), "pw.json")
	b, _ := json.Marshal(map[string]string{srv.Coalition: srv.Password})
	if err := os.WriteFile(pw, b, 0o600); err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(`{"set_n":4}` + "\n" + `{"hold":1}` + "\n" + `{"report":"e2e"}` + "\n" + `{"quit":true}` + "\n")
	var out, errw bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	code := run(ctx, []string{
		"run", "--scenario", "one-big-net", "--arm", "S",
		"--control", srv.ControlAddr, "--voice", voiceAddr,
		"--allow", srv.ControlAddr + "," + voiceAddr,
		"--passwords", pw, "--seed", "1",
	}, in, &out, &errw)
	if code != 0 {
		t.Fatalf("exit = %d; stderr=%q stdout=%q", code, errw.String(), out.String())
	}
	var steps int
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var s loadgen.StepReport
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Fatalf("stdout line %q: %v", line, err)
		}
		if s.Type == "step" {
			steps++
			if s.Step != "e2e" || s.Listeners != 3 || s.Unique == 0 {
				t.Fatalf("step = %+v", s)
			}
		}
	}
	if steps != 1 {
		t.Fatalf("step lines = %d, want 1; stdout=%q", steps, out.String())
	}
}
