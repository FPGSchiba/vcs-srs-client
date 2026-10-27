# CL5 — Client Build, CI & Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Execution context:** this plan is executed later in a **fresh git worktree** of `D:\Projects\vcs-srs-client` on branch **`chore/cleanup-cl5`**, branched from **`origin/main`** (read at `f8ccb43`). All paths below are relative to that worktree root.

**Goal:** Make the client's build/release pipeline produce a revision-stamped, correctly-labelled Go 1.27.3 build packaged as a per-machine NSIS installer, a Linux AppImage and an arm64 macOS `.app` plus `SHA256SUMS`, with least-privilege workflows, no repeated work, no dead Wails template leftovers, and working Go dependency updates.

**Architecture:** Repo-hygiene facts (go.mod, Taskfiles, rendered assets, workflow text) are pinned by a test-only Go package `internal/buildcheck` that reads files relative to the repo root; each change lands test-first. Workflow behaviour is additionally verified by `actionlint`, the PR's Test run, a branch `workflow_dispatch` Release run, and a user-confirmed pre-release tag run.

**Tech Stack:** Go 1.27.3, Task (via `wails3 task`), Wails v3 beta.22 CLI (unchanged), NSIS 3.13.0, linuxdeploy `1-alpha-20251107-1`, GitHub Actions, `gh` CLI.

**Spec:** `docs/superpowers/specs/2026-10-27-phase-6-9-cl5-build-ci-release-design.md` (committed by Task 11; working copy `design-CL5.md`). Umbrella: `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md` §3 CL5, §9.

## Global Constraints

- `go.mod`: `go 1.27.0` and `toolchain go1.27.3` — must be identical to server track SV5.
- Every Go invocation passes `-tags purego` (root `Taskfile.yml` header).
- No new Go or npm module dependency. `actionlint` is run via `go run …@v1.7.12` (tool invocation only).
- Wails CLI pin `v3.0.0-beta.22` lines are **not** touched (track UP).
- MSIX tasks, `wails.json`, `build:server`/`run:server`, darwin `package:universal`/`build:universal`, all `sign*` tasks stay untouched.
- macOS release artefact: **arm64 only**. Windows installer: **per-machine** (`INSTALL_SCOPE` default `machine`).
- Pinned tools: buf `v1.47.2` (existing), linuxdeploy `1-alpha-20251107-1` sha256 `c20cd71e3a4e3b80c3483cef793cda3f4e990aca14014d23c544ca3ce1270b4d`, chocolatey `nsis` `3.13.0`, actionlint `v1.7.12`.
- Client CI server checkout pinned to `c3096ce6acb421d2e530e0238471dacdf1b1128e`.
- Dependency-update secret name: `DEPS_BOT_TOKEN`. **Orchestrator: SV5's plan must use the same name** (SV5's plan was not available when this was written).
- Release names: `vcs-client-<VER>-windows-amd64-setup.exe`, `vcs-client-<VER>-linux-x86_64.AppImage`, `vcs-client-<VER>-macos-arm64.app.zip`, `SHA256SUMS`.
- Releases are created as **drafts**; the workflow never publishes. Draft and tag are left for the user.
- No direct pushes to `main`, no force-push.

## Prerequisites (once, in the worktree, before Task 1)

```bash
go install github.com/bufbuild/buf/cmd/buf@v1.47.2 && buf generate
go run github.com/wailsapp/wails/v3/cmd/wails3 generate bindings -ts -f "-tags purego" -clean=true
cd frontend && npm ci && npm run build && cd ..
```
These produce gitignored `srspb/`, `frontend/bindings/`, `frontend/dist/` so the full suite compiles.

**Full suite** (referred to as "the suite" below):
```bash
go build -tags purego ./...
go vet -tags purego ./...
go test -race -count=1 -tags purego ./...
cd frontend && npx vitest run && npx tsc --noEmit && npm run build
```
Known unrelated flake: `TestTXWriteFrameNeverBlocks` (owned by CL2). If it fails, re-run once and report the output verbatim; do not fix it here.

**Workflow lint** (referred to as "actionlint"): `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` — expected: no output, exit 0.

## Review Focus

1. A tag whose core version differs from `version.Client` (e.g. `v0.2.0` while the code says `0.1.0`) must fail in `preflight` before any build — Task 8's `TestReleaseWorkflowShape` asserts the comparison is present.
2. An untidy `go.mod` in CI must fail the build rather than be silently rewritten — Task 3's `TestTaskfileAvoidsRepeatedWork` asserts `go mod tidy -diff` under `CI`.
3. A missing `DEPS_BOT_TOKEN` must fail `go-deps.yml` with a clear `::error::` instead of opening a PR that no CI runs on — Task 9's `TestGoDepsWorkflowShape`.
4. A build with no VCS info (source tarball, `go run`) must still report `dev`, not crash or show empty — Task 1's `TestBuildFromSettings` nil/no-revision cases.
5. `task build` for macOS/Linux on the wrong host must stop with a clear "cross-compiling with cgo is not supported" message instead of trying a missing docker image — Task 5's `TestNoUnsupportedPackagingTasks`.

---

### Task 1: `internal/buildcheck` scaffold + testable `version.Build`

**Files:**
- Create: `internal/buildcheck/doc.go`, `internal/buildcheck/buildcheck_test.go`
- Create: `internal/version/version_test.go`
- Modify: `internal/version/version.go:14-31`

