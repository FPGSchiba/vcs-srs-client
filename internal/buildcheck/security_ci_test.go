package buildcheck

import (
	"strings"
	"testing"
)

// govulncheckRun is the one command the backend job runs. Pinned to an exact
// version; -tags purego matches every other Go invocation in CI.
const govulncheckRun = "go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -tags purego ./..."

// Phase 6.9 SEC D5: govulncheck analyses only the host GOOS, so it must run on
// every leg of the backend matrix, after the packages can load.
func TestGovulncheckGatesEveryBackendLeg(t *testing.T) {
	const wf = ".github/workflows/test.yml"
	text := readRepoFile(t, wf)
	job := workflowJobs(t, wf, text)["backend"]
	if job == "" {
		t.Fatal("test.yml has no backend job")
	}
	if n := strings.Count(job, govulncheckRun); n != 1 {
		t.Errorf("backend job runs govulncheck %d times, want exactly 1: %q", n, govulncheckRun)
	}
	if !hasLine(job, "os: [ubuntu-latest, macos-latest, windows-latest]") {
		t.Error("backend job lost its three-OS matrix; govulncheck analyses only the host GOOS (D5), so all three legs must run it")
	}
	const marker = "- name: govulncheck"
	start := strings.Index(job, marker)
	if start < 0 {
		t.Fatal("backend job has no `- name: govulncheck` step")
	}
	block := job[start+len(marker):]
	if end := strings.Index(block, "\n      - "); end >= 0 {
		block = block[:end]
	}
	for _, l := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "if:") {
			t.Errorf("govulncheck step has a condition (%q); it must run on all three legs (D5)", strings.TrimSpace(l))
		}
	}
	// A step- or job-level continue-on-error would make the gate non-blocking.
	if strings.Contains(job, "continue-on-error") {
		t.Error("backend job uses continue-on-error; govulncheck must be a blocking gate (D5)")
	}
	idx := func(s string) int {
		i := strings.Index(job, s)
		if i < 0 {
			t.Fatalf("backend job has no %q", s)
		}
		return i
	}
	gv := idx("name: govulncheck")
	if !(idx("name: Provide embed placeholder for frontend/dist") < gv && gv < idx("name: Go vet")) {
		t.Error("govulncheck must run after the frontend/dist placeholder and before Go vet (D5): packages must load first")
	}
	if !(idx("name: Generate proto bindings") < gv) {
		t.Error("govulncheck must run after proto generation (D5): srspb must exist to load packages")
	}
}

// Phase 6.9 SEC D-b: no lifecycle scripts from npm packages in CI.
func TestTestWorkflowNpmCiIgnoresScripts(t *testing.T) {
	const wf = ".github/workflows/test.yml"
	job := workflowJobs(t, wf, readRepoFile(t, wf))["frontend"]
	if !hasLine(job, "run: npm ci --ignore-scripts") {
		t.Error("frontend job must run `npm ci --ignore-scripts` (D-b)")
	}
	for _, f := range workflowFiles(t) {
		for i, l := range strings.Split(readRepoFile(t, f), "\n") {
			if strings.TrimSpace(l) == "run: npm ci" {
				t.Errorf("%s:%d: bare `npm ci`; use `npm ci --ignore-scripts` (D-b)", f, i+1)
			}
		}
	}
}
