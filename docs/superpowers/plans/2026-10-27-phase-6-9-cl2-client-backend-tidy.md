# Phase 6.9 / CL2 — Client Backend Tidy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Where this runs:** a fresh git worktree of `vcs-srs-client` (`D:\Projects\vcs-srs-client`) on branch **`chore/cleanup-cl2`**, branched from **`origin/main`** (f8ccb43 at planning time). All paths below are repo-relative to that worktree.

**Goal:** Tidy the client backend with no user- or wire-visible change: fix the macOS `-race` flake first, then shrink the webview-bound `App` surface, move config/windows.json disk I/O out from under hot locks, make the radios observer non-blocking, and delete dead code.

**Architecture:** Test-only fix for the TX flake. A single `persistCfg` transaction helper (snapshot under `sb.mu` → save under a new `saveMu` with no `sb.mu` → re-apply under `sb.mu`) replaces all 11 `config.Save` sites. A FIFO keybind-refresh worker takes `applyHotkeys` off the store-observer goroutine. Main-only wiring moves from `*App` methods to package-level `func X(a *App, …)` seams, following the existing `SetNotifier`/`SetHistory` convention, and a reflection test pins the bound surface.

**Tech Stack:** Go (`-tags purego`), Wails v3 bindings, React/TypeScript frontend (only `frontend/src/shared/api/events.ts` changes).

**Spec:** `docs/superpowers/specs/2026-10-27-phase-6-9-cl2-client-backend-tidy-design.md`. Umbrella: `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md` §3 CL2.

## Global Constraints

- No dependency, proto, CI/CD, repo-setting or secret change. No file is deleted. If a task seems to need one, stop and ask.
- **`hotkeys:state` emission count and order unchanged** (user decision D1; CLAUDE.md "chatty emitters"). Do not touch the `hotkeys:state` / `audio:state` emit sites.
- Opus geometry, `srs.proto`, and the Welcome/SSO UI are untouched.
- Keep for CL1: `Session.Control()`, `Session.UpdateRadioInfo/Disconnect/Reconnect`, `control.Client.SyncClient/UpdateRadioInfo/Disconnect`, all of `internal/auth`. Keep `App.DismissNotification` (ROADMAP 7.2 groundwork). Keep `EventAuthFlowStep`, `EventWindowGeometry`.
- Lock order in `internal/app`: **`writeMu → saveMu → mu`**. No helper emits an event while holding `sb.mu`.
- gopls diagnostics in this repo are unreliable — trust only `go build`/`go vet`.
- Verification (client), run from the worktree root unless noted:
  - `go build -tags purego ./...`
  - `go vet -tags purego ./...`
  - `go test -race -count=1 -tags purego ./...`
  - in `frontend/`: `npx vitest run`, `npx tsc --noEmit`, `npm run build`

## Review Focus

1. **A layout drag landing while a settings save is on disk** — the drag must survive the commit (no lost update). Pinned in Task 6 (`TestPersistCfgKeepsConcurrentLayoutEdit`).
2. **Two writers racing to disk** (`rememberDevice` from the refresh worker vs `persistKeybinds` from the UI) — the file on disk must contain both changes. Pinned in Task 7 (`TestRememberDeviceAndKeybindsBothReachDisk`).
3. **Quit while a keybind refresh is queued** — quit must not hang and must not register hotkeys after `hk.Close()`. Pinned in Task 8 (`TestKeybindRefreshWorkerStopsBeforeHotkeyClose`).
4. **Two window settles in quick succession** — `windows.json` must end with the later geometry, not the earlier. Pinned in Task 3 (`TestRegistryPersistWritesLatestLast`).
5. **A frontend method silently disappearing from the bindings** — `tsc` must fail, and the reflection allowlist must name every remaining bound method. Pinned in Task 9 (`TestBoundSurfaceIsTheFrontendAPI` + bindings diff).

---

### Task 1: Make `TestTXWriteFrameNeverBlocks` robust (client `main` red on macOS)

Ship this task first; it is self-contained (test-only, `internal/voice`) so the orchestrator may open it as its own early PR if it wants `main` green before the rest lands.

**Files:**
- Modify: `internal/voice/tx_test.go` (`TestTXWriteFrameNeverBlocks`, currently :356-410)

