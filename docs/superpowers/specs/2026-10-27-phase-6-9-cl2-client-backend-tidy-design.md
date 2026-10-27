# Phase 6.9 / CL2 — Client backend tidy

**Status: draft for approval** (pass 2 — the pass-1 questions are answered and the flaky-test fix added to scope)

**Repo:** vcs-srs-client · **Branch (at execution):** `chore/cleanup-cl2` (fresh worktree from `origin/main`) · **Wave:** 1 · **Visible:** no (user/wire); webview-surface only
**Umbrella:** `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md` §3 CL2
**Baseline verified 2026-10-27 on code identical to origin/main f8ccb43 (Go side):** `go build -tags purego ./...` OK, `go vet -tags purego ./...` OK, `GOOS={windows,linux,darwin} go vet -tags purego ./internal/joystick` OK.

## Decisions

| # | Decision | Value | By |
|---|---|---|---|
| D1 | GO-06 refresh queue | **FIFO, one `applyHotkeys` per local-radio signature change** (not a coalescing mailbox). `hotkeys:state` and `keybinds:changed` emission count and order are unchanged. | user decision (confirmed 2026-10-27) |
| D2 | Added scope: `TestTXWriteFrameNeverBlocks` flake | Fixed in CL2 as the **first** task so client `main` goes green early; make the test robust without weakening what it proves (see §3 TX-FLAKE). | user decision (2026-10-27); assignment by umbrella §9 |
| D3 | Open questions from pass 1 | None remained; the settled worker's calls in §10 stand. | user decision ("no open questions") |
| D4 | Approval gates | None: no dependency, proto, CI, wire, repo-setting, secret or file-deletion change. | umbrella §3 CL2 |

## 1. Intent

Tidy the client backend without changing anything a user or the server can observe:
shrink the webview-bound `App` surface to what the frontend actually calls, take disk I/O
out from under the lock the PTT path takes, stop a store observer blocking the gRPC update
goroutine, stop silently swallowing `windows.json` failures, and delete dead code.

## 2. Re-verification of owned findings

