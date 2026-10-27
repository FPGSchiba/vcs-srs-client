# Phase 6.9 — CL3: Frontend correctness — design

**Status:** draft for approval (pass 2 of 2; the pass-1 open questions are answered, see §0).
**Date:** 2026-10-27
**Repo:** `vcs-srs-client`. Mostly frontend, plus a **narrow** Go change in `internal/app` (user decision Q1). There is no new bound method, no dependency change, no proto change and no CI change.
**Branch:** `chore/cleanup-cl3`, cut from `origin/main` at `5a888e2` (CL2 #43, CL4 #41 and CL5 #42 are all merged; Wave 2 of the umbrella).
**Designed against:** `origin/main` at `5a888e2`. The pass-1 draft read the frontend from the CL4 branch head `30d8b75`. Between that commit and `main`, the only `frontend/src` difference is three removed `EV` entries in `shared/api/events.ts` (`clientState`, `settingsUpdate` and `serverAction`), and CL3 uses none of them. Every frontend line reference below was re-checked on `main`. The Go references were re-checked on `5a888e2`. No Go file changed between `4824781` and `5a888e2`.
**Umbrella:** `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md`, §3 CL3.

---

## 0. Decisions

| # | Decision | Source |
|---|---|---|
| D1 | **FE09 failures are reported through the notification channel.** Go raises an error notification (`internal/notify`, as `notifyProfileError` does) from each user-initiated binding that fails: settings save, radio select, radio edit (save and send), layout reset, mic-test start and effect preview. The user sees a main-window toast and a bell entry. A failure in the Comms popout therefore shows in the main window; the user accepted this. The frontend still corrects its own optimistic UI: a failed `selectRadio` re-reads `voiceState`, and settings drafts are dropped. Nothing rolls back silently to a value that is false. There is no inline error line. | **user decision** (Q1, 2026-10-27) |
| D2 | **FE05: knob and VOX drags commit live, throttled.** They commit at most once per 150 ms during the drag, then once more on release. This is frontend only. | **user decision** (Q2, 2026-10-27) |
| D3 | `TestBoundSurfaceIsTheFrontendAPI` stays accurate: no bound method is added or removed. The new Go helpers are unexported. | user constraint (Q1/Q2 answers) |
| D4 | Keep CL3's Go edits narrow. CL1 (Wave 3) edits `internal/app/bindings.go`, `profiles.go` and `session.go` afterwards and rebases on CL3. | user constraint |
| D5 | The notification-channel rules in CLAUDE.md are respected. Each failure raises on a **stable key**, with only user-visible fields set, so `notify`'s fingerprint collapses repeats. No emitter is de-chattied. | user constraint and CLAUDE.md |
| D6 | **A success resolves its key** (`notify.Resolve`), as `notifyHistoryFlush` already does. Without this, the same failure after an intervening success would keep the same fingerprint as the still-live item and be dropped silently: no toast, no sound. | worker's call, with precedent |
| D7 | The Go raise runs **after** `writeMu` is released. Each binding gets a named `err` return and a deferred report. | worker's call (lock hygiene) |
| D8 | `SetCommsLayout` gets no notification and the frontend does no rollback for it. It cannot fail: it always returns `nil` and only writes memory (`profiles.go:764-791`). This corrects pass 1, which planned to "re-hydrate the layout on a refused flush". | verification |
| D9 | `ExportHistoryCSV` already raises `history:export` on a write failure (`history.go:271-297`), so there is no Go change for it. The frontend only adds a `catch`. | verification |
| D10 | SaveProfile, SaveProfileAs and LoadProfile/RevertProfile already notify through `notifyProfileError`, and they are not changed. Their "no active profile" error returns cannot be reached from the UI, because SAVE and REVERT render only when `active_path` is set. | verification |
| D11 | The settings commit path uses a per-window **pending overlay**: the newest full struct sent but not yet settled. Every commit is built from `pending ?? store`, which fixes the lost update. The sections render from `pending ?? store`. When a commit settles (resolves or rejects), the overlay is cleared and the store, which only the echo updates, is shown again. A rejection is therefore the rollback. | worker's call |
| D12 | The Knob fires `onChangeEnd` on `pointerup`, `pointercancel` and **`keyup`**, not on every keydown. Keyboard auto-repeat therefore goes through the same throttle. | worker's call |
| D13 | Notification categories use the keys from the frontend table (`shared/components/notificationCategories.ts`): `system` for settings and audio, `comms` for radios, `profile` for the layout reset. Icons come from `Icon.tsx`: `settings`, `radio`, `layout` and `mic`. | worker's call |

## 1. Intent

Make the frontend tell the truth and respond to the keyboard:

- the Comms popout follows selection and session changes made anywhere;
- hydrate-then-subscribe can't roll a store back to an older value;
- a user action that fails says so, through the notification channel, and its optimistic UI is undone;
- a knob drag no longer writes the config file once per pointer-move frame;
- the primary navigation can be used without a mouse;
- error toasts are announced.

Nothing on the wire changes.

## 2. Findings — re-verification (on `5a888e2`)

| ID | Status | Evidence |
|---|---|---|
| **FE01** | holds | Comms reads the selection once, at `windows/comms/CommsApp.tsx:139-144`. Its `hotkey:pressed` handler (`:151-153`) only calls `setPTTHeld`. In Go, `settings.go:1044-1045` dispatches `dispatchAudioPressed` (which sets the selection for `radio.<n>.select`, `audio.go:221-222`) **before** `em.HotkeyPressed`. `applyProfile` re-validates the selection (`profiles.go`). No selection event exists. |
| **FE02** | holds | Comms hydrates `getClientState` once and never listens to `control:connection` or `auth:session_changed`. Only `windows/main/MainApp.tsx:110-124` handles them. `App.Disconnect` does not reset the Go store until CL1's CON-07, so the frontend must clear explicitly. |
| **FE08** | holds-with-correction | The unguarded hydrates are `useNotificationsSync.ts:20-22`, the six snapshot channels in `useSettingsSync.ts:33-93`, `History.tsx:36-42` and `getClientState` in Comms and Main. **Correction:** the stale-flag rule is right only for snapshot channels. Delta channels (history, client state) need re-fetch (§3.3). |
| **FE09** | holds-with-correction | The fire-and-forget sites are `General.tsx:26`, `Audio.tsx:87,106,109`, `Effects.tsx:61,70-77` (which has no `catch`, so an unhandled rejection), `RadioCard.tsx:64,69`, `CommsApp.tsx:76,233,244,253` and `History.tsx:123`. **Corrections:** (a) `SelectRadio` (`voice.go:668-671`) applies the selection before the persist that can fail, so the right undo is to re-read, not to roll back. (b) D8, D9 and D10 above. |
| **FE05** | holds | `Knob.tsx:63-77` → `Audio.tsx:86-92` → `SetSettings` (`settings.go:268-301`). Each call is one `config.Save` under `writeMu` plus one broadcast to every window, once per frame. Each update also spreads the render-time `settings`, which loses updates. |
| **FE15** | holds | The window listeners are added inside the handler and removed only on `up`. There is no `pointercancel` and no unmount cleanup. |
| **FE04** | holds | `NavRail.tsx:55-64` (items) and `:70-78` (Logout), `SettingsScreen.tsx:89-96` (the rail) and `Home.tsx:43-55` (the Comms tile) are all `div onClick`. None has a role, a key handler or `aria-current`. `shared/components/activatable.ts` exists for exactly this. |
| **FE14 (toast half)** | holds | `ToastHost.tsx:206-238` has no role and no live region. It shows error severity only. |

## 3. Approach

### 3.1 FE01 + FE02 — `useClientStateSync`

**New file:** `frontend/src/shared/store/useClientStateSync.ts`. It is mounted once in `MainApp` and once in `CommsApp`. It owns:

- **Hydrate.** `getClientState` fills clients, radios, `self` **and** `selfGuid`, using the delta rule (§3.3). `voiceState` fills `selectedRadioId`, using the snapshot rule.
- **Re-hydrate** on `control:connection` = `connected`.
- **Re-read `voiceState`** on `profile:state`.
- **Clear on `auth:session_changed` = `logged_out`:** clients, radios, `self`, `selfGuid` and the selection, plus held PTT and the frozen global-PTT target. This uses a new `useRadios().reset()`.
- **Deltas:** `radio_update`, `client_update` and `client_left`.
- **Selection from hotkeys:** a `hotkey:pressed` whose id matches `/^radio\.(\d+)\.select$/` calls `setSelectedRadioId(n)`. It also marks the in-flight `voiceState` hydrate stale.

`MainApp` keeps `setConn`/`setPhase` and the window list. Its `hydrate()` function and its client, radio and auth clearing move into the hook. `CommsApp` keeps the layout, the profile, and the PTT `setPTTHeld` subscription.

After logout the Comms popout falls into its existing "No radios — connect first" state. It does not close itself.

### 3.2 (removed in pass 2; the Comms empty state is folded into §3.1)

### 3.3 FE08 — `hydrate.ts`

**New file:** `frontend/src/shared/store/hydrate.ts`, with two plain functions. Each returns `{ pushed(): void; dispose(): void }`.

- `hydrateSnapshot(fetch, apply, onError)`. If a push arrives while the fetch is in flight, the fetch result is dropped. This is the rule for notifications, the six `useSettingsSync` channels, `voiceState`, Comms `getProfileState`, and MainApp `getOpenWindows`.
- `hydrateDelta(fetch, apply, onError, maxAttempts = 3)`. If a push arrives while the fetch is in flight, the result is discarded and the data is re-fetched. On the last attempt the result is applied anyway, with a `console.warn`. This is the rule for history and client state.

`getAudioEffectPresets` is static and gets no guard. The Comms `hydrateLayout` keeps its own `pending` guard. The stale `/* not wired yet */` catches log through `console.error`.

### 3.4 FE09 — Go raises, the frontend corrects

**Go.** A new file, `internal/app/notify_actions.go`, holds:

```go
type actionFailure struct{ key, category, icon, title string }
func (a *App) reportAction(f actionFailure, err error) // err != nil → Raise(f.key, …Body: err.Error()); nil → Resolve(f.key); no-op when a.notif == nil
```

Each binding below gets a named `err` return and `defer func() { a.reportAction(…, err) }()`, registered **before** any lock. The defer therefore runs after the lock is released (D7). The keys, titles and categories are fixed:

| Binding (file) | Key | Category / Icon | Title |
|---|---|---|---|
| `SetSettings` (`settings.go`) | `action:settings.save` | system / settings | `Could not save settings` |
| `SelectRadio` (`voice.go`) | `action:radio.select` | comms / radio | `Radio selected, but the selection was not saved` |
| `UpdateRadioInfo`, persist step (`bindings.go`) | `action:radio.save` | comms / radio | `Could not save the radio change` |
| `UpdateRadioInfo`, send step (`bindings.go`) | `action:radio.send` | comms / radio | `Radio change saved, but not sent to the server` |
| `ResetLayout` (`profiles.go`) | `action:layout.reset` | profile / layout | `Could not reset the layout` |
| `StartMicTest` (`audio.go`) | `action:audio.mictest` | system / mic | `Could not start the mic test` |
| `PreviewEffect` (`audio.go`) | `action:audio.preview` | system / volume | `Could not preview the effect` |

Error texts are deterministic. For example, `config.Save` uses the fixed `path + ".tmp"`. A repeated identical failure is therefore one no-op `Raise`, not a toast storm during a knob drag on a read-only config directory.

`UpdateRadioInfo` reports its two steps separately. A persist failure raises `radio.save` and does not send. A send failure, including `not connected`, raises `radio.send`. A full success resolves both keys.

**Frontend.** Every listed call site gets a `.catch` (logged with `console.error`; the Go notification is the user surface) and the matching UI correction:

| Site | Correction |
|---|---|
| General / Audio / Effects `setSettings` | `commitSettings` (D11). A rejection clears the overlay, and the control shows the store value again. |
| Audio mic test | `testing` turns true only after `startMicTest` resolves. Stop sets it false in `finally`. |
| Effects PREVIEW | A `catch` is added, which fixes the unhandled rejection. |
| RadioCard `select` | On rejection, re-read `api.voiceState()` and set `selectedRadioId` from it. |
| RadioCard `updateRadioInfo` | `catch` only. The store follows the echo. |
| Comms SAVE / REVERT / RESET / layout flush / close | `catch` only. |
| History EXPORT CSV | `catch` only. |

### 3.5 FE05 + FE15 — Knob, throttle, commit path

- **Knob.** The drag is effect-scoped, with `pointermove`/`pointerup`/`pointercancel` on `window`, removed in the cleanup (unmount included). `setPointerCapture` is called inside a try/catch. The Knob shows a local `draft` while dragging. A new optional prop, `onChangeEnd?(v)`, fires on `pointerup`, `pointercancel` and `keyup` (D12). Without that prop, the Knob behaves as it does today.
- **New file:** `frontend/src/shared/hooks/useThrottledCommit.ts`. `useThrottledCommit<T>(send, 150)` returns `{ push(v), flush(v) }`, a leading-plus-trailing throttle. It flushes on unmount.
- **New file:** `frontend/src/shared/store/settingsCommit.ts`, holding `usePendingSettings`, `useEffectiveSettings()` and `commitSettings(change)` (D11).
- **Audio.** Each of the four level knobs and the VOX range uses its own throttle: `push` on change, `flush` on end. The range's `flush` runs on `pointerup`, `keyup` and `blur`.

### 3.6 FE04 — keyboard navigation

These elements spread `activatable(...)`:

- **NavRail items:** also `aria-current="page"` when active, and an `aria-label`.
- **NavRail Logout.**
- **Settings rail:** `aria-current="true"` when active.
- **Home Comms tile:** keeps `aria-pressed`. The inert tiles are not focusable.

The collapse toggle gets `type="button"` and an `aria-label`. Nothing changes visually.

### 3.7 FE14 (toast half)

Each `.toast` gets `role="alert"` and `aria-atomic="true"`. Welcome belongs to CL1.

## 4. Files

**Go (modified, narrow):**
- `internal/app/settings.go`: `SetSettings` only.
- `internal/app/voice.go`: `SelectRadio` only.
- `internal/app/bindings.go`: `UpdateRadioInfo` only.
- `internal/app/profiles.go`: `ResetLayout` only.
- `internal/app/audio.go`: `StartMicTest` and `PreviewEffect`.

**Go (new):** `internal/app/notify_actions.go` and `notify_actions_test.go`.

**Frontend (modified):**
- `shared/components/{Knob,NavRail,ToastHost}.tsx`
- `shared/store/{useNotificationsSync,useSettingsSync,radios}.ts`
- `windows/comms/{CommsApp,RadioCard}.tsx`
- `windows/main/MainApp.tsx`
- `windows/main/screens/{History,Home}.tsx`
- `windows/main/screens/settings/{SettingsScreen.tsx,sections/{Audio,Effects,General}.tsx}`

**Frontend (new), each with a test:**
- `shared/store/{hydrate,useClientStateSync,settingsCommit}.ts`
- `shared/hooks/useThrottledCommit.ts`

**Not touched:** `api/client.ts`, `api/events.ts`, `Welcome.tsx` (CL1), `Profiles.tsx`, `NotificationsApp.tsx`, `TopBar.tsx`, `StatusBar.tsx`, `Keybinds.tsx`, any emitter, and `internal/notify`.

## 5. Tests first

The plan pins each test. In summary:

- **Go.**
  - Each binding raises exactly one item, with its key, title and category, on failure.
  - A second identical failure publishes nothing.
  - A success resolves the key.
  - With no notifier wired, nothing panics.
  - A notifier `OnChange` callback that calls back into `App` does not deadlock.
- **Frontend.**
  - `useClientStateSync`: FE01 and FE02.
  - `hydrate`: the snapshot and delta rules.
  - `settingsCommit`: the lost update, and rollback on rejection.
  - `useThrottledCommit`: the cadence.
  - `Knob`: cancel, unmount, draft and `onChangeEnd`.
  - Audio drag cadence: a 2 s drag makes at most 16 calls, and the last one carries the release value.
  - FE09: the correction at each site.
  - FE04: Tab, Enter/Space and `aria-current`.
  - FE14: `role="alert"`.

## 6. Out of scope (recorded follow-ups)

- **Fire-and-forget sites outside FE09's evidence:** `NotificationsApp`, the `TopBar`/`StatusBar` window toggles, `Keybinds.clearKeybind`, MainApp Logout `disconnect` (owned by CL1), and the refresh catch in `Profiles.tsx`.
- **The hotkey-path selection persist** (`queueSelectedRadioPersist`). It only logs a failure; it is not a binding.
- **The PTT delay and VOX min-length number inputs.** Each keystroke commits, and an empty field becomes `0`.
- **`RadioBlock`'s resize drag**, which has no `pointercancel`.
- **The `notifyProfileError` category string `"Profiles"`.** It is not a key in the category table, so it falls back to System. This is existing behaviour and is not changed.
- **Closing the Comms popout on logout.**

## 7. Open questions

None. Q1 and Q2 are resolved as D1 and D2.

## 8. Behaviour changes (user-visible; nothing is wire-visible)

1. The Comms highlight follows `radio.<n>.select` hotkeys and profile load/revert.
2. After logout the Comms popout shows "No radios — connect first". After re-login it shows the new session's radios without being reopened.
3. A failed settings save, radio select, radio edit (save or send), layout reset, mic-test start or effect preview raises an error notification: a main-window toast plus a bell entry, even when the action was taken in the Comms popout. A repeated identical failure does not re-toast until a success clears it. A failed select keeps the highlight on what the backend actually targets.
4. A level knob moves under the pointer immediately, and the level is heard about every 150 ms during a drag.
5. NavRail, Logout, the Settings rail and the Home Comms tile can be used by keyboard. `aria-current` and error toasts are announced.

## 9. Approval gates

None. Nothing in this design changes a dependency, the proto, CI or a bound method, and it is not a destructive git operation. The Go edits were directed by the user's Q1 answer.

## 10. Risks

- **R1. CL1 rebase.** CL1 edits `bindings.go` and `profiles.go` after CL3. CL3 touches only `UpdateRadioInfo` and `ResetLayout` in those files, plus one new file. CL1 must keep the deferred `reportAction` lines if it restructures either function.
- **R2.** Comms subscribes to `hotkey:pressed` twice, once in the hook (select) and once in CommsApp (PTT). This is harmless, and a test pins it.
- **R3.** The delta re-fetch is bounded at 3 attempts. Under a sustained burst, history may be one row behind until the next append, and a `console.warn` records it.
- **R4.** The throttle means about 7 disk writes per second during a drag, serialised under `writeMu`.
- **R5.** The Comms popout cannot show failures by design (D1). If the main window is hidden in the tray, the toast is not seen until the window is shown; the bell keeps the entry.
- **R6.** None of this has been run in the real Wails webview or checked with a screen reader.

## 11. Cross-track contracts

- **CL1 (Wave 3, after CL3).**
  - It rebases on CL3's `bindings.go` and `profiles.go` edits (R1).
  - Session expiry must reach the windows as `auth:session_changed` = `logged_out`, or CL1 extends the clear branch of `useClientStateSync`.
  - CON-07's `ResetRoster` makes Go agree with what the hook already clears.
  - A Connect/Disconnect failure surface should use the same `reportAction` helper.
  - The umbrella overlap matrix (CL3 × CL1 cell) should now read "bindings, profiles". DOC records this.
- **CL2 (merged).** No bound-surface change.
- **UP.** It must keep the `on()` envelope (`e.data`).
- **Manual checklist** (`docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`): see the plan's final section.