**Interfaces:**
- Produces (test-only, in `tx_test.go`): `func minBatch(batches, n int, f func()) time.Duration` — runs `f` `n` times per batch, `batches` times, returns the shortest batch's elapsed time.

- [ ] **Step 1: Write the failing helper test**

```go
func TestMinBatchIgnoresOneSlowBatch(t *testing.T) {
	batch := 0
	got := minBatch(5, 1, func() {
		if batch == 2 { time.Sleep(50 * time.Millisecond) }
		batch++
	})
	if got >= 20*time.Millisecond {
		t.Fatalf("minBatch = %v; one stalled batch must not set the result", got)
	}
}
```

- [ ] **Step 2: Run it — expect FAIL (`undefined: minBatch`)**

Run: `go test -count=1 -tags purego -run TestMinBatchIgnoresOneSlowBatch ./internal/voice`

- [ ] **Step 3: Implement `minBatch` and rewrite the timing section of `TestTXWriteFrameNeverBlocks`**

Keep unchanged: the queue-fill loop + "never reported a drop" check, the `AllocsPerRun(2000, …) != 0` check, the `ts.voiceCount() == 0` check, `release()`. Replace the single 1000-call sample with:
- `before := s.TXStats().DroppedFull`; run `best := minBatch(10, 1000, func() { s.WriteFrame(frame) })` **on a goroutine**, with the main goroutine waiting on its done channel or `time.After(5 * time.Second)` → `t.Fatal("WriteFrame blocked against a full queue")`.
- `if d := s.TXStats().DroppedFull - before; d != 10*1000/2 { t.Fatalf(...) }` — every second call flushes (two 10 ms frames per 20 ms Opus frame) and every flush must take the drop path.
- `if best >= 10*time.Millisecond { t.Fatalf("best of 10 batches of 1000 WriteFrame calls took %v; the DSP tick budget is 10 ms", best) }`.
- Rewrite the comment above the timing block: it explains why min-of-N (a regression moves every batch, a runner stall moves one) and that the exact drop count is the clock-free proof of non-blocking. Reference run 40500422701 (10.36 ms, macos-latest, `-race`).

- [ ] **Step 4: Run — expect PASS, then stress**

Run: `go test -race -count=1 -tags purego -run "TestMinBatchIgnoresOneSlowBatch|TestTXWriteFrameNeverBlocks" ./internal/voice` → `ok`.
Run: `go test -race -count=50 -tags purego -run TestTXWriteFrameNeverBlocks ./internal/voice` → `ok` (no single failure).
Sanity check that it still catches a regression (do NOT commit): temporarily add `time.Sleep(20 * time.Microsecond)` at the top of `flushLocked` in `tx.go`, rerun the single test → FAIL on the budget; revert with `git checkout internal/voice/tx.go`.

- [ ] **Step 5: Commit**

```bash
git add internal/voice/tx_test.go
git commit -m "test(voice): make TestTXWriteFrameNeverBlocks robust under -race on shared runners"
```

---

### Task 2: GO-09 — `joystick.Manager.Apply` returns nothing

**Files:**
- Modify: `internal/joystick/manager.go:252` (`Apply`), `internal/app/settings.go:1253` (`_ = jm.Apply(joyBinds)`)
- Test: `internal/joystick/hat_test.go:166,191`

**Interfaces:**
- Produces: `func (m *Manager) Apply(binds map[string][]Binding)`

- [ ] **Step 1: Write the failing test** — in `internal/joystick/manager_test.go` add `TestApplyHasNoErrorResult` containing `var _ func(map[string][]Binding) = (&Manager{}).Apply` (a method-value assignment that only compiles once `Apply` returns nothing). In `hat_test.go:166,191` replace `if err := m.Apply(b); err != nil { … }` with `m.Apply(b)`.

Run: `go vet -tags purego ./internal/joystick` → FAIL: `cannot use (&Manager{}).Apply (value of type func(binds map[string][]Binding) error) as func(map[string][]Binding) value`.

- [ ] **Step 2: Change the signature; drop `return nil`; update `settings.go:1253` to `jm.Apply(joyBinds)`**

- [ ] **Step 3: Verify, including all three joystick backends**

Run: `go build -tags purego ./... && go test -race -count=1 -tags purego ./internal/joystick ./internal/app`
Run (bash): `for os in windows linux darwin; do GOOS=$os go vet -tags purego ./internal/joystick || exit 1; done` → no output.

