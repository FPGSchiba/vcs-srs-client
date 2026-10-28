**Status: approved 2026-10-28**

# Phase 6.9 / SEC: GitHub security alerts (client + server)

**Repos:** `vcs-srs-client` (`origin/main` `b80b0e6`) and `vcs-srs-server` (`origin/main` `df8d9f3`). Suggested branches: `chore/cleanup-sec` in each, cut from `origin/main`.
**Umbrella:** `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md:269` (track SEC), decisions at §9 (`:405`). Gates: **dependency, ci-cd, server behaviour, cross-repo (ordering only)**.
**Evidence basis:** three research passes. (1) A dependency trial: real `go get` + tidy + codegen + build/vet/race tests on the server, govulncheck before/after under both tag sets, and the client's real-server integration suite run against the bumped server binary. (2) A triage pass over SonarCloud (both projects), the server esbuild alert, a server-side bcrypt cost cap, and the Windows reserved-port flake. (3) A review pass (see *Review notes* at the end): the client frontend job re-run end to end with `--ignore-scripts` (incl. `npm run build`), a cold-cache govulncheck timing on the server, the x/net advisories' GitHub status, and the C1–C17 contract located and read. Citations were re-checked against `git show origin/main:…` for this design. gopls was not used; every compile claim rests on `go build` / `go vet`.

**Parallel with SV7: yes.** No file overlap: SEC touches server `go.mod`/`go.sum`, `frontend/package*.json`, `.github/workflows/{test,release}.yml`, `srs/auth_service{,_test}.go`, `utils/general{,_test}.go`, `build/linux/appimage/build.sh`, `dev/config/control.yaml` (comment only), optionally `Dockerfile`. SV7 touches only `README.md`. The one content link: if Q2 takes a cap, SV7's *Client connections* section should state it as a client requirement, so SV7 needs Q2 answered (or writes against the recommended value, 12) before its README lands. The umbrella's "SEC lands first, then SV7" (`umbrella:347`) becomes: design both now, SEC server PR merges first or SV7 cites the decided value.

## Decisions (D1–D10 proposed; the user decides Q1–Q6 in §9)

Two items are **proposed additions** to the umbrella's SEC scope (`umbrella:300-315`), not settled decisions: the bcrypt cost cap (Q2, a server behaviour change) and the Windows port flake (Q5). The umbrella routed earlier flakes to other tracks (§9: `TestTXWriteFrameNeverBlocks` → CL2, `umbrella:422`; `TestTLS_UntrustedCertificateIsRefused` → CL1, `umbrella:431`), so both are put to the user.

| # | Decision | Source |
|---|---|---|
| D1 | **Server** `google.golang.org/grpc` v1.77.0 → **v1.85.0** and `github.com/quic-go/quic-go` v0.54.0 → **v0.64.1**, plus the forced moves in §6.1. Needs re-approval: the approved floor `≥ v1.83.1` is insufficient (Q1). | §4.1 |
| D2 | **Client: no Go bump.** PR #46 already moved it to grpc v1.85.0 / x/net v0.60.0 / protobuf v1.36.12; govulncheck reports 0 reachable. | §4.2 |
| D3 | `go mod tidy` runs **after** codegen on the server (3× protoc + gqlgen), as in UP D7. | UP D7 |
| D4 | **x/net v0.62.0 is a question (Q6), not a decision.** Its two findings (GO-2026-6866/6860, `x/net/html`) are unreachable in both repos, but the umbrella's exemption (`umbrella:316`) is conjunctive: an alert is skipped only if govulncheck shows it unreachable **and** UP clears it. UP did not clear these. v0.62.0 is inside the approved range (`x/net ≥ v0.60.0`, `umbrella:441`), so taking it needs no new approval. Both advisories already have GitHub entries (GHSA-vwcj-r8v9-p53f, GHSA-75pg-wvh3-34jr, `unreviewed`); once reviewed, Dependabot opens alerts in both repos. Recommended: take it (Q6 = A). | §4.5 |
| D5 | **govulncheck CI step** in both Test workflows, blocking, `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0`. **Client:** `-tags purego ./...` in the `backend` job on **all three matrix legs** (`test.yml:20-21`; govulncheck analyses only the host GOOS, so one leg would silently skip the darwin- and windows-tagged code), placed **after the `frontend/dist` placeholder** (`test.yml:253-256`), which comes after `buf generate`. **Server:** `-tags headless ./...` in the `test` job after the generate action, on its single `ubuntu-latest` runner; no timeout change (measured, §4.5). The server's Windows-only code and the GUI tag set are never analysed in CI; the GUI set is checked locally in the PR, the Windows gap is recorded (R13). Note: through `go run` a finding exits **1** (`go run` prints `exit status 3` and exits 1), still a failure. | SEC scope 5; §4.5 |
| D6 | **Proposed (Q2): bcrypt cost cap on the server**, enforced in `GuestLogin` before the coalition loop and before `settingsState.RLock`, plus defence in depth in `utils.CheckPasswordHash`. Value, response shape, and whether SEC takes it at all are Q2. **Server behaviour change.** The client's own hashing (`internal/session/session.go:744`, `bcrypt.DefaultCost`) is untouched, per CLAUDE.md. | §3 #B1 |
| D7 | **Proposed (Q5): Windows reserved-port flake**, test-only, client `internal/voice/testdata/server_fixture.go`: voice port probed with a UDP bind, plus a bounded retry on bind errors. Its tests live in `internal/voice`, not in `testdata` (see C1). Not cross-repo. | §3 #C1 |
| D8 | Sonar fixes in code: S4036 (client), S6506 (delete the unused server `build.sh`), S6505 (`--ignore-scripts` in both Test workflows). S2068: a comment in the file plus "mark safe" by the user. | §3 |
| D9 | Sonar items that only the user can act on (mark safe, `design/**` exclusion, secret scanning) are recorded in §8, not done by the track. | `umbrella:312-314` |
| D10 | Two PRs, **server first**, then client. The client PR's last commit bumps its pinned server SHA (`.github/workflows/test.yml:49`, today `850d984`) to the merged server SEC commit. That moves client CI across **every** server commit from `850d984` to the SEC merge, including UP's server changes (`df8d9f3`), not just SEC; the pin comment (`test.yml:43-48`) is rewritten to describe that range. §4.2 already tested the client against `df8d9f3` plus the SEC bumps. | §5.3 |

## 1. Intent and scope

Bring both repos to **0 reachable govulncheck findings** and close every open Dependabot alert, triage the SonarCloud vulnerability/hotspot list, and keep it that way with a govulncheck CI gate. Two items are **proposed** to join the umbrella's list: a **server-side cap on the bcrypt cost** of client-supplied hashes (a cheap remote CPU-exhaustion vector found during triage; Q2, which also offers deferring it) and the **Windows reserved-port flake** in the client's real-server test fixture (Q5, which also offers routing it to another track).

**In scope:** the server Go bumps (§6.1), x/net v0.62.0 (Q6), the esbuild alert (Q3), the bcrypt cap (if Q2 ≠ D), the port flake fix (if Q5 = A), the code-level Sonar fixes (D8), the govulncheck CI step (D5), `--ignore-scripts`, the client's server-pin bump, and the user actions in §8.

**Out of scope:** see §10.

## 2. What changed since the umbrella was written (2026-10-27)

