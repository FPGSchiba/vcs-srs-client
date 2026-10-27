# Phase 6.9 CL3 — Frontend Correctness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Comms popout follow selection and session changes, stop hydrates from rolling stores back, report every failed user action through the notification channel without leaving optimistic UI wrong, throttle knob and VOX drags, and make the primary navigation and the toasts accessible.

**Architecture:** Go gets one unexported helper, `reportAction`. It raises on failure and resolves on success, keyed and fingerprint-deduped, and is called from a deferred closure in each of six user-initiated bindings. The frontend gets four small shared units:
- a hydrate guard with snapshot and delta rules;
- `useClientStateSync`, mounted in both window shells;
- a pending-overlay settings commit path;
- a leading-and-trailing throttle hook.

The existing screens are rewired onto these units.

**Tech Stack:** Go 1.27.3 (`internal/app`, `internal/notify`). React 19, TypeScript, Zustand and Vitest with Testing Library (`frontend/`).

**Spec:** `docs/superpowers/specs/2026-10-27-phase-6-9-cl3-frontend-correctness-design.md`. Until Task 14 commits it, the spec is the scratchpad `design-CL3.md`. The umbrella is `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md` §3 CL3.

**Worktree / branch:** a fresh worktree on `chore/cleanup-cl3`, cut from `origin/main` (`5a888e2` or later). Run `npm ci` in `frontend/` once before Task 3. Every command below runs from the worktree root unless it says `cd frontend`.

## Global Constraints

- **No new or removed bound method.** `TestBoundSurfaceIsTheFrontendAPI` (`internal/app/surface_test.go:43`) must pass unchanged. All new Go identifiers are unexported.
- **No dependency change** (Go or npm). No proto change, no CI/workflow change, no `api/client.ts` or `api/events.ts` change.
- **Go edits stay narrow.**
  - `settings.go`: only `SetSettings`.
  - `voice.go`: only `SelectRadio`.
  - `bindings.go`: only `UpdateRadioInfo`.
  - `profiles.go`: only `ResetLayout`.
  - `audio.go`: only `StartMicTest` and `PreviewEffect`.
  - Plus the new `notify_actions.go` and `notify_actions_test.go`.

  CL1 rebases on these files.
- **Do not touch any emitter or `internal/notify`.** Respect CLAUDE.md's notification rules: stable keys, user-visible fields only, no de-chattying.
- **Notification table (exact values):**

  | key | Category | Icon | Title |
  |---|---|---|---|
  | `action:settings.save` | `system` | `settings` | `Could not save settings` |
  | `action:radio.select` | `comms` | `radio` | `Radio selected, but the selection was not saved` |
  | `action:radio.save` | `comms` | `radio` | `Could not save the radio change` |
  | `action:radio.send` | `comms` | `radio` | `Radio change saved, but not sent to the server` |
  | `action:layout.reset` | `profile` | `layout` | `Could not reset the layout` |
  | `action:audio.mictest` | `system` | `mic` | `Could not start the mic test` |
  | `action:audio.preview` | `system` | `volume` | `Could not preview the effect` |

  Every row uses Severity `notify.SeverityError` and Body `err.Error()`.
- Drag throttle interval: **150 ms**, leading and trailing, with a final commit on release.
- Delta-hydrate retry bound: **3 attempts**.
- **No Tailwind utility classes** (CL4 removed Tailwind). Never use the className `flex` for `display:flex`. Use existing `components.css`/`global.css` classes or inline styles.
- Do not touch `Welcome.tsx` (CL1).
- **gopls is unreliable in this repo.** Trust only `go build` and `go vet`.
- **Go verification (full):**

  ```bash
  go build -tags purego ./...
  go vet -tags purego ./...
  go test -race -count=1 -tags purego ./...
  ```
- **Frontend verification (full):**

  ```bash
  cd frontend
  npx vitest run
  npx tsc --noEmit
  npm run build
  ```
- **Server:** CL3 changes nothing in `vngd-srs-server`. Do **not** run the server suite or its codegen for this track.

## Review Focus

1. **A knob drag on a read-only config directory.**
   - *Expected:* one toast, not one per throttled commit.
   - *Test:* Task 1, `TestReportActionDedupesIdenticalFailure`, checks the OnChange count.
2. **A failure, then a success, then the same failure again.**
   - *Expected:* the second failure toasts again.
   - *Test:* Task 1, `TestSetSettingsSuccessResolvesThenRefailureIsNews`.
3. **Re-login with a different GUID while Comms stays open.**
   - *Expected:* the new radios render.
   - *Test:* Task 5, `"re-login with a new guid renders the new radios"`.
4. **Two different knobs changed before either echo lands.**
   - *Expected:* both changes survive.
   - *Test:* Task 7, `"two commits before any echo both survive"`.
5. **A Knob unmounted mid-drag**, for example when VOX is toggled off under the slider.
   - *Expected:* no further `onChange` calls.
   - *Test:* Task 9, `"unmount mid-drag removes window listeners"`.

---

### Task 1: Go `reportAction` helper, wired into `SetSettings`

**Files:**
- Create: `internal/app/notify_actions.go`
- Create: `internal/app/notify_actions_test.go`
- Modify: `internal/app/settings.go:268` (`SetSettings` only)

