package buildcheck

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The shared composite action every Linux job installs its GUI deps through.
const linuxGUIDepsAction = ".github/actions/linux-gui-deps/action.yml"

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

// Phase 6.9 follow-up: apt installs go through the shared composite action
// only, so every install is retried and time-capped the same way.
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

// linuxGUIDepsStepMinutes is the timeout-minutes every calling step must set.
// The action's worst case (every attempt at its caps, plus sleeps) must fit
// inside it; see TestLinuxGUIDepsTimingFitsStepTimeout.
const linuxGUIDepsStepMinutes = 15

// Every call of the composite action is bounded by a step timeout (composite
// steps cannot carry timeout-minutes themselves), and every job that calls it
// has its own timeout with at least 10 minutes left after that step.
func TestLinuxGUIDepsCallsAreBounded(t *testing.T) {
	stepTimeout := regexp.MustCompile(`(?m)^        timeout-minutes: ` + strconv.Itoa(linuxGUIDepsStepMinutes) + `\s*$`)
	jobTimeout := regexp.MustCompile(`(?m)^    timeout-minutes: ([0-9]+)\s*$`)
	calls := 0
	for _, wf := range workflowFiles(t) {
		for name, job := range workflowJobs(t, wf, readRepoFile(t, wf)) {
			jobCalls := 0
			for _, step := range strings.Split(job, "\n      - ")[1:] {
				if !strings.Contains(step, "uses: ./.github/actions/linux-gui-deps") {
					continue
				}
				jobCalls++
				if !stepTimeout.MatchString(step) {
					t.Errorf("%s: linux-gui-deps step has no `timeout-minutes: %d`:\n%s", wf, linuxGUIDepsStepMinutes, step)
				}
			}
			if jobCalls == 0 {
				continue
			}
			calls += jobCalls
			m := jobTimeout.FindStringSubmatch(job)
			if m == nil {
				t.Errorf("%s: job %q calls linux-gui-deps but has no job-level timeout-minutes", wf, name)
				continue
			}
			if n, _ := strconv.Atoi(m[1]); n < linuxGUIDepsStepMinutes+10 {
				t.Errorf("%s: job %q timeout-minutes %d leaves under 10 min after its %d-min linux-gui-deps step", wf, name, n, linuxGUIDepsStepMinutes)
			}
		}
	}
	// test.yml backend + frontend, release.yml linux build, go-deps.yml update.
	if calls != 4 {
		t.Errorf("found %d linux-gui-deps calls, want 4", calls)
	}
}