- [ ] **Step 4: Commit** — `git add -A internal/joystick internal/app && git commit -m "refactor(joystick): Apply cannot fail; drop the error return"`

---

### Task 3: GO-07 — `windows.json` save: tmp cleanup, out from under `r.mu`, WARN on failure

**Files:**
- Modify: `internal/windowstate/store.go:36-49` (`Save`), `internal/app/windows.go` (`Registry` struct :15, the three `_ = windowstate.Save(r.path, r.geom)` sites at :74, :135, :182)
- Test: `internal/windowstate/store_test.go`, `internal/app/windows_test.go`

**Interfaces:**
- Produces (unexported, `internal/app/windows.go`): `Registry.saveMu sync.Mutex` (order `saveMu → mu`); `Registry.saveFn func(path string, m map[string]windowstate.Geometry) error` (nil → `windowstate.Save`); `func (r *Registry) persist()` — locks `saveMu`, clones `r.geom` under `r.mu` (`maps.Clone`), releases `r.mu`, saves, logs `slog.Default().Warn("save windows.json failed", "path", r.path, "err", err)` on error. Callers must NOT hold `r.mu`.

- [ ] **Step 1: Write the failing tests**
  - `windowstate`: `TestSaveRemovesTmpOnRenameFailure` — make `path` a non-empty directory (`os.MkdirAll(filepath.Join(path,"x"),0o755)`), call `Save(path, m)`; assert `err != nil` and `os.Stat(path+".tmp")` is `os.IsNotExist`.
  - `app`: `TestRegistryPersistDoesNotHoldMu` — `saveFn` blocks on a channel; trigger a settle on a fake handle; assert `r.OpenWindows()` (or any `r.mu` reader) returns within 1 s while the save is parked.
  - `app`: `TestRegistryPersistFailureLogsWarn` — install a capturing `slog` handler via `slog.SetDefault` (restore in `t.Cleanup`), `saveFn` returns an error; assert exactly one WARN record with message `save windows.json failed`.
  - `app`: `TestRegistryPersistWritesLatestLast` — two settles back to back with different geometry; `saveFn` records the maps; the last recorded map holds the second geometry.

- [ ] **Step 2: Run — expect FAIL** (tmp left behind; the blocking-save test times out; no WARN)

Run: `go test -race -count=1 -tags purego ./internal/windowstate ./internal/app -run "Save|RegistryPersist"`

- [ ] **Step 3: Implement** — `Save`: `os.Remove(tmp)` best-effort on WriteFile and Rename failure. `Registry`: add the fields and `persist()`; each of the three sites updates `r.geom` under `r.mu`, unlocks, then calls `r.persist()`. Keep `settleBounds`'s observer call after the unlock.

- [ ] **Step 4: Run — expect PASS** (same command)

- [ ] **Step 5: Commit** — `git commit -am "fix(windows): save windows.json outside r.mu, log failures, clean up tmp"`

---

### Task 4: GO-08 (part 1) — dead control RPCs and never-subscribed events

**Files:**
- Modify: `internal/control/client.go:79-94` (delete `UpdateClientInfo`, `GetServerSettings`), `internal/control/stream.go:61-67`, `internal/events/events.go` (`EventClientState`, `EventSettingsUpdate`, `EventServerAction`, `Tagged.ClientState/SettingsUpdate/ServerAction`), `frontend/src/shared/api/events.ts` (`EV.clientState/settingsUpdate/serverAction`)
- Test: `internal/control/stream_test.go`

**Interfaces:**
- Consumes/Produces: nothing new. The grpctest fake server's `UpdateClientInfo`/`GetServerSettings` implementations stay (they satisfy the generated server interface).

- [ ] **Step 1: Write failing tests in `stream_test.go`**
  - `TestServerSettingsChangedUpdatesStoreWithoutEvent` — route a `SERVER_SETTINGS_CHANGED` update; assert the store's settings reflect it and the recording emitter saw **0** events.
  - `TestServerActionEmitsNothing` — route `SERVER_ACTION`; assert 0 events.

- [ ] **Step 2: Run — expect FAIL** (one event each today)

Run: `go test -race -count=1 -tags purego -run "ServerSettingsChanged|ServerAction" ./internal/control`

