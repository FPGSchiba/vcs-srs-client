package buildcheck

import (
	"regexp"
	"strings"
	"testing"
)

// The shared composite action every Linux job installs its GUI deps through.
const linuxGUIDepsAction = ".github/actions/linux-gui-deps/action.yml"

// awalsh128/cache-apt-pkgs-action v1.6.3, pinned by commit.
const cacheAptPkgsPin = "awalsh128/cache-apt-pkgs-action@553a35bb8ebd9fcabcb1c9451aa4c98e1b4ca8a9"

var (
	jobKeyRe     = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):\s*$`)
	jobTimeoutRe = regexp.MustCompile(`(?m)^    timeout-minutes: [1-9][0-9]*\s*$`)
	aptInstallRe = regexp.MustCompile(`\bapt(-get|-fast)?\s+(-[^\s]+\s+)*install\b`)
)

// workflowJobs splits a workflow's `jobs:` map into job name -> job text.
func workflowJobs(t *testing.T, wf, text string) map[string]string {
	t.Helper()
	start := strings.Index(text, "\njobs:\n")
	if start < 0 {
		t.Fatalf("%s: no top-level jobs:", wf)
	}
	body := text[start+len("\njobs:\n"):]
	// The jobs map runs until the next top-level key (if any).
	if end := regexp.MustCompile(`(?m)^[A-Za-z]`).FindStringIndex(body); end != nil {
		body = body[:end[0]]
	}
	locs := jobKeyRe.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		t.Fatalf("%s: jobs: has no jobs", wf)
	}
	jobs := make(map[string]string, len(locs))
	for i, loc := range locs {
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		jobs[body[loc[2]:loc[3]]] = body[loc[0]:end]
	}
	return jobs
}

// Phase 6.9 follow-up: no job may inherit GitHub's 6h default. A hung apt
// mirror (2026-10-27) held runners for hours because nothing bounded a job.
func TestEveryJobHasTimeout(t *testing.T) {
	for _, wf := range workflowFiles(t) {
		for name, job := range workflowJobs(t, wf, readRepoFile(t, wf)) {
			if !jobTimeoutRe.MatchString(job) {
				t.Errorf("%s: job %q has no job-level timeout-minutes", wf, name)
			}
		}
	}
}

// Phase 6.9 follow-up: apt installs go through the cached composite action
// only, so a slow or hanging Ubuntu mirror is off the hot path.
func TestNoRawAptInstallInWorkflows(t *testing.T) {
	for _, wf := range workflowFiles(t) {
		for i, l := range strings.Split(readRepoFile(t, wf), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			if aptInstallRe.MatchString(l) {
				t.Errorf("%s:%d: raw apt install; use ./.github/actions/linux-gui-deps instead: %s", wf, i+1, strings.TrimSpace(l))
			}
		}
	}
}

// Every call of the composite action is bounded by a step timeout (composite
// steps cannot carry timeout-minutes themselves).
func TestLinuxGUIDepsCallsAreBounded(t *testing.T) {
	calls := 0
	for _, wf := range workflowFiles(t) {
		text := readRepoFile(t, wf)
		for _, step := range strings.Split(text, "\n      - ")[1:] {
			if !strings.Contains(step, "uses: ./.github/actions/linux-gui-deps") {
				continue
			}
			calls++
			if !regexp.MustCompile(`(?m)^        timeout-minutes: 10\s*$`).MatchString(step) {
				t.Errorf("%s: linux-gui-deps step has no `timeout-minutes: 10`:\n%s", wf, step)
			}
		}
	}
	// test.yml backend + frontend, release.yml linux build, go-deps.yml update.
	if calls != 4 {
		t.Errorf("found %d linux-gui-deps calls, want 4", calls)
	}
}

// The composite action caches through the SHA-pinned cache-apt-pkgs-action,
// falls back to a retried plain apt-get when the cache step fails, refreshes
// the linker cache after a restore (which skips postinst scripts), and proves
// the pkg-config modules Wails' cgo needs are present either way.
func TestLinuxGUIDepsActionShape(t *testing.T) {
	text := readRepoFile(t, linuxGUIDepsAction)
	for _, m := range usesRe.FindAllStringSubmatch(text, -1) {
		target := strings.Trim(m[1], `"'`)
		if strings.HasPrefix(target, "actions/") || strings.HasPrefix(target, "./") {
			continue
		}
		if !pinnedRe.MatchString(target) {
			t.Errorf("%s: third-party action %s is not pinned to a commit SHA", linuxGUIDepsAction, target)
		}
	}
	for _, want := range []string{
		"uses: " + cacheAptPkgsPin,
		"continue-on-error: true",
		"steps.cache.outcome",
		"Acquire::Retries=3",
		"sudo ldconfig",
		"libgtk-4-dev libwebkitgtk-6.0-dev libsoup-3.0-dev libx11-dev",
		`bash "$GITHUB_ACTION_PATH/verify.sh"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q", linuxGUIDepsAction, want)
		}
	}
	verify := readRepoFile(t, ".github/actions/linux-gui-deps/verify.sh")
	for _, want := range []string{"pkg-config --exists --print-errors gtk4 webkitgtk-6.0 libsoup-3.0 gio-unix-2.0 x11", "Xvfb xvfb-run xauth"} {
		if !strings.Contains(verify, want) {
			t.Errorf("linux-gui-deps/verify.sh lacks %q", want)
		}
	}
	if !regexp.MustCompile(`(?m)^\s+version: linux-gui-deps-v[0-9]+\s*$`).MatchString(text) {
		t.Errorf("%s: cache step has no `version: linux-gui-deps-vN` key", linuxGUIDepsAction)
	}
}