**Interfaces:**
- Produces (test helpers in `buildcheck_test.go`, package `buildcheck`): `func repoPath(t *testing.T, rel string) string` (abs path of `../../<rel>`), `func readRepoFile(t *testing.T, rel string) string` (fatal if missing), `func mustNotExist(t *testing.T, rel string)`.
- Produces: `func buildFrom(settings []debug.BuildSetting) string` in `internal/version` (unexported); `Build()` becomes `buildFrom(info.Settings)` after the `ReadBuildInfo` check.

- [ ] **Step 1: Write the failing test** `internal/version/version_test.go`:
```go
func TestBuildFromSettings(t *testing.T) {
	full := "0123456789abcdef0123456789abcdef01234567"
	cases := []struct{ name string; in []debug.BuildSetting; want string }{
		{"full sha truncated", []debug.BuildSetting{{Key: "vcs.revision", Value: full}}, "0123456"},
		{"short kept", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}, "abc"},
		{"nil", nil, "dev"},
		{"no revision key", []debug.BuildSetting{{Key: "vcs.time", Value: "2026-10-27T00:00:00Z"}}, "dev"},
	}
	// each: if got := buildFrom(c.in); got != c.want { t.Errorf(...) }
}
```
- [ ] **Step 2: Run it, expect FAIL** — `go test -count=1 -tags purego ./internal/version/` → `undefined: buildFrom`.
- [ ] **Step 3: Extract `buildFrom`** from the existing loop; `Build()` behaviour unchanged.
- [ ] **Step 4: Create `internal/buildcheck/doc.go`** (`// Package buildcheck holds repository-hygiene tests for the build, packaging and CI configuration. It has no runtime code.` + `package buildcheck`) and `buildcheck_test.go` with the three helpers only.
- [ ] **Step 5: Run, expect PASS** — `go test -count=1 -tags purego ./internal/version/ ./internal/buildcheck/` → `ok` for both.
- [ ] **Step 6: Commit** — `git add internal/version internal/buildcheck && git commit -m "test(version): extract buildFrom for unit testing; add buildcheck package"`

---

### Task 2: Go 1.27.3 toolchain (CI-04)

> **GATE — needs user approval before this task:** dependency change (`go 1.25.0` → `go 1.27.0` + `toolchain go1.27.3`) and CI/CD edit (Go-version guard steps added to `test.yml` and `release.yml`).

**Files:**
- Modify: `go.mod:3`, `go.sum`
- Modify: `.github/workflows/test.yml` (after each `Setup Go`, both jobs), `.github/workflows/release.yml` (after `Setup Go`)
- Test: `internal/buildcheck/buildcheck_test.go`

**Interfaces:** Consumes Task 1 helpers.

- [ ] **Step 1: Write failing tests:**
  - `TestGoModPinsPatchedToolchain`: `go.mod` contains a line exactly `go 1.27.0` and a line exactly `toolchain go1.27.3`.
  - `TestEveryGoWorkflowAssertsToolchain`: for every `.github/workflows/*.yml` that contains `actions/setup-go`, the file contains `go1.27.3` and `::error::`.
- [ ] **Step 2: Run, expect FAIL** — `go test -count=1 -tags purego ./internal/buildcheck/ -run 'TestGoModPinsPatchedToolchain|TestEveryGoWorkflowAssertsToolchain'` → both fail (`go 1.25.0`; no `go1.27.3` in workflows).
- [ ] **Step 3: Bump go.mod** — `GOTOOLCHAIN=go1.27.3 go mod edit -go=1.27.0 -toolchain=go1.27.3 && GOTOOLCHAIN=go1.27.3 go mod tidy`. Expected diff: only those two go.mod lines plus possible `go.sum` churn; no `require` version changes (if any require changes, stop and report).
- [ ] **Step 4: Add the guard step** after every `Setup Go` (test.yml backend + frontend jobs, release.yml build job), `shell: bash`:
```yaml
      - name: Assert patched Go toolchain
        shell: bash
        run: |
          go version
          go version | grep -q ' go1.27.3 ' || { echo "::error::expected go1.27.3 from go.mod's toolchain directive, got: $(go version)"; exit 1; }
```
- [ ] **Step 5: Run, expect PASS** — the two tests pass; then the full suite under `go1.27.3` (the toolchain line makes the local `go` switch automatically). Expected: all green (R6: new vet analyzers — if `go vet` reports anything, stop and report it verbatim).
- [ ] **Step 6: actionlint** — no output.
- [ ] **Step 7: Commit** — `git commit -am "build: move to Go 1.27 with patched toolchain go1.27.3 (CI-04)"`

---

### Task 3: Revision-stamped production builds + Taskfile repeat-work fixes (CI-05, CI-12 Taskfile half)

> **GATE — needs user approval before this task:** user-visible change — release builds' Welcome-screen `BUILD` field shows the 7-char git revision instead of `dev`; developer-visible: Taskfile uses `npm ci` and `go mod tidy -diff` under `CI`.

**Files:**
- Modify: `build/windows/Taskfile.yml:74`, `build/linux/Taskfile.yml:62`, `build/darwin/Taskfile.yml:51` (production branch of `BUILD_FLAGS`)
- Modify: `build/Taskfile.yml:4-26` (`go:mod:tidy`, `install:frontend:deps:npm`)
- Test: `internal/buildcheck/buildcheck_test.go`

- [ ] **Step 1: Write failing tests:**
  - `TestProductionBuildFlagsKeepVCS`: for each of the three platform Taskfiles, find the line containing `BUILD_FLAGS: '{{if eq .DEV "true"}}`; `dev, prod, ok := strings.Cut(line, "{{else}}")`; assert `ok`, `strings.Contains(dev, "-buildvcs=false")`, `!strings.Contains(prod, "-buildvcs")`, `strings.Contains(prod, "-trimpath")`.
  - `TestTaskfileAvoidsRepeatedWork`: in `build/Taskfile.yml`, the text between `install:frontend:deps:npm:` and the next top-level task key contains `- npm ci` and not `- npm install`; the `go:mod:tidy:` block contains `run: once` and `go mod tidy -diff`.