| ID | Status | Evidence (origin/main) |
|---|---|---|
| GO-04 | holds-with-correction | Regenerated bindings (`wails3 generate bindings -ts -f "-tags purego"` into scratchpad `cl2-bindings/`, identical to local `frontend/bindings`): **79 bound methods**; `frontend/src/shared/api/client.ts` (the only frontend file importing bindings) calls **56**. The 23 unused: `SetApp, SetBackend, SetConnHealth, SetSettingsBackend, SetAudioBackend, SetJoystickBackend, SetupTray, SeedBuiltinProfiles, OnMainWindowClose, PlayConnectionSFX, VoiceBridge, VoiceRTT, Store` (main.go wiring), `Pressed, Released, ForceReleased` (hotkey/joystick handler — `hotkeys.New(...,gui)` main.go:228, `joystick.New(...,gui,...)` main.go:262), `RefreshKeybinds` (store observer, app.go:227), `TrayAvailable` (tray.go:45, internal), `ConnHealth` (app.go:183, zero callers), `WailsApp` (app.go:258, zero callers), `GetWindowGeometry`/`SetWindowGeometry` (bindings.go:121/126, zero callers), `DismissNotification` (notifications.go:67). Correction: the finding named only main.go wiring; the handler trio, the observer and the zero-caller accessors are bound too (the trio means webview JS can key PTT). Generator warnings today for `SetBackend` (app.go:154) and `SetSettingsBackend` (app.go:188) interface params vanish after the fix. |
| GO-05 | holds-with-correction | Finding cited 4 sites; there are **11** `config.Save` calls, every one of them under `sb.mu` except `flushConfig` (profiles.go:888): settings.go:276, :388, :1140; voice.go:1061, :1174; profiles.go:181, :474, :535, :636, :715. Correction 2: `sb.mu` is also taken for in-memory-only `cfg` writes **without** `writeMu` (`SetCommsLayout` profiles.go:804-813, `captureCommsWindowSize` :847-857), and `rememberDevice` (settings.go:373-395) saves without `writeMu` and is reachable both under `writeMu` (RefreshKeybinds→applyHotkeys/GetKeybinds→connectedDevices) and outside it. So "snapshot → save under writeMu → `sb.cfg = &next`" as proposed would lose concurrent layout edits and mis-order disk writes; design below adjusts. |
| GO-06 | holds | `RefreshKeybinds` settings.go:1279-1297 takes `sb.writeMu` then runs `applyHotkeys` (OS registration + joystick Apply + `emitHotkeyState`); registered via `a.st.OnRadiosChanged(a.RefreshKeybinds)` app.go:227; store.go:70-72 "observers run synchronously on the mutating goroutine, so an observer must not block". The mutating goroutine includes `ConsumeUpdates` (control/stream.go route→`st.SetRadios`). |
| GO-07 | holds | windows.go:74, :135, :182 `_ = windowstate.Save(r.path, r.geom)` while `r.mu` held; windowstate/store.go:36-49 leaves `windows.json.tmp` behind on rename failure. `Registry` has no logger field. |
| GO-08 | holds-with-correction | `control.Client.UpdateClientInfo` client.go:79-89 and `GetServerSettings` :91-94 — no non-test caller (fake server's implementations in grpctest stay; they satisfy the generated server interface). `EventClientState`/`Tagged.ClientState` events.go:15/:220 never called. `state:settings_update`/`state:server_action` emitted with `struct{}{}` at stream.go:64/:67, `EV.settingsUpdate/serverAction/clientState` events.ts:5-10 have zero subscribers (prod or test). `audio.ClippingPresets` effect.go:83 zero callers; **correction:** `audio.VoicePresets` effect.go:77 is test-only too (effect_test.go:11). `Manager.NotificationAvailable` manager.go:458 zero callers. `SetWindowGeometry` bindings.go:126 zero callers (+`GetWindowGeometry` :121, handled under GO-04). `Session.Control()` session.go:370-375 is called only by reconnect_test.go:25 — **kept** (see §3 GO-08). Also never emitted but **not in this finding**: `EventAuthFlowStep` (Phase 2 SSO; master spec 2026-05-31 line 288 `auth:flow_step`) and `EventWindowGeometry` — left alone, flagged to DOC. |
| GO-09 | holds | joystick/manager.go:252-277 `Apply` returns only `nil`; settings.go:1253 `_ = jm.Apply(joyBinds)`; tests `hat_test.go:166,191` check the error. No interface requires the signature. |

| TX-FLAKE | holds | `TestTXWriteFrameNeverBlocks` (internal/voice/tx_test.go:356, budget assertion at :399) failed on `macos-latest` under `-race`: 1000 `WriteFrame` calls took 10.36 ms against a 10 ms budget (client `main` run 40500422701). |

## 3. Per-finding approach

### TX-FLAKE — `TestTXWriteFrameNeverBlocks` (added scope, D2)
**What the test guards.** `WriteFrame` runs on the DSP goroutine; with `txLoop` parked in the encoder the PCM queue fills, and `flushLocked` must take its `select … default` drop path (count `dropFull`, return) rather than wait. The test proves three things: (1) the queue is bounded (a drop is observed); (2) the full-queue path allocates nothing (`AllocsPerRun == 0`); (3) it is cheap — one 1000-call batch under 10 ms (the DSP tick).
**Root cause.** (3) is a single wall-clock sample on a shared runner under the race detector. Every second call flushes, so the batch is 1000 short critical sections plus 500 atomic adds — the code comment puts the real cost ~30x under budget without `-race`. A single scheduler preemption or GC assist inside the one sample is enough to cross 10 ms; nothing in `WriteFrame` changed. The budget is right; the sampling is wrong.
**Fix (test-only; `tx.go` unchanged).**
- Keep (1) and (2) as they are.
- Add a structural non-blocking proof that has no clock in it: across the measured calls `DroppedFull` rises by **exactly** `calls/2` (each flush took the drop path; a flush that waited would never return, and one that succeeded would not count), and no datagram escapes.
- Run the measured loop on a separate goroutine with a **5 s watchdog**: a blocking `WriteFrame` fails the test as "blocked" instead of hanging until the package timeout. The watchdog is a liveness bound, not a performance bound.
- Replace the single sample with the **minimum over 10 batches of 1000 calls**, still against the unchanged **10 ms** budget. A real regression (an added lock, syscall, log call, wait) raises every batch, so the minimum still catches it; a one-off stall raises one batch and is ignored. A small `minBatch(batches, n int, f func()) time.Duration` helper carries the measurement and gets its own deterministic unit test (one artificially slow batch must not move the result).
**Not weakened:** the 10 ms budget, the zero-allocation check, the bounded-queue check and the escaped-datagram check are all unchanged; the new exact drop count is strictly stronger than before. What is given up is only "every single 1000-call sample on a noisy runner is under 10 ms", which is a property of the runner, not of `WriteFrame`.


### GO-09 — `joystick.Manager.Apply` drops the error
`func (m *Manager) Apply(binds map[string][]Binding)`. Update settings.go:1253 and `hat_test.go:166,191`. Cross-compile `internal/joystick` for windows/linux/darwin.

### GO-07 — windows.json save
- `windowstate.Save`: on rename failure (and on WriteFile failure) `os.Remove(tmp)` best-effort.
- `Registry`: new `saveMu` (disk-order lock, order `saveMu → mu`). `persist()` helper: lock `saveMu`; under `r.mu` copy `r.geom` (maps.Clone); release `r.mu`; `windowstate.Save`; on error `slog.Default().Warn("save windows.json failed", "path", …, "err", …)`. Three call sites use it. Worker's call: `slog.Default()` rather than a new constructor param (main.go installs the default logger; avoids touching `NewRegistry`'s callers), and a package-private `saveFn` seam on Registry for tests.