// actionInt reads a `name=N` shell assignment from the composite action.
func actionInt(t *testing.T, text, name string) int {
	t.Helper()
	m := regexp.MustCompile(`(?m)^\s*` + name + `=([0-9]+)\s*$`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("%s: no `%s=<n>` assignment", linuxGUIDepsAction, name)
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// Test run 40546134379 (2026-10-27): 3 x (120 s update + 240 s install) plus
// sleeps was ~18 min against a 10-min step timeout, so attempt 3 could never
// finish. The worst case must now fit, with a minute to spare for the cache
// restore/save and verify.sh.
func TestLinuxGUIDepsTimingFitsStepTimeout(t *testing.T) {
	text := readRepoFile(t, linuxGUIDepsAction)
	attempts := actionInt(t, text, "attempts")
	updateCap := actionInt(t, text, "update_cap")
	installCap := actionInt(t, text, "install_cap")
	retrySleep := actionInt(t, text, "retry_sleep")
	for _, want := range []string{
		`timeout "$update_cap" apt-get`,
		`timeout "$install_cap" apt-get`,
		`for attempt in $(seq 1 "$attempts")`,
		`sleep "$retry_sleep"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q (the caps must be the parsed variables)", linuxGUIDepsAction, want)
		}
	}
	if attempts < 2 {
		t.Errorf("attempts=%d, want at least 2", attempts)
	}
	// Sleeps happen only between attempts.
	worst := attempts*(updateCap+installCap) + (attempts-1)*retrySleep
	if budget := linuxGUIDepsStepMinutes*60 - 60; worst > budget {
		t.Errorf("worst case %d s = %d x (%d + %d) + %d x %d s sleep exceeds %d s (the %d-min step timeout less 60 s)",
			worst, attempts, updateCap, installCap, attempts-1, retrySleep, budget, linuxGUIDepsStepMinutes)
	}
}

// The composite action installs with a retried, per-attempt time-capped plain
// apt-get and proves the pkg-config modules Wails' cgo needs are present.
// Downloaded .debs are cached with first-party actions/cache only:
// awalsh128/cache-apt-pkgs-action was tried and removed (2026-10-27) because
// on GitHub runners it never refreshes the package lists, so a stale image
// list made it save an empty cache and every "hit" restored 0 packages. Here
// apt-get update still runs every time and apt hash-verifies every cached
// .deb against the fresh index, so a stale or partial cache cannot pass.
func TestLinuxGUIDepsActionShape(t *testing.T) {
	text := readRepoFile(t, linuxGUIDepsAction)
	var uses []string
	for _, m := range usesRe.FindAllStringSubmatch(text, -1) {
		uses = append(uses, strings.Trim(m[1], `"'`))
	}
	wantUses := []string{"actions/cache/restore@v6", "actions/cache/save@v6"}
	if strings.Join(uses, " ") != strings.Join(wantUses, " ") {
		t.Errorf("%s uses %v, want exactly %v (first-party cache only, restore before save)", linuxGUIDepsAction, uses, wantUses)
	}
	for _, unwanted := range []string{"cache-apt-pkgs-action", "continue-on-error", "/var/cache/apt/archives"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("%s still contains %q", linuxGUIDepsAction, unwanted)
		}
	}
	for _, want := range []string{
		"Acquire::Retries=3",
		// apt-get update runs on every attempt, cache hit or not.
		`apt-get "${apt_opts[@]}" update`,
		// A user-owned archive dir the cache tar can read, used by install
		// and autoclean alike.
		`$HOME/.cache/apt-archives`,
		`-o "Dir::Cache::Archives=$DEBS_DIR/"`,
		"APT::Keep-Downloaded-Packages=true",
		`"${dir_opts[@]}" install -y`,
		`"${dir_opts[@]}" autoclean`,
		`sudo chown -R "$USER"`,
		`rm -rf "$DEBS_DIR"/partial/* "$DEBS_DIR/lock"`,
		// Key: OS, arch, runner image and the package list; restore falls
		// back to the same OS/arch/image family.
		`apt-debs-${RUNNER_OS}-${RUNNER_ARCH}-${ImageOS}-`,
		`${ImageVersion}-${pkg_hash}`,
		"restore-keys: ${{ steps.plan.outputs.restore-prefix }}",
		// Save only when the exact key missed.
		"if: steps.restore.outputs.cache-hit != 'true'",
		"libgtk-4-dev libwebkitgtk-6.0-dev libsoup-3.0-dev libx11-dev",
		`bash "$GITHUB_ACTION_PATH/verify.sh"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q", linuxGUIDepsAction, want)
		}
	}
	// verify.sh stays the final gate.
	if strings.LastIndex(text, "verify.sh") < strings.Index(text, "actions/cache/save") {
		t.Errorf("%s: verify.sh must be the last step, after the cache save", linuxGUIDepsAction)
	}
	verify := readRepoFile(t, ".github/actions/linux-gui-deps/verify.sh")
	for _, want := range []string{"pkg-config --exists --print-errors gtk4 webkitgtk-6.0 libsoup-3.0 gio-unix-2.0 x11", "Xvfb xvfb-run xauth"} {
		if !strings.Contains(verify, want) {
			t.Errorf("linux-gui-deps/verify.sh lacks %q", want)
		}
	}
}