- [ ] **Step 2: Run, expect FAIL** (`-run 'TestProductionBuildFlagsKeepVCS|TestTaskfileAvoidsRepeatedWork'`).
- [ ] **Step 3: Edit** — remove ` -buildvcs=false` from the production branch only in all three files. `go:mod:tidy` gets `run: once` and cmd `'{{if .CI}}go mod tidy -diff{{else}}go mod tidy{{end}}'` (Task exposes environment variables as template variables; GitHub Actions sets `CI=true`). Requirement: `CI=true` → `-diff` (fails on an untidy module), unset → plain tidy. `install:frontend:deps:npm` cmd → `npm ci`.
- [ ] **Step 4: Run, expect PASS**; then `CI=true go run github.com/wailsapp/wails/v3/cmd/wails3 task common:go:mod:tidy` → exit 0 with no output (tree is tidy).
- [ ] **Step 5: Commit** — `git commit -am "build: keep vcs.revision in production builds; npm ci and tidy -diff in CI (CI-05, CI-12)"`

---

### Task 4: Rendered Windows metadata + v3 NSIS layout, per-machine (CI-07, CI-08 Windows)

> **GATE — needs user approval before this task:** file deletion (`build/windows/installer/project.nsi`, `build/windows/installer/wails_tools.nsh`) and user-visible change (Windows exe Details and manifest identity `com.fpgschiba.vcsclient` v`0.1.0.0`; installer installs per-machine to `Program Files\FPG Schiba\VCS Client` with UAC).

**Files:**
- Delete: `build/windows/installer/project.nsi`, `build/windows/installer/wails_tools.nsh`
- Create: `build/windows/nsis/project.nsi` (verbatim copy of `$(go env GOMODCACHE)/github.com/wailsapp/wails/v3@v3.0.0-beta.22/internal/commands/build_assets/windows/nsis/project.nsi.tmpl`), `build/windows/nsis/wails_tools.nsh` (rendered)
- Modify: `build/windows/info.json`, `build/windows/wails.exe.manifest` (rendered), `.gitignore`
- Test: `internal/buildcheck/buildcheck_test.go`

- [ ] **Step 1: Write failing tests:**
  - `TestWindowsAssetsRendered`: `build/windows/info.json`, `build/windows/wails.exe.manifest`, `build/windows/nsis/wails_tools.nsh` contain no `{{`; `info.json` unmarshals into `struct{ Info map[string]map[string]string \`json:"info"\` }` with `["0000"]` `ProductName=="VCS Client"`, `CompanyName=="FPG Schiba"`, `ProductVersion=="0.1.0"`; manifest contains `name="com.fpgschiba.vcsclient" version="0.1.0.0"`; `wails_tools.nsh` contains `!define INFO_PROJECTNAME "vcs-client"` and `!define INFO_PRODUCTVERSION "0.1.0"`; `nsis/project.nsi` contains `OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"` and `$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}`.
  - `TestVersionsAgree`: `build/config.yml` `version: "X"` (regex `(?m)^\s+version:\s*"([^"]+)"`) == `version.Client` == `build/darwin/Info.plist` `CFBundleShortVersionString` string == info.json `ProductVersion`; manifest version == `version.Client + ".0"`.
  - `TestInfoPlistKeepsMicrophoneUsage`: `build/darwin/Info.plist` contains `<key>NSMicrophoneUsageDescription</key>` (guard; passes today).
  - `TestPackagingPathsExistWindows`: files exist `build/windows/nsis/project.nsi`, `build/windows/nsis/wails_tools.nsh`, `build/windows/icon.ico`; `build/windows/Taskfile.yml` contains `dir: build/windows/nsis`; `build/windows/installer` does not exist (`mustNotExist`).
- [ ] **Step 2: Run, expect FAIL** (placeholders / missing `nsis/`); `TestInfoPlistKeepsMicrophoneUsage` passes.
- [ ] **Step 3: Render into a scratch dir** (not in-tree — the command would otherwise recreate `build/ios/*`, `build/linux/nfpm/*`, `build/linux/desktop`):
```bash
S=$(mktemp -d)
go run github.com/wailsapp/wails/v3/cmd/wails3 update build-assets -name vcs-client -binaryname vcs-client -config "$PWD/build/config.yml" -dir "$S"
cp "$S/windows/info.json" "$S/windows/wails.exe.manifest" build/windows/
mkdir -p build/windows/nsis && cp "$S/windows/nsis/wails_tools.nsh" build/windows/nsis/
cp "$(go env GOMODCACHE)/github.com/wailsapp/wails/v3@v3.0.0-beta.22/internal/commands/build_assets/windows/nsis/project.nsi.tmpl" build/windows/nsis/project.nsi
git rm build/windows/installer/project.nsi build/windows/installer/wails_tools.nsh
```
  Then hand-edit the manifest `assemblyIdentity` `version="0.1.0"` → `version="0.1.0.0"`. Append to `.gitignore` under "generated": `build/windows/nsis/MicrosoftEdgeWebview2Setup.exe` and `*.syso`.
- [ ] **Step 4: Review the diff** — `git diff --stat` shows only the files listed above; `git status --porcelain` shows nothing under `build/darwin`, `build/linux`, `build/ios`.
- [ ] **Step 5: Run, expect PASS** (the four tests), then `go build -tags purego ./...`.
- [ ] **Step 6: Commit** — `git add -A build/windows .gitignore internal/buildcheck && git commit -m "build(windows): render exe metadata, move to v3 NSIS layout (CI-07, CI-08)"`