| Umbrella said | Now (2026-10-28) | Evidence |
|---|---|---|
| Server: 22 open Dependabot alerts | **9 open**: grpc #38/#93/#118/#120/#121, quic-go #15/#23/#67, npm esbuild #2. UP cleared go-git/go-billy/circl on 2026-10-28T12:42Z; x/crypto #77–#89 and x/net #76 were fixed 2026-10-27. | `gh api repos/FPGSchiba/vcs-srs-server/dependabot/alerts?state=open` / `?state=fixed` |
| Client: Dependabot disabled (403) | **Enabled** (`GET vulnerability-alerts` → 204), **0 alerts** in any state. The user action in scope item 6 is done. | `gh api repos/FPGSchiba/vcs-srs-client/vulnerability-alerts -i`; `…/dependabot/alerts` → `[]` |
| Server govulncheck: 10 reachable in 3 modules | **6 reachable in 2 modules** (grpc ×4, quic-go ×2), identical for headless and GUI. x/net is gone (UP). | `scratchpad\secsrv-vuln-before-{headless,gui}.txt` |
| Client govulncheck: 9 reachable | **0 reachable.** PR #46 bumped grpc to v1.85.0 and x/net to v0.60.0. Client SEC needs no Go bump. | `scratchpad\seccli-vuln.txt`; client `go.mod` |
| Approved floor: server grpc `≥ v1.83.1` (`umbrella:441`) | **Insufficient.** GO-2026-6443 is fixed in v1.82.2 / **v1.83.2**; pinned at v1.83.1 it stays reachable. | probe §4.1; https://github.com/grpc/grpc-go/releases/tag/v1.83.2 |
| Sonar client: 14 open vulnerabilities | **12**: 6 S2819, **4** S8545 (3 resolved), 1 S6505, 1 S4036. 0 hotspots. | https://sonarcloud.io/api/issues/search?componentKeys=FPGSchiba_vcs-srs-client&resolved=false&ps=500 (fetched 2026-10-28) |
| Sonar server: 6 open | Still 6, but the **2 S8545 were re-raised** on 2026-10-27 because their lines moved (`Dockerfile:27`, `.github/actions/generate/action.yml:68`), so the earlier "safe" triage no longer covers them. **Plus 8 TO_REVIEW hotspots the umbrella never listed**: S7637 ×7 (`release.yml:49,52,55,63,73,97,102`, actions not SHA-pinned) and S6471 ×1 (`Dockerfile:53`, distroless runs as root). | https://sonarcloud.io/api/issues/search?componentKeys=FPGSchiba_vcs-srs-server&resolved=false&ps=500 ; https://sonarcloud.io/api/hotspots/search?projectKey=FPGSchiba_vcs-srs-server |
| esbuild #2: "bump it, or dismiss it" | **No bump exists.** The vulnerable copy is 0.17.19 nested under the dev dependency `typed-scss-modules@8.1.1`, which is the latest release and pins `esbuild ^0.17.0`. The top-level esbuild is already 0.28.2. | §3 #A5 |
| (not mentioned) bcrypt | New: the server accepts any cost 4..31 from the client hash; a forged cost-31 hash costs ~21 h of CPU per coalition, under a read lock. | §3 #B1 |
| (not mentioned) port flake | New: root-caused (TCP-probed port bound over UDP on Windows). | §3 #C1 |
| UP owns no grpc/quic-go | Unchanged; UP's §7 handed both to SEC. UP also left x/net at v0.60.0 on the server. | `design-UP.md` §7 |

Side note, not acted on: the local `D:\Projects\vcs-srs-server` checkout's `main` is at `850d984`, behind `origin/main` `df8d9f3`. The implementer must branch from `origin/main`.

## 3. Change list

### A. Dependencies and alerts