**Interfaces:**
- Produces:

  ```go
  type actionFailure struct{ key, category, icon, title string }
  var (
      actSettingsSave, actRadioSelect, actRadioSave, actRadioSend,
      actLayoutReset, actAudioMicTest, actAudioPreview actionFailure // values from the Global Constraints table
  )
  func (a *App) reportAction(f actionFailure, err error)
  ```

  `reportAction` behaves as follows:
  - `a.notif == nil` → no-op.
  - `err != nil` → `a.notif.Raise(f.key, notify.Item{Category, Severity: notify.SeverityError, Icon, Title, Body: err.Error()})`.
  - `err == nil` → `a.notif.Resolve(f.key)`.

- [ ] **Step 1: Write the failing tests** in `notify_actions_test.go`. Use `newTestAppWithPath`, `unwritablePath` (`settings_test.go:302,330`) and `a.setNotifier(...)`.

```go
func TestSetSettingsFailureRaisesActionNotification(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	if err := a.SetSettings(a.GetSettings()); err == nil {
		t.Fatal("want error")
	}
	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Key != "action:settings.save" ||
		items[0].Title != "Could not save settings" || items[0].Category != "system" ||
		items[0].Severity != notify.SeverityError || items[0].Body == "" {
		t.Fatalf("items = %+v", items)
	}
}

func TestReportActionDedupesIdenticalFailure(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	var publishes atomic.Int32
	a.setNotifier(notify.New(notify.Options{OnChange: func(notify.Snapshot) { publishes.Add(1) }}))
	_ = a.SetSettings(a.GetSettings())
	_ = a.SetSettings(a.GetSettings())
	if got := publishes.Load(); got != 1 {
		t.Fatalf("publishes = %d, want 1 (fingerprint dedupe)", got)
	}
}

func TestSetSettingsSuccessResolvesThenRefailureIsNews(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfgdir")
	a, _, _ := newTestAppWithPath(t, filepath.Join(dir, "config.toml"))
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	_ = a.SetSettings(a.GetSettings()) // dir missing -> fail
	if err := os.MkdirAll(dir, 0o755); err != nil { t.Fatal(err) }
	if err := a.SetSettings(a.GetSettings()); err != nil { t.Fatal(err) }
	if it := n.Snapshot().Items[0]; !it.Resolved {
		t.Fatalf("success must resolve: %+v", it)
	}
	if err := os.RemoveAll(dir); err != nil { t.Fatal(err) }
	_ = a.SetSettings(a.GetSettings())
	if it := n.Snapshot().Items[0]; it.Resolved || !it.Unread {
		t.Fatalf("a recurrence after a success must be news: %+v", it)
	}
}

func TestReportActionWithoutNotifierIsNoop(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	_ = a.SetSettings(a.GetSettings()) // must not panic
}

func TestActionNotifyRunsOutsideSettingsLocks(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	a.setNotifier(notify.New(notify.Options{OnChange: func(notify.Snapshot) {
		// Re-enter both settings locks from the publish callback.
		a.settings.writeMu.Lock(); a.settings.writeMu.Unlock()
		_ = a.GetSettings()
	}}))
	done := make(chan struct{})
	go func() { _ = a.SetSettings(a.GetSettings()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: reportAction ran under writeMu")
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test -race -count=1 -tags purego ./internal/app/ -run 'TestSetSettingsFailureRaises|TestReportAction|TestSetSettingsSuccessResolves|TestActionNotifyRunsOutside'`

Expected: FAIL, because no item is raised (the snapshot is empty).

- [ ] **Step 3: Implement the helper and wire it into `SetSettings`**

  - Implement `notify_actions.go` with the values from the table. Give the helper a doc comment that cites D6 (resolve on success) and CLAUDE.md's stable-key rule.
  - Change `SetSettings(s SettingsDTO) error` to `(err error)`.
  - As its **first** statement, before `sb.writeMu.Lock()`, add `defer func() { a.reportAction(actSettingsSave, err) }()`. Defers run LIFO, so the unlock runs first.

- [ ] **Step 4: Run the tests and confirm they pass**

  1. Run the Step 2 command again. Expected: PASS.
  2. Run `go test -race -count=1 -tags purego ./internal/app/`. Expected: `ok`, with `TestSetSettingsFailsWithoutMutatingOnPersistError` still passing.

- [ ] **Step 5: Commit**

```bash
git add internal/app/notify_actions.go internal/app/notify_actions_test.go internal/app/settings.go
git commit -m "feat(app): raise an action notification when SetSettings fails (FE09)"
```

### Task 2: Wire the remaining failing bindings

**Files:**
- Modify:
  - `internal/app/voice.go:668` (`SelectRadio`)
  - `internal/app/bindings.go:99` (`UpdateRadioInfo`)
  - `internal/app/profiles.go:498` (`ResetLayout`)
  - `internal/app/audio.go:131,163` (`StartMicTest`, `PreviewEffect`)
- Test: `internal/app/notify_actions_test.go`

**Interfaces:**
- Consumes: `reportAction` and the `act*` values from Task 1.

- [ ] **Step 1: Write the failing tests.** Each one wires `n := notify.New(notify.Options{})` and asserts that `n.Snapshot().Items` holds exactly one item with the expected `Key` and `Title`.