---

### Task 5: Delete docker/deb/rpm/AUR packaging; fix Linux AppImage path (CI-08)

> **GATE — needs user approval before this task:** file/task deletion — Taskfile tasks `build:docker`, `run:docker`, `setup:docker` (`build/Taskfile.yml`); `build:docker` + `CROSS_IMAGE` (all three platform Taskfiles); `create:deb`, `create:rpm`, `create:aur`, `generate:deb`, `generate:rpm`, `generate:aur`, `sign:deb`, `sign:rpm`, `sign:packages` (`build/linux/Taskfile.yml`). Developer-visible: cross-builds now stop with a message.

**Files:**
- Modify: `build/Taskfile.yml:240-284`, `build/windows/Taskfile.yml:14-28,82-111`, `build/linux/Taskfile.yml:13-39,70-110,129-166,178,185-224`, `build/darwin/Taskfile.yml:12-26,62-91`
- Modify: `.gitignore` (`build/linux/appimage/`, `build/linux/*.desktop`)
- Modify: `internal/audio/opus/VENDOR.md:299-303`
- Test: `internal/buildcheck/buildcheck_test.go`

- [ ] **Step 1: Write failing tests:**
  - `TestNoUnsupportedPackagingTasks`: the concatenation of `Taskfile.yml` and `build/**/Taskfile.yml` contains none of `build/docker`, `wails-cross`, `CROSS_IMAGE`, `build:docker`, `setup:docker`, `nfpm`, `-format deb`, `-format rpm`, `-format archlinux`, `create:deb`, `sign:packages`; `build/linux/Taskfile.yml` contains `CATEGORIES: 'AudioVideo;Network;'`; each of `build/{darwin,linux}/Taskfile.yml` contains `cross-compiling with cgo is not supported`; `build/windows/Taskfile.yml` contains `cross-compiling with cgo is not supported` (for `CGO_ENABLED=1` on a non-Windows host).
  - `TestPackagingPathsExistLinuxDarwin`: `build/appicon.png`, `build/darwin/Info.plist`, `build/darwin/icons.icns` exist; in `build/linux/Taskfile.yml` the `package:` task's cmds are exactly one `- task: create:appimage` (assert the text between `  package:` and `  create:appimage:` contains `create:appimage` once and no `deps:`).
  - `TestVendorDocHasNoDanglingDockerPath`: `internal/audio/opus/VENDOR.md` does not contain `wails-cross`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Edit the Taskfiles.** Platform `build` dispatchers become:
  - darwin: `cmds: - task: build:native` with `preconditions: - sh: '[ "{{OS}}" = "darwin" ]' msg: "cross-compiling with cgo is not supported; build on macOS"`.
  - linux: same shape, condition `'[ "{{OS}}" = "linux" ] && [ "{{.HAS_CC}}" = "true" ] && [ "{{.TARGET_ARCH}}" = "{{ARCH}}" ]'`, msg `"cross-compiling with cgo is not supported; build on Linux (amd64 host, gcc or clang installed)"`.
  - windows: precondition `'[ "{{OS}}" = "windows" ] || [ "{{.CGO_ENABLED}}" = "0" ]'`, msg `"cross-compiling with cgo is not supported; build on Windows, or pass CGO_ENABLED=0 for a voiceless binary"`; the existing CGO comment block is updated to drop the docker sentence.
  - linux `package`: drop `deps`, cmds `- task: create:appimage` only. `CATEGORIES: 'AudioVideo;Network;'`.
  - Delete the listed tasks and `CROSS_IMAGE` vars. Do not touch `create:msix:package`, `install:msix:tools`, `build:server`, `run:server`, `sign*` (other than the deleted linux ones), `build:universal*`, `package:universal`.
- [ ] **Step 4: VENDOR.md** — replace the sentence "A second, untested Windows path exists: … that path was not exercised either." with: "A Zig-in-Docker cross-compile path (`build:docker` / `wails-cross`) existed in the template Taskfiles and was removed in Phase 6.9 (track CL5); cgo builds are made on the target OS." Leave the later `CGO_ENABLED=0` sentence (DOC owns it).
- [ ] **Step 5: `.gitignore`** — add `build/linux/appimage/` and `build/linux/*.desktop`.
- [ ] **Step 6: Run, expect PASS**, then `go run github.com/wailsapp/wails/v3/cmd/wails3 task --list-all` → exit 0 (Taskfiles parse) and lists no `docker`/`deb`/`rpm`/`aur` task.
- [ ] **Step 7: Commit** — `git commit -am "build: drop docker cross-build and deb/rpm/aur packaging; AppImage-only Linux package (CI-08)"`

---

### Task 6: Delete stale root leftovers (CI-13)

> **GATE — needs user approval before this task:** deletion of 13 tracked files: `linux/Taskfile.yml`, `linux/appimage/build.sh`, `linux/desktop`, `linux/nfpm/nfpm.yaml`, `linux/nfpm/scripts/postinstall.sh`, `linux/nfpm/scripts/postremove.sh`, `linux/nfpm/scripts/preinstall.sh`, `linux/nfpm/scripts/preremove.sh`, `config.yml`, `appicon.icon/icon.json`, `appicon.icon/Assets/wails_icon_vector.svg`, `appicon.png`, `frontend/package.json.md5`.

**Files:** Delete the 13 files above. Test: `internal/buildcheck/buildcheck_test.go`.

