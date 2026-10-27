package buildcheck

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoPath returns the absolute path of rel, relative to the repository root
// (two levels above this package).
func repoPath(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("resolve %s: %v", rel, err)
	}
	return abs
}

// readRepoFile returns the contents of a repository file; missing is fatal.
// CRLF line endings are normalised so text assertions hold on any checkout.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(repoPath(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// mustNotExist fails the test if rel exists in the repository.
func mustNotExist(t *testing.T, rel string) {
	t.Helper()
	_, err := os.Stat(repoPath(t, rel))
	if err == nil {
		t.Errorf("%s exists; it must not", rel)
		return
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %s: %v", rel, err)
	}
}

// workflowFiles returns the repository-relative paths of every workflow file.
func workflowFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(repoPath(t, ".github/workflows/*.yml"))
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no workflow files found")
	}
	rels := make([]string, 0, len(matches))
	for _, m := range matches {
		rels = append(rels, ".github/workflows/"+filepath.Base(m))
	}
	return rels
}

func hasLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// CI-04: the module pins a patched Go toolchain, identical to the server.
func TestGoModPinsPatchedToolchain(t *testing.T) {
	mod := readRepoFile(t, "go.mod")
	for _, want := range []string{"go 1.27.0", "toolchain go1.27.3"} {
		if !hasLine(mod, want) {
			t.Errorf("go.mod has no line %q", want)
		}
	}
}

// CI-04: every workflow that installs Go asserts it got go.mod's toolchain,
// through the shared assert script (which compares `go env GOVERSION` with
// go.mod's toolchain directive, pinned above to go1.27.3).
func TestEveryGoWorkflowAssertsToolchain(t *testing.T) {
	for _, wf := range workflowFiles(t) {
		text := readRepoFile(t, wf)
		if !strings.Contains(text, "actions/setup-go") {
			continue
		}
		setups := strings.Count(text, "actions/setup-go")
		asserts := strings.Count(text, "bash .github/scripts/assert-go-toolchain.sh")
		if asserts < setups {
			t.Errorf("%s: %d setup-go steps but only %d assert-go-toolchain.sh calls", wf, setups, asserts)
		}
	}
}

// The portable CI scripts (shared verbatim with the server repo) carry their
// own offline bash tests; run them where bash and jq exist. Windows is skipped
// because `bash` on PATH there may be WSL rather than Git Bash.
func TestCIScripts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CI script tests run on Linux/macOS (and manually in Git Bash)")
	}
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	for _, s := range []string{".github/scripts/assert-go-toolchain_test.sh", ".github/scripts/go-deps-update_test.sh"} {
		cmd := exec.Command("bash", repoPath(t, s))
		cmd.Dir = repoPath(t, ".")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("%s failed: %v\n%s", s, err, out)
		}
	}
}

// taskBlock returns the text of the Taskfile task named key (at two-space
// indent) up to the next task key at the same indent.
func taskBlock(t *testing.T, text, key string) string {
	t.Helper()
	start := strings.Index(text, "\n  "+key+":\n")
	if start < 0 {
		t.Fatalf("task %q not found", key)
	}
	rest := text[start+1:]
	next := regexp.MustCompile(`\n  [A-Za-z0-9:_.-]+:\n`).FindStringIndex(rest[len(key)+3:])
	if next == nil {
		return rest
	}
	return rest[:len(key)+3+next[0]]
}

// CI-05: production builds keep VCS stamping so version.Build() reports the
// git revision; dev builds keep -buildvcs=false.
func TestProductionBuildFlagsKeepVCS(t *testing.T) {
	for _, os := range []string{"windows", "linux", "darwin"} {
		rel := "build/" + os + "/Taskfile.yml"
		var line string
		for _, l := range strings.Split(readRepoFile(t, rel), "\n") {
			if strings.Contains(l, `BUILD_FLAGS: '{{if eq .DEV "true"}}`) {
				line = l
				break
			}
		}
		if line == "" {
			t.Errorf("%s: no BUILD_FLAGS line", rel)
			continue
		}
		dev, prod, ok := strings.Cut(line, "{{else}}")
		if !ok {
			t.Errorf("%s: BUILD_FLAGS has no {{else}} branch", rel)
			continue
		}
		if !strings.Contains(dev, "-buildvcs=false") {
			t.Errorf("%s: dev branch lost -buildvcs=false", rel)
		}
		if strings.Contains(prod, "-buildvcs") {
			t.Errorf("%s: production branch still sets -buildvcs: %s", rel, prod)
		}
		if !strings.Contains(prod, "-trimpath") {
			t.Errorf("%s: production branch lost -trimpath", rel)
		}
	}
}

// CI-12: the Taskfile installs from the lockfile and fails on an untidy
// module in CI instead of rewriting it.
func TestTaskfileAvoidsRepeatedWork(t *testing.T) {
	text := readRepoFile(t, "build/Taskfile.yml")
	npm := taskBlock(t, text, "install:frontend:deps:npm")
	if !strings.Contains(npm, "- npm ci") || strings.Contains(npm, "- npm install") {
		t.Errorf("install:frontend:deps:npm must run `npm ci`, not `npm install`:\n%s", npm)
	}
	tidy := taskBlock(t, text, "go:mod:tidy")
	for _, want := range []string{"run: once", "go mod tidy -diff"} {
		if !strings.Contains(tidy, want) {
			t.Errorf("go:mod:tidy lacks %q:\n%s", want, tidy)
		}
	}
}
