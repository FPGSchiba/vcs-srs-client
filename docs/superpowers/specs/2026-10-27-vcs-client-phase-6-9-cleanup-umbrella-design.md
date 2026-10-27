# Phase 6.9 — Cleanup & Improvements (umbrella)

**Status:** draft — awaiting approval
**Date:** 2026-10-27
**Repos:** `vcs-srs-client` (this repo) and `vcs-srs-server` (CLAUDE.md calls it `vngd-srs-server`)
**Slot:** between Phase 7.3 (done) and Phase 7.4 (not started). The number 6.9 is the user's; the phase runs now, after 7.3.

---

## 1. Intent

Take a step back before building Phase 7.4 on top of the current code, and fix what is broken or rotting in **both** repos so that the server, not just the client, is left in a usable state. The user's words: *"I would like to fix all issues we found, so we leave the server in a usable state as well … if it's mainly server work I am fine with that."*

The user reviewed the full suggested-fix list and said **"leave everything in"**: every finding below is in scope, including the items flagged as needing their call (Welcome-screen changes, dependency removals, the Wails v3 GA upgrade, distributed-node fixes).

**Success:**
- A VCS client stays usable against a real server for longer than 10 minutes (today it does not — SRV-01).
- The server cannot be crashed or deadlocked by the paths found here.
- Server CI is green and safe.
- The client's connect path cannot hang.
- The docs describe the code as it is.

**Anti-goals:**
- Calling a behaviour change "cleanup". Anything a user or the wire observes is listed in §5 and called out in its track's PR.
- Touching anything CLAUDE.md marks *do not fix* beyond the explicit allowances in §6.
- Claiming hardware verification. Nothing here can be heard or seen on real hardware in this environment.

## 2. Where the findings came from

A Stage-1 read-only scan (6 lenses: client Go, client frontend, server, client↔server contract, build/CI, docs drift) produced 102 raw findings. These were deduplicated to **85**: 32 confirmed against code, 16 partially confirmed, and 37 not checked (mostly low severity). The full record, with evidence and file:line references, is in the scan output. Finding IDs (`SRV-01`, `GO-S5`, `FE01`, …) are used throughout.

**Rule for unchecked findings:** a track's design step must re-verify every `not-checked` / `partially-confirmed` finding it owns against current code before planning it. A finding that does not hold up is dropped from that track and recorded in the track's design doc as refuted, with the reason. gopls diagnostics are not evidence (CLAUDE.md); only `go build` / `go vet` / tests are.

Baseline at scan time (2026-10-27):
- Client: `go build`/`go vet`/`go test -race` (`-tags purego`) green; `vitest` 498/498 green; `tsc --noEmit` green.
- Server: `go build` and `go test -race` green; **`go vet` fails** (`srs/srs_service.go:590` unreachable code); **CI on `main` is red** (`wails3@latest` requires Go 1.26, workflows pin 1.25).

**Seeded items:**

| Seed | Outcome |
|---|---|
| S1 — Windows release builds ship with no audio | Already fixed in code (CGO on, guarded). Docs only → DOC. |
| S2 — `release.yml` never runs `buf generate` | Already fixed. Docs only → DOC. |
| S3 — server `version == "0.1.0"` | Already fixed (semver floor). Docs only → DOC. |
| S4 — Windows CI leg never run | Runs green on real runners now. Docs only → DOC. |
| S5 — post-dial auth on `context.Background()` | **Confirmed.** Reconnect has the same shape → CL1. |
| S6 — stale CLAUDE.md | **Confirmed**, and wider than seeded → DOC. |

The server's previously uncommitted local edits were committed separately at the start of this phase as `vcs-srs-server` PR #223 (`chore/commit-local-edits`). That PR also fixes **CI-11** (the Taskfile proto output names and gqlgen ordering). **All server tracks branch from `main` after #223 merges.**

## 3. Tracks

There are 12 tracks. Each is one spec → plan → execute cycle with its own worktree and branch (`chore/cleanup-<track>`) in each repo it touches. Every track follows TDD: a failing test first for every bug fix.

Verification per track:
- **Client:** `go build ./...`, `go vet ./...`, `go test -race -count=1 ./...` (all with `-tags purego`); in `frontend/`: `npx vitest run`, `npx tsc --noEmit`, `npm run build`.
- **Server:** `go build -tags headless ./...`, `go vet -tags headless ./...`, `go test -race -count=1 -tags headless ./...`.

Approval gates (CLAUDE.md) are hard stops at **plan approval**. The user's "leave everything in" approves the *scope*. It does not pre-approve a specific dependency version, workflow edit or wire change, which the user sees and approves in each track's plan.