- [ ] **Step 1: Write failing test** `TestNoStaleRootLeftovers`: `mustNotExist` for `linux`, `config.yml`, `appicon.icon`, `appicon.png`, `frontend/package.json.md5`; and `build/appicon.png`, `build/config.yml` still exist.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Confirm no references** — `git grep -nE "(^|[^/])(linux/nfpm|appicon\.icon|package\.json\.md5)" -- ':!docs' ':!internal/buildcheck'` and `git grep -n "\./config.yml\|-config config.yml" -- ':!docs'` → only `build/Taskfile.yml`'s `update:build-assets` (`dir: build`, i.e. `build/config.yml`). If anything else matches, stop and report.
- [ ] **Step 4: `git rm -r linux config.yml appicon.icon appicon.png frontend/package.json.md5`.**
- [ ] **Step 5: Run, expect PASS**; then the full suite.
- [ ] **Step 6: Commit** — `git commit -m "chore: remove Wails v2/v3 template leftovers from the repo root (CI-13)"`

---

### Task 7: Test workflow — pin server ref, fix stale comment (CI-03 client half, CI-16)

> **GATE — needs user approval before this task:** CI/CD edit to `.github/workflows/test.yml` (server checkout pinned to `c3096ce6acb421d2e530e0238471dacdf1b1128e`; comment change).

**Files:** Modify `.github/workflows/test.yml:44-49,159`. Test: `internal/buildcheck/buildcheck_test.go`.

- [ ] **Step 1: Write failing test** `TestServerCheckoutPinnedToSHA`: regex `repository: FPGSchiba/vcs-srs-server\s*\n\s*ref: ([0-9a-f]{40})` matches in `test.yml` with group == `c3096ce6acb421d2e530e0238471dacdf1b1128e`; file does not contain `alpha.96`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Edit** — `ref:` → the SHA, with comment above it: "Pinned, not `main`: client CI must not go red because the server moved. Bump this SHA deliberately in the PR that needs newer server behaviour (Phase 6.9 track CL1 bumps it after SV1/SV3)." Line 159 → `# Wails v3 links against GTK4 + WebKitGTK 6.0 on Linux.` (no version).
- [ ] **Step 4: Run, expect PASS; actionlint clean.**
- [ ] **Step 5: Commit** — `git commit -am "ci(test): pin the integration server checkout to a SHA; fix stale Wails comment (CI-03, CI-16)"`

---

### Task 8: Release workflow — package, checksum, draft, least privilege (CI-09, CI-12, CI-14)

> **GATE — needs user approval before this task:** CI/CD edit — full restructure of `.github/workflows/release.yml` (four jobs, `gh` CLI replaces `softprops/action-gh-release`, top-level `contents: read`, pinned linuxdeploy `1-alpha-20251107-1` and chocolatey `nsis 3.13.0`). User-visible: release artefacts change from bare binaries to installer/AppImage/.app.zip + `SHA256SUMS`.

**Files:** Rewrite `.github/workflows/release.yml`. Test: `internal/buildcheck/buildcheck_test.go`.

**Interfaces:** Consumes Task 2's guard step text (copy it), Task 4's NSIS output `bin/vcs-client-amd64-installer.exe`, Task 5's AppImage output `bin/vcs-client-x86_64.AppImage`, darwin `bin/vcs-client.app`.