- [ ] **Step 3: Delete** the listed symbols. `SERVER_SETTINGS_CHANGED` keeps `st.SetSettings(s)`; `SERVER_ACTION` becomes an explicit empty case with a comment ("no client-side consumer; ignored by design — see Phase 6.9 CL2"). Remove the three `EV` keys.

- [ ] **Step 4: Run — expect PASS; frontend still type-checks**

Run: `go build -tags purego ./... && go test -race -count=1 -tags purego ./internal/control ./internal/events`
Run (in `frontend/`): `npx tsc --noEmit && npx vitest run`

- [ ] **Step 5: Commit** — `git commit -am "chore(control): delete unused RPC wrappers and never-subscribed state events"`

---

### Task 5: GO-08 (part 2) — dead audio helpers and window-geometry bindings

**Files:**
- Modify: `internal/audio/effect.go:77-83` (delete `VoicePresets`, `ClippingPresets`; delete `presetIDs` if unused afterwards), `internal/audio/manager.go:458` (delete `NotificationAvailable`), `internal/app/bindings.go:121-126` (delete `App.GetWindowGeometry`, `App.SetWindowGeometry`; `windowsAPI` keeps `Geometry`/`SetGeometry`)
- Test: `internal/audio/effect_test.go:11`, any `internal/app` test calling the two geometry methods

- [ ] **Step 1: Rewrite `effect_test.go`'s preset test against `VoicePresetOptions()` ids** (same assertions on the id set it checks today). It passes before and after — this task is pure deletion, so the "red" is the compile check in Step 3.

- [ ] **Step 2: Delete the symbols**

- [ ] **Step 3: Verify**

Run: `go build -tags purego ./... && go vet -tags purego ./... && go test -race -count=1 -tags purego ./internal/audio ./internal/app`
Run: `git grep -n "VoicePresets\b\|ClippingPresets\|NotificationAvailable\|GetWindowGeometry\|SetWindowGeometry" -- '*.go' 'frontend/src'` → no matches.

- [ ] **Step 4: Commit** — `git commit -am "chore: delete unused audio preset helpers and window-geometry bindings"`

---

### Task 6: GO-05 (part 1) — `persistCfg` helper; convert settings.go / voice.go sites

**Files:**
- Modify: `internal/app/settings.go` (struct :58 — add `saveMu sync.Mutex`, `save func(path string, c *config.Config) error`; sites :276 `SetSettings`, :1140 `persistKeybinds`), `internal/app/voice.go` (:1061 `persistRadios`, :1174 `persistSelectedRadioSeq`)
- Create: `internal/app/persistcfg.go`, `internal/app/persistcfg_test.go`