| Test | Setup | Expected key / extra assertion |
|---|---|---|
| `TestSelectRadioPersistFailureRaisesAction` | `newTestAppWithPath(t, unwritablePath(t))`, then `a.SelectRadio(3)` returns an error | `action:radio.select`. Also `a.st.SelectedRadio() == 3`, which pins the "applied but not saved" semantics. |
| `TestUpdateRadioInfoPersistFailureRaisesActionAndDoesNotSend` | unwritable path, `fc := &fakeControlSession{}`, `a.sess = fc` | `action:radio.save`. Also `fc.updateCount() == 0`. |
| `TestUpdateRadioInfoSendFailureRaisesAction` | `newTestApp(t)` (empty path, so the persist step is a no-op), then `a.sess = &failingSendSession{}`. Define this type in the test file: it embeds `fakeControlSession` and overrides `UpdateRadioInfo` to return `errors.New("not connected")`. | `action:radio.send`, with `Body == "not connected"` |
| `TestUpdateRadioInfoSuccessResolvesBothKeys` | First raise `action:radio.save` and `action:radio.send` by calling `a.reportAction(actRadioSave, errors.New("x"))` and `a.reportAction(actRadioSend, errors.New("x"))`. Then make a successful `UpdateRadioInfo` call with `fakeControlSession`. | Both items have `Resolved` set. |
| `TestResetLayoutFailureRaisesAction` | `newTestAppWithPath(t, unwritablePath(t))`, then `a.ResetLayout()` returns an error | `action:layout.reset` |
| `TestMicTestAndPreviewWithoutBackendRaise` | `newTestApp(t)` with no audio backend; call `StartMicTest()` and `PreviewEffect("tx_start")` | Two items, keys `action:audio.mictest` and `action:audio.preview` |

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test -race -count=1 -tags purego ./internal/app/ -run 'FailureRaisesAction|ResolvesBothKeys|WithoutBackendRaise'`

Expected: FAIL, because the snapshots are empty.

- [ ] **Step 3: Wire each binding**

  For each binding, switch to a named `err` return and add `defer func() { a.reportAction(<act>, err) }()` as the **first** statement. That ordering places the report after any lock release (ResetLayout releases `writeMu` inside a closure, which is also fine).

  `UpdateRadioInfo` reports its two steps separately, so it does not use a single defer:

  ```go
  func (a *App) UpdateRadioInfo(info RadioInfoDTO) error {
  	err := a.persistRadios(info.Radios)
  	a.reportAction(actRadioSave, err)
  	if err != nil {
  		return err
  	}
  	a.refreshRXContext()
  	err = a.sess.UpdateRadioInfo(context.Background(), RadioInfoToProto(info))
  	a.reportAction(actRadioSend, err)
  	return err
  }
  ```

  Keep all existing doc comments, and append one line to each: `Failures are reported through reportAction (Phase 6.9 CL3, FE09).`

- [ ] **Step 4: Run the tests and confirm they pass**

  1. Run the Step 2 command again. Expected: PASS.
  2. Run the full Go verification from the Global Constraints. Expected:
     - build and vet are silent;
     - every package is `ok`;
     - `TestBoundSurfaceIsTheFrontendAPI` passes.

- [ ] **Step 5: Commit**

```bash
git add internal/app/{voice,bindings,profiles,audio,notify_actions_test}.go
git commit -m "feat(app): report radio, layout and audio action failures via notify (FE09)"
```

### Task 3: The hydrate guard (`hydrate.ts`)

**Files:**
- Create: `frontend/src/shared/store/hydrate.ts`
- Create: `frontend/src/shared/store/hydrate.test.ts`

**Interfaces:**
- Produces:

  ```ts
  export interface HydrateHandle { pushed(): void; dispose(): void }
  export function hydrateSnapshot<T>(fetch: () => Promise<T>, apply: (v: T) => void, onError: (e: unknown) => void): HydrateHandle
  export function hydrateDelta<T>(fetch: () => Promise<T>, apply: (v: T) => void, onError: (e: unknown) => void, maxAttempts?: number /* default 3 */): HydrateHandle
  ```

  - `hydrateSnapshot`: a `pushed()` before resolve means the result is dropped. `dispose()` drops any later result.
  - `hydrateDelta`: a `pushed()` during an attempt means that attempt's result is discarded and `fetch` is called again. On attempt `maxAttempts` the result is applied anyway, with `console.warn("hydrate: deltas kept racing the snapshot; applied attempt", n)`. `pushed()` while no fetch is in flight is a no-op. A rejection calls `onError` and does not retry.

- [ ] **Step 1: Write the failing tests.** Use controllable promises (`let resolve!: (v: T) => void; new Promise(r => resolve = r)`):
  - `"snapshot: a push during flight drops the hydrate"`: `pushed()` then `resolve(1)` means `apply` is never called.
  - `"snapshot: no push means apply(result)"`
  - `"snapshot: dispose drops a late result"`
  - `"delta: a push during flight re-fetches and applies the second result"`: `fetch` is called twice and `apply` once, with the second value.
  - `"delta: after 3 racing attempts the third result applies with a warning"`: spy on `console.warn`, and assert that `fetch` was called 3 times and `apply` got the third value.
  - `"delta: a push after resolve does not re-fetch"`
  - `"rejection calls onError once and applies nothing"` (run for both rules).
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/shared/store/hydrate.test.ts`. Expected: FAIL, because the module is not found.
- [ ] **Step 3: Implement `hydrate.ts`.** Use a closure with `inflight` and `dirty` flags. Hold the attempt counter inside `hydrateDelta`.
- [ ] **Step 4: Run the tests and confirm they pass.** Run the same command. Expected: PASS.
- [ ] **Step 5: Commit.** Run `git commit -m "feat(frontend): snapshot/delta hydrate guard (FE08)"`.