- [ ] **Step 1: Write failing tests:**
  - `TestWorkflowsLeastPrivilege`: every `.github/workflows/*.yml` matches `(?m)^permissions:\n  contents: read$`; every line matching `^\s*-?\s*uses:\s*(\S+)` whose target does not start with `actions/` or `./` ends in `@` + 40 hex; `release.yml` contains `contents: write` exactly once.
  - `TestReleaseWorkflowShape`: `release.yml` contains `wails3 task package`, `SHA256SUMS`, `gh release create`, `--draft`, `--verify-tag`, `vcs.revision=${GITHUB_SHA}`, `CGO_ENABLED=1`, `GOARCH=arm64`, `1-alpha-20251107-1`, `c20cd71e3a4e3b80c3483cef793cda3f4e990aca14014d23c544ca3ce1270b4d`, `nsis --version=3.13.0`, `ditto -c -k --keepParent`, `${GITHUB_REF_NAME#v}`; does **not** contain `wails3 generate bindings`, `npm run build`, `npm ci`, `softprops`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Write `release.yml`.** Triggers unchanged (`push: tags: ['v*']`, `workflow_dispatch`). Top-level `permissions: contents: read`. Jobs:
  1. **`preflight`** (ubuntu-latest): checkout; read `CFG` = `build/config.yml` `version:` and `CLIENT` = `Client = "…"` from `internal/version/version.go` (grep/sed); fail `::error::` if they differ; if `GITHUB_REF` starts with `refs/tags/v`: `CORE=${GITHUB_REF_NAME#v}; CORE=${CORE%%-*}`, fail if `CORE != CLIENT`, set `ver=${GITHUB_REF_NAME#v}`; else `ver=dispatch-${GITHUB_SHA::7}`. Output `ver`.
  2. **`build`** (needs preflight; same three-row matrix as today: darwin-arm64/macos-latest, linux-amd64/ubuntu-latest, windows-amd64/windows-latest; add `artifact:` per row = `bin/vcs-client.app`, `bin/vcs-client-x86_64.AppImage`, `bin/vcs-client-amd64-installer.exe`; `defaults.run.shell: bash`). Steps: checkout → setup-go (go.mod) → Task 2 guard → setup-node 24 (npm cache) → Linux GUI deps (existing apt line, keep its comment) → **Windows: install NSIS** `choco install -y nsis --version=3.13.0`, `echo "C:\Program Files (x86)\NSIS" >> "$GITHUB_PATH"`, next step `makensis -VERSION` → buf install + `buf generate` (keep comment, drop the bindings sentence) → wails3 install (unchanged pin line) → **Linux: pre-seed linuxdeploy** `mkdir -p build/linux/appimage/build && curl -fsSL --proto '=https' --tlsv1.2 -o build/linux/appimage/build/linuxdeploy-x86_64.AppImage https://github.com/linuxdeploy/linuxdeploy/releases/download/1-alpha-20251107-1/linuxdeploy-x86_64.AppImage && echo "c20cd71e…b4d  build/linux/appimage/build/linuxdeploy-x86_64.AppImage" | sha256sum -c -` with a comment: Wails skips its own `continuous` download when the file exists (beta.22 `internal/commands/appimage.go`); `AppRun`, linuxdeploy's runtime fetch and the WebView2 bootstrapper remain unpinnable floating downloads (design R2) → `wails3 task package` (comment: owns bindings + frontend; `CI=true` makes tidy `-diff`) → existing cgo assertion (keep text) extended: `echo "$settings" | grep -qE "build\s+vcs.revision=${GITHUB_SHA}"` else `::error::…no vcs.revision — BUILD would show dev`; on darwin also `grep -qE "build\s+GOARCH=arm64"` → **collect**: `mkdir dist`; windows `cp bin/vcs-client-amd64-installer.exe "dist/vcs-client-${VER}-windows-amd64-setup.exe"`; linux `cp bin/vcs-client-x86_64.AppImage "dist/vcs-client-${VER}-linux-x86_64.AppImage"`; darwin `ditto -c -k --keepParent bin/vcs-client.app "dist/vcs-client-${VER}-macos-arm64.app.zip"` → `actions/upload-artifact@v7` name `pkg-${{ matrix.platform }}`, path `dist/*`, `if-no-files-found: error`.
  3. **`checksums`** (needs build, ubuntu, no extra permissions): `actions/download-artifact@v8` `pattern: pkg-*`, `merge-multiple: true`, `path: release`; `cd release && sha256sum * > SHA256SUMS && cat SHA256SUMS`; verify exactly 3 package files + SHA256SUMS (`ls | wc -l` == 4, else `::error::`); upload `release-bundle`.
  4. **`release`** (needs [preflight, checksums]; `if: startsWith(github.ref, 'refs/tags/v')`; `permissions: contents: write`): download `release-bundle` to `release/`; write `notes.md` (heredoc) with: "Unsigned pre-release build (code signing is planned for Phase 10).", Windows: SmartScreen → *More info* → *Run anyway*; installs per-machine and asks for administrator rights; macOS (Apple Silicon only): unzip, then `xattr -dr com.apple.quarantine vcs-client.app` before first launch; Linux: `chmod +x` the AppImage; needs glibc ≥ 2.39 (Ubuntu 24.04 or newer) and WebKitGTK 6 runtime; "Verify downloads with `sha256sum -c SHA256SUMS`." Then `PRE=""; [[ "$GITHUB_REF_NAME" == *-* ]] && PRE=--prerelease; gh release create "$GITHUB_REF_NAME" --repo "$GITHUB_REPOSITORY" --draft --verify-tag $PRE --title "VCS Client $GITHUB_REF_NAME" --notes-file notes.md --generate-notes release/*` with `env: GH_TOKEN: ${{ github.token }}`. Comment: the workflow never publishes; publishing the draft is the user's act.
- [ ] **Step 4: Run, expect PASS; actionlint clean;** full `go test -count=1 -tags purego ./internal/buildcheck/`.
- [ ] **Step 5: Commit** — `git commit -am "ci(release): package installer/AppImage/.app, SHA256SUMS, draft via gh, least privilege (CI-09, CI-12, CI-14)"`

---

### Task 9: Go dependency updates via `go-deps.yml` (CI-06 client half)

> **GATE — needs user approval before this task:** CI/CD edit (new `.github/workflows/go-deps.yml`; `gomod` block removed from `.github/dependabot.yml`) and **secret** — the workflow requires a repo secret **`DEPS_BOT_TOKEN`** (fine-grained PAT or GitHub App token with Contents: read/write and Pull requests: read/write on `FPGSchiba/vcs-srs-client`) that the user must create. Name chosen here because SV5's plan was not available; the orchestrator aligned SV5 to it.

> **Orchestrator alignment with SV5 (2026-10-27):** the server's SV5 plan defines portable scripts `.github/scripts/go-deps-update.sh` (+ `_test.sh`) and `.github/scripts/assert-go-toolchain.sh` (+ `_test.sh`). They contain no repo-specific values; the repo-specific values live only in the workflow `env` (`GO_DEPS_EXCLUDE`, `GO_BUILD_TAGS`) and in its generate step.
> - **Copy those scripts verbatim from the merged SV5 branch, or from `plan-SV5.md` if SV5 has not merged yet**, and drive `go-deps.yml` through them instead of inlining equivalent shell.
> - Client values: `GO_BUILD_TAGS=purego`, `GO_DEPS_EXCLUDE` includes `github.com/wailsapp/wails/v3`, and the generate step is `buf generate`.
> - Use the same three-job shape as SV5: a secretless update job, then a PR job that holds the token but runs no Go.
> - Task 2's Go guard step may reuse `assert-go-toolchain.sh` instead of its own check.

**Files:** Create `.github/workflows/go-deps.yml`; modify `.github/dependabot.yml:4-11`. Test: `internal/buildcheck/buildcheck_test.go`.