**Interfaces:**
- Produces:
```go
// cfgTxn is one config write. mutate must only ASSIGN fields of the Config
// it is given (never write into a map/slice shared with sb.cfg) because it
// runs twice: on the snapshot that is saved, and on the then-current config
// at commit, so concurrent in-memory edits (SetCommsLayout,
// captureCommsWindowSize) are not lost. Values derived from other state
// (kb.Snapshot(), DTOs) are computed BEFORE persistCfg and captured.
type cfgTxn struct {
	check    func(cur *config.Config) bool // optional; under sb.mu; false → abort, no save, (false, nil)
	mutate   func(c *config.Config)
	onCommit func()                        // optional; under sb.mu after a successful save
}
func (sb *settingsBackend) persistCfg(tx cfgTxn) (committed bool, err error)
```
  Steps: lock `saveMu` → under `mu`: `check`, `next := *sb.cfg`, `mutate(&next)` → unlock `mu` → if `cfgPath != ""` `sb.saveFn()(cfgPath, &next)` with **no `mu` held** → on success under `mu`: `cur := *sb.cfg; mutate(&cur); sb.cfg = &cur; onCommit()`. On failure `sb.cfg` is untouched (today's contract). `sb.save == nil` → `config.Save`.

- [ ] **Step 1: Write failing tests in `persistcfg_test.go`** (all use a blocking/failing `sb.save` seam)
  - `TestPersistCfgDoesNotHoldMuDuringSave` — save parked; `sb.mu.TryLock()` succeeds and `a.resolveTXTarget(...)` returns within 1 s.
  - `TestPersistCfgKeepsConcurrentLayoutEdit` — park `persistRadios`'s save, call `a.SetCommsLayout(...)`, release; final `sb.cfg` has both the new radios and the new layout.
  - `TestPersistCfgFailureLeavesCfgUnchanged` — save returns an error; `sb.cfg` pointer and contents unchanged; `onCommit` not called.
  - `TestPersistCfgCheckAbortSkipsSave` — `check` returns false; save seam not called; `(false, nil)`.

- [ ] **Step 2: Run — expect FAIL** (`undefined: cfgTxn` / the TryLock test fails once compiled against the old sites)

Run: `go test -race -count=1 -tags purego -run PersistCfg ./internal/app`

- [ ] **Step 3: Implement `persistcfg.go` and convert the four sites.** `SetSettings`: `onCommit` captures `mgr = sb.audio`; `SetConfig`/`SettingsChanged` stay after the call. `persistKeybinds`: build the keybinds map from `sb.kb.Snapshot()` before the call. `persistSelectedRadioSeq`: `check` is `seq == 0 || seq >= sb.radioSelectPersistedSeq`; `onCommit` sets `radioSelectPersistedSeq`. Wrap errors exactly as today (`save settings: %w`, `save keybinds: %w`, `save radios: %w`, `save selected radio: %w`).

- [ ] **Step 4: Run — expect PASS**

Run: `go test -race -count=1 -tags purego ./internal/app`

- [ ] **Step 5: Commit** — `git commit -am "perf(settings): write config.toml outside sb.mu via persistCfg"`

---

### Task 7: GO-05 (part 2) — profiles.go sites, `rememberDevice`, `flushConfig`

**Files:**
- Modify: `internal/app/profiles.go` (:181, :474, :535, :636, :715, `flushConfig` :874-898), `internal/app/settings.go` (`rememberDevice` :373-395)
- Test: `internal/app/persistcfg_test.go`, existing `internal/app/profiles_test.go`

**Interfaces:**
- Consumes: `cfgTxn`, `persistCfg` (Task 6).

- [ ] **Step 1: Write failing tests**
  - `TestRememberDeviceAndKeybindsBothReachDisk` — real temp `cfgPath`; run `a.rememberDevice("dev1","Stick")` and `a.persistKeybinds()` (after a bind change) concurrently 50×; `config.Load(cfgPath)` contains both the device name and the binding.
  - `TestProfileSaveDoesNotHoldMu` — park the save inside one profile flow (the :636 site); `sb.mu.TryLock()` succeeds.

- [ ] **Step 2: Run — expect FAIL** (`-race` report or missing field on disk for the first; TryLock fails for the second)

Run: `go test -race -count=1 -tags purego -run "RememberDevice|ProfileSaveDoesNotHoldMu" ./internal/app`

- [ ] **Step 3: Convert one site at a time, running `go test -race -count=1 -tags purego -run Profile ./internal/app` between each.** Pre-checks move into `check`; restore-on-failure logic stays where it is (only the save moves out of `mu`). `rememberDevice`: `check` keeps the early-out (`name == "" || deviceNames[id] == name`), `sb.deviceNames[id] = name` stays in-memory before the call as today, `mutate` assigns a fresh `KeybindDevices` map copied from `cur.KeybindDevices` plus the new entry (built inside `mutate` from its argument, so the re-apply sees the current map). `flushConfig`: takes `saveMu` (inside `writeMu`), keeps the `layoutPending` clear-then-restore-on-failure logic.

- [ ] **Step 4: Verify no `config.Save` remains under `sb.mu`**

Run: `git grep -n "config.Save" -- internal/app ':!*_test.go'` → only `persistcfg.go` (the default seam).
Run: `go test -race -count=1 -tags purego ./internal/app` → `ok`.

- [ ] **Step 5: Commit** — `git commit -am "perf(profiles): route every config write through persistCfg"`

---

### Task 8: GO-06 — non-blocking radios observer with a FIFO refresh worker

**Files:**
- Create: `internal/app/keybindrefresh.go`, `internal/app/keybindrefresh_test.go`
- Modify: `internal/app/settings.go:1279-1297` (`RefreshKeybinds` → `onRadiosChanged`), `internal/app/app.go:227` (observer registration), `internal/app/app.go` `ServiceShutdown` (stop worker before `hk.Close()`); existing tests that call `RefreshKeybinds` or assert per-radio keybinds right after `SetRadios`

**Interfaces:**
- Produces (unexported):
  - `type keybindRefresher struct` — FIFO slice under its own mutex + `wake chan struct{}` (cap 1) + `stop`/`done` channels.
  - `func (a *App) onRadiosChanged()` — `sig := radioSignature(a.radioRefs())`; compare/update `sb.radioSig` under `sb.mu` only; if changed, `enqueue()`. Never takes `writeMu`; never blocks.
  - Per job on the worker: `sb.writeMu.Lock(); a.applyHotkeys(); sb.em.KeybindsChanged(a.GetKeybinds()); sb.writeMu.Unlock()`.
  - `func (a *App) stopKeybindRefresh()` — in-flight job finishes, queued jobs are discarded, joins the goroutine; idempotent.
  - Test seam: `func waitKeybindRefreshIdle(t *testing.T, a *App)` (queue empty and no job running, 2 s timeout → `t.Fatal`).
- The worker is started in `SetSettingsBackend` (renamed in Task 9). Test helpers that call it register `t.Cleanup(a.stopKeybindRefresh)`.

- [ ] **Step 1: Write failing tests**
  - `TestRadiosObserverDoesNotBlockOnWriteMu` — test holds `sb.writeMu`; `st.SetRadios(selfGUID, …)` in a goroutine returns within 1 s.
  - `TestRadiosObserverOneRefreshPerSignatureChange` — 3 distinct local radio sets in a row → after `waitKeybindRefreshIdle`, `emitter.count("hotkeys:state")` rose by exactly 3 and `count("keybinds:changed")` by exactly 3, and the names alternate `hotkeys:state, keybinds:changed` per job; a remote client's radio change and a same-signature local change add 0.
  - `TestKeybindRefreshWorkerStopsBeforeHotkeyClose` — enqueue a job blocked on `writeMu`, call `ServiceShutdown` from a goroutine, release `writeMu`; shutdown returns within 3 s and the fake registrar records no `Register` after its `Close`.

- [ ] **Step 2: Run — expect FAIL** (first test times out on today's synchronous observer)

Run: `go test -race -count=1 -tags purego -run "RadiosObserver|KeybindRefreshWorker" ./internal/app`

- [ ] **Step 3: Implement**; add `waitKeybindRefreshIdle` to tests that depended on the synchronous refresh (find them with `go test` failures, not by guessing).

- [ ] **Step 4: Run — expect PASS**

Run: `go test -race -count=1 -tags purego ./internal/app` (and once with `-count=20 -run "RadiosObserver|KeybindRefresh"`).

- [ ] **Step 5: Commit** — `git commit -am "fix(settings): radios observer enqueues keybind refresh instead of blocking the update stream"`

---

### Task 9: GO-04 — bound `App` surface equals the frontend API

**Files:**
- Modify: `internal/app/{app,audio,connsfx,profiles,settings,tray,voice}.go`, `main.go`, `main_wiring_test.go`, tests in `internal/app` that call the moved methods (mechanical rename)
- Create: `internal/app/surface_test.go`, `internal/app/hotkeyhandler.go`

**Interfaces:**
- Produces (package-level, `internal/app`):
  `SetApp(a *App, w *application.App)`, `SetBackend(a *App, sess sessionAPI, windows windowsAPI)`, `SetConnHealth(a *App, m *connhealth.Monitor)`, `SetSettingsBackend(a *App, cfg *config.Config, cfgPath string, kb *keybinds.Store, hk *hotkeys.Manager, em events.Emitter)`, `SetAudioBackend(a *App, m *audio.Manager)`, `SetJoystickBackend(a *App, jm *joystick.Manager)`, `SetupTray(a *App, icon []byte)`, `SeedBuiltinProfiles(a *App)`, `OnMainWindowClose(a *App) (preventClose bool)`, `PlayConnectionSFX(a *App, state string)`, `VoiceBridge(a *App) *sessionBridge`, `VoiceRTTFunc(a *App) func() time.Duration`, `StateStore(a *App) *state.Store`, `HotkeyHandler(a *App) *hotkeyHandler`.
  `type hotkeyHandler struct{ a *App }` with methods `Pressed`, `Released`, `ForceReleased` (implements `hotkeys.Handler`, `hotkeys.StaleReleaser`, `joystick.Handler`; not a registered service, so not bound).
- On `*App`: `Pressed/Released/ForceReleased` → `pressed/released/forceReleased`; `TrayAvailable` → `trayAvailable`; `ConnHealth`, `WailsApp` deleted (zero callers). `Store()` is renamed only through `StateStore` — delete the method.
- main.go: `hotkeys.New(hotkeys.NewOSRegistrar(), app.HotkeyHandler(gui))`, `joystick.New(joySrc, app.HotkeyHandler(gui), appLog)`, `VoiceRTT: app.VoiceRTTFunc(gui)`, and `app.X(gui, …)` for each seam above.

- [ ] **Step 1: Write the failing tests**
  - `surface_test.go` `TestBoundSurfaceIsTheFrontendAPI` — reflect over `reflect.TypeOf(&App{})`'s exported methods; assert set equality with this explicit allowlist (59 names) — the 56 methods `frontend/src/shared/api/client.ts` calls at f8ccb43:
  `AddTrigger BeginCapture BrowseProfilesDir ClearHistory ClearKeybind ClearNotifications CloseWindow Connect DeleteProfile Disconnect EndCapture ExportHistoryCSV ExportProfile FocusMainWindow GetAudioDevices GetAudioEffectPresets GetAudioState GetBuildInfo GetClientState GetCommsLayout GetConnectionState GetHistory GetHotkeyState GetJoystickState GetKeybinds GetNotifications GetOpenWindows GetProfileState GetSettings ImportProfile ListProfiles LoadProfile MarkAllNotificationsRead MarkNotificationRead OpenHotkeyPermissionSettings OpenProfilesDir OpenWindow PreviewEffect RecheckHotkeyPermission Reconnect ReconnectVoice RemoveTrigger RenameProfile RequestHotkeyPermission ResetLayout RevertProfile SaveProfile SaveProfileAs SelectRadio SetCommsLayout SetSettings StartMicTest StopMicTest ToggleWindow UpdateRadioInfo VoiceState`
  + `DismissNotification` + `ServiceStartup` + `ServiceShutdown`. The failure message lists extras and missing names separately and says that adding a bound method means adding it here deliberately. (If CL3/UP has landed a new frontend call by the time this runs, rebase first and add it — the test is the guard, not a snapshot to force.)
  - `TestHotkeyHandlerSatisfiesBothHandlerInterfaces` — compile-time `var _ hotkeys.Handler = (*hotkeyHandler)(nil)`, `var _ hotkeys.StaleReleaser = …`, `var _ joystick.Handler = …`, plus one press/release through it reaching the refcounted PTT path.
  - `main_wiring_test.go` `TestMainCallsWiringSeamsNotMethods` — banned substrings `gui.SetApp(`, `gui.SetBackend(`, `gui.SetConnHealth(`, `gui.SetSettingsBackend(`, `gui.SetAudioBackend(`, `gui.SetJoystickBackend(`, `gui.SetupTray(`, `gui.SeedBuiltinProfiles(`, `gui.OnMainWindowClose(`, `gui.PlayConnectionSFX(`, `gui.VoiceBridge(`, `gui.VoiceRTT`, `gui.Store(`, `hotkeys.NewOSRegistrar(), gui)`; required `app.HotkeyHandler(gui)` twice. Update the existing source-text assertions (`TestAudioBackendIsWired`, `TestVoiceBridgeIsWiredAsBothSinkAndSource`, `TestHistoryIsWired`'s `SeedBuiltinProfiles` count, etc.) to the new forms.

- [ ] **Step 2: Run — expect FAIL** (22 extras; banned call forms present)

Run: `go test -count=1 -tags purego -run "BoundSurface|HotkeyHandler|MainCallsWiringSeams" . ./internal/app`

- [ ] **Step 3: Implement** the moves; update `main.go`; rename tests mechanically. Keep the existing `TestNotificationSeamsAreNotOnTheExportedMethodSet` (it is now subsumed, but stays as the documented rationale).

- [ ] **Step 4: Run — expect PASS**

Run: `go build -tags purego ./... && go vet -tags purego ./... && go test -race -count=1 -tags purego . ./internal/app`
Run (bash): `for os in windows linux darwin; do GOOS=$os go vet -tags purego ./internal/app ./internal/joystick || exit 1; done`

- [ ] **Step 5: Prove nothing the frontend uses disappeared**

Let `$SP` be the session scratchpad directory (never commit `frontend/bindings/`, which is gitignored).
Run (bash, repo root):
- On a checkout of `origin/main` (e.g. `git worktree add $SP/cl2-base origin/main`): `cd $SP/cl2-base && wails3 generate bindings -ts -f "-tags purego" -clean=true -d $SP/b-before`.
- In the CL2 worktree: `wails3 generate bindings -ts -f "-tags purego" -clean=true -d $SP/b-after`.
- `for d in b-before b-after; do grep -o "export function [A-Za-z]*" $SP/$d/github.com/FPGSchiba/vcs-srs-client/internal/app/app.ts | sort > $SP/$d.txt; done; diff $SP/b-before.txt $SP/b-after.txt`
Expected: only `<` lines, exactly these 22 (79 → 57): `ConnHealth ForceReleased GetWindowGeometry OnMainWindowClose PlayConnectionSFX Pressed RefreshKeybinds Released SeedBuiltinProfiles SetApp SetAudioBackend SetBackend SetConnHealth SetJoystickBackend SetSettingsBackend SetWindowGeometry SetupTray Store TrayAvailable VoiceBridge VoiceRTT WailsApp`. The generator's warnings for `SetBackend`/`SetSettingsBackend` interface params are gone. Remove the temp worktree with `git worktree remove $SP/cl2-base`.
Run (in `frontend/`): `npx tsc --noEmit && npx vitest run && npm run build` → all pass.

- [ ] **Step 6: Commit** — `git commit -am "refactor(app): move main-only wiring off the bound App service"`

---

### Task 10: Full verification

- [ ] **Step 1: Run the whole client suite** (verbatim output into the PR on any failure)

```
go build -tags purego ./...
go vet -tags purego ./...
go test -race -count=1 -tags purego ./...
cd frontend && npx vitest run && npx tsc --noEmit && npm run build
```
Expected: all exit 0; `go test` prints `ok` for every package.

- [ ] **Step 2: Re-stress the two timing-sensitive tests**

`go test -race -count=30 -tags purego -run "TestTXWriteFrameNeverBlocks|RadiosObserver|KeybindRefresh|PersistCfg" ./internal/voice ./internal/app` → `ok`.

- [ ] **Step 3: Confirm the Global Constraints** — `git diff --stat origin/main` touches no `go.mod`, `go.sum`, `package*.json`, `.github/`, `srs.proto`; `git diff origin/main --diff-filter=D --name-only` is empty.

---

### Task 11: Commit design + plan, open the PR

**Files:**
- Create: `docs/superpowers/specs/2026-10-27-phase-6-9-cl2-client-backend-tidy-design.md` (from scratchpad `design-CL2.md`)
- Create: `docs/superpowers/plans/2026-10-27-phase-6-9-cl2-client-backend-tidy.md` (this plan)

- [ ] **Step 1: Copy both files in; fix the plan's **Spec:** line to the committed path**
- [ ] **Step 2: Commit** — `git add docs/superpowers && git commit -m "docs(phase-6.9): CL2 design and implementation plan"` (ends with the session's attribution trailer)
- [ ] **Step 3: Push and open the PR** — `git push -u origin chore/cleanup-cl2`; `gh pr create --base main --title "Phase 6.9 CL2: client backend tidy" --body …` listing: the TX-flake fix (with run 40500422701), GO-04…GO-09, the internal behaviour changes from spec §7, "no gates", the Task 10 command outputs, and the manual items below. Body ends with the session's PR attribution line.

## Manual verification items (for the orchestrator to merge into `docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`)

- **CL2-M1:** Connect to a real server with per-radio keybinds bound; the per-radio hotkeys register and fire after connect (keybind refresh is now asynchronous), and the Keybinds panel shows the per-radio rows without touching any other keybind.
- **CL2-M2:** Hold PTT while saving Settings and while dragging the Comms layout; transmission does not stutter or drop (config writes no longer hold `sb.mu`).
- **CL2-M3:** Make `windows.json` read-only (or its directory unwritable), move a window; the log shows one `save windows.json failed` WARN per save, no `windows.json.tmp` is left behind, and the app keeps running. Restore permissions; geometry persists across a restart.
- **CL2-M4:** Quit (tray Quit, Cmd+Q / window close) immediately after connecting; the app exits promptly with no hotkey registered after quit.