### Task 4: Apply the snapshot rule to `useSettingsSync` and `useNotificationsSync`

**Files:**
- Modify: `frontend/src/shared/store/useSettingsSync.ts`
- Modify: `frontend/src/shared/store/useNotificationsSync.ts`
- Test: `frontend/src/shared/store/useSettingsSync.test.tsx`
- Test: `frontend/src/shared/store/useNotificationsSync.test.tsx`

**Interfaces:**
- Consumes: `hydrateSnapshot` (Task 3).

- [ ] **Step 1: Write the failing tests.** For each channel, make the hydrate promise resolve **after** an emitted push, then assert the store holds the pushed value:
  - `settings` (`settings:changed`)
  - `keybinds` (`keybinds:changed`)
  - `hotkeyState` (`hotkeys:state`)
  - `joystickState` (`joystick:state`)
  - `audioDevices` (`audio:devices_changed`)
  - `audioState` (`audio:state`)
  - notifications (`notifications:changed`)

  Use `it.each` over a table of `[apiFn, event, storeSelector]`. Use the existing `emit` and `vi.mock` harness in those files.
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/shared/store/useSettingsSync.test.tsx src/shared/store/useNotificationsSync.test.tsx`. Expected: the new cases FAIL, because the store is rolled back to the hydrate value.
- [ ] **Step 3: Wrap each hydrate in `hydrateSnapshot`.**
  - Each matching `on(...)` handler calls `handle.pushed()` before it writes the store.
  - The effect cleanup disposes every handle.
  - The existing `console.error` messages are kept as each channel's `onError`.
  - The silent catches become `console.error("<call> failed", err)`.
  - `getAudioEffectPresets` stays unguarded.
- [ ] **Step 4: Run the tests and confirm they pass.** Run the same command. Expected: PASS, with the old cases unchanged.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): stale-hydrate guard for settings and notifications sync (FE08)"`.

### Task 5: `useClientStateSync`, mounted in MainApp and CommsApp (FE01, FE02)

**Files:**
- Create: `frontend/src/shared/store/useClientStateSync.ts`
- Create: `frontend/src/shared/store/useClientStateSync.test.tsx`
- Modify: `frontend/src/shared/store/radios.ts`, adding `reset(): void`, which sets `radios: {}`, `selectedRadioId: 0`, `heldPTT: new Set()` and `globalPttTargetId: 0`.
- Modify: `frontend/src/windows/main/MainApp.tsx`
  - Delete `hydrate()`.
  - Remove the client, radio and auth-clearing subscriptions.
  - Keep `setConn`, `setPhase` and `windowState`.
  - Wrap `getOpenWindows` in `hydrateSnapshot`, pushed on `window:state`.
- Modify: `frontend/src/windows/comms/CommsApp.tsx`
  - Remove the `getClientState`, `voiceState` and `radioUpdate` code at `:128-158`.
  - Keep the `hotkeyPressed`/`hotkeyReleased` → `setPTTHeld` subscriptions.
  - Wrap `getProfileState` in `hydrateSnapshot`, pushed on `profile:state`.
- Test: `frontend/src/windows/comms/CommsApp.test.tsx`

**Interfaces:**
- Consumes: `hydrateSnapshot` and `hydrateDelta` (Task 3).
- Produces: `export function useClientStateSync(): void`. It writes `useClients`, `useRadios` and `useSession` (both `self` and `selfGuid`).
  - **Hydrate.** `getClientState` uses the delta rule. Its push sources are `radio_update`, `client_update` and `client_left`. `voiceState` uses the snapshot rule. Its push source is a `radio.<n>.select` hotkey.
  - **On `control:connection` = `connected`:** start a fresh pair of hydrates and dispose the previous pair.
  - **On `profile:state`:** start a fresh `voiceState` snapshot hydrate.
  - **On `auth:session_changed` = `logged_out`:**
    - `useClients.replaceAll({})`
    - `useRadios.reset()`
    - `setSelf(null)`
    - `setSelfGuid("")`
    - dispose the in-flight hydrates
  - **On `hotkey:pressed`:** the regex is `/^radio\.(\d+)\.select$/`. A match calls `setSelectedRadioId(Number(m[1]))`.