- [ ] **Step 1: Write failing tests:**
  - `TestDependabotHasNoGomod`: `dependabot.yml` does not contain `gomod`; contains `package-ecosystem: npm` and `package-ecosystem: github-actions`.
  - `TestGoDepsWorkflowShape`: `go-deps.yml` exists and contains `cron:`, `workflow_dispatch:`, `secrets.DEPS_BOT_TOKEN`, `::error::`, `buf generate`, `github.com/wailsapp/wails/v3` (exclusion), `go get`, `go mod tidy`, `-race`, `xvfb-run`, `gh pr create`, `--label go-deps`, `go1.27.3`; and (via `TestWorkflowsLeastPrivilege`) top-level `contents: read`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Write `go-deps.yml`.** `on: schedule: - cron: '0 6 * * 1'` + `workflow_dispatch`. Top-level `permissions: contents: read`; `concurrency: go-deps`. One job on ubuntu-latest, `env: GH_TOKEN: ${{ secrets.DEPS_BOT_TOKEN }}`. Steps, in order:
  1. **Require token:** `[ -n "$GH_TOKEN" ] || { echo "::error::secret DEPS_BOT_TOKEN is not set. Create a fine-grained PAT or GitHub App token (contents + pull-requests write) and add it as a repo secret; a GITHUB_TOKEN-opened PR would not trigger the Test workflow."; exit 1; }`
  2. **Skip if open:** if `gh pr list --repo "$GITHUB_REPOSITORY" --label go-deps --state open --json number --jq length` > 0 → `echo "open go-deps PR exists; skipping"` and set output `skip=true`; later steps `if: steps.open.outputs.skip != 'true'`.
  3. `actions/checkout@v7` with `token: ${{ secrets.DEPS_BOT_TOKEN }}`.
  4. setup-go (go.mod) + Task 2 guard step.
  5. Linux GUI deps + `xvfb` (same apt line as test.yml backend).
  6. buf `v1.47.2` install + `buf generate`; embed placeholder `frontend/dist/index.html` (same as test.yml).
  7. **Update:** comment "direct requirements only, mirroring Dependabot's default; Wails is excluded until Phase 6.9 track UP moves it to GA". `for m in $(go list -m -f '{{if not (or .Indirect .Main)}}{{.Path}}{{end}}' all); do case "$m" in github.com/wailsapp/wails/v3) continue;; esac; go get "$m@latest"; done; go mod tidy`. If `git diff --quiet go.mod go.sum` → `echo "no updates"` and set `changed=false`.
  8. If changed: `go vet -tags purego ./...`, `xvfb-run -a go test -race -count=1 -tags purego ./...`, `go build -tags purego -o ./vcs-client-ci .`.
  9. If changed: `B=deps/go-$(date -u +%F)`; `git switch -c "$B"`; `git -c user.name="vcs-deps-bot" -c user.email="vcs-deps-bot@users.noreply.github.com" commit -am "chore(deps): bump direct Go dependencies"`; `git push origin "$B"` (no force); `gh pr create --base main --head "$B" --label go-deps --title "chore(deps): bump direct Go dependencies ($(date -u +%F))" --body` listing `git diff HEAD~1 -- go.mod` and the run URL `$GITHUB_SERVER_URL/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID`. (If the `go-deps` label does not exist, `gh label create go-deps --force` first.)
- [ ] **Step 4: `dependabot.yml`** — remove the `gomod` entry; add a comment: "Go modules are updated by .github/workflows/go-deps.yml: Dependabot cannot resolve the gitignored generated srspb/ package."
- [ ] **Step 5: Run, expect PASS; actionlint clean.**
- [ ] **Step 6: Commit** — `git add .github && git commit -m "ci: replace failing Dependabot gomod updates with go-deps workflow (CI-06)"`

---

### Task 10: Full verification + branch Release dry run

> **GATE — needs user approval before this task:** outward-facing — pushing branch `chore/cleanup-cl5` and dispatching the Release workflow on it (`gh workflow run release.yml --ref chore/cleanup-cl5`). The dispatch run builds and uploads workflow artefacts only; the `release` job is skipped (no tag), nothing is published.

- [ ] **Step 1: Full suite** (see Prerequisites) — all green; report any failure verbatim.
- [ ] **Step 2: actionlint** — clean. `go test -count=1 -tags purego ./internal/buildcheck/ -v` → every test listed in Tasks 1–9 PASS.
- [ ] **Step 3: Push** — `git push -u origin chore/cleanup-cl5`.
- [ ] **Step 4: Dispatch** — `gh workflow run release.yml --ref chore/cleanup-cl5`, then `gh run watch <id> --exit-status`. Expected: `preflight`, all three `build` legs and `checksums` succeed; `release` skipped. Each build log shows `go version go1.27.3`, `vcs.revision=<branch HEAD sha>`, `CGO_ENABLED=1`; macOS shows `GOARCH=arm64`; Windows shows `makensis` v3.13; Linux log shows no linuxdeploy download (pre-seeded).
- [ ] **Step 5: Inspect** — `gh run download <id> -n release-bundle -D "$SCRATCH/cl5-dryrun"`; in it `sha256sum -c SHA256SUMS` → 3× `OK`; filenames match `vcs-client-dispatch-<sha7>-{windows-amd64-setup.exe,linux-x86_64.AppImage,macos-arm64.app.zip}`; `unzip -l` the `.app.zip` lists `vcs-client.app/Contents/Info.plist` and `Contents/MacOS/vcs-client`. If any leg fails, fix in a new commit on the branch and re-dispatch (no force-push).

---

### Task 11: Commit design + plan, open PR