| # | Item | Repo | Files:lines | Change | Evidence |
|---|---|---|---|---|---|
| A1 | grpc 4 reachable (GO-2026-4762 authz bypass, -6443 crash, -6348, -6061); Dependabot #38/#93/#118/#120/#121 | server | `go.mod:18` (`grpc v1.77.0`), `go.sum` | → **v1.85.0** (min v1.83.2) | §4.1 |
| A2 | quic-go 2 reachable (GO-2026-5676, GO-2025-4233); Dependabot #15/#23/#67 | server | `go.mod:53` (`quic-go v0.54.0 // indirect`), `:52` qpack | → **v0.64.1** (min v0.59.1, untested); forces qpack v0.6.0 | §4.1 |
| A3 | forced moves | server | `go.mod:19` protobuf v1.36.11 → v1.36.12; genproto/googleapis/rpc pseudo-version; `go.sum`-only testify v1.12.1, gonum v0.17.0, go.yaml.in/yaml/v3 v3.0.5, quic-go/go-ossfuzz-seeds; otel/logr entries dropped | none by hand; listed in the PR | `scratchpad\secsrv-go.mod.{before,after}` |
| A4 | protoc-gen-go follows protobuf | server + client CI | server `.github/actions/generate/action.yml` (`go install protoc-gen-go@$(go list -m …)`), `Dockerfile:26-27`; client `.github/workflows/test.yml:148` (resolved against the server's go.mod) | **no edit**: all derive from the server's go.mod. protoc-gen-go-grpc stays v1.6.2. | probe codegen passed with protoc-gen-go v1.36.12 |
| A5 | esbuild #2 (GHSA-67mh-4wv8-2f99, dev-server CORS, `<= 0.24.2`) | server | `frontend/package.json:42` (`typed-scss-modules`), `frontend/package-lock.json:3356,5903` | per Q3; recommended `npm uninstall typed-scss-modules` | `gh api …/dependabot/alerts/2` (transitive, scope development); `npm view typed-scss-modules`; nothing in scripts, `vite.config.ts` or CI references it |
| A6 | Client Go | client | `go.mod` | **none for grpc** (D2). x/net v0.60.0 → v0.62.0 only if Q6 = A (no forced moves: client x/crypto v0.58.0, x/sys v0.49.0, x/text v0.43.0 already meet x/net v0.62.0's go.mod) | `scratchpad\seccli-vuln.txt`; https://proxy.golang.org/golang.org/x/net/@v/v0.62.0.mod |
| A6b | x/net (Q6) | server | `go.mod` (`x/net v0.60.0 // indirect`) | → v0.62.0 if Q6 = A; forces x/crypto v0.57.0 → v0.58.0, x/sys v0.48.0 → v0.49.0, x/text v0.42.0 → v0.43.0, and x/term v0.47.0 (not in the server's go.mod today; may appear in go.sum only) (per x/net's go.mod; **untested**, re-probe in the plan) | same `.mod` URL; server `go.mod` |
| A7 | govulncheck gate | both | client `.github/workflows/test.yml` `backend` job, all three legs, **after the `frontend/dist` placeholder** (`:253-256`); server `.github/workflows/test.yml` `test` job (`:14`, after the generate action) | new step `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -tags <purego\|headless> ./...`; a finding makes `go run` exit 1 (child exit 3) and fails the job | SEC scope 5; v1.1.4 panics under Go 1.27 (`umbrella:286`); §4.5 |

### B. Server behaviour: bcrypt cost cap

| # | Item | Files:lines | Change | Evidence |
|---|---|---|---|---|
| B1 | Unbounded client-chosen bcrypt cost | `srs/auth_service.go:484-494` (`RLock`, loop calling `utils.CheckPasswordHash` at `:488`, `RUnlock`); `utils/general.go:9-12` | Before `:484`: `cost, err := bcrypt.Cost([]byte(request.Password))`; on error or `cost > maxBcryptCost` return `GuestLoginResponse{Success:false, ErrorMessage:"Invalid password hash: …"}`, nil error, plus a WARN with peer IP. Pending entry **not** consumed (matches the other failures; it is consumed only on success at `:520`). `CheckPasswordHash` also returns false above the cap. Optional (Q2b): copy `Coalitions` under the RLock and compare outside it. | x/crypto v0.57.0 `bcrypt.go:108-125` (`checkCost` allows 4..31); only call site `srs/auth_service.go:488` (plugin flow never compares) |
| B2 | Why it matters beyond CPU | `srs/auth_service.go:484-494` | Compare runs under `settingsState.RLock`. A queued writer (`app/settings.go:23,45,67,89,110,131`, `app/server_control.go:50,268`, `services/coalitions.go:23,29`) blocks every new `RLock`, so one slow login plus an admin save freezes all settings reads. Context cancellation does not stop bcrypt. | Go `sync.RWMutex` semantics |
| B3 | No concurrency bound | `srs/auth_service.go:46-58,154-171,468`; `control/server.go:162-179` | **None in SEC** (Q2c deferred). The SV3 pending cap (10,000 / 32 per IP) bounds InitAuth entries, not GuestLogin calls; one GUID can drive unlimited parallel failing logins; no `MaxConcurrentStreams` (grpc default `math.MaxUint32`). With the cap at 12 each call costs ≤ ~141 ms × coalitions. | research facts |
| B4 | Cap value vs. clients | client `internal/session/session.go:744` (cost 10); C# peer `VNGD-SimpleRadioStandalone` **PR #253 head** (`develop`, `e8fc8c8`) `DCS-SR-Client/Network/VcsClientSyncHandler.cs:140` (`GenerateSalt(12)`). PR #253 is unmerged, so cost 12 is not guaranteed on the peer's main branch either. | Recommended max **12**. Record it as a client requirement (SV7 README *Client connections*, and a `docs/PROTO_GAPS.md` note in the client). | research facts |

### C. Client test infrastructure

| # | Item | Files:lines | Change | Evidence |
|---|---|---|---|---|
| C1 | Windows flake `bind: An attempt was made to access a socket in a way forbidden by its access permissions.` | `internal/voice/testdata/server_fixture.go:107-109` (`httpPort/voicePort/controlPort := freePort(t)`), `:187-189` (`freePort` binds `tcp 127.0.0.1:0`), `waitReady` `:298-316` | (1) new `freeUDPPort` (binds `udp 127.0.0.1:0`) for `voicePort`; http/control stay TCP. (2) `Start` retries up to 3× with fresh ports when the server log shows a bind error (`forbidden by its access permissions` / `Only one usage of each socket address` / `address already in use`); today `waitReady` fails fast on any `level=ERROR` (`:310`). (3) **Test placement:** `freeUDPPort` and the classifier stay in `testdata`, but their tests go in `internal/voice` (e.g. `internal/voice/fixture_ports_test.go`, beside `integration_test.go`, which already imports the package at `:18`). A `_test.go` inside `testdata` would never run: `./...` skips `testdata` directories, as `server_fixture.go:4-7` itself says. The helpers are exported (`testdata.FreeUDPPort`, `testdata.IsBindError`) for that reason. | server binds voice over UDP (`voice/server.go:165`, `net.ListenUDP`); Windows keeps separate UDP/TCP excluded ranges (§4.4) |
| C2 | Other harnesses | `internal/voice/testserver_test.go:56` (already UDP :0), `internal/grpctest/fakeserver.go:423,466` (bufconn) | none | — |

### D. SonarCloud

| # | Rule | Repo | Location | Action |
|---|---|---|---|---|
| D-a | S4036 | client | `internal/hotkeys/permission_darwin_pane.go:73` `exec.Command("open", url)` | → `"/usr/bin/open"` via a named constant. The assertion goes in `permission_darwin_test.go` (`//go:build darwin && cgo`, line 1), which runs on the macOS CI leg; the stub-based `permission_darwin_nocgo_test.go` is `darwin && !cgo` (line 1) and does **not** run there. The assertion is a compile-level guard only (it pins the constant, not the exec). |
| D-b | S6505 | client | `.github/workflows/test.yml:344` `npm ci` | → `npm ci --ignore-scripts` (only install script in the lock: `fsevents`, darwin-only optional). Verified for the full job: `npm ci --ignore-scripts`, `npm run test`, **`npm run build`** (the step CI runs at `test.yml:350-352`), with bindings generated by wails3 v3.0.0 (§4.5). |
| D-c | S6505 | server | `.github/workflows/test.yml:79` `npm ci --legacy-peer-deps` | → add `--ignore-scripts` |
| D-d | S6505 (not flagged) | both | client `build/Taskfile.yml:30`, server `build/Taskfile.yml:25` | **not changed**: these build the shipped frontend, where vite/rolldown/esbuild native binaries matter; the CI evidence covers tests only |
| D-e | S6506 | server | `build/linux/appimage/build.sh:18` | delete the file: unused Wails-alpha template (Taskfile calls `wails3 generate appimage`, `build/linux/Taskfile.yml:46`); its quoted glob at `:24` could never have worked |
| D-f | S2068 ×2 | server | `dev/config/control.yaml:16,20` (`blue123`/`red123`, mounted read-only by `docker-compose.dev.yml:37`) | add a comment line ("dev-only docker-compose fixture, throwaway passwords") and the user marks safe. Env placeholders would need config env expansion, which the server lacks (no `ExpandEnv`/`Getenv` in non-test code): new feature, out of scope |
| D-g | S8545 ×4 client, ×2 server | client `release.yml:146`, `test.yml:166,244,319`; server `Dockerfile:27`, `generate/action.yml:68` | user re-marks safe (existing decision, `umbrella:428`). The new govulncheck `go run …@v1.9.0` steps (A7) will raise the same class; also marked safe |
| D-h | S2819 ×6 | client | `design/vcs/project/tweaks-panel.jsx:171,212,220,257,258,264` | user excludes `design/**` in the SonarCloud UI (Administration > Analysis Scope; no `sonar-project.properties`, `sonar.exclusions` empty) or marks the 6 safe |
| D-i | S7637 ×7 | server | `.github/workflows/release.yml:49,52,55,63,73,97,102` | Q4 |
| D-j | S6471 | server | `Dockerfile:53` `FROM gcr.io/distroless/static-debian12` | Q4 |
| D-k | new smells (`wails-update-check.sh` S7688/S7679/S1192/S131, others) | both | — | none; Phase 10. S131 at `wails-update-check.sh:85` is a deliberate one-arm `case` |

## 4. Probe results (verbatim)

### 4.1 Server dependency trial (worktree `probe-secsrv`, detached `df8d9f3`, removed)

Sequence: codegen → baseline build/vet (both tag sets OK) → govulncheck baseline (6 reachable) → `go get grpc@latest quic-go@latest` → tidy → regenerate with protoc-gen-go v1.36.12 → build/vet both OK → race tests OK → govulncheck 0 reachable (both tag sets) → a v1.83.1 floor test showed GO-2026-6443 is still reachable.

| Command | Result | Output |
|---|---|---|
| govulncheck v1.9.0 `-tags headless` / GUI (before) | 6 reachable, identical | grpc v1.77.0 GO-2026-6443 (fix v1.82.2), GO-2026-6348 (fix v1.83.1), GO-2026-6061 (fix v1.82.1), GO-2026-4762 (fix v1.79.3); quic-go v0.54.0 GO-2026-5676 (fix v0.59.1), GO-2025-4233 (fix v0.57.0) |
| `go get google.golang.org/grpc@latest github.com/quic-go/quic-go@latest && go mod tidy` | pass | grpc v1.77.0→v1.85.0, protobuf v1.36.11→v1.36.12 (forced), quic-go v0.54.0→v0.64.1, qpack v0.5.1→v0.6.0 (forced), genproto/googleapis/rpc 20251022142026-3a174f9686a8 → 20260817212433-ac3dfec99bb1 (forced). x/tools stays v0.49.0, gqlgen v0.17.89, x/net v0.60.0 |
| 3× protoc + gqlgen (after) | pass | gqlgen marked `graph/schema.resolvers.go` modified; `git diff --ignore-cr-at-eol` empty (CRLF only). Local protoc 36.0, CI pins 36.2 |
| build / vet, headless and GUI (placeholder `frontend/dist`) | pass | `BUILD_H_OK / VET_H_OK / BUILD_GUI_OK / VET_GUI_OK` |
| `go test -race -count=1 -tags headless ./...` | pass | app, control, events, logging, rest, srs, state, utils, voice, voiceontrol all `ok` |
| govulncheck v1.9.0 (after), both tag sets | pass | exit 0, `Your code is affected by 0 vulnerabilities.` Module-level, unreachable: x/net v0.60.0 GO-2026-6866, GO-2026-6860 (fix v0.62.0); x/crypto v0.57.0 GO-2026-5932 (openpgp, no fix) |
| `go get google.golang.org/grpc@v1.83.1` + govulncheck `-tags headless` | **fail** | `affected by 1 vulnerability` — GO-2026-6443, Found in v1.83.1, Fixed in v1.83.2 |
| `go mod why -m github.com/quic-go/quic-go` | info | `app -> gin -> quic-go/http3`. gin v1.11.0 builds against v0.64.1 |

Evidence files (scratchpad): `secsrv-go.mod.before/.after`, `secsrv-go.sum.before/.after`, `secsrv-vuln-{before,after}-{headless,gui}.txt`, `grpc-relnotes.txt`. Bumped binary: `C:\Users\janne\AppData\Local\Temp\claude\D--Projects-vcs-srs-client\61e6be5b-33d5-417e-8854-ccbdd02e4132\scratchpad\probe-bin\vcs-server-bumped.exe`.

### 4.2 Client against the bumped server (worktree `probe-seccli`, detached `b80b0e6`, removed)

| Command | Result | Output |
|---|---|---|
| govulncheck v1.9.0 `-tags purego ./...` | pass | 0 reachable, 0 package-level. Module-only: x/net v0.60.0 GO-2026-6866/6860 (fix v0.62.0), x/crypto v0.58.0 GO-2026-5932 (no fix) |
| `buf generate`; placeholder `frontend/dist`; `VCS_SERVER_BIN=<bumped headless server>`; `go test -race -count=1 -tags purego ./internal/voice/ ./internal/session/` | pass | **252 PASS / 0 SKIP / 0 FAIL**, including `TestIntegrationRelayBetweenTwoClients`, `TestIntegrationWrongSecretRejected`, `TestUpdateRadioInfo_UnauthenticatedEndsSession` (`scratchpad\seccli-integ.txt`) |

### 4.3 Triage probes (worktrees `probe-sec-client`, `probe-sec-server`, `probe-ports`, removed)

1. **bcrypt timing** (server, x/crypto v0.57.0): `cost 10 compare: 35.75ms`, `cost 12 compare: 141.0ms`, `cost 14 compare: 563.2ms`. A cost-4 hash with `$04$` rewritten to `$31$`: `forged cost=31 err=<nil>` from `bcrypt.Cost`. Extrapolated cost 31: 2^21 × 36 ms ≈ 75,000 s ≈ 21 h per coalition.
2. **Client frontend:** `npm ci --ignore-scripts` then `npm run test`: `Tests 459 passed (459)`; npm reported 0 vulnerabilities. *Superseded by §4.5 #1, which also runs `npm run build` and counts 594 tests at `b80b0e6`; the 459 figure from this pass is not explained and is not relied on.*
3. **Server frontend:** `npm ci --ignore-scripts --legacy-peer-deps` then `npm test`: passed, coverage printed. `npm run build` transformed 1033 modules, then failed only on `UNRESOLVED_IMPORT ../../bindings/...` (gitignored bindings; same without `--ignore-scripts`; server CI does not run `npm run build`). After `npm uninstall typed-scss-modules`: `npm ls esbuild` shows only `esbuild@0.28.2` (deduped under `vite@8.3.5`, optional peer `^0.27||^0.28`), `npm audit` 0 vulnerabilities, tests pass.
4. **Windows ports:** `netsh int ipv4 show excludedportrange protocol=udp`: 50000-50059*, 54526-55125, 58120-58219. `protocol=tcp`: 27339, 50000-50059*, 54288, 62842-63478, 65164-65263. Dynamic range 49152 + 16384 for both. On 54600 and 58150 the TCP bind returns nil and the UDP bind returns `bind: An attempt was made to access a socket in a way forbidden by its access permissions.` (the exact CI error). 3/3000 TCP `:0` probes fell in a UDP-excluded range; 0/3000 UDP `:0` probes did.

### 4.5 Review-pass probes (worktrees `probe-secnpm` at client `b80b0e6`, `probe-secvt` at server `df8d9f3`, both removed and pruned)

| # | Command | Result | Output |
|---|---|---|---|
| 1 | client: `buf generate`; `wails3 generate bindings -ts -f "-tags purego" -clean=true` (wails3 **v3.0.0**, the go.mod version, installed to a scratch GOBIN); `cd frontend && npm ci --ignore-scripts && npm run test && npm run build` | pass | `found 0 vulnerabilities`; `Test Files 51 passed (51)`, `Tests 594 passed (594)`; build exit 0, `✓ built`, postbuild `check-dist: dist/ contains index.html, main.html, comms.html, notifications.html` (`scratchpad\secnpm-build.txt`) |
| 2 | server: 3× protoc + gqlgen; fresh empty `GOCACHE` (module cache warm, as `setup-go`'s cache restores it on CI); `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -tags headless ./...` | 6 found, as expected | **37 s** cold (compile govulncheck + fetch DB + analyse), 4 s warm. `Your code is affected by 6 vulnerabilities from 2 modules.` then `exit status 3`; the shell saw exit **1** (`scratchpad\secvt-cold.txt`) |
| 3 | `gh run view 40689922000` (server Test on `main`, 2026-10-28T12:41Z) | info | `Go Test` 12:41:44 → 12:43:59 = **2 m 15 s** of its 10 min budget (`.github/workflows/test.yml:17`). Adding ~40-60 s on a CI runner leaves ample margin: **no timeout change**. |
| 4 | https://vuln.go.dev/ID/GO-2026-6866.json, …/GO-2026-6860.json; `gh api /advisories?cve_id=…` | info | both published 2026-10-27T21:16Z, `golang.org/x/net/html`, fixed v0.62.0, aliases CVE-2026-97045 / CVE-2026-78661 → GHSA-vwcj-r8v9-p53f / GHSA-75pg-wvh3-34jr, both `unreviewed` with no package mapping yet (published 2026-10-28T00:30Z). Dependabot alerts only on reviewed advisories, so none are open today (server open list = the 9 in §2; client 0). |
| 5 | https://proxy.golang.org/golang.org/x/net/@v/v0.62.0.mod / `.info` | info | released 2026-10-27T19:00Z; `go 1.26.0`; requires x/crypto v0.58.0, x/sys v0.49.0, x/term v0.47.0, x/text v0.43.0 |
| 6 | C1–C17 contract | info | C1–C12: server `docs/superpowers/specs/2026-10-27-phase-6-9-sv1-session-liveness-design.md:207-218` (SV1 §11, the SV1 → CL1 table). C13–C17: server `…-sv2-state-event-correctness-design.md:195-199`. Every row concerns a valid method name (token state, session state, stream semantics, roster contents, the C17 subscription ack); none depends on how a malformed `:path` is answered. C11 says AuthService failures are `Success:false` bodies, which the bcrypt cap follows. |

### 4.4 grpc v1.78–v1.85 release-note review (server-relevant)

No change to interceptor chaining, `status.Error` codes (`authError`, `control/server.go:544-559`), keepalive enforcement or TLS creds. Relevant: from v1.79.3 non-canonical `:path` is rejected with `Unimplemented` before any interceptor (v1.82.0 removed the opt-out env var); requests missing both `:authority` and `Host` → `Internal` (v1.82.2/v1.83.2/v1.85.0); HTTP/2 control-frame flood throttle (v1.82.1/v1.83.0, limit 10,000 in v1.85.0); `Stop()` now forcibly terminates while `GracefulStop` waits (v1.85.0; affects `control/server.go:409-420`, an improvement); truncated frames return `io.ErrUnexpectedEOF` instead of `io.EOF` (v1.84/v1.85); minimum Go 1.25 (v1.81). `TestAuthInterceptorCodes` (`control/server_test.go:271`), `TestAuthErrorMapping` (`:349`) and the session E2E tests (`control/session_e2e_test.go:138,170`) stay green. Notes: https://github.com/grpc/grpc-go/releases/tag/v1.85.0 , /v1.84.0, /v1.83.0, /v1.82.1, /v1.81.0, /v1.80.0, /v1.79.0, /v1.78.0.

GO-2026-4762 analysis (not a test): on v1.77 the server's `authenticate` (`control/server.go:481-497`) splits `FullMethod` on `/` and returns `Unauthenticated` for fewer than 3 elements, so `srs.SRSService/SyncClient` most likely failed closed already. After the bump, a path-less request sees `Unimplemented` instead; only hand-crafted clients can notice.

quic-go: the server never serves HTTP/3. The HTTP API is `net/http` `ListenAndServe` over gin (`app/server_control.go:36-70`); `RunQUIC`/http3 are never called. govulncheck's "reachable" trace goes through interface dispatch in `handshakeLoggingCreds` (`control/server.go:274-278`), an over-approximation. The bump is still taken because it is cheap and clears three Dependabot alerts.

## 5. Plan shape

### 5.1 Server PR (`chore/cleanup-sec`, first)

1. **bcrypt tests first** (only if Q2 ≠ D; `srs/auth_service_test.go`, `utils/general_test.go`). Forged hashes are a real low-cost hash with the cost field rewritten (`$04$` → `$NN$`); `bcrypt.Cost` accepts them (§4.3 #1).
   - **Red tests** (fail quickly on today's code, no hang):
     - (a) GuestLogin with a forged **cost-13** hash (one above the cap) → `Success:false`, message starts `Invalid password hash`, pending entry still present. Today this compare takes ~280 ms (2 × the measured 141 ms at cost 12) and returns `No coalition found with that password` (`srs/auth_service.go:496-500`), so the prefix assertion fails cleanly.
     - (c) GuestLogin with a malformed hash (`"not-a-hash"`) → message starts `Invalid password hash`. Today: `No coalition found with that password`.
     - (d) `utils.CheckPasswordHash` on a forged **cost-14** hash returns false in < 50 ms. Today ~560 ms (§4.3 #1), an ~11× margin over the threshold; after the fix it returns before hashing (µs).
     - (e) only if Q2 option (b) is taken: **lock seam.** `srs` gets a package-level `var comparePassword = utils.CheckPasswordHash`, used at `auth_service.go:488`. The test swaps in a stub that signals `entered` and blocks on a `release` channel, starts GuestLogin in a goroutine, waits for `entered`, then calls `s.settingsState.Lock()` in a second goroutine and asserts it acquires within 100 ms; it then unlocks and closes `release`. Today the compare runs under `RLock` (`:484-494`), so `Lock()` waits for `release` and the test fails at 100 ms; with the slice copied under the RLock and compared outside it, `Lock()` acquires at once.
   - **Regression guards** (green before and after): (b) the correct password hashed at exactly the max cost → success; existing tests that use `DefaultCost`/`MinCost`.
   - **After-fix only:** a forged **cost-31** case asserting return in < 1 s. It is never run against the old code (it would take ~21 h and the job's `timeout-minutes: 10`, `.github/workflows/test.yml:17`, would kill the whole job rather than report red). The plan orders it after step 2.
2. Implement the cap (B1) with a named constant `maxBcryptCost = 12` (per Q2) in `utils`; WARN log with peer IP.
3. Go bumps (A1–A3, and A6b if Q6 = A): `go get google.golang.org/grpc@v1.85.0 github.com/quic-go/quic-go@v0.64.1` (per Q1) [`golang.org/x/net@v0.62.0`], codegen, then tidy (D3). x/net v0.62.0 is untested on the server: build/vet both tag sets, race tests and govulncheck re-run with it. Guard: existing `TestAuthInterceptorCodes`, `TestAuthErrorMapping`, session E2E, full `go test -race -tags headless ./...`; govulncheck headless **and** GUI = 0 reachable, outputs pasted in the PR.
4. esbuild (A5, per Q3): `npm uninstall typed-scss-modules --legacy-peer-deps`; `npm ls esbuild`, `npm audit`, `npm test`.
5. CI: `--ignore-scripts` (D-c); govulncheck step (A7). Red evidence already exists: at `df8d9f3` the exact step reports 6 and `go run` exits 1 (§4.5 #2); green on the branch. Record the job's wall time in the PR (budget: 10 min, today 2 m 15 s).
6. Sonar housekeeping: delete `build/linux/appimage/build.sh` (D-e); comment in `dev/config/control.yaml` (D-f); Q4 items if taken.
7. Full suite; `superpowers:requesting-code-review`; report ready; the user merges.

### 5.2 Client PR (`chore/cleanup-sec`, after the server merges)

1. **Port fix test first** (only if Q5 = A), in `internal/voice/fixture_ports_test.go` (**not** under `testdata`, which `go test ./...` never runs; `server_fixture.go:4-7`): `TestFreeUDPPortIsUDPBindable` (the returned port can be re-bound with `net.ListenUDP`) and a table test for `testdata.IsBindError` (the three strings in C1, plus a non-bind `level=ERROR` that must still fail fast). Both are red by compile failure until step 2 exports the helpers. The flake itself cannot be made deterministic in a unit test; §4.3 #4 is the root-cause evidence.
2. Implement `FreeUDPPort` + `IsBindError` + bounded retry in `server_fixture.go` (C1).
3. S4036 (D-a): extract the binary path as a constant and assert it in `permission_darwin_test.go` (`darwin && cgo`, runs on the macOS CI leg). Compile-level guard only.
4. Optional guard (part of Q2): a test asserting `bcrypt.DefaultCost <= 12`, so an x/crypto default change cannot silently lock the Go client out.
5. If Q6 = A: `go get golang.org/x/net@v0.62.0`, tidy (no forced moves expected, A6), build/vet/race with `-tags purego`, govulncheck → 0 reachable and no x/net module-level line.
6. CI: `--ignore-scripts` at `test.yml:344` (D-b); govulncheck step on all three `backend` legs after the placeholder (A7); `internal/buildcheck` test first that asserts both, including the step order (the repo already shape-tests workflows in `internal/buildcheck/buildcheck_test.go` / `ci_hardening_test.go`).
7. Bump the pinned server SHA at `.github/workflows/test.yml:49` to the merged server SEC commit, and rewrite its comment (`:43-48`) to describe the whole range it now covers (`850d984` → SEC merge: UP's server changes at `df8d9f3` plus SEC), not SEC alone. The `TestServerCheckoutPinnedToSHA` guard moves with it. This also moves client CI's derived protoc-gen-go to v1.36.12 (A4).
8. Docs (if Q2 takes a cap): `docs/PROTO_GAPS.md` note "server rejects bcrypt cost > 12" (wording only; no proto change). Full suite, review, report ready.

### 5.3 Order

Server first: it closes the live exposure (GO-2026-4762 / GO-2026-6443 on the internet-facing control port, the accepted risk at `umbrella:320`). The client PR can be opened in parallel; only its final pin-bump commit waits for the server merge. SV7 runs alongside, see the header.

## 6. Gates needing approval

### 6.1 Dependency changes

**Server** (probe diff, `scratchpad\secsrv-go.mod.{before,after}`):
- Direct: `google.golang.org/grpc v1.77.0` → **`v1.85.0`** (re-approval needed: the approved `≥ v1.83.1` does not fix GO-2026-6443; the true floor is v1.83.2). `google.golang.org/protobuf v1.36.11` → `v1.36.12` (forced).
- Indirect: `github.com/quic-go/quic-go v0.54.0` → `v0.64.1`; `github.com/quic-go/qpack v0.5.1` → `v0.6.0` (forced); `google.golang.org/genproto/googleapis/rpc` `20251022142026-3a174f9686a8` → `20260817212433-ac3dfec99bb1` (forced).
- `go.sum` only: testify v1.12.1, gonum v0.17.0, go.yaml.in/yaml/v3 v3.0.5, quic-go/go-ossfuzz-seeds added; otel/logr entries dropped.
- Unchanged: x/tools v0.49.0, gqlgen v0.17.89, gin v1.11.0. x/net stays v0.60.0 unless Q6 = A.
- If Q6 = A (inside the approved `x/net ≥ v0.60.0`, `umbrella:441`; the forced moves are shown for approval): `golang.org/x/net` v0.60.0 → v0.62.0; forced x/crypto v0.57.0 → v0.58.0, x/sys v0.48.0 → v0.49.0, x/text v0.42.0 → v0.43.0, x/term v0.47.0 (go.sum). Untested; re-probed in the plan.
- npm (Q3 = A): remove dev dependency `typed-scss-modules` from `frontend/package.json:42` and its lock subtree (incl. nested `esbuild 0.17.19`).

**Client:** none, unless Q6 = A: `golang.org/x/net` v0.60.0 → v0.62.0, no forced moves expected (A6).

### 6.2 CI changes

| Repo | File:line | Change |
|---|---|---|
| both | `.github/workflows/test.yml` (client `backend` job, all three OS legs, after the `frontend/dist` placeholder `:253-256`; server `test` job after the generate action) | new blocking govulncheck v1.9.0 step (approved in principle by SEC scope 5; exact step and placement approved here). Server `timeout-minutes: 10` (`:17`) is **not** changed (measured 2 m 15 s + ~37 s cold, §4.5); raising it would be a separate CI gate. |
| client | `.github/workflows/test.yml:344` | `npm ci --ignore-scripts` |
| server | `.github/workflows/test.yml:79` | `npm ci --ignore-scripts --legacy-peer-deps` |
| client | `.github/workflows/test.yml:49` (+ comment `:43-48`, rewritten to describe the `850d984` → SEC-merge range) | pinned server SHA → merged server SEC commit |
| server | `.github/workflows/release.yml:49,52,55,63,73,97,102` | only if Q4 = A: SHA-pin the 7 actions with a version comment |

No change to protoc/buf/gqlgen pins: protoc-gen-go is derived from go.mod everywhere (A4).

### 6.3 Server behaviour changes

| Change | Visible to | Before | After |
|---|---|---|---|
| bcrypt cost cap (B1, if Q2 ≠ D) | any client (**wire-visible**) | hash of any cost 4..31 accepted and compared at that cost; a malformed or over-cost hash that matches no coalition gets `No coalition found with that password` (`srs/auth_service.go:496-500`) | cost > 12 or unparseable hash → `GuestLoginResponse{Success:false, ErrorMessage:"Invalid password hash: bcrypt cost N exceeds the server maximum of 12"}` (or `…: malformed`), nil error, pending entry kept. Go client (10) and C# peer (12, PR #253 head) unaffected |
| Compare outside the lock (Q2b) | admins | admin settings save can stall behind a slow login | no stall |
| grpc v1.85.0 | hand-crafted clients only | path without leading `/` → `Unauthenticated` (from the interceptor) | `Unimplemented` (grpc rejects before the interceptor). Missing `:authority` and `Host` → `Internal`. `Stop()` forcibly terminates streams |
| HTTP/2 control-frame flood throttle | abusive peers | none | throttled (grpc-internal) |

### 6.4 Cross-repo

Ordering only (D10). No proto change; no client code depends on the server bump. The bcrypt cap (if taken) is a wire-visible behaviour change documented as a client requirement (SV7 README, client `PROTO_GAPS.md`).

### 6.5 Umbrella corrections (for DOC, or for the user to approve)

The umbrella's behaviour table row for SEC (`umbrella:365`) reads "gRPC/x/net/quic-go upgrades …; dev config credentials replaced". After this design it should read:
- **dev config credentials kept**, with a "dev-only fixture" comment and a user mark-safe (permitted by scope item 4, `umbrella:309`: "or mark them as dev-only fixtures if they are deliberate"); not "replaced".
- **plus**, if Q2 ≠ D, the wire-visible bcrypt cap (§6.3): GuestLogin with a hash of cost > 12 or a malformed hash now answers `Invalid password hash …` instead of `No coalition found with that password`. Visible to: any client, operators.
- **plus**, if Q5 = A, the port flake moves to SEC (test-only, no behaviour row).
- The §9 decisions table gains the server grpc floor correction (`≥ v1.83.1` → `≥ v1.83.2`, taken as v1.85.0 per Q1).

## 7. Risks

- **R1 govulncheck turns CI red with no code change.** A new advisory against any module fails `main` in both repos. Mitigation: a separately named step so the failure is attributable; the weekly go-deps workflow is the routine fix. The step also needs network access to `vuln.go.dev`. Accepting this is the point of the gate.
- **R2 Dependabot vs govulncheck ranges disagree.** Dependabot's #121 (`< 1.82.2`) does not flag v1.83.0/1.83.1; govulncheck does. Rely on govulncheck (D1 uses v1.85.0, above both).
- **R3 Interceptor contract (settled).** C1–C12 are the SV1 → CL1 table (server `docs/superpowers/specs/2026-10-27-phase-6-9-sv1-session-liveness-design.md:207-218`); C13–C17 are SV2's additions (`…-sv2-state-event-correctness-design.md:195-199`; C17 is the subscription ack the client pin comment at `test.yml:46` names). All seventeen rows describe calls to valid method names; none specifies the code for a malformed `:path`, so grpc v1.85.0 answering such a path with `Unimplemented` before the interceptor breaks no contract row. C11 (AuthService failures are `Success:false` bodies) is what the bcrypt cap follows. Guards: `TestAuthInterceptorCodes` / `TestAuthErrorMapping` / session E2E plus the client integration suite, all green on the bumped server (§4.1, §4.2).
- **R4 quic-go jumps ten minors (v0.54 → v0.64).** It is linked only via gin's http3 package and never served (§4.4); build and tests pass. v0.59.1 is the untested minimum (Q1).
- **R5 bcrypt cap lockout.** A future client hashing above 12 is rejected with an explicit message. The C# peer sits exactly at the cap; a peer change to 13 would break it. Mitigation: documented requirement; optional client `DefaultCost <= 12` test.
- **R6 The cap does not bound concurrency** (B3). With the cap, worst case is ~141 ms × coalitions per call, unbounded in parallel. Deferred unless Q2c is chosen.
- **R7 `--ignore-scripts`** can break a future dependency that needs a postinstall in tests. Today only `fsevents` (client) and esbuild/@parcel/watcher/fsevents (server) have scripts, and tests pass without them. Build Taskfiles are deliberately unchanged (D-d).
- **R8 Port fix is probabilistic.** The UDP probe removes the excluded-range cause (0/3000); the release-to-bind race remains and is covered by the bounded retry. CI evidence needs several Windows runs, not one.
- **R9 Windows-only probe host.** The server bump was built for both tag sets but Linux/macOS legs run only in CI. The client's Linux/macOS legs exercise the server headless build after the pin bump.
- **R10 Module-level findings remain.** x/crypto GO-2026-5932 (openpgp, no fix) stays in both repos; govulncheck exits 0 on it. x/net GO-2026-6866/6860 go away if Q6 = A. If Q6 = B, Dependabot opens x/net alerts in both repos as soon as GitHub reviews GHSA-vwcj-r8v9-p53f / GHSA-75pg-wvh3-34jr (§4.5 #4), and the goal "close every open Dependabot alert" fails shortly after merge.
- **R13 govulncheck coverage gaps.** Server CI analyses only `linux` + `headless`: the server's Windows-only and GUI-tagged code is never checked in CI (GUI is checked locally in the PR). The client covers all three GOOS via the matrix, `purego` only.
- **R14 Brand-new x/net (Q6 = A).** v0.62.0 was tagged 2026-10-27T19:00Z and has not been built in either repo; the plan re-runs build/vet/race/integration with it before merge.
- **R11 Removing typed-scss-modules** loses a CLI someone might run by hand to regenerate `.scss.d.ts` files. Nothing in the repo invokes it.
- **R12 Secret scanning unchecked** (needs a higher-scoped token). User action §8.

## 8. Manual verification

Not hardware-bound for the most part; items the track cannot do itself:
1. **After the server PR merges:** Dependabot shows 0 open Go alerts on the server (#38, #93, #118, #120, #121, #15, #23, #67 auto-close) and #2 closes (Q3 = A) or is dismissed with the written reason (Q3 = C).
2. **SonarCloud (user, after the next analysis):** re-mark S8545 safe (client 4 incl. any new govulncheck `go run` lines; server 2); mark S2068 safe; exclude `design/**` or mark the 6 S2819 safe; confirm S4036, S6505, S6506 resolved; review S7637/S6471 per Q4.
3. **Secret scanning (user):** check both repos with a suitably scoped login.
4. **Real server, real client:** connect the client to the bumped server over TLS on a non-loopback host (`internal/session/dial.go`), log in as guest, open a voice channel; then kick the client. Confirms grpc v1.85.0 against a real network path, not only the loopback integration suite.
5. **C# peer (when PR #253's voice-secret blocker is resolved):** guest login with its cost-12 hash succeeds against the capped server.
6. **Client Windows CI:** the integration leg passes on at least five consecutive Windows runs with no `forbidden by its access permissions` bind error.
7. **Docker image (only if Q4 takes S6471):** the image starts as UID 65532; the mounted banned-clients file and log folder are writable; port 80 binds; **`/certs` is writable by UID 65532** (`docker-compose.dev.yml:38` mounts `./dev/certs:/certs` writable, and the server generates its key pair and VoiceControl certificate there on first start, `dev/config/control.yaml:38-39,47-48`). This is the likeliest non-root breakage.
8. **Dependabot, x/net (Q6 = A):** after both PRs merge, no x/net alert opens once the two GHSAs are reviewed.

## 9. Questions for the user

**Q1 – Server grpc and quic-go targets (dependency re-approval).**
- **A (recommended):** grpc **v1.85.0** (matches the client; tested end to end) and quic-go **v0.64.1** (latest; tested), with the forced moves in §6.1 (protobuf v1.36.12, qpack v0.6.0, genproto rpc).
- B: minimum fixes: grpc v1.83.2 and quic-go v0.59.1. Neither was tested here; a re-probe would be needed. Diverges from the client's grpc.

**Q2 – bcrypt cost cap (server behaviour change).**
- **A (recommended):** max **12**; reject above it or on a malformed hash with a `Success:false` body (nil error, per the SV3 body decision) and a WARN log; check before the loop and the lock; defence in depth in `CheckPasswordHash`; **and (b)** copy the coalition slice under the RLock and compare outside it. Defer (c) a concurrency limit. Add the client `DefaultCost <= 12` guard test and document the cap (SV7 README, client `PROTO_GAPS.md`).
- B: as A but max **13** (headroom; ~280 ms worst case per coalition).
- C: as A, and also (c) a bcrypt in-flight semaphore sized to `runtime.NumCPU()`. New concurrency behaviour; more test surface.
- D: **don't cap in SEC**; file it for a later track (Phase 10 or a new server track). SEC then has no server behaviour change, and SV7 has nothing to document. The exposure (one request ≈ 21 h of CPU per coalition, under the settings read lock) stays open on the internet-facing control port until that track lands.

Recommendation stays **A**: the change is small (one check before `:484`, one in `CheckPasswordHash`), the vector is remote and unauthenticated beyond InitAuth, and SEC is the security track already touching this server.

**Q3 – Server esbuild alert #2.**
- **A (recommended):** remove the unused dev dependency `typed-scss-modules` (a dependency change). Clears the alert; `npm audit` 0; vite keeps esbuild 0.28.2; tests pass. (`vite-plugin-typed-css-modules` is also unused but carries no alert; it stays.)
- B: npm `overrides` forcing its esbuild to ≥ 0.25. Untested; may break the package's `bundle-require` on 0.17 APIs.
- C: dismiss in GitHub as "dev-only transitive copy, never shipped; advisory affects only esbuild's dev server".

**Q4 – The 8 server hotspots the umbrella never listed.**
- **A (recommended):** take **S7637** in SEC (SHA-pin the 7 `release.yml` actions, matching the client's practice; a CI change) and **mark S6471 safe** for now with the reason "distroless static image, no shell; non-root switch needs a writable-volume check", leaving the `:nonroot` switch to Phase 10.
- B: take both: also switch `Dockerfile:53` to `gcr.io/distroless/static-debian12:nonroot` and verify volume permissions (manual §8 #7). Server deployment behaviour change.
- C: leave both for the Phase 10 quality gate.

**Q5 – Windows reserved-port flake (client, test-only): which track?**
- **A (recommended):** take it in SEC (C1, §5.2 #1-2). It is root-caused (§4.3 #4), test-only, touches only `internal/voice/testdata/server_fixture.go` plus a new `internal/voice/fixture_ports_test.go`, and SEC's own client PR runs the Windows integration leg it destabilises.
- B: assign it elsewhere, as the umbrella did for the other flakes (§9: `TestTXWriteFrameNeverBlocks` → CL2, TLS-timing → CL1). The natural owner is CL2 (it owns `internal/voice` tidy-up), but CL2 may already be past design; otherwise a small standalone PR.
- C: leave it; re-run flaky Windows jobs by hand.

**Q6 – x/net v0.62.0 (GO-2026-6866/6860, unreachable, not cleared by UP).**
- **A (recommended):** take v0.62.0 in **both** repos in SEC. Within the approved `≥ v0.60.0` (`umbrella:441`); the server's forced x/crypto/x/sys/x/text moves are listed in §6.1 for approval. Clears the module-level findings and pre-empts the Dependabot alerts that will open once GitHub reviews the two GHSAs. Cost: one more `go get` per repo and a re-run of the suites; v0.62.0 is one day old.
- B: don't take it; the weekly go-deps workflow picks it up. govulncheck stays at 0 reachable either way, but "close every open Dependabot alert" can fail within days of merge, and the umbrella exemption (`umbrella:316`) does not literally cover it (it requires UP to have cleared the alert).

## 10. Out of scope

- x/net v0.62.0, only if Q6 = B (the weekly go-deps workflow then handles it).
- The bcrypt cap, only if Q2 = D; the port flake, only if Q5 ≠ A.
- A gin bump (v1.12.0 exists; gin v1.11.0 builds against quic-go v0.64.1).
- Any change to the client's password hashing (CLAUDE.md "do not fix"; `umbrella:374`).
- A config env-expansion feature to replace the S2068 dev passwords.
- `--ignore-scripts` in the build Taskfiles (D-d).
- Server binds `:0` and reports its voice port (would fix the flake cross-repo; the server logs the configured address, not the bound port, `voice/server.go:185`).
- A bcrypt concurrency limit, unless Q2 = C.
- The distroless `:nonroot` switch, unless Q4 = B.
- Code smells, including those from UP's `wails-update-check.sh`; the Phase 10 SonarCloud quality gate and React Doctor.
- Removing `vite-plugin-typed-css-modules`.
- Secret-scanning remediation (only the check, §8 #3, is recorded).

## Review notes (2026-10-28, revision 2)

A critique of revision 1 raised 13 points. Each was judged and, where a claim changed, re-verified against `origin/main` (client `b80b0e6`, server `df8d9f3`) or a fresh probe (§4.5). The critique's "claims that hold" list was not re-litigated.

| # | Point | Verdict | Change |
|---|---|---|---|
| 1 | Port-flake tests under `testdata` never run | **Accepted.** `server_fixture.go:4-7` confirms `./...` skips `testdata`; `integration_test.go:18` imports the package. | Tests move to `internal/voice/fixture_ports_test.go`; helpers exported (C1, §5.2 #1). Chose the package location over an extra CI step: no CI change. |
| 2 | bcrypt red tests can't go red / hang | **Accepted.** (b) passes today; a cost-31 compare before the fix runs ~21 h and the job's `timeout-minutes: 10` (`server test.yml:17`) kills it. | Red tests use forged cost-13 (GuestLogin message) and cost-14 (`CheckPasswordHash` < 50 ms, ~11× margin); malformed-hash case is also red; (b) relabelled a guard; cost-31 is after-fix only (§5.1 #1). The "No coalition found…" text is at `auth_service.go:496-500`. |
| 3 | `--ignore-scripts` not verified for `npm run build` | **Accepted, now verified.** | §4.5 #1: `npm ci --ignore-scripts && npm run test && npm run build` with v3.0.0 bindings, all exit 0, 594 tests, `check-dist` passes. The earlier 459-test count is superseded. |
| 4 | govulncheck placement underspecified | **Accepted.** | All three client legs, after the placeholder (`test.yml:253-256`); server timing measured (37 s cold vs 2 m 15 s job, budget 10 min → no timeout change); Windows/GUI server gap recorded (R13). New finding: through `go run` a hit exits 1, not 3 (A7, D5). |
| 5 | Scope creep presented as decisions | **Accepted.** | D6/D7 marked proposed; Q2-D (defer) and Q5 (port flake: SEC or elsewhere) added; header and §1 reworded. Recommendations unchanged (A, A). |
| 6 | D4 misreads the umbrella; x/net is a question | **Accepted.** `umbrella:316` is conjunctive ("unreachable **and** UP clears"); v0.62.0 is within `umbrella:441`. Re-verified: both advisories have GHSAs (`unreviewed`), so Dependabot alerts are pending. | D4 rewritten; Q6 added (recommend take it); A6/A6b, §6.1, R10, R14, §8 #8. Server forced moves derived from x/net v0.62.0's go.mod, untested. |
| 7 | Q4-B check misses `/certs` | **Accepted.** `docker-compose.dev.yml:38` (writable, auto-generated on first run). | §8 #7. |
| 8 | Umbrella SEC row goes stale | **Accepted.** `umbrella:365` vs scope item 4 `umbrella:309`. | New §6.5 lists the corrections for DOC / user approval, incl. the wire-visible message change (§6.3 before/after now states it). |
| 9 | S4036 test near-tautological, wrong file | **Accepted.** `permission_darwin_test.go:1` is `darwin && cgo`; `…_nocgo_test.go:1` is `darwin && !cgo`. | D-a and §5.2 #3: assertion in `permission_darwin_test.go`, stated as a compile-level guard only. |
| 10 | Pin bump moves more than SEC | **Accepted** (risk already covered by §4.2). | D10, §5.2 #7, §6.2: comment describes the `850d984` → SEC-merge range. |
| 11 | Citation fixes | **Accepted.** Log line is `voice/server.go:185`. C# cost 12 is PR #253 head `e8fc8c8`, `VcsClientSyncHandler.cs:140`, unmerged. | §10, B4. |
| 12 | R3 left to a non-existent owner | **Accepted and settled.** C1–C12 at SV1 design `:207-218`, C13–C17 at SV2 design `:195-199` (server repo). No row concerns malformed paths. | R3 rewritten; §4.5 #6. |
| 13 | Q2b seam left to the plan | **Accepted.** | §5.1 #1 (e): package-level `var comparePassword = utils.CheckPasswordHash`, blocking stub, `settingsState.Lock()` must acquire within 100 ms. |

Probe hygiene: `probe-secnpm` and `probe-secvt` were detached worktrees under the scratchpad, removed with `git worktree remove --force` and pruned; the scratch `GOCACHE` and `GOBIN` were deleted. Neither repo nor any existing worktree was modified (`git status` clean in both).

## User decisions (2026-10-28)

All recorded design questions were answered by the user on 2026-10-28. Design approval is still pending.

- SEC Q1 = A: server grpc v1.85.0 and quic-go v0.64.1, plus the moves they force (protobuf v1.36.12, qpack v0.6.0, genproto rpc). This re-approves grpc above the old v1.83.1 floor.
- SEC Q2 = A: cap the accepted bcrypt cost at 12.
  - Over-cost or malformed hashes are rejected with a Success:false body ("Invalid password hash …") and a WARN log.
  - The compare runs outside the settings lock.
  - A client guard keeps its own cost at or below 12.
  - SV7's README documents the cap.
- SEC Q3 = A: remove the unused dev dependency typed-scss-modules, which closes esbuild alert #2.
- SEC Q4 = A: SHA-pin the 7 actions in the server release.yml (S7637). Mark S6471 (container runs as root) safe for now. The image does not move to nonroot.
- SEC Q5 = A: fix the Windows reserved-port flake in SEC. This is a test-only change.
- SEC Q6 = A: x/net v0.62.0 in both repos, plus the moves it forces on the server (x/crypto v0.58.0, x/sys v0.49.0, x/text v0.43.0, x/term v0.47.0).
- SV7 Q1 = A: fix the two wrong deploy docs, docs only. Drop no :ro mount.
- SV7 Q2 = A: document today's TLS key-loader behaviour (PKCS#1 RSA only, the OpenSSL conversion command, no ECDSA). The code fix is recorded as a follow-up.
- SV7 Q4 = A: fix the Taskfile build tags in their own commit. Change only the release-build lines: Linux :121/:123 and Darwin :75/:79.