- [ ] **Step 1: Write the failing tests.**
  - In `useClientStateSync.test.tsx`, use a harness component that only calls the hook:
    - `"hydrates self and selfGuid"`
    - `"connected re-hydrates"`
    - `"logged_out clears radios, selfGuid, selection and held PTT"`
    - `"radio.3.select hotkey selects 3; radio.3.ptt does not change selection"`
    - `"profile:state re-reads voiceState"`
    - `"a radio_update during the client-state hydrate triggers a re-fetch"`
  - In `CommsApp.test.tsx`:
    - `"re-login with a new guid renders the new radios"`. Hydrate with guid `A`, emit `auth:session_changed` `"logged_out"` and assert "No radios — connect first". Then make `getClientState` return guid `B` with radio `"Fleet"`, emit `control:connection` `"connected"`, and assert `"Fleet"` renders.
    - `"select hotkey and PTT both still work"` (R2). After `radio.2.select` and then `global.ptt`, `useRadios.getState().globalPttTargetId === 2`.
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/shared/store/useClientStateSync.test.tsx src/windows/comms/CommsApp.test.tsx`. Expected: FAIL, because the hook is missing and Comms stays on guid `A`.
- [ ] **Step 3: Implement the hook and rewire both shells.** Call `useClientStateSync()` next to `useSettingsSync()` in each shell. Update the CommsApp and MainApp doc comments to say that the hook owns client state.
- [ ] **Step 4: Run the tests and confirm they pass.** Run `cd frontend && npx vitest run`. Expected: everything passes.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): useClientStateSync — Comms follows selection and session (FE01, FE02)"`.

### Task 6: Delta rule for History

**Files:**
- Modify: `frontend/src/windows/main/screens/History.tsx:36-47`
- Test: `frontend/src/windows/main/screens/History.test.tsx`

**Interfaces:**
- Consumes: `hydrateDelta` (Task 3).

- [ ] **Step 1: Write the failing test.** `"an append during the hydrate re-fetches and shows no duplicate"`.
  1. The first `getHistory` stays pending.
  2. Emit `history:appended` with entry E.
  3. Resolve the first call with `[]`.
  4. The second `getHistory` resolves `[E]`.
  5. Assert that E is rendered **exactly once**.
- [ ] **Step 2: Run the test and confirm it fails.** Run `cd frontend && npx vitest run src/windows/main/screens/History.test.tsx`. Expected: FAIL, because E disappears (it is replaced by `[]`).
- [ ] **Step 3: Implement.**
  - Wrap `getHistory` in `hydrateDelta(..., (rows) => replaceAll(rows ?? []), (e) => console.error("getHistory failed", e))`.
  - Both `history:appended` and `history:cleared` call `pushed()` before they mutate.
  - Remove the `/* not wired yet */` catch.
- [ ] **Step 4: Run the test and confirm it passes.** Run the same command. Expected: PASS.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): history hydrate re-fetches when deltas race it (FE08)"`.

### Task 7: The settings commit path, General/Effects/Audio toggles and the mic test

**Files:**
- Create: `frontend/src/shared/store/settingsCommit.ts`
- Create: `frontend/src/shared/store/settingsCommit.test.ts`
- Modify: `frontend/src/windows/main/screens/settings/sections/General.tsx`
- Modify: `frontend/src/windows/main/screens/settings/sections/Effects.tsx`
- Modify: `frontend/src/windows/main/screens/settings/sections/Audio.tsx`, except the knobs and the VOX range (Task 10)
- Test: `Audio.test.tsx`, `Effects.test.tsx`, and a new `General.test.tsx`

**Interfaces:**
- Produces:

  ```ts
  export const usePendingSettings: UseBoundStore<StoreApi<{ pending: Settings | null }>>
  export function useEffectiveSettings(): Settings | null   // pending ?? useSettings.settings
  export function commitSettings(change: (s: Settings) => Settings): Promise<void>
  ```

  `commitSettings` does the following:
  1. Sets `base = pending ?? store.settings`. If there is none, it returns a resolved promise.
  2. Sets `next = change(base)` and `pending = next`.
  3. Calls `Promise.resolve(api.setSettings(next))`. The call is synchronous, because some test mocks return `undefined`.
  4. On settle, clears `pending` **only if it is still `next`**.
  5. On rejection, also calls `console.error("setSettings failed; reported via the notification channel", err)`. It never rethrows.

- [ ] **Step 1: Write the failing tests.**
  - In `settingsCommit.test.ts`:
    - `"two commits before any echo both survive"`. Commit `master = 0.5`, then `voice = 0.2`. The second `setSettings` payload has both values.
    - `"rejection clears pending so the effective value is the store again"`
    - `"a settle of an older commit keeps the newer pending"`
  - Section tests:
    - `General.test.tsx` `"a rejected save flips the toggle back"`: `setSettings` rejects, and after the rejection settles the toggle again renders the store's value (the Toggle is `role="switch"`; assert its `aria-checked`).
    - Audio `"a rejected startMicTest leaves the button on TEST MIC"`.
    - Effects `"a rejected preview leaves no unhandled rejection and clears the ..."`: `previewEffect` rejects, and the button text returns to `PREVIEW`.
  - Add `usePendingSettings.setState({ pending: null })` to each section test's `beforeEach`.
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/shared/store/settingsCommit.test.ts src/windows/main/screens/settings`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - The sections read `useEffectiveSettings()` instead of `useSettings((s) => s.settings)`.
  - Every `update(patch)` becomes `void commitSettings((s) => ({ ...s, ...patch }))`. For audio and effects, the patch is applied to `s.audio` from the **base**, not from the render-time value.
  - Mic test: `startMicTest().then(() => setTesting(true)).catch(console.error)`. Stop runs `stopMicTest().catch(console.error).finally(() => setTesting(false))`.
  - Effects `preview` gets `catch (e) { console.error("previewEffect failed", e) }`.
  - Rewrite each section's "no local mirror" doc comment. Go remains the source of truth. `pending` is the in-flight request, which is cleared on settle, and a failure is reported by Go's notification channel.
- [ ] **Step 4: Run the tests and confirm they pass.** Run `cd frontend && npx vitest run`. Expected: everything passes.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): settings commit overlay — no lost updates, rollback on failure (FE05, FE09)"`.