### GO-08 — dead code
Delete: `control.Client.UpdateClientInfo`, `control.Client.GetServerSettings`; `events.EventClientState`, `EventSettingsUpdate`, `EventServerAction`, `Tagged.ClientState/SettingsUpdate/ServerAction`; the two emits in stream.go (store update `st.SetSettings` for SERVER_SETTINGS_CHANGED **stays**; SERVER_ACTION becomes an explicit no-op case with a comment); `EV.clientState/settingsUpdate/serverAction` in events.ts; `audio.ClippingPresets`, `audio.VoicePresets` (effect_test rewritten against `VoicePresetOptions` ids — `presetIDs` deleted if then unused); `Manager.NotificationAvailable`; `App.GetWindowGeometry/SetWindowGeometry` (the `windowsAPI` interface keeps `Geometry`/`SetGeometry`, used by profiles.go:199,546).
Kept for CL1 (worker's call): `Session.Control()`, `Session.UpdateRadioInfo/Disconnect/Reconnect`, `control.Client.SyncClient/UpdateRadioInfo/Disconnect`, everything in `internal/auth`. `App.DismissNotification` kept — ROADMAP Phase 7.2 records it as deliberate groundwork for 7.4's per-item dismiss (a product decision, not dead code).

### GO-05 — `config.Save` outside `sb.mu`
One helper on `settingsBackend` (worker's call on name/shape):

```go
// persistCfg: mutate must be a pure field-setter (re-applied at commit).
func (sb *settingsBackend) persistCfg(mutate func(*config.Config)) error
```
1. lock `sb.saveMu` (new; lock order **writeMu → saveMu → mu**);
2. under `sb.mu`: `next := *sb.cfg` (deep-copy the map/slice fields the mutation touches), `mutate(&next)`; unlock;
3. `sb.save(sb.cfgPath, &next)` (seam, defaults to `config.Save`; skipped when `cfgPath == ""`) — **no `sb.mu` held**;
4. on success, under `sb.mu`: `cur := *sb.cfg; mutate(&cur); sb.cfg = &cur` — re-applying onto the *current* config so a concurrent `SetCommsLayout`/`captureCommsWindowSize` landing during the write is not clobbered; on failure leave `sb.cfg` untouched (today's contract).
`saveMu` (not `writeMu`) orders disk writes so `rememberDevice`, which can run with or without `writeMu`, is serialised with every other writer. `flushConfig` moves under `saveMu` too. All 11 sites are converted (leaving any would keep a PTT-visible stall); sites with pre-checks under `mu` (`persistSelectedRadioSeq` seq check, `rememberDevice` early-out, the profile snapshot/restore flows in profiles.go) keep the check in step 2 and abort without saving. Profile flows that restore on failure keep their restore logic; only the `config.Save` call moves out of `mu`.

### GO-06 — non-blocking radios observer
- Observer (`onRadiosChanged`, unexported): compute `radioSignature(a.radioRefs())` (store snapshot, no app locks), compare/update `sb.radioSig` under `sb.mu` only (as today), and if changed enqueue one job to a refresh worker. Never touches `writeMu`.
- Worker goroutine: started in the settings-backend setup, stopped/joined in `ServiceShutdown` before `hk.Close()`; per job: `writeMu` → `applyHotkeys()` → `KeybindsChanged(GetKeybinds())`. On stop the in-flight job finishes and queued jobs are discarded (the OS hotkey layer is closed immediately after, so applying them would only register keys to unregister them).
- **Emission-rate constraint (umbrella/CLAUDE.md) — user decision D1:** the queue is FIFO with **one job per signature change** — not a size-1 coalescing mailbox — so N local-radio-set changes still produce exactly N `applyHotkeys` → N `hotkeys:state` and N `keybinds:changed`, in the same order as today. Dedupe still happens at enqueue (unchanged signature → no job, as today); the umbrella's "coalescing worker" wording is read as that enqueue-side dedupe, because coalescing *distinct* changes would reduce `hotkeys:state` emissions, which the constraint forbids. Queue is an unbounded slice under its own mutex + a 1-buffered wake channel, so the observer never blocks.
- Test seam `waitKeybindRefreshIdle(t)` for tests that today rely on synchronous refresh.

### GO-04 — bound surface = frontend API
- Package-level functions taking `*App`, matching the existing `SetNotifier(gui, …)`/`SetHistory(gui, …)` convention (worker's call on names): `SetApp`, `SetBackend`, `SetConnHealth`, `SetSettingsBackend`, `SetAudioBackend`, `SetJoystickBackend`, `SetupTray`, `SeedBuiltinProfiles`, `OnMainWindowClose`, `PlayConnectionSFX`, `VoiceBridge`, `VoiceRTTFunc` (returns the `func() time.Duration` main.go hands connhealth), `StateStore`.
- `Pressed/Released/ForceReleased` become unexported on `App`; `app.HotkeyHandler(gui)` returns an unexported adapter type implementing `hotkeys.Handler` and the joystick handler interface (an unregistered type's exported methods are not bound). main.go passes it to `hotkeys.New` and `joystick.New`.
- `RefreshKeybinds` → unexported observer (GO-06); `TrayAvailable` → `trayAvailable`; `ConnHealth`, `WailsApp` deleted.
- main.go and `main_wiring_test.go` source-text assertions updated to the new call forms; package tests renamed mechanically.
- Result: exported `*App` methods = 56 frontend-used + `DismissNotification` + `ServiceStartup/ServiceShutdown` (lifecycle; not emitted into bindings). Bindings go 79 → 57.

## 4. Files touched
`internal/voice/tx_test.go` (TX-FLAKE, test-only); `main.go`, `main_wiring_test.go`; `internal/app/{app,audio,bindings,connsfx,notifications?,profiles,settings,tray,voice,windows}.go` + their tests (incl. `history_wiring_test`, `connhealth_test`, `permission_test`, `tray_test`, `profiles_test`, `bindings_test`); new `internal/app/surface_test.go`; `internal/audio/{effect,manager}.go` (+`effect_test.go`); `internal/control/{client,stream}.go` (+`stream_test.go`); `internal/events/events.go`; `internal/joystick/manager.go` (+`hat_test.go`); `internal/windowstate/store.go` (+test); `frontend/src/shared/api/events.ts`. Not touched: `internal/session/session.go` (finding location was `Control()`, kept).

## 5. Tests first (TDD)
1. **GO-09:** compile-level; `hat_test` updated to the new signature (red = build failure until Apply changes).
2. **GO-07:** `windowstate`: `Save` to a `path` that is a non-empty directory → error and no `*.tmp` left (fails today). `Registry`: with a blocking `saveFn`, `OpenWindows()` returns while the save is in flight (fails today: deadlock → timeout); a failing `saveFn` produces one WARN record (captured `slog` handler).
3. **GO-08:** `stream_test`: SERVER_SETTINGS_CHANGED updates the store and emits **no** event; SERVER_ACTION emits nothing (both fail today). Deletions otherwise compile-checked; `vitest`/`tsc` for events.ts.
4. **GO-05:** with a blocking `sb.save` seam inside `persistRadios`: (a) `sb.mu.TryLock()` succeeds / `resolveTXTarget` returns while the save blocks (fails today); (b) a `SetCommsLayout` issued during the blocked save survives the commit; (c) a failing save leaves `sb.cfg` unchanged; (d) concurrent `rememberDevice` + `persistKeybinds` — final on-disk config contains both changes. Run with `-race`.
5. **GO-06:** (a) with `sb.writeMu` held by the test, `st.SetRadios(self, …)` returns within 1 s (fails today); (b) N distinct local-radio signature changes → exactly N `hotkeys:state` and N `keybinds:changed` after `waitKeybindRefreshIdle`; unchanged signature (remote client / frequency-only change) → 0; (c) shutdown joins the worker.
6. **GO-04:** `surface_test.go` reflects over `*App`'s exported method set and asserts equality with an allowlist (the 56 frontend methods + `DismissNotification` + lifecycle hooks) — fails today with 22 extras. Post-change proof: regenerate bindings, diff the `app.ts` function list (79 → 57, only the 22 listed gone), then `npx tsc --noEmit` (client.ts imports `App` from the generated bindings, so any removed-but-used method is a type error).
7. **TX-FLAKE:** `TestMinBatchIgnoresOneSlowBatch` (deterministic, fails until `minBatch` exists); the rewritten `TestTXWriteFrameNeverBlocks` asserts the exact `DroppedFull` delta; stress run `-race -count=50 -run TestTXWriteFrameNeverBlocks`.
8. Full suite per umbrella §3 (Go `-tags purego` build/vet/test -race; frontend vitest/tsc/build), plus `GOOS=windows|linux|darwin go vet -tags purego ./internal/joystick ./internal/app`.

## 6. Out of scope
- Anything changing `hotkeys:state`/`audio:state` emit sites (CLAUDE.md "chatty emitters").
- `EventAuthFlowStep`, `EventWindowGeometry` (never emitted; master-spec/Phase 2 contract) — noted for DOC.
- Connect/Reconnect/Disconnect timeouts and roster reset (CL1). `config.Save`'s own tmp cleanup (not in a finding).
- `DismissNotification` UI (product decision, ROADMAP 7.2).

## 7. Behaviour changes
- **User/wire:** none.
- **Webview-visible (internal):** 22 bound methods removed (none called by the frontend; proven by tsc); three `EV` names and two empty-payload events (`state:settings_update`, `state:server_action`) no longer emitted (zero subscribers). `keybinds:changed`/`hotkeys:state` after a local radio-set change now arrive asynchronously (same count, same order) instead of before the store mutation returns.
- **Logs:** new WARN on `windows.json` save failure.
- **Tests:** `TestTXWriteFrameNeverBlocks` measures min-of-10 batches plus an exact drop count (test-only change).

## 8. Approval gates
None — no dependency, proto, CI or wire change. (The scan tagged GO-04 "architecture"; the umbrella scoped CL2 gates to none since regenerated bindings are gitignored.)

## 9. Risks
- **GO-05 lost-update / re-entrancy:** commit re-applies a pure mutation; any site whose mutation is not a pure field-set (e.g. maps built from `sb.kb.Snapshot()`) must compute values *before* step 2 and capture them. `emitProfileState` re-enters `sb.mu` — helpers never emit while holding it (existing trap, test `SetCommsLayoutDoesNotDeadlockOnEmit`).
- **GO-06 async:** existing tests that assert per-radio keybinds immediately after `SetRadios` need the idle seam; shutdown ordering (worker must stop before `hk.Close`).
- **Profiles flows (7.3, recently merged):** most intricate `config.Save` sites; convert one at a time with their existing tests green between.
- **Wave interaction:** CL1 rebases on this (shares `bindings.go`, `profiles.go`); UP regenerates bindings — the surface test is the guard.
- gopls false positives (CLAUDE.md): rely on `go build`/`go vet` only.

## 10. Open questions
None (user decision D3). Settled as worker's calls: helper names/shapes, `saveMu` locks, `slog.Default()` in Registry, keeping `Session.Control()` and `DismissNotification`, FIFO (non-coalescing) refresh queue to honour the emission-rate constraint, hotkey-handler adapter.
