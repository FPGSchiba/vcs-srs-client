package buildcheck

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/version"
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

// CI-04: every workflow that installs the CLIENT's Go asserts it got go.mod's
// toolchain, through the shared assert script (which compares `go env
// GOVERSION` with go.mod's toolchain directive, pinned above to go1.27.3).
// A setup-go reading the checked-out server's go.mod (test.yml) is not ours
// to assert: that go.mod is pinned by the server repo and, at the pinned SHA,
// has no toolchain directive for the script to compare against.
func TestEveryGoWorkflowAssertsToolchain(t *testing.T) {
	for _, wf := range workflowFiles(t) {
		text := readRepoFile(t, wf)
		if !strings.Contains(text, "actions/setup-go") {
			continue
		}
		setups := strings.Count(text, "actions/setup-go") - strings.Count(text, "go-version-file: .vcs-srs-server/go.mod")
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

type windowsInfo struct {
	Info map[string]map[string]string `json:"info"`
}

func readWindowsInfo(t *testing.T) map[string]string {
	t.Helper()
	var wi windowsInfo
	if err := json.Unmarshal([]byte(readRepoFile(t, "build/windows/info.json")), &wi); err != nil {
		t.Fatalf("parse build/windows/info.json: %v", err)
	}
	return wi.Info["0000"]
}

// CI-07: the Windows metadata files are rendered from build/config.yml, not
// left as Wails template placeholders.
func TestWindowsAssetsRendered(t *testing.T) {
	for _, rel := range []string{"build/windows/info.json", "build/windows/wails.exe.manifest", "build/windows/nsis/wails_tools.nsh"} {
		if strings.Contains(readRepoFile(t, rel), "{{") {
			t.Errorf("%s still contains template placeholders", rel)
		}
	}
	info := readWindowsInfo(t)
	for k, want := range map[string]string{"ProductName": "VCS Client", "CompanyName": "FPG Schiba", "ProductVersion": "0.1.0"} {
		if info[k] != want {
			t.Errorf("info.json %s = %q, want %q", k, info[k], want)
		}
	}
	if c := info["Comments"]; c == "" || c == "This is a comment" {
		t.Errorf("info.json Comments = %q: Wails template default, set info.comments in build/config.yml", c)
	}
	if m := readRepoFile(t, "build/windows/wails.exe.manifest"); !strings.Contains(m, `name="com.fpgschiba.vcsclient" version="0.1.0.0"`) {
		t.Error(`wails.exe.manifest lacks name="com.fpgschiba.vcsclient" version="0.1.0.0"`)
	}
	nsh := readRepoFile(t, "build/windows/nsis/wails_tools.nsh")
	for _, want := range []string{`!define INFO_PROJECTNAME "vcs-client"`, `!define INFO_PRODUCTVERSION "0.1.0"`} {
		if !strings.Contains(nsh, want) {
			t.Errorf("wails_tools.nsh lacks %s", want)
		}
	}
	nsi := readRepoFile(t, "build/windows/nsis/project.nsi")
	for _, want := range []string{`OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"`, `$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}`} {
		if !strings.Contains(nsi, want) {
			t.Errorf("project.nsi lacks %s", want)
		}
	}
}

// CI-07: every place that states the product version agrees.
func TestVersionsAgree(t *testing.T) {
	m := regexp.MustCompile(`(?m)^\s+version:\s*"([^"]+)"`).FindStringSubmatch(readRepoFile(t, "build/config.yml"))
	if m == nil {
		t.Fatal("build/config.yml: no info.version")
	}
	if m[1] != version.Client {
		t.Errorf("build/config.yml version %q != version.Client %q", m[1], version.Client)
	}
	p := regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([^<]+)</string>`).FindStringSubmatch(readRepoFile(t, "build/darwin/Info.plist"))
	if p == nil || p[1] != version.Client {
		t.Errorf("Info.plist CFBundleShortVersionString = %v, want %q", p, version.Client)
	}
	if got := readWindowsInfo(t)["ProductVersion"]; got != version.Client {
		t.Errorf("info.json ProductVersion %q != version.Client %q", got, version.Client)
	}
	if !strings.Contains(readRepoFile(t, "build/windows/wails.exe.manifest"), `version="`+version.Client+`.0"`) {
		t.Errorf("wails.exe.manifest assemblyIdentity version != %s.0", version.Client)
	}
}

// Guard: the macOS bundle must keep its microphone usage string, or macOS
// kills the app on first microphone access.
func TestInfoPlistKeepsMicrophoneUsage(t *testing.T) {
	if !strings.Contains(readRepoFile(t, "build/darwin/Info.plist"), "<key>NSMicrophoneUsageDescription</key>") {
		t.Error("build/darwin/Info.plist lost NSMicrophoneUsageDescription")
	}
}

// CI-08: the NSIS task's working dir holds the v3 installer sources.
func TestPackagingPathsExistWindows(t *testing.T) {
	for _, rel := range []string{"build/windows/nsis/project.nsi", "build/windows/nsis/wails_tools.nsh", "build/windows/icon.ico"} {
		readRepoFile(t, rel)
	}
	if !strings.Contains(readRepoFile(t, "build/windows/Taskfile.yml"), "dir: build/windows/nsis") {
		t.Error("build/windows/Taskfile.yml: NSIS task no longer runs in build/windows/nsis")
	}
	mustNotExist(t, "build/windows/installer")
}

// CI-08: only the three supported packages remain (Windows NSIS, Linux
// AppImage, macOS .app); docker cross-builds and deb/rpm/AUR are gone, and a
// cgo build on the wrong host stops with a clear message.
func TestNoUnsupportedPackagingTasks(t *testing.T) {
	files := []string{"Taskfile.yml", "build/Taskfile.yml", "build/windows/Taskfile.yml", "build/linux/Taskfile.yml", "build/darwin/Taskfile.yml"}
	var all strings.Builder
	for _, f := range files {
		all.WriteString(readRepoFile(t, f))
	}
	text := all.String()
	for _, banned := range []string{"build/docker", "wails-cross", "CROSS_IMAGE", "build:docker", "setup:docker", "nfpm", "-format deb", "-format rpm", "-format archlinux", "create:deb", "sign:packages"} {
		if strings.Contains(text, banned) {
			t.Errorf("Taskfiles still mention %q", banned)
		}
	}
	if !strings.Contains(readRepoFile(t, "build/linux/Taskfile.yml"), "CATEGORIES: 'AudioVideo;Network;'") {
		t.Error("build/linux/Taskfile.yml: .desktop categories are not AudioVideo;Network;")
	}
	for _, os := range []string{"darwin", "linux", "windows"} {
		if !strings.Contains(readRepoFile(t, "build/"+os+"/Taskfile.yml"), "cross-compiling with cgo is not supported") {
			t.Errorf("build/%s/Taskfile.yml: no cross-compile precondition message", os)
		}
	}
	// The Linux precondition compares TARGET_ARCH with the host ARCH, so its
	// hint must not claim an amd64 host is required.
	if lin := readRepoFile(t, "build/linux/Taskfile.yml"); strings.Contains(lin, "amd64 host") ||
		!strings.Contains(lin, "build on a Linux host of the target architecture") {
		t.Error("build/linux/Taskfile.yml: cross-compile hint does not match its TARGET_ARCH == ARCH check")
	}
}

// CI-08/CI-12: the Linux package is the AppImage only, built once.
func TestPackagingPathsExistLinuxDarwin(t *testing.T) {
	for _, rel := range []string{"build/appicon.png", "build/darwin/Info.plist", "build/darwin/icons.icns"} {
		if _, err := os.Stat(repoPath(t, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	pkg := taskBlock(t, readRepoFile(t, "build/linux/Taskfile.yml"), "package")
	if n := strings.Count(pkg, "create:appimage"); n != 1 {
		t.Errorf("linux package task references create:appimage %d times, want 1:\n%s", n, pkg)
	}
	if strings.Contains(pkg, "deps:") {
		t.Errorf("linux package task has deps (create:appimage already builds):\n%s", pkg)
	}
}

func TestVendorDocHasNoDanglingDockerPath(t *testing.T) {
	if strings.Contains(readRepoFile(t, "internal/audio/opus/VENDOR.md"), "wails-cross") {
		t.Error("internal/audio/opus/VENDOR.md still describes the removed wails-cross docker path")
	}
}

// CI-13: Wails v2/v3 template leftovers at the repo root are gone; the real
// build assets under build/ remain.
func TestNoStaleRootLeftovers(t *testing.T) {
	for _, rel := range []string{"linux", "config.yml", "appicon.icon", "appicon.png", "frontend/package.json.md5"} {
		mustNotExist(t, rel)
	}
	for _, rel := range []string{"build/appicon.png", "build/config.yml"} {
		if _, err := os.Stat(repoPath(t, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
}

// CI-03 (client half): the integration server checkout is pinned to a SHA so
// client CI cannot go red because the server's main moved.
func TestServerCheckoutPinnedToSHA(t *testing.T) {
	text := readRepoFile(t, ".github/workflows/test.yml")
	m := regexp.MustCompile(`repository: FPGSchiba/vcs-srs-server\s*\n(?:\s*#[^\n]*\n)*\s*ref: ([0-9a-f]{40})\b`).FindStringSubmatch(text)
	if m == nil {
		t.Fatal("test.yml: vcs-srs-server checkout is not pinned to a 40-hex SHA")
	}
	if want := "850d984e186099d13acd6066f40ceb22ed2f7760"; m[1] != want {
		t.Errorf("server checkout pinned to %s, want %s", m[1], want)
	}
	if strings.Contains(text, "alpha.96") {
		t.Error("test.yml still names Wails alpha.96")
	}
}

// CI-04 / CL5 unblock: the checked-out server is generated and built with the
// SERVER's own Go (its go.mod), and only afterwards does the job switch to the
// client's pinned toolchain. Running the server's gqlgen under the client's
// go1.27.3 with GOTOOLCHAIN=local exits 1 silently, so the order matters.
func TestBackendJobBuildsServerWithServerGo(t *testing.T) {
	text := readRepoFile(t, ".github/workflows/test.yml")
	end := strings.Index(text, "\n  frontend:")
	if end < 0 {
		t.Fatal("test.yml: no frontend job found")
	}
	backend := text[:end]
	pos := func(needle string) int {
		t.Helper()
		i := strings.Index(backend, needle)
		if i < 0 {
			t.Fatalf("backend job has no %q", needle)
		}
		return i
	}
	serverCheckout := pos("repository: FPGSchiba/vcs-srs-server")
	serverGo := pos("go-version-file: .vcs-srs-server/go.mod")
	plugins := pos("name: Install protoc plugins for the VCS server")
	generate := pos("name: Generate the VCS server's protobuf and GraphQL code")
	build := pos("name: Build the headless VCS server")
	clientGo := pos("go-version-file: go.mod")
	assert := pos("bash .github/scripts/assert-go-toolchain.sh")
	buf := pos("name: Install buf")

	if !(serverCheckout < serverGo && serverGo < plugins && plugins < generate && generate < build) {
		t.Error("backend job: setup-go from .vcs-srs-server/go.mod must follow the server checkout and precede the server's plugin install, generate and build")
	}
	if !(build < clientGo && clientGo < assert && assert < buf) {
		t.Error("backend job: setup-go from the client's go.mod and the toolchain assert must follow the server build and precede the client steps")
	}
	if n := strings.Count(backend, "actions/setup-go"); n != 2 {
		t.Errorf("backend job has %d setup-go steps, want 2 (server, then client)", n)
	}

	// The server's setup-go exports GOTOOLCHAIN=local to every later step, and
	// setup-go (src/installer.ts parseGoVersionFile) ignores go.mod's
	// `toolchain` directive whenever GOTOOLCHAIN=local -- it falls back to the
	// `go` directive and installs go1.27.0, not go1.27.3. The client's
	// setup-go must therefore override GOTOOLCHAIN on its own step.
	const clientStep = "name: Setup Go (client's go.mod)"
	start := pos(clientStep)
	step := backend[start+len(clientStep):]
	if next := strings.Index(step, "\n      - "); next >= 0 {
		step = step[:next]
	}
	if !regexp.MustCompile(`(?m)^\s+env:\n\s+GOTOOLCHAIN: auto$`).MatchString(step) {
		t.Error("backend job: the client's setup-go step must set env GOTOOLCHAIN: auto, or setup-go resolves go.mod's go directive (1.27.0) instead of its toolchain directive (1.27.3)")
	}
}

var (
	usesRe   = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*(\S+)`)
	pinnedRe = regexp.MustCompile(`@[0-9a-f]{40}$`)
)

// CI-14: workflows default to read-only tokens, third-party actions are
// SHA-pinned, and only the release job may write contents.
func TestWorkflowsLeastPrivilege(t *testing.T) {
	topRead := regexp.MustCompile(`(?m)^permissions:\n  contents: read$`)
	for _, wf := range workflowFiles(t) {
		text := readRepoFile(t, wf)
		if !topRead.MatchString(text) {
			t.Errorf("%s: no top-level `permissions: contents: read`", wf)
		}
		for _, m := range usesRe.FindAllStringSubmatch(text, -1) {
			target := strings.Trim(m[1], `"'`)
			if strings.HasPrefix(target, "actions/") || strings.HasPrefix(target, "./") {
				continue
			}
			if !pinnedRe.MatchString(target) {
				t.Errorf("%s: third-party action %s is not pinned to a commit SHA", wf, target)
			}
		}
	}
	if n := strings.Count(readRepoFile(t, ".github/workflows/release.yml"), "contents: write"); n != 1 {
		t.Errorf("release.yml has %d `contents: write`, want exactly 1 (the release job)", n)
	}
}

// CI-09/CI-12: the release packages the three supported formats, checksums
// them, drafts a release via gh, checks the tag against the code version, and
// leaves bindings/frontend to the Taskfile.
func TestReleaseWorkflowShape(t *testing.T) {
	text := readRepoFile(t, ".github/workflows/release.yml")
	for _, want := range []string{
		"wails3 task package", "SHA256SUMS", "gh release create", "--draft", "--verify-tag",
		"vcs.revision=${GITHUB_SHA}", "CGO_ENABLED=1", "GOARCH=arm64",
		"1-alpha-20251107-1", "c20cd71e3a4e3b80c3483cef793cda3f4e990aca14014d23c544ca3ce1270b4d",
		"nsis --version=3.13.0", "ditto -c -k --keepParent", "${GITHUB_REF_NAME#v}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("release.yml lacks %q", want)
		}
	}
	for _, banned := range []string{"wails3 generate bindings", "npm run build", "npm ci", "softprops"} {
		if strings.Contains(text, banned) {
			t.Errorf("release.yml still contains %q", banned)
		}
	}
}