### Task 8: `useThrottledCommit`

**Files:**
- Create: `frontend/src/shared/hooks/useThrottledCommit.ts`
- Create: `frontend/src/shared/hooks/useThrottledCommit.test.ts`

**Interfaces:**
- Produces:

  ```ts
  export function useThrottledCommit<T>(send: (v: T) => void, intervalMs = 150): { push(v: T): void; flush(v: T): void }
  ```

  The returned functions are stable across renders. The throttle is leading plus trailing. `flush` cancels any trailing timer and sends `v` immediately. Unmount sends the pending trailing value, if there is one.

- [ ] **Step 1: Write the failing tests.** Use `renderHook` with `vi.useFakeTimers()`.
  - `"leading call sends immediately"`
  - `"pushes inside the window coalesce into one trailing send of the last value"`
  - `"120 pushes over 2000ms send at most ceil(2000/150)+1 times"`
  - `"flush sends now and cancels the trailing send"`
  - `"unmount sends the pending value"`
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/shared/hooks/useThrottledCommit.test.ts`. Expected: FAIL, because the module is missing.
- [ ] **Step 3: Implement.** Use refs for `last`, `pendingValue`, `timer` and the latest `send`.
- [ ] **Step 4: Run the tests and confirm they pass.** Run the same command. Expected: PASS.
- [ ] **Step 5: Commit.** Run `git commit -m "feat(frontend): useThrottledCommit (FE05)"`.

### Task 9: Knob — effect-scoped drag, draft and `onChangeEnd` (FE15)

**Files:**
- Modify: `frontend/src/shared/components/Knob.tsx`
- Test: `frontend/src/shared/components/Knob.test.tsx`

**Interfaces:**
- Produces: `KnobProps` gains `onChangeEnd?: (v: number) => void`.
  - The `pointerdown` handler calls `setPointerCapture` inside a try/catch and records the drag start (`startY`, `startVal`) in state.
  - A `useEffect([drag])` adds `pointermove`, `pointerup` and `pointercancel` on `window`, and its cleanup removes them.
  - While dragging, the Knob renders a local `draft`. Each `pointermove` sets `draft` and calls `onChange`.
  - `pointerup` and `pointercancel` call `onChangeEnd?.(draft)` and then clear the drag.
  - Keyboard: each keydown calls `onChange` as today. `onKeyUp` for the same keys calls `onChangeEnd?.(lastKeyValue)`.

- [ ] **Step 1: Write the failing tests.**
  - `"pointercancel ends the drag"`: after cancel, a `pointermove` calls nothing.
  - `"unmount mid-drag removes window listeners"`: unmount, then fire `pointermove` on `window`, and `onChange` is not called.
  - `"renders the draft while dragging"`: `aria-valuenow` follows the pointer while the `value` prop stays fixed.
  - `"onChangeEnd fires once per drag"`
  - `"onChangeEnd fires on keyup, not on each keydown"`

  The existing 11 tests stay unchanged.
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/shared/components/Knob.test.tsx`. Expected: the new cases FAIL.
- [ ] **Step 3: Implement** as described in the Interfaces block. `RadioBlock.tsx:73-94` is the in-repo model for the effect-scoped drag.
- [ ] **Step 4: Run the tests and confirm they pass.** Run the same command. Expected: 16/16 pass.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): Knob drag is effect-scoped with pointercancel and a draft (FE15)"`.

### Task 10: Audio drag cadence — level knobs and the VOX range (FE05)

**Files:**
- Modify: `frontend/src/windows/main/screens/settings/sections/Audio.tsx`
- Test: `frontend/src/windows/main/screens/settings/sections/Audio.test.tsx`

**Interfaces:**
- Consumes:
  - `useThrottledCommit` (Task 8)
  - `Knob` `onChangeEnd` (Task 9)
  - `commitSettings` (Task 7)
- Produces: a file-local component, `function LevelKnob({ bus, label, value }: { bus: keyof AudioLevels; label: string; value: number })`.
  - It owns one throttle whose `send` is `(v) => void commitSettings((s) => ({ ...s, audio: { ...s.audio, levels: { ...s.audio.levels, [bus]: fromKnob(v) } } }))`.
  - It passes `onChange={push}` and `onChangeEnd={flush}` to the Knob.
  - The VOX range keeps a local draft and gets its own throttle: `onChange` pushes, while `onPointerUp`, `onKeyUp` and `onBlur` flush the draft.