**Files:**
- Create: `docs/superpowers/specs/2026-10-27-phase-6-9-cl5-build-ci-release-design.md` (from scratchpad `design-CL5.md`)
- Create: `docs/superpowers/plans/2026-10-27-phase-6-9-cl5-build-ci-release.md` (this plan)

- [ ] **Step 1: Copy both documents, commit** — `git add docs/superpowers && git commit -m "docs(phase-6.9): CL5 design and implementation plan"`; push.
- [ ] **Step 2: Request review** — `superpowers:requesting-code-review` on the branch; address findings in new commits.
- [ ] **Step 3: Open PR** — `gh pr create --base main --head chore/cleanup-cl5 --title "Phase 6.9 CL5: client build, CI & release"`; body: findings closed (CI-03 client, CI-04, CI-05, CI-06 client, CI-07, CI-08, CI-09, CI-12, CI-13, CI-14, CI-16); **Behaviour changes** (design §7); **Approval gates exercised** (design §8); link to the dry-run Release run; hand-offs: (a) CL1 bumps the pinned server SHA, (b) SV5 must use secret `DEPS_BOT_TOKEN` and `go1.27.3`, and must not merge its Go bump before this PR, (c) UP: MSIX/`wails.json`/`build:server`/`package:universal` left in place, floating `AppRun`/WebView2 downloads; (d) user must create `DEPS_BOT_TOKEN`. End with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
- [ ] **Step 4: Confirm the PR's Test run is green** (`gh pr checks --watch`); `TestTXWriteFrameNeverBlocks` failing on macOS is the known CL2 flake — report it, do not fix.

#### Post-merge steps (run only after the PR is merged to `main`; orchestrator-driven)

> **GATE — needs user approval at that moment:** push tag `v0.1.0-rc.1` (outward-facing; creates a draft pre-release).

- [ ] **Step 5:** After merge, re-run the suite on `main`. Then ask the user to confirm the tag push; only on explicit confirmation: `git tag -a v0.1.0-rc.1 <merge-commit-sha> -m "VCS Client 0.1.0-rc.1" && git push origin v0.1.0-rc.1`.
- [ ] **Step 6:** `gh run watch` the Release run; expected all four jobs green. `gh release download v0.1.0-rc.1 -D "$SCRATCH/cl5-rc1"` then `sha256sum -c SHA256SUMS` → 3× `OK`; `gh release view v0.1.0-rc.1 --json isDraft,isPrerelease` → `{"isDraft":true,"isPrerelease":true}`. Leave the draft and the tag in place — publishing or deleting them is the user's decision.

> **GATE — needs user approval at that moment:** first `workflow_dispatch` of `go-deps.yml` (may open a PR in the repo; requires `DEPS_BOT_TOKEN` to exist).

- [ ] **Step 7:** Once the user confirms the secret exists: `gh workflow run go-deps.yml --ref main`; expected either "no updates" or a `go-deps` PR whose own Test run starts automatically. If the secret is absent the run must fail with the `DEPS_BOT_TOKEN is not set` error — report that as expected.

---

## Review corrections (2026-10-27)

The independent review found that Task 9's literal `go get "$m@latest"` (step 7) would downgrade `github.com/robotn/gohook` v1.0.0-beta1 → v0.50.0. `.github/scripts/go-deps-update.sh` now uses `@upgrade`, and `go-deps-update_test.sh` has a stub-`go` case (`update-uses-upgrade`) that asserts it. Step 9's branch name is now `deps/go-$(date -u +%F)-${GITHUB_RUN_ID}`. The Linux cross-compile hint now says "a Linux host of the target architecture". These three changes are disclosed and were not in the original task diffs: `build/config.yml` `info.comments`, the `.gitattributes` `eol=lf` rules, and the manifest's `trustInfo` block (Task 4).

## Manual verification items (for the orchestrator to merge into `docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`)

- [ ] **CL5-1 Windows installer:** on Windows 10/11 x64, run `vcs-client-0.1.0-rc.1-windows-amd64-setup.exe`: SmartScreen "More info → Run anyway", UAC prompt appears, installs to `C:\Program Files\FPG Schiba\VCS Client`, Start-menu + desktop shortcuts launch the app; WebView2 bootstrapper runs if WebView2 is missing; uninstall via Settings → Apps removes the folder.
- [ ] **CL5-2 Windows metadata:** installed `vcs-client.exe` → Properties → Details shows Product name "VCS Client", Company "FPG Schiba", version 0.1.0 (no `{{` placeholders).
- [ ] **CL5-3 BUILD field:** the Welcome screen of the installed release build shows the 7-char commit of the tagged merge commit, not `dev` (all three OSes).
- [ ] **CL5-4 Linux AppImage:** on Ubuntu 24.04 (glibc 2.39) and one other current distro, `chmod +x` and run the AppImage; window renders (WebKitGTK 6 bundled via the gtk plugin), audio devices enumerate, global PTT hotkey works. Record whether it starts on an older distro (expected: no, glibc floor).
- [ ] **CL5-5 macOS `.app`:** on Apple Silicon, unzip, confirm the "damaged" Gatekeeper message without the workaround, then `xattr -dr com.apple.quarantine vcs-client.app` → app launches, microphone permission prompt shows the `NSMicrophoneUsageDescription` text.
- [ ] **CL5-6 Checksums:** `sha256sum -c SHA256SUMS` against the downloaded draft-release assets passes on a machine other than CI.
- [ ] **CL5-7 Dependency bot:** after the user creates `DEPS_BOT_TOKEN`, the next weekly `go-deps` run either reports "no updates" or opens a PR whose Test workflow runs automatically.