### SV1 — Server session liveness & reaping
- **Repo:** server
- **Visible:** yes (wire + user)
- **Scope:**
  - **SRV-01:** refresh `ClientState.LastUpdate` on `Ping`, `SyncClient` and `UpdateRadioInfo`, so a pinging client is never reaped. Test: a ping-only client survives past `staleAfter`.
  - **SRV-02:** one removal helper used by every removal path (reaper, stream end after a **120 s** grace period, Kick, Disconnect). It deletes the client from both maps, drops the voice binding, cancels the stream and publishes `ClientLeft`.
    - **The 120 s grace period is the user's decision.** It clears the ~70 s gRPC transport floor plus a manual Reconnect click, so a normal network blip still reconnects with the stored token. After 120 s, Reconnect gets `Unauthenticated` and the user logs in again (CL1).
    - `srs.SimpleRadioServer` has no handle on `voice.Server` today; that wiring lives in `app`. The helper gets the voice side through an interface injected from `app`.
    - **In distributed mode, the helper uses the existing `kick_client` ControlMessage in `control.proto` to drop the binding on remote voice nodes. No proto change.** *(Orchestrator's call.)*
  - **SRV-10:** Kick/Disconnect are enforced through that helper.
    - **"Revoking the token" means the SRV-06 check: a JWT whose GUID is no longer in `serverState.Clients` is rejected. There is no token denylist.** A kicked client may log in again; a ban remains enforced by the existing ban path. *(Orchestrator's call: tokens are stateless JWTs, and a denylist would be new infrastructure.)*
  - **CON-05:** a duplicate `SubscribeToUpdates` while the old stream's cleanup is pending is resolved server-side. The design step picks between *last subscriber wins* and `codes.AlreadyExists`; the choice is a wire-visible decision and goes to the user.
  - **SRV-06 (server half):** the auth interceptor rejects a valid JWT whose GUID is no longer in `serverState.Clients`, returning `codes.Unauthenticated`.
- **Out of scope:** the client's reaction (→ CL1); token refresh (→ PROTO_GAPS, see SV3).
- **Files:** `srs/srs_service.go`, `srs/srs_service_test.go`, `control/server.go` (interceptor), `app/clients.go`, `voice/server.go` (binding drop), `state/server.go`.
- **Gates:** server-behaviour, cross-repo.
- **Depends on:** SV3 (shares `srs_service.go`, `control/server.go`, and SV3 introduces `Unauthenticated` codes).

### SV2 — Server state & event correctness
- **Repo:** server
- **Visible:** yes
- **Scope:**
  - **SRV-08:** a subscriber whose buffer overflows is logged and has its stream closed, forcing a resubscribe and `SyncClient` instead of silently diverging. Enlarge the buffer.
  - **SRV-09:** event snapshots deep-copy `ClientState`/`RadioState`.
  - **SRV-15:** the `SyncClient` roster carries `UnitId`/`RoleId`, and `SyncClient` stops publishing a nil-GUID `CLIENT_INFO_UPDATE`.
  - **SRV-16:** guests can call `UpdateClientInfo` (no-op role), and the RPC validates first, then applies atomically and publishes.
- **Files:** `events/bus.go`, `srs/srs_service.go`, `srs/srs_service_test.go`, `srs/utils.go`, `app/clients.go`.
- **Gates:** server-behaviour.
- **Depends on:** SV1 (`srs_service.go`).

### SV3 — Server hardening
- **Repo:** server
- **Visible:** partly — SRV-03 and SRV-07 are visible
- **Scope:**
  - **SRV-03:** coalition isolation. Look up the receiver by `clientGuid`, not `senderId`. **Visible:** cross-coalition non-global voice stops being delivered. Table tests: same coalition, cross coalition, global frequency, disabled radio.
  - **SRV-04:** `InitAuth` uses `GetCapabilities()` getters and rejects a nil `Capabilities`; add unary and stream panic-recovery interceptors.
  - **SRV-05:** `control.Server.Start` unlocks and closes its listeners on every listen failure.
  - **SRV-17:** bound `authenticatingClients` (cap + periodic sweep).
  - **SRV-14:** rate-limit the UDP parse-error and banned-IP logs; demote parse failures to WARN.
  - **SRV-19:** delete the unreachable `return nil` (`srs_service.go:590`) and fix the keepalive comment, so `go vet` is green.
  - **SRV-07:** unary auth failures return `codes.Unauthenticated`, and the auth RPCs return either a body or an error, never both. **Wire-visible.**
    - **The user's decision (revised 2026-10-27, after SV3's re-verification corrected this spec): bodies for auth RPCs, status codes elsewhere.**
    - **The correction.** This spec first claimed that every auth failure returns both a body and an error, so gRPC drops the body and clients only ever see an error. That is true for only **6** sites (`srs/auth_service.go:284,360,411,620,704,788`). About **30** other failure sites, including ban, guest-disabled and wrong-password, return `Success:false` + `ErrorMessage` with a nil error today, and the C# peer reads those bodies.
    - **The decision:**
      - `AuthService` failures return `Success:false` + `ErrorMessage` with a **nil** error. Only the 6 outlier sites change, to match the other ~30.
      - The **auth interceptor** on non-auth RPCs (expired, invalid or unknown-GUID token; SRV-06) returns proper status codes, `codes.Unauthenticated` (and `PermissionDenied` for an insufficient role), instead of `fmt.Errorf`'s `Unknown`. This is the signal CL1's "session expired" handling uses.
      - The recovery interceptor (SRV-04) returns `Internal`, and a nil `Capabilities` gets `Success:false` with a message. File a token-refresh RPC as a new PROTO_GAPS entry (the doc edit is owned by DOC; SV3 hands DOC the text).
  - **SRV-20:** the missing tests on client-critical paths are written here and in SV1/SV2 as part of each fix, not as a separate track.
- **Files:** `state/server.go`, `state/state_test.go`, `srs/auth_service.go`, `srs/auth_service_test.go`, `srs/srs_service.go`, `control/server.go`, `control/server_test.go`, `voice/server.go`.
- **Gates:** server-behaviour.
- **Depends on:** #223 merged. **Runs first** among the server code tracks: it is small and makes the server safe to run.

### SV4 — Server voice path & distributed node
- **Repo:** server
- **Visible:** yes
- **Scope:**
  - **SRV-13:** replace the goroutine per packet and per recipient with a per-sender worker (or inline handling), so frames are no longer reordered and goroutines are bounded. **Visible:** voice frame ordering.
  - **SRV-11:** a distributed voice node advertises its configured UDP port and public address and refuses to register without `PublicAddr`.
  - **SRV-12:** single-flight `VoiceControlClient` reconnection, with per-connection contexts for heartbeat/receive and mutex-guarded shared fields.
- **Out of scope:** any client-side distributed-mode work (Phase 9).
- **Files:** `voice/server.go`, `voiceontrol/client.go`, `voiceontrol/client_test.go`, `voiceontrol/service.go`.
- **Gates:** server-behaviour.
- **Depends on:** SV1 (`voice/server.go`).

### SV5 — Server CI & build
- **Repo:** server
- **Visible:** no (SRV-21 changes how the Windows headless binary launches)
- **Scope:**
  - **SRV-18 (security):** `test.yml` moves from `pull_request_target` to `pull_request` + `push: [main]`, with `permissions: contents: read`. The Codecov upload becomes tokenless/OIDC or a separate `workflow_run` job. Add `-race`.
  - **CI-02:** delete the unused "Install Wails" steps and use `go-version-file: go.mod`, so CI goes green.
  - **CI-03 (server half):** tests run on push to `main`.
  - **CI-10:** pin `protoc`, `protoc-gen-go` (derived from `go.mod`) and `protoc-gen-go-grpc`.
  - **Go 1.27 (the user's decision):** the server's `go.mod` `go` directive, the workflows (`go-version-file: go.mod`) and the Dockerfile all move to Go 1.27. Today they disagree: 1.25, `'1.25'` and `golang:1.27`. This is a **dependency** gate, approved in scope; the exact patch version is approved in the plan.
  - **SRV-19 (CI half):** add `go vet` to `test.yml` *after* SV3 makes it pass; until then, it lands as a follow-up commit.
  - **SRV-21:** drop `-H windowsgui` from the headless Windows target.
  - **CI-06 (server half):** make Dependabot Go updates work with gitignored generated code.
- **Files:** `.github/workflows/test.yml`, `.github/workflows/release.yml`, `.github/dependabot.yml` (+ any new workflow), `Dockerfile`, `build/Taskfile.yml`, `build/windows/Taskfile.yml`.
- **Gates:** **ci-cd**, cross-repo.
- **Depends on:** #223 merged. **Merges first** of all server tracks, so every later server PR runs on green, safe CI.

### SV7 — Server docs
- **Repo:** server
- **Scope:** **DOC-15:** the server README gains a *Client connections* section (client-facing `clientTLS`, the client version floor) and corrected build commands.
- **Files:** `README.md`.
- **Gates:** cross-repo.
- **Depends on:** SV1–SV5 (it documents their result). Runs in the final wave with DOC.

*(There is no SV6. Server test coverage is folded into SV1–SV3; see §7.)*

### CL1 — Client connect & session robustness
- **Repo:** client
- **Visible:** yes
- **Scope:**
  - **GO-S5:** bound the post-dial `InitAuth` → `GuestLogin` → `SyncClient` → subscribe sequence in both `Connect` and `Reconnect` with a timeout. On timeout, close the conn and return an actionable "server did not respond" error.
  - **GO-03:** per-call timeouts on `Disconnect`, `UpdateRadioInfo` and the persisted-radio push; local teardown proceeds regardless.
  - **GO-01:** `disconnected` is published on every failed Connect.
  - **GO-02:** a second Connect (or Connect racing Reconnect) closes the old conn/stream first.
  - **GO-10:** Disconnect clears `lastDialer`/`lastToken`.
  - **CON-07:** `Store.ResetRoster()` on Disconnect and before re-hydrating.
  - **SRV-06 / SRV-07 (client half):** `codes.Unauthenticated`, or an empty `voice_secret` from Reconnect's `SyncClient`, is a failed reconnect that routes to Welcome with a "session expired" message. That happens, for example, more than 120 s after the stream ended (SV1). Login failures keep using the existing `GetSuccess()`/`GetErrorMessage()` body handling (SV3 keeps auth failures as bodies). Non-auth RPCs map the interceptor's `Unauthenticated`/`PermissionDenied` codes to the session-expired path. The client must also cope with CON-05's server-side behaviour as decided in SV1.
  - **Welcome-screen changes (approved in scope by the user, exact copy confirmed at plan approval):**
    - a **CANCEL** action in the "establishing tactical link" panel (aborts the bounded sequence);
    - **FE06:** the FFID label no longer says "(optional)" for a field that rejects blank;
    - **FE18:** Enter submits, and the last server address is remembered (**never** the password);
    - **FE14 (Welcome half):** the login error is a live region (`role="alert"`).
  - The CLAUDE.md rule still holds: the two-stage welcome UX and the disabled SSO button are untouched; the changes keep the prototype's classNames and layout.
- **Files:** `internal/app/bindings.go`, `internal/app/profiles.go`, `internal/session/session.go` (+ tests), `internal/control/client.go`, `internal/state/store.go` (+ test), `internal/config/*` (server-address persistence), `frontend/src/windows/main/screens/Welcome.tsx` (+ test).
- **Gates:** none beyond user-visible UI (approved in scope).
- **Depends on:** SV1 and SV3 merged (the `Unauthenticated` semantics and the duplicate-subscribe decision are what CL1 reacts to); CL2 merged (shares `bindings.go`, `profiles.go`, `session.go`, `control/client.go`).

### CL2 — Client backend tidy
- **Repo:** client
- **Visible:** no
- **Scope:**
  - **GO-04:** main.go-only wiring methods move off the bound `App` service to package-level functions; diff the regenerated bindings to prove nothing the frontend uses disappears.
  - **GO-05:** `config.Save` disk I/O happens outside `sb.mu` (snapshot → save under `writeMu` → commit).
  - **GO-06:** the `RefreshKeybinds` store observer no longer blocks; a coalescing worker applies. **Constraint:** the `hotkeys:state` emission rate is unchanged (CLAUDE.md "chatty emitters" note).
  - **GO-07:** `windows.json` save errors are logged at WARN, the save happens outside `r.mu`, and the tmp file is removed on rename failure.
  - **GO-08:** delete dead control RPC wrappers, the never-emitted `state:client_state`, the unsubscribed events and the superseded helpers (keep anything CL1 needs).
  - **GO-09:** `joystick.Manager.Apply` drops its impossible error.
- **Files:** `main.go`, `internal/app/{app,audio,bindings,profiles,settings,tray,voice,windows}.go`, `internal/audio/{effect,manager}.go`, `internal/control/{client,stream}.go`, `internal/events/events.go`, `internal/joystick/manager.go`, `internal/session/session.go`, `internal/windowstate/store.go`, `frontend/src/shared/api/events.ts`.
- **Gates:** none (the binding-surface change is internal; regenerated bindings are gitignored).
- **Depends on:** nothing. Wave 1.

### CL3 — Frontend correctness
- **Repo:** client
- **Visible:** yes
- **Scope:**
  - **FE01:** the Comms popout follows backend selection changes (`radio.<n>.select`, profile load).
  - **FE02:** the Comms popout follows `control:connection` / `auth:session_changed`, ideally via a shared `useClientStateSync` hook.
  - **FE08:** one stale-hydrate guard pattern for every hydrate-then-subscribe hook.
  - **FE09:** user-initiated backend calls surface failures and roll back optimistic UI.
  - **FE05:** knob/VOX drags hold a local draft and commit on release (or debounce).
  - **FE15:** Knob pointer listeners are effect-scoped with `pointercancel` and unmount cleanup.
  - **FE04:** NavRail, Logout, the Settings section rail and the Home Comms tile are keyboard-operable with `aria-current`.
  - **FE14 (toast half):** toasts are a live region.
- **Files:** `frontend/src/shared/components/{Knob,NavRail,ToastHost}.tsx`, `frontend/src/shared/store/{useNotificationsSync,useSettingsSync}.ts`, `frontend/src/windows/comms/{CommsApp,RadioCard}.tsx` (+ test), `frontend/src/windows/main/screens/{History,Home}.tsx`, `frontend/src/windows/main/screens/settings/{SettingsScreen.tsx,sections/{Audio,Effects,General}.tsx}`.
- **Gates:** none.
- **Depends on:** CL4 merged (shares `CommsApp.tsx`, `SettingsScreen.tsx`).

### CL4 — Frontend cleanup & stack trim
- **Repo:** client
- **Visible:** FE07 only (Settings stub text); FE12 must be visually neutral
- **Scope:**
  - **FE07:** Settings stubs stop advertising shipped phases; Profiles points at the Profiles screen.
  - **FE10:** delete dead components (`Placeholder`, `Pill`, `StatusPill`, `isOpen`, the write-only `session.server`/`error`, unreachable `KeyChip` branches).
  - **FE11:** remove `@tanstack/react-query`, `@tanstack/react-router` and `zod` (declared, never imported). **Dependency change.**
  - **FE12:** remove Tailwind **entirely** (package, config, `@import "tailwindcss"`, preflight). Its `.flex` collides with the design's `.flex` helper. **Dependency/stack change**, which also changes the stack line in CLAUDE.md and the master spec, so DOC records it as a deliberate deviation. *(Orchestrator's calls follow.)*
    - **The baseline is the current app as rendered today, not the prototype.** The removal must not change rendering.
    - About 10 elements use `className="flex …"` and get `display:flex` from Tailwind on top of `components.css`'s `.flex { flex:1; min-width:0 }`. Each one gets an explicit `display:flex` from a non-colliding class.
    - Preflight's resets (margins, button and heading defaults) that the app relies on move into `global.css`.
    - **Evidence before merge:** before/after screenshots of every screen and popout from the Vite build in a browser, attached to the PR. The real Wails webview check stays on the manual checklist.
  - **FE13:** `api/client.ts` aliases generated binding types instead of hand-declared DTOs and `as Promise<T>` casts.
  - **FE16:** a `.popout.native` modifier or `PopoutShell` replaces the duplicated 9-property inline style; delete the dead `.client` CSS.
  - **FE19:** fix the stale TopBar comment.
- **Files:** `frontend/package.json`, `frontend/package-lock.json`, `frontend/tailwind.config.ts` (and PostCSS/Vite config if Tailwind is wired there), `frontend/src/shared/api/client.ts`, `frontend/src/shared/components/{KeyChip,Pill,Placeholder,StatusPill,TopBar}.tsx`, `frontend/src/shared/store/{session,windows}.ts`, `frontend/src/shared/styles/{components,global}.css`, `frontend/src/windows/comms/CommsApp.tsx`, `frontend/src/windows/main/screens/settings/{SettingsScreen.tsx,sections/Deferred.tsx}`, `frontend/src/windows/notifications/NotificationsApp.tsx`.
- **Gates:** **dependency**, **architecture** (stack trim vs the master spec).
- **Depends on:** nothing. Wave 1.
- **Overlap:** this phase does part of the Phase 10 "code-quality pass before v1". The Phase 10 TODO (React Doctor, SonarCloud) stays in Phase 10 and is *not* run here.

### CL5 — Client build, CI & release
- **Repo:** client (CI-06 also touches the server, see below)
- **Visible:** CI-05 (the BUILD field shows a real revision) and CI-07 (exe metadata)
- **Scope:**
  - **CI-04 + Go 1.27 (the user's decision):** the client's `go.mod` `go` directive and the workflows move to Go 1.27, using a patched release, the same as the server (SV5). This is done in Wave 1 so that UP does not re-edit them later (Wails v3 GA needs Go ≥ 1.26).
  - **CI-05:** production builds keep `vcs.revision` (drop `-buildvcs=false` or inject via `-ldflags -X`).
  - **CI-07:** the Windows version resource and manifest identity are rendered, not template placeholders.
  - **CI-08 (the user's decision on supported packages):** **Windows NSIS installer**, **Linux AppImage** and **macOS `.app` bundle**. Their packaging tasks are fixed to point at real paths. The **deb, rpm, AUR and docker cross-build tasks are deleted**; the deleted paths are listed in the plan. Code signing and notarisation stay in Phase 10, so the macOS `.app` is unsigned.
  - **CI-09:** the release publishes those three artefacts plus `SHA256SUMS`, exercised once with a pre-release tag. **A tag push is outward-facing: the user confirms it at that moment.**
  - **CI-12:** release stops repeating work (`npm ci` only, a single bindings/frontend build, `go mod tidy -diff`).
  - **CI-13:** delete stale Wails v2/v3 migration leftovers after salvaging what CI-08 needs.
  - **CI-14:** top-level `contents: read`, write only on the release job, third-party actions SHA-pinned.
  - **CI-16:** fix the stale comment.
  - **CI-03 (client half):** client CI pins the server ref it integration-tests against.
  - **CI-06 (client half):** fix Dependabot Go updates with gitignored generated code. The mechanism is chosen once and applied to both repos (server half in SV5).
- **Files:** `.github/workflows/{test,release}.yml`, `.github/dependabot.yml` (+ any new workflow), `build/Taskfile.yml`, `build/{darwin,linux,windows}/Taskfile.yml`, `build/windows/{info.json,wails.exe.manifest,installer/*}`, `go.mod` (toolchain line only), `internal/version/version.go`, root leftovers (`linux/`, `config.yml`, `appicon.icon/`, `appicon.png`, `frontend/package.json.md5`).
- **Gates:** **ci-cd**, **dependency** (toolchain), destructive-ish (deleting tracked files — listed in the plan, approved there).
- **Depends on:** nothing. Wave 1.

### UP — Wails v3 GA upgrade (both repos)
- **Repo:** both
- **Visible:** potentially, which is why it gets its own track
- **Scope:** **CI-15:** move the client from `v3.0.0-beta.22` and the server from `alpha.43` to Wails v3.0.0 GA. GA requires Go ≥ 1.26, which is already met by Wave 1's move to 1.27. Derive the `wails3` CLI version from `go.mod` instead of duplicating it in workflows. Upgrade the `@wailsio/runtime` npm package in step. Regenerate bindings and fix the breakage. The design step reads the beta→GA changelog first and lists every breaking change before planning.
- **Out of scope:** adopting new GA features.
- **Files:** `go.mod`/`go.sum` and `frontend/package*.json` in both repos, workflows, plus whatever the breakage list names (likely `main.go`, `internal/app/*`, window/tray setup).
- **Gates:** **dependency**, **ci-cd**, cross-repo.
- **Depends on:** **all other code tracks merged.** It touches nearly everything, so it runs alone.

### DOC — Docs drift (client), runs last
- **Repo:** client
- **Scope:**
  - **FE20 / S6:** CLAUDE.md status rows: 7.3 marked done with its own row; Phase 6.9 row added; `make proto` → `task proto` / `buf generate`; design paths gain `project/`.
  - **DOC-03:** Phase 5's "four issues" marked resolved with dates (S1–S4) in CLAUDE.md and ROADMAP; keep the `protoc-gen-go-grpc` drift note if it is still true.
  - **DOC-04:** the stack line is accurate (React 19; no shadcn; TanStack/Tailwind per CL4's outcome), with a dated correction note in the master spec.
  - **DOC-08:** CLAUDE.md states the `-tags purego` rule.
  - **DOC-10:** the generated-artifacts list matches reality.
  - **DOC-11:** a real README.
  - **DOC-12:** fix the SFX history wording only. No asset change; the "no placeholder WAVs" rule is untouched.
  - **DOC-13 / DOC-14:** ROADMAP uses symbol references instead of line numbers, and `superpowers:brainstorming` instead of `/brainstorm`.
  - **DOC-06 / DOC-07:** spec Status headers and the master-spec risk table/CI section get dated correction notes.
  - **CON-11:** document that the client advertises STANDALONE only (ROADMAP Phase 9).
  - **CON-12** plus SV3's new token-refresh gap: PROTO_GAPS "Today:" lines corrected, and #7/#8 rewritten.
  - **GO-11:** fix the voice comments to match what Reconnect actually does (comment fix only; making Reconnect start voice would be a behaviour change and is not in scope).
  - Final: ROADMAP Phase 6.9 entry `[x]`, CLAUDE.md "Current status", and the manual-verification checklist (§8).
- **Files:** `CLAUDE.md`, `README.md`, `docs/ROADMAP.md`, `docs/PROTO_GAPS.md`, `docs/superpowers/specs/*.md`, comments in `internal/app/{bindings,voice}.go`.
- **Gates:** none.
- **Depends on:** everything (it documents the end state). Runs with SV7 in the final wave.

## 4. Conflict matrix & schedule

The file overlaps that force serialisation, computed from each finding's touched files:

| | SV1 | SV2 | SV3 | SV4 | SV5 | CL1 | CL2 | CL3 | CL4 | CL5 | UP | DOC |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **SV1** | | srs_service | srs_service, control/server | voice/server | | *(semantic)* | | | | | all | |
| **SV2** | | | srs_service | | | | | | | | all | |
| **SV3** | | | | voice/server | *(vet gate)* | *(semantic)* | | | | | all | PROTO_GAPS |
| **CL1** | | | | | | | bindings, profiles, session, control/client | | | | all | bindings |
| **CL2** | | | | | | | | | | | all | bindings, voice |
| **CL3** | | | | | | | | | CommsApp, SettingsScreen | | all | |
| **CL5** | | | | | | | | | | | workflows, go.mod | |

*"all"*: UP touches go.mod, the bindings and the workflows of both repos, so it conflicts with every code track. *"semantic"*: no shared file, but CL1 reacts to wire behaviour that SV1/SV3 define.

**Waves** (tracks within a wave run concurrently in separate worktrees; a wave starts when its dependencies have **merged**):

| Wave | Tracks | Notes |
|---|---|---|
| 0 | server PR #223 | Already open. Merge before any server track branches. |
| 1 | **SV5**, **SV3**, **CL2**, **CL4**, **CL5** | Disjoint files. SV5 merges first of the server tracks, so later server PRs get green CI. |
| 2 | **SV1** (after SV3), **CL3** (after CL4) | |
| 3 | **SV2**, **SV4** (after SV1), **CL1** (after SV1 + SV3 + CL2) | SV2 and SV4 don't share files. |
| 4 | **UP** | Alone. |
| 5 | **DOC**, **SV7** | Final state documented. |

**Merges:** one track at a time into `main` of its repo, via PR. Re-run that repo's full suite on `main` after each merge. There are no direct pushes to `main` and no force-pushes. Each track gets `superpowers:requesting-code-review` before merge.

## 5. Behaviour changes (not cleanup)

| Track | Change | Who sees it |
|---|---|---|
| SV1 | Live clients no longer evicted at 10 min; dead streams reaped with `ClientLeft`; Kick/Disconnect actually disconnect; stale tokens get `Unauthenticated`; duplicate-subscribe handling | wire, all clients (incl. the C# peer) |
| SV2 | Slow subscribers are disconnected to resync; roster gains UnitId/RoleId; no more nil-GUID broadcast; guests can UpdateClientInfo | wire, all clients |
| SV3 | **Cross-coalition voice stops** (it was a bug, but it is observable); auth errors become `Unauthenticated`; bad `InitAuth` is rejected instead of crashing | wire, all clients |
| SV4 | Voice frames are delivered in order; the distributed node advertises its real port and address | wire |
| SV5 | Windows headless server becomes a console binary | operators |
| CL1 | Connect/Reconnect time out with an error; CANCEL button; FFID label; Enter submits; server address remembered; "session expired" path | user |
| CL3 | Comms popout tracks selection/session correctly; failure messages appear; keyboard navigation | user |
| CL4 | Settings stub text; Tailwind removal (must be visually neutral) | user (text only) |
| CL5 | BUILD shows a real revision; exe metadata; release artefacts | user, operators |
| UP | Whatever GA changes — enumerated in UP's design | potentially user |

The C# peer (`VNGD-SimpleRadioStandalone`) shares the server, so SV1–SV4 change what it sees too. Interop is already blocked on PR #253 (no voice secret), so this cannot be tested here; it is noted on the checklist.

## 6. "Do not fix" notes — how this phase respects them

| CLAUDE.md note | Handling |
|---|---|
| Chatty `hotkeys:state` / `audio:state` emitters | Not touched. GO-06 changes only the *observer's* blocking; emission rate unchanged. |
| Client bcrypt password hashing | Not touched. |
| Opus geometry | Not touched. SV4 changes server-side fan-out scheduling only, not the codec. |
| Disabled SSO button / two-stage Welcome | Not touched. CL1's Welcome changes keep the layout, classNames and disabled button. |
| No placeholder WAVs | Not touched. DOC-12 is a wording fix. |

## 7. Not done as separate work (and why)

No finding is rejected outright — the user kept everything. These are closed or absorbed:

| Finding | Disposition |
|---|---|
| CI-11 | Already fixed by server PR #223. |
| CI-S1, CI-S2, SRV-S3, CI-S4 | Already fixed in code; only the docs are stale → DOC-03. |
| SRV-20 (missing server tests) | Not a track: tests are written test-first inside SV1–SV3. |
| GO-11 "make Reconnect start voice" | Only the comment fix is in scope. The behaviour change was offered as an alternative, never requested. |
| Phase 10 React Doctor / SonarCloud | Stays in Phase 10 (deliberately last, per ROADMAP). |
| Any `not-checked` finding that fails re-verification | Dropped in its track's design doc with the reason (§2 rule). |

## 8. Verification & reporting

- **Automated:** the per-repo suites in §3, green before every PR and again on `main` after every merge. Failing output is reported verbatim, never summarised as "mostly passing".
- **Manual:** none of this can be hardware-verified here. Tracks append items to a new checklist, `docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`, at minimum:
  - a client stays connected and routable against a real server past 10 minutes (SV1);
  - cross-coalition isolation heard with two clients (SV3);
  - Welcome CANCEL and timeout against a server that accepts TLS and then stalls (CL1);
  - the Tailwind removal is visually neutral in a real Wails webview (CL4);
  - the Windows NSIS installer, the Linux AppImage and the unsigned macOS `.app` install and run, and `SHA256SUMS` matches (CL5);
  - the Wails GA build runs on Windows/Linux/macOS (UP);
  - C# peer interop once PR #253 is fixed.
- **Live status table** kept by the orchestrator: track, stage, branch, blockers, waiting-on-user.

## 9. Decisions taken in the umbrella

| Decision | Value | By |
|---|---|---|
| Scope | Every finding in; nothing rejected | user |
| Go version, both repos | **1.27**, set in Wave 1 (SV5, CL5) | user |
| SV1 stream-end grace period | **120 s** | user |
| SRV-07 auth failures | AuthService failures return a **body** (`Success:false`, nil error); interceptor failures return **status codes** (`Unauthenticated`/`PermissionDenied`). Revised after the original "status errors everywhere" answer, which rested on an incorrect claim in this spec. | user |
| Pending-login limits (SRV-17) | 5-min TTL re-armed per step; caps of 10,000 total and 32 per IP; 60s sweep; over the cap → the new InitAuth gets a `Success:false` body. The answer said `ResourceExhausted`; the orchestrator reconciled it with the SRV-07 decision that AuthService failures are bodies. Existing entries are never evicted. | user (reconciled by orchestrator) |
| Dependency-update secret name | `DEPS_BOT_TOKEN` in both repos | orchestrator |
| Go 1.27 breaks gqlgen v0.17.89 (found in execution) | Server: `golang.org/x/tools` v0.42.0 → v0.52.0, and gqlgen runs in-module (`go run github.com/99designs/gqlgen generate`); gqlgen itself unchanged. Client CI: the server is generated and built with the **server's own** Go version (separate `setup-go`). | user |
| SonarCloud S8545 on `go install …buf@v1.47.2` | the user triages it as safe in SonarCloud (same class as `test.yml`'s documented false positive) | user |
| CL4 neutrality proof | must be produced with a connected browser before #41 leaves draft; not waived | user |
| Merging | the orchestrator reports each PR as ready; **the user merges**; the orchestrator re-runs the full suite on `main` after each merge | user |
| `TestTLS_UntrustedCertificateIsRefused` timing flake (seen once during CL4) | assigned to CL1 | orchestrator |
| Go dependency updates (CI-06) | weekly workflow in both repos that opens PRs with a PAT/GitHub App token (user creates it) | user |
| Codecov (server) | tokenless OIDC upload; fork/Dependabot PRs skipped; `CODECOV_TOKEN` retired (user rotates it) | user |
| Tailwind `pulse` collision | design prototype's 40% keyframe wins (the one deliberate visual change in CL4) | user |
| Settings Notifications/Legacy stubs | "Not yet scheduled" | user |
| macOS `.app` | Apple Silicon only | user |
| Windows installer | per-machine (Program Files, UAC) | user |
| Client/server CI ordering | CL5 pins client CI to a server SHA and merges before SV5's Go bump | orchestrator |
| `TestTXWriteFrameNeverBlocks` flake (client `main` red on macOS) | assigned to CL2 | orchestrator |
| Supported release packages | **Windows NSIS, Linux AppImage, macOS `.app`**; deb/rpm/AUR/docker deleted; no signing (Phase 10) | user |
| Token revocation | SRV-06's unknown-GUID check; no denylist; kicked clients may log in again | orchestrator |
| Distributed-mode kick | existing `kick_client` ControlMessage; no proto change | orchestrator |
| Tailwind | removed entirely; baseline is the current rendering; before/after screenshots | orchestrator |

## 10. Left to each track's design step

- The exact timeout values (CL1, GO-03), buffer sizes (SV2) and the cap/TTL for `authenticatingClients` (SV3). Workers propose with a recommendation and do not guess.
- CON-05: last-subscriber-wins vs `AlreadyExists` (user decision, wire-visible).
- CI-06: the dependency-update mechanism (scheduled workflow vs Renovate) — CI/CD decision for the user.
- CL1: the exact FFID label copy and where the remembered server address lives in `config.toml`.
- SV3: the code chosen for each auth failure.
- UP: the list of GA breaking changes.