- [ ] **Step 1: Write the failing tests.** Use fake timers.
  - `"a 2s / 120-move MASTER drag calls setSettings at most 16 times and the last carries the release value"`. Fire `pointerDown`, then 120 `pointermove` events on `window` while advancing timers by 16.67 ms each, then `pointerup`.
  - `"VOX threshold drag is throttled the same way"`. First seed `vox: true`.
  - `"keyboard auto-repeat on a knob is throttled and commits on keyup"`.
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/windows/main/screens/settings/sections/Audio.test.tsx`. Expected: FAIL, with about 120 calls.
- [ ] **Step 3: Implement** as described in the Interfaces block.
- [ ] **Step 4: Run the tests and confirm they pass.** Run the same command. Expected: PASS.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): throttle knob and VOX drags to 150ms with a final commit (FE05)"`.

### Task 11: Comms, RadioCard and History FE09 corrections

**Files:**
- Modify: `frontend/src/windows/comms/RadioCard.tsx:63-70`
- Modify: `frontend/src/windows/comms/CommsApp.tsx`, the `flush`, SAVE, REVERT, RESET and close handlers
- Modify: `frontend/src/windows/main/screens/History.tsx:123`
- Test: `RadioCard.test.tsx`, `CommsApp.test.tsx` and `History.test.tsx`

- [ ] **Step 1: Write the failing tests.**
  - RadioCard `"a rejected selectRadio re-reads voiceState and shows the backend's selection"`. Set `selectRadio` to reject and `voiceState` to resolve `{ selected_radio: 2 }`. Click radio 2, then assert `selectedRadioId === 2` and that `voiceState` was called once.
  - CommsApp `"rejected save/revert/reset/layout flush produce no unhandled rejection"`. Make each mock reject. Attach a `process.on("unhandledRejection")` spy, and assert after a flush that it was not called.
  - History `"a rejected export produces no unhandled rejection"`.
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/windows/comms src/windows/main/screens/History.test.tsx`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - **RadioCard `select`:** keep the optimistic `setSelectedRadioId`, then call `api.selectRadio(id).catch(() => api.voiceState().then((vs) => useRadios.getState().setSelectedRadioId(vs.selected_radio)).catch(console.error))`.
  - **RadioCard `commit`:** `api.updateRadioInfo(...).catch((e) => console.error("updateRadioInfo failed; reported via notifications", e))`.
  - **CommsApp:** each `void api.x()` becomes `api.x().catch((e) => console.error("<x> failed", e))`.
  - **History:** `exportHistoryCsv` gets the same treatment.
  - Update the RadioCard doc so that it describes the re-read on failure.
- [ ] **Step 4: Run the tests and confirm they pass.** Run the same command. Expected: PASS.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): failed Comms/History actions correct their UI and never reject unhandled (FE09)"`.

### Task 12: Keyboard navigation (FE04)

**Files:**
- Modify: `frontend/src/shared/components/NavRail.tsx`, at the items (`:55-64`), Logout (`:70-78`) and the toggle (`:44-50`)
- Modify: `frontend/src/windows/main/screens/settings/SettingsScreen.tsx:89-96`
- Modify: `frontend/src/windows/main/screens/Home.tsx:43-55`
- Create tests: `frontend/src/shared/components/NavRail.test.tsx`, `frontend/src/windows/main/screens/settings/SettingsScreen.test.tsx` and `frontend/src/windows/main/screens/Home.test.tsx`

**Interfaces:**
- Consumes: `activatable(onActivate)` (`shared/components/activatable.ts`).

- [ ] **Step 1: Write the failing tests** with `userEvent`.
  - NavRail:
    - Tab reaches every item and Logout.
    - `{Enter}` and `{ }` call `onSelect` and `onLogout`.
    - Exactly one element has `aria-current="page"`, the one for `activeKey`.
    - The toggle has `type="button"` and an accessible name of either `Collapse` or `Expand`.
  - SettingsScreen:
    - Every section can be tabbed to.
    - Enter switches to it.
    - Exactly one element has `aria-current="true"`.
  - Home:
    - Tab reaches the Comms tile.
    - Enter calls `toggleWindow`.
    - No inert tile is in the tab order: `getAllByRole("button")` contains only the Comms tile among the tiles.
- [ ] **Step 2: Run the tests and confirm they fail.** Run `cd frontend && npx vitest run src/shared/components/NavRail.test.tsx src/windows/main/screens/settings/SettingsScreen.test.tsx src/windows/main/screens/Home.test.tsx`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - **NavRail items:** spread `{...activatable(() => onSelect(it.key))}` in place of `onClick`, and add `aria-current={active ? "page" : undefined}` and `aria-label={it.label}`.
  - **NavRail Logout:** the same spread, without `aria-current`.
  - **Settings rail:** `aria-current={active ? "true" : undefined}`.
  - **Home:** spread `activatable(toggleComms)` only when the tile is Comms.

  classNames and DOM shape stay unchanged.
- [ ] **Step 4: Run the tests and confirm they pass.** Run the same command. Expected: PASS.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): keyboard-operable NavRail, Settings rail and Comms tile with aria-current (FE04)"`.

### Task 13: Toasts as a live region (FE14, toast half)