// CI-06: Dependabot cannot resolve the gitignored generated srspb/ package,
// so Go modules are updated by go-deps.yml instead.
func TestDependabotHasNoGomod(t *testing.T) {
	text := readRepoFile(t, ".github/dependabot.yml")
	if strings.Contains(text, "gomod") {
		t.Error("dependabot.yml still has a gomod ecosystem")
	}
	for _, want := range []string{"package-ecosystem: npm", "package-ecosystem: github-actions"} {
		if !strings.Contains(strings.ReplaceAll(text, `"`, ""), want) {
			t.Errorf("dependabot.yml lacks %q", want)
		}
	}
}

// CI-06: the weekly Go dependency workflow regenerates code, bumps direct
// requirements (Wails excluded), runs the race suite, and opens a PR with a
// token whose absence fails loudly.
func TestGoDepsWorkflowShape(t *testing.T) {
	text := readRepoFile(t, ".github/workflows/go-deps.yml")
	for _, want := range []string{
		"cron:", "workflow_dispatch:", "secrets.DEPS_BOT_TOKEN", "::error", "buf generate",
		"GO_DEPS_EXCLUDE: github.com/wailsapp/wails/v3", "bash .github/scripts/go-deps-update.sh",
		"bash .github/scripts/assert-go-toolchain.sh", "-race", "xvfb-run", "gh pr create", "--label go-deps",
		// The run id keeps a same-day rerun (or a rerun after a closed,
		// unmerged PR left its branch behind) from colliding on push.
		`branch="deps/go-$(date -u +%F)-${GITHUB_RUN_ID}"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("go-deps.yml lacks %q", want)
		}
	}
	// The job that runs third-party Go code (go get, tests) must not hold the
	// token: secrets may appear only before the `update:` job or after it.
	upd := taskBlock(t, text, "update")
	if strings.Contains(upd, "secrets.") {
		t.Errorf("go-deps.yml: the update job references a secret:\n%s", upd)
	}
}
