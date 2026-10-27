# CL5 — Client build, CI & release (design)

**Status: draft for approval**

**Track:** CL5 of Phase 6.9 · **Repo:** client (`vcs-srs-client`) · **Branch:** `chore/cleanup-cl5` (from `origin/main`) · **Wave:** 1
**Read against:** client `origin/main` @ `f8ccb43`, server `origin/main` @ `c3096ce6acb421d2e530e0238471dacdf1b1128e` (#223 merged).
**Umbrella:** `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md` §3 CL5, §9.

## Decisions

| Decision | Value | By |
|---|---|---|
| Go version | `go 1.27.0` + `toolchain go1.27.3` — identical to SV5 | user (1.27), user (1.27.3 = SV5's patch) |
| Supported packages | Windows NSIS installer, Linux AppImage, macOS `.app`; deb/rpm/AUR/docker deleted; no signing (Phase 10) | user (umbrella §9) |
| macOS architecture (was Q1) | **Apple Silicon only** — `darwin-arm64` `.app`; `package:universal` is not used by the release and is left untouched | user decision |
| Windows install scope (was Q2) | **per-machine** — Program Files, UAC prompt (the template default, `INSTALL_SCOPE=machine`) | user decision |
| CI-06 mechanism | Same as SV5: weekly + `workflow_dispatch` workflow `.github/workflows/go-deps.yml` that regenerates code (`buf generate`), bumps **direct** Go deps (Wails excluded until track UP), tidies, runs build + race tests, opens a PR with a PAT/GitHub App token secret, failing clearly if the secret is absent. Secret name **`DEPS_BOT_TOKEN`** (SV5's plan was not available to read; the orchestrator must make SV5 use the same name) | user decision |
| Ordering vs SV5 | CL5 merges **before** SV5's Go bump; CL5 pins client CI's server checkout to `c3096ce…` (CL1 bumps it later) | user decision |
| Out of scope | MSIX tasks, root `wails.json`, `build:server`/`run:server` stay untouched (note for track UP) | user decision |
| Tag push | `v0.1.0-rc.1` push is a GATE, confirmed by the user at that moment | user decision |
| Rendering of Windows assets | Render via `wails3 update build-assets` into a **scratch directory** and copy only the three Windows files in (avoids the command recreating `build/ios/*`, `build/linux/nfpm/*`, `build/linux/desktop` and re-merging plists in-tree) | worker's call (refines pass 1's "run in-tree, revert") |

## 1. Intent

Make the client's build and release pipeline produce what it claims: a revision-stamped, correctly-labelled binary built with a patched Go 1.27, packaged as the three decided artefacts (Windows NSIS installer, Linux AppImage, macOS arm64 `.app`) plus `SHA256SUMS`, from a release workflow that has least privilege, does each step once, and has run end-to-end at least once. Delete the Wails v2/v3 template leftovers that make the build tree lie about what it supports.

## 2. Findings — re-verification (origin/main)

| ID | Status | Evidence |
|---|---|---|
| CI-03 (client half) | holds | `.github/workflows/test.yml:44-49` checks out `FPGSchiba/vcs-srs-server` at `ref: main`. |
| CI-04 | holds | `go.mod:3` = `go 1.25.0`, no `toolchain` line. Release run 36226799338 log: `go version go1.25.0 linux/amd64`. Go 1.27.3 is the newest 1.27 patch. `GOTOOLCHAIN=go1.27.3 go vet -tags purego ./...` is green on the current tree (language version still 1.25; re-run after the directive change). |
| CI-05 | holds | `internal/version/version.go:17-31` reads `vcs.revision`, falls back to `"dev"`. Production `BUILD_FLAGS` carry `-buildvcs=false` at `build/windows/Taskfile.yml:74`, `build/linux/Taskfile.yml:62`, `build/darwin/Taskfile.yml:51`. Consumer: `internal/app/bindings.go:16`. |
| CI-06 (client half) | holds | Every weekly Go Dependabot run fails (40363413519, 39289305154, 38466086030, 37344219139, 36454314444): `dependency_file_not_resolvable … imports …/srspb: cannot find module providing package`. npm and github-actions updates succeed. |
| CI-07 | holds-with-correction | `build/windows/info.json:3,7-12` and `wails.exe.manifest:3` are unrendered v2-era placeholders (`{{.Info.X}}`); **`build/windows/installer/wails_tools.nsh:8-20` is unrendered too**. v3 templates use `.ProductVersion` / `.ProductIdentifier`; the v3 manifest template emits `version="{{.ProductVersion}}"` (3-part) and must be hand-fixed to 4-part. |
| CI-08 | holds-with-correction | NSIS: `build/windows/Taskfile.yml:133,141` uses `dir: build/windows/nsis`, files live in `build/windows/installer/`; v2 `project.nsi:74` `OutFile "..\..\bin\…"` would land in `build/bin` from `nsis/`. Linux deb/rpm/aur point at missing `./build/linux/nfpm/nfpm.yaml`. `create:appimage` is viable (`wails3 generate appimage` is Go code in the CLI). Docker tasks reference missing `build/docker/*` and the `wails-cross` image. macOS `create:app:bundle` works. Nothing in root `linux/` needs salvaging. |
| CI-09 | holds | `release.yml:90-91,112-117` uploads bare `bin/<binary>`. Two Release runs ever, both `workflow_dispatch`; no tags, no releases. |
| CI-12 | holds | Release log 36226799338: `go mod tidy` twice, `npm install` after the workflow's `npm ci`, bindings + `npm run build` both in the workflow (`release.yml:77-86`) and in `wails3 task build`. Additionally, `build/linux/Taskfile.yml` `package` deps `build` and its `create:appimage` deps `build` again (no `run: once`). |
| CI-13 | holds | Root `linux/` (8 files), `config.yml`, `appicon.icon/`, `appicon.png` (byte-identical to `build/appicon.png`), `frontend/package.json.md5`. No references outside the leftovers. |
| CI-14 | holds | `release.yml:8-9` top-level `contents: write`; `:147` `softprops/action-gh-release@v3` (tag-pinned third party). |
| CI-16 | holds | `test.yml:159` says `Wails v3 (alpha.96)`; the pin is beta.22. |

New facts found in pass 2:
- `windows-latest` is Windows Server 2025 (image 20261021); its readme lists **no NSIS**, so `makensis` must be installed: `choco install nsis --version=3.13.0` (newest on chocolatey), following test.yml's choco-pinned-version precedent for protoc.
- linuxdeploy's newest tagged release is `1-alpha-20251107-1`, asset `linuxdeploy-x86_64.AppImage`, GitHub-recorded digest `sha256:c20cd71e3a4e3b80c3483cef793cda3f4e990aca14014d23c544ca3ce1270b4d`.
- `wails3 generate appimage` writes `bin/vcs-client-x86_64.AppImage`; the v3 NSIS template writes `bin/vcs-client-amd64-installer.exe`.

## 3. Per-finding approach

### CI-04 — Go 1.27 (dependency gate)
`go.mod`: `go 1.27.0` + `toolchain go1.27.3` (`go mod edit -go=1.27.0 -toolchain=go1.27.3`, then `go mod tidy` under `GOTOOLCHAIN=go1.27.3`). Workflows already use `go-version-file: go.mod`. Every Go job in `test.yml`, `release.yml`, `go-deps.yml` gets a step that fails unless `go version` prints `go1.27.3` (same guard as SV5).

Cross-repo ordering: client CI builds the server checkout with the client's Go. Because CL5 pins that checkout to `c3096ce…` (Go 1.25 server) and moves the client to 1.27.3, SV5's later server bump cannot break client CI; CL1 bumps the SHA.

### CI-05 — real BUILD revision
Drop `-buildvcs=false` from the **production** branch of `BUILD_FLAGS` in all three platform Taskfiles; dev builds keep it. `version.go` gets a pure helper `buildFrom([]debug.BuildSetting) string`, unit-tested; behaviour unchanged. The release build job asserts `go version -m bin/<binary>` contains `vcs.revision=$GITHUB_SHA`. `vcs.modified` is not asserted (icon regeneration can touch tracked files).

### CI-07 — rendered exe metadata
Render with `wails3 update build-assets -name vcs-client -binaryname vcs-client -config build/config.yml -dir <scratch>` and copy only `windows/info.json`, `windows/wails.exe.manifest`, `windows/nsis/wails_tools.nsh` into `build/windows/`. Hand-fix the manifest `assemblyIdentity version` to `0.1.0.0`. `build/darwin/Info.plist` is not touched.

### CI-08 — packaging for the three formats; others deleted
- **Windows NSIS (per-machine):** replace `build/windows/installer/` with `build/windows/nsis/` = v3 `project.nsi` template (`OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"`, per-machine `$PROGRAMFILES64\FPG Schiba\VCS Client`) + rendered `wails_tools.nsh`. `INSTALL_SCOPE` default stays `machine`. `.gitignore` gets `build/windows/nsis/MicrosoftEdgeWebview2Setup.exe`.
- **Linux AppImage:** `package` → `task: create:appimage` only (which itself deps `build`, removing the double build). Delete `create:deb/rpm/aur`, `generate:deb/rpm/aur`, `sign:deb/rpm/packages`. `.desktop` `Categories` → `AudioVideo;Network;`. The release workflow pre-seeds the pinned linuxdeploy at `build/linux/appimage/build/linuxdeploy-x86_64.AppImage` with a sha256 check (Wails skips its own download when the file exists). `.gitignore` gets `build/linux/appimage/` and `build/linux/*.desktop`.
- **macOS `.app` (arm64 only):** `create:app:bundle` unchanged (ad-hoc codesign). Release runs `wails3 task package` on `macos-latest` (arm64) and asserts `GOARCH=arm64` in the binary; zips with `ditto -c -k --keepParent`.
- **Docker / cross-build deleted:** `build:docker`, `run:docker`, `setup:docker` (`build/Taskfile.yml`); `build:docker` + `CROSS_IMAGE` in each platform Taskfile. Each platform `build` dispatcher loses its docker branch and gets a `preconditions` failure: "cross-compiling with cgo is not supported; build on <OS>". Windows on a non-Windows host with `CGO_ENABLED=0` still takes `build:native` (documented voiceless escape hatch). `*.syso` added to `.gitignore`. `internal/audio/opus/VENDOR.md:299-303` sentence about `build:docker`/Zig rewritten to say the path was removed in Phase 6.9.
- **Left alone:** MSIX tasks + `wails.json`, `build:server`/`run:server`, `sign*` tasks, `package:universal`/`build:universal` (darwin). Noted for UP.

### CI-09 — release publishes three artefacts + SHA256SUMS
`release.yml` restructured into four jobs:
1. `preflight` (ubuntu, `contents: read`): `build/config.yml` version == `version.Client`; on a tag run, the tag's core (`v0.1.0-rc.1` → `0.1.0`) must equal it too. Computes `VER` (tag without `v`, or `dispatch-<sha7>`).
2. `build` matrix (needs preflight): darwin-arm64 / linux-amd64 / windows-amd64 → `wails3 task package` → cgo + `vcs.revision` (+ arm64 on macOS) assertions → collect to `dist/` as `vcs-client-<VER>-windows-amd64-setup.exe`, `vcs-client-<VER>-linux-x86_64.AppImage`, `vcs-client-<VER>-macos-arm64.app.zip` → upload.
3. `checksums` (ubuntu, `contents: read`, always): download + merge, `sha256sum * > SHA256SUMS`, upload `release-bundle`.
4. `release` (ubuntu, `contents: write`, tag only): download bundle, `gh release create "$GITHUB_REF_NAME" --draft --verify-tag [--prerelease if '-' in tag] --notes-file notes.md --generate-notes release/*`. Notes state the build is unsigned with SmartScreen / Gatekeeper (`xattr -dr com.apple.quarantine`) / AppImage (`chmod +x`, glibc ≥ 2.39) instructions.

The draft is never published by the workflow; publishing or deleting it is the user's act.

### CI-12 — no repeated work
`release.yml` drops its own bindings / `npm ci` / `npm run build` steps (the Taskfile owns them; it keeps `buf generate`). `install:frontend:deps:npm` runs `npm ci`. `go:mod:tidy` gets `run: once` and runs `go mod tidy -diff` when `CI` is set, else `go mod tidy`.

### CI-13 — delete leftovers
See §5.

### CI-14 — least privilege
`release.yml` top-level `permissions: contents: read`; only `release` job has `contents: write`. `gh` CLI replaces the third-party action. A unit test asserts every `uses:` outside `actions/*` is pinned to a 40-hex SHA (none remain today).

### CI-16
Comment becomes "Wails v3 links against GTK4 + WebKitGTK 6.0…" (no version).

### CI-03 (client half)
`test.yml` server checkout: `ref: c3096ce6acb421d2e530e0238471dacdf1b1128e` with a comment that it is bumped deliberately in the PR that needs newer server behaviour. **Hand-off:** CL1's plan bumps it.

### CI-06 (client half) — `go-deps.yml`
Weekly (Monday) + `workflow_dispatch`. Job `permissions: contents: read` (all writes go through the PAT). Steps: fail with `::error::` if `secrets.DEPS_BOT_TOKEN` empty → skip if an open PR labelled `go-deps` exists → checkout with the token → setup-go (go.mod) + `go1.27.3` guard → apt GUI deps + xvfb → `buf` v1.47.2 + `buf generate` → embed placeholder `frontend/dist/index.html` → for each direct, non-excluded requirement (`go list -m -f '{{if not (or .Indirect .Main)}}{{.Path}}{{end}}' all`, excluding `github.com/wailsapp/wails/v3`) `go get <mod>@latest` → `go mod tidy` → exit 0 with "no updates" if `go.mod`/`go.sum` unchanged → `go vet`/`xvfb-run -a go test -race -count=1 -tags purego ./...`/`go build -tags purego` → commit to `deps/go-YYYY-MM-DD`, push, `gh pr create --label go-deps`. `dependabot.yml` loses its `gomod` block (npm + actions stay).

## 4. Tests first (TDD)

New package `internal/buildcheck` (`doc.go` + `buildcheck_test.go`; repo root = `../..`; regex/line parsing only, no new dependency). Each test is seen failing on origin/main before its fix:

| Test | Fails today because | Finding |
|---|---|---|
| `TestGoModPinsPatchedToolchain` | `go 1.25.0`, no toolchain | CI-04 |
| `TestProductionBuildFlagsKeepVCS` | all 3 carry `-buildvcs=false` | CI-05 |
| `TestWindowsAssetsRendered` | placeholders; `nsis/` missing | CI-07 |
| `TestVersionsAgree` | info.json placeholder | CI-07 |
| `TestInfoPlistKeepsMicrophoneUsage` | passes (guard) | CI-07 |
| `TestPackagingPathsExist` | `nsis/project.nsi` missing | CI-08 |
| `TestNoUnsupportedPackagingTasks` | docker/nfpm tasks present | CI-08 |
| `TestNoStaleRootLeftovers` | files present | CI-13 |
| `TestServerCheckoutPinnedToSHA` | `ref: main` | CI-03 |
| `TestWorkflowsLeastPrivilege` | release top-level `contents: write`; softprops action | CI-14 |
| `TestDependabotHasNoGomod` / `TestGoDepsWorkflowShape` | gomod present; no go-deps.yml | CI-06 |
| `internal/version` `TestBuildFromSettings` | `buildFrom` undefined | CI-05 |

Workflow behaviour beyond these text assertions is verified by pinned `actionlint` v1.7.12 (`go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12`, tool invocation, not a module dependency), the PR's Test run, a branch `workflow_dispatch` Release run, and the user-confirmed tag run.

## 5. Files

**Deleted (tracked):** `linux/Taskfile.yml`, `linux/appimage/build.sh`, `linux/desktop`, `linux/nfpm/nfpm.yaml`, `linux/nfpm/scripts/{postinstall,postremove,preinstall,preremove}.sh`, `config.yml`, `appicon.icon/icon.json`, `appicon.icon/Assets/wails_icon_vector.svg`, `appicon.png`, `frontend/package.json.md5`, `build/windows/installer/project.nsi`, `build/windows/installer/wails_tools.nsh` (15 files).

**Tasks deleted:** `build:docker`, `run:docker`, `setup:docker` (common); `build:docker` + `CROSS_IMAGE` (×3); `create:deb/rpm/aur`, `generate:deb/rpm/aur`, `sign:deb`, `sign:rpm`, `sign:packages` (linux).

**Modified:** `.github/workflows/{release,test}.yml`, `.github/dependabot.yml`, `go.mod`/`go.sum`, `build/Taskfile.yml`, `build/{windows,linux,darwin}/Taskfile.yml`, `build/windows/{info.json,wails.exe.manifest}`, `.gitignore`, `internal/version/version.go`, `internal/audio/opus/VENDOR.md`.

**Added:** `build/windows/nsis/{project.nsi,wails_tools.nsh}`, `internal/buildcheck/{doc.go,buildcheck_test.go}`, `internal/version/version_test.go`, `.github/workflows/go-deps.yml`.

## 6. Out of scope
- Wails CLI pin / beta→GA (UP, CI-15). `wails3@v3.0.0-beta.22` lines untouched.
- Signing / notarisation, `sign*` tasks (Phase 10).
- MSIX tasks, `wails.json`, `build:server`/`run:server`, darwin `package:universal` — **note for UP** (probably dead; deletion not approved).
- README / CLAUDE.md / ROADMAP (DOC). VENDOR.md's other stale sentence ("`build/windows/Taskfile.yml` defaults `CGO_ENABLED=0`", line ~307) → DOC.
- `TestTXWriteFrameNeverBlocks` flake (assigned to CL2); CL5's PR may hit it on macOS.

## 7. Behaviour changes
- Welcome `BUILD` shows the 7-char git revision in release builds.
- Windows exe Details: "VCS Client", "FPG Schiba", 0.1.0; manifest identity `com.fpgschiba.vcsclient` v`0.1.0.0`.
- Releases ship a per-machine NSIS installer (UAC), an AppImage, a zipped unsigned arm64 `.app`, and `SHA256SUMS`, as a **draft**; pre-release for `-` tags.
- Developers: docker cross-builds gone (clear precondition message); `task linux:package` → AppImage only; Taskfile uses `npm ci`; CI fails on untidy `go.mod`.
- Go dependency updates arrive as weekly `go-deps` PRs (once `DEPS_BOT_TOKEN` exists) instead of failing Dependabot runs.

## 8. Approval gates
- **dependency:** `go 1.27.0` + `toolchain go1.27.3`.
- **ci-cd:** `release.yml` restructure; `test.yml` server pin + Go guard + comment; `dependabot.yml` gomod removal; new `go-deps.yml`; pinned linuxdeploy `1-alpha-20251107-1` + choco NSIS `3.13.0`.
- **file deletion:** the 15 tracked files and listed tasks.
- **secret:** `DEPS_BOT_TOKEN` (user creates a fine-grained PAT or GitHub App token with contents + pull-requests write on this repo).
- **outward-facing:** branch `workflow_dispatch` Release run (builds only, publishes nothing); push tag `v0.1.0-rc.1` (user confirms at that moment); first `go-deps` dispatch opens a PR.
- **user-visible:** BUILD field, exe metadata, release artefacts.

## 9. Risks
- **R1 AppImage + WebKitGTK 6:** fragile; glibc 2.39 floor (ubuntu-24.04). Field verification only.
- **R2 floating downloads Wails can't pin:** `AppRun` (AppImageKit `continuous`), linuxdeploy's own appimage runtime fetch, and the WebView2 bootstrapper (Microsoft). Documented exceptions; re-check in UP.
- **R3 NSIS install on the Windows runner:** chocolatey's `nsis` may not put `makensis` on PATH; the workflow appends `C:\Program Files (x86)\NSIS` to `GITHUB_PATH` and prints `makensis -VERSION`.
- **R4 unsigned arm64 `.app`:** "damaged" until quarantine removed; documented in notes.
- **R5 asset rendering:** mitigated by rendering into a scratch dir.
- **R6 Go 1.27 vet analyzers:** suite re-run after the bump.
- **R7 pinned server SHA goes stale:** CL1 bumps it.
- **R8 tarball builds:** fall back to `dev`, acceptable.
- **R9 `DEPS_BOT_TOKEN` absent:** `go-deps.yml` fails loudly every week until the user creates it.