**Files:**
- Modify: `frontend/src/shared/components/ToastHost.tsx:211`
- Test: `frontend/src/shared/components/ToastHost.test.tsx`

- [ ] **Step 1: Write the failing test.** `"an error toast is exposed as an alert"`. Seed one unresolved error item, then assert that `getByRole("alert")` has the item's title and that it carries `aria-atomic="true"`.
- [ ] **Step 2: Run the test and confirm it fails.** Run `cd frontend && npx vitest run src/shared/components/ToastHost`. Expected: FAIL.
- [ ] **Step 3: Implement.** Add `role="alert" aria-atomic="true"` to each `.toast` div. The stack still returns `null` when it is empty.
- [ ] **Step 4: Run the tests and confirm they pass.** Run the same command. Expected: PASS, with the `ToastHost.rerender` tests unchanged.
- [ ] **Step 5: Commit.** Run `git commit -m "fix(frontend): announce error toasts (FE14)"`.

### Task 14: Full verification, docs, PR

**Files:**
- Create: `docs/superpowers/specs/2026-10-27-phase-6-9-cl3-frontend-correctness-design.md`, copied from the scratchpad `design-CL3.md`
- Create: `docs/superpowers/plans/2026-10-27-phase-6-9-cl3-frontend-correctness.md`, this plan

- [ ] **Step 1: Run the full Go verification** (Global Constraints). Expected:
  - `go build` and `go vet` print nothing;
  - every package is `ok`;
  - the `-run TestBoundSurfaceIsTheFrontendAPI` output shows PASS.
- [ ] **Step 2: Run the full frontend verification** (Global Constraints). Expected:
  - vitest: all files pass, with a count of at least 517 plus the new tests;
  - `tsc` prints nothing;
  - `vite build` succeeds.
- [ ] **Step 3: Invoke `superpowers:verification-before-completion`** and `superpowers:requesting-code-review` on the branch diff. Fix the findings, re-run Steps 1 and 2, and commit.
- [ ] **Step 4: Commit the docs**

```bash
git add docs/superpowers/specs/2026-10-27-phase-6-9-cl3-frontend-correctness-design.md docs/superpowers/plans/2026-10-27-phase-6-9-cl3-frontend-correctness.md
git commit -m "docs(phase-6.9): CL3 frontend correctness design and plan"
```

- [ ] **Step 5: Push and open the PR**

```bash
git push -u origin chore/cleanup-cl3
gh pr create --base main --title "Phase 6.9 CL3: frontend correctness" --body-file <scratchpad>/cl3-pr-body.md
```

  The PR body must include:
  - the FE01, FE02, FE04, FE05, FE08, FE09, FE14 and FE15 summary;
  - the behaviour changes (design §8);
  - the notification key table;
  - the verification output;
  - the CL1 rebase note (design §11);
  - the manual-verification items below;
  - the attribution line `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

  Do not merge. The orchestrator merges.

## Manual verification (for `docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`; DOC owns that file)

Run these in a real Wails build (`wails3 build`) against a real server. None of them can be automated here.

1. **Select hotkey.** With Comms open, press a `radio.<n>.select` hotkey. The Comms highlight moves to R0n. Load and then revert a profile, and the highlight matches the profile's selection.
2. **Re-login.** With Comms open, Logout. Comms shows "No radios — connect first". Log back in, and Comms shows the new session's radios without being reopened.
3. **Forced failure.** Make the config directory read-only, then do each of the following:
   - toggle a General setting;
   - drag MASTER;
   - select a radio in Comms;
   - rename a radio;
   - press RESET in Comms.

   Each kind raises **one** main-window toast plus a bell entry, including the actions taken in Comms. Dragging repeatedly does not stack toasts or sounds. Each failed toggle or knob returns to its stored value, and the selection highlight stays on the radio the backend targets. Restore write access and repeat one action. The bell entry shows as resolved.
4. **No audio backend.** On a machine or VM with no audio backend, TEST MIC raises "Could not start the mic test", and the button stays "TEST MIC".
5. **Send failure.** Rename a radio while the control link is down (stop the server, then edit before the banner clears). Expect "Radio change saved, but not sent to the server".
6. **Live level.** Start a mic test or receive voice, then drag a level knob. The level changes audibly during the drag, in steps of about 150 ms, and the final value persists across a restart.
7. **Keyboard only.** Use Tab, Enter and Space through the NavRail, Logout, the Settings rail and the Home Comms tile. The focus ring is visible, and nothing changes visually otherwise.
8. **Screen reader.** With NVDA on Windows (or VoiceOver), an error toast is announced, and `aria-current` is read on the active nav item and Settings section.
9. **Hidden main window.** Trigger a Comms failure (item 3) while the main window is minimised to the tray. The bell entry and badge are present when the window is restored. This is accepted risk R5.
10. **No snap-back on commit (review finding, added after implementation).** Toggle a General setting several times, and release a level knob after a drag. Watch for a one-frame flicker back to the old value. `commitSettings` clears its pending overlay when `SetSettings` resolves; Go emits the `settings:changed` echo before it returns, but in Wails v3 the call reply and the event travel on separate channels, so their arrival order in the webview is not guaranteed. If the flicker is visible, keep `pending` until the echo arrives (with a timeout so a no-op save cannot pin it).
