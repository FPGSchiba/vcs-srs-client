# Hotkey permission + banner de-duplication — implementation report

**Branch:** `feat/phase-3-settings-keybinds` (PR #23)
**Date:** 2026-09-15

Three approved changes on top of the open PR: suppress duplicate per-row
warnings while the banner is showing, record the Phase 7 alerting requirement,
and request the macOS Input Monitoring permission that global hotkeys need.

---

## Change 1 — suppress per-row warnings when the banner is showing

**File:** `frontend/src/windows/main/screens/settings/sections/Keybinds.tsx`

`renderChip` now reads:

```ts
const failedReason = hotkeys.registered ? hotkeys.failed[kb.action_id] : undefined;
```

Since the final review's fix to `hotkeys.Manager.Registered`, `registered ===
false` means *nothing* registered at all, which implies `failed` names every
bound action. The old unconditional lookup therefore rendered the banner's one
message plus an identical warning under every single row. A partial failure
(one `Numpad7` among nineteen working binds) keeps `registered === true`, so
those rows still get their per-row reason — the only case where the inline
text says something the banner does not.

The component's doc-comment point 3 was extended to state the invariant, and a
new point 4 documents the permission-aware banner.

**Tests added** (`Keybinds.test.tsx`):

- `does not repeat the banner as a per-row warning on every binding` — sets
  `registered: false` with `failed` naming all three fixture rows, asserts the
  banner renders once and that **no row** repeats it. Scoped per row (matching
  `[data-row], tr`, since per-radio binds live in a `.tbl` row rather than a
  `[data-row]` div) so a regression that leaks the reason onto even one row
  fails.
- `still shows per-row reasons for a PARTIAL failure, with no banner` — the
  complementary case: `registered: true` with one failing action, asserts the
  reason appears on that row, on *only* that row, and that no banner renders.

---

## Change 2 — record the alerting requirement for Phase 7

**`docs/ROADMAP.md`**, Phase 7 headline deliverables, gains an entry:

> Route hotkey-registration failures through the notification channel,
> replacing Phase 3's inline banner in the Keybinds section. Two distinct
> cases, and they must stay distinct: a GLOBAL failure (nothing registered at
> all) notifies **once**, carrying the permission state and its remedial
> action; a PER-BINDING failure notifies **per action**, naming the action.

**`docs/superpowers/specs/2026-09-15-vcs-client-phase-3-settings-keybinds-design.md`**,
§12 risk R12, was rewritten from an unqualified plan to a MITIGATED entry that
records what actually shipped (the `permission` field, the `PermissionChecker`
seam, the two CoreGraphics calls, the verified settings-pane URL, the
"grant is concluded from preflight only" rule, and the focus-plus-bounded-poll
detection strategy) and ends with an explicit **Deferred to Phase 7** pointer
back to `docs/ROADMAP.md`. The requirement is now discoverable from the phase
that created it, not only from Phase 7.

---

## Change 3 — request macOS Input Monitoring permission

### New package files

| File | Contents |
|---|---|
| `internal/hotkeys/permission.go` | `Permission` enum + `String()`, `PermissionChecker` interface, `ErrNoPermissionSettings` |
| `internal/hotkeys/permission_darwin.go` | cgo over `CGPreflightListenEventAccess` / `CGRequestListenEventAccess`, `OpenSettings()` |
| `internal/hotkeys/permission_other.go` | `PermissionNotApplicable` no-op checker |

### The build-tag partition, and why it is exhaustive

| File | Tag |
|---|---|
| `permission_darwin.go` | `//go:build darwin && cgo` |
| `permission_other.go` | `//go:build !darwin \|\| !cgo` |

The second tag is the literal De Morgan complement of the first:
`¬(darwin ∧ cgo) ≡ (¬darwin ∨ ¬cgo)`. Exhaustiveness and disjointness are
therefore structural, not enumerated — every build matches exactly one of the
two, and no build can match both or neither. This mirrors the existing
registrar split (`windows || cgo` vs `!windows && !cgo`), which is the same
construction.

`darwin && cgo` rather than plain `darwin` for two reasons. The mechanical one
is that the CoreGraphics calls *are* cgo. The substantive one: a `darwin`
build without cgo has no hotkey backend at all — `registrar_nocgo.go`
(`!windows && !cgo`) claims it and fails every `Register` with
`ErrBackendUnavailable` — so an Input Monitoring grant would enable nothing,
and reporting `"denied"` there would send the user to System Settings to fix a
problem only a rebuild can fix. `PermissionNotApplicable` is the honest answer
for that build.

Note this pair is a *wider* split than the registrar pair, deliberately: the
registrars partition on "is there a usable hotkey backend", these partition on
"is there an OS permission to ask for". Windows and Linux/X11 have a backend
and no permission, so they land on the working registrar *and* on the no-op
checker.

**Verified empirically** with `go list` across every reachable target:

```
darwin  cgo   -> [hotkeys.go keymap_darwin.go permission.go registrar_x.go]  CGO:[permission_darwin.go]
darwin  nocgo -> [hotkeys.go permission.go permission_other.go registrar_nocgo.go]
windows cgo   -> [hotkeys.go keymap_windows.go permission.go permission_other.go registrar_x.go]
linux   cgo   -> [hotkeys.go keymap_linux.go permission.go permission_other.go registrar_x.go]
linux   nocgo -> [hotkeys.go permission.go permission_other.go registrar_nocgo.go]
```

Exactly one `permission_*.go` per target in all five. `go build` succeeds for
darwin+cgo, darwin+nocgo and windows (the cgo file genuinely compiles against
the real CoreGraphics headers here; only the final link fails, which is the
documented sandbox limitation).

### The settings-pane URL — VERIFIED, no fallback needed

`x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent`

I could not reach Apple's documentation from this environment (not on the
network allowlist), so I verified it against the shipping OS instead, on
**macOS 26.6.2 build 25G83**:

1. **Pane identifier.** `/System/Library/PreferencePanes/Security.prefPane/Contents/Info.plist`
   → `CFBundleIdentifier = "com.apple.preference.security"`.
2. **Anchor.** `/System/Library/PreferencePanes/Security.prefPane/Contents/Resources/PrivacyTCCServices.plist`
   maps the TCC service a `CGEventTap` needs:
   ```
   "kTCCServiceListenEvent" => {
       "revealElementKeyName" => "Privacy_ListenEvent"
       ...
   }
   ```

Both halves of the URL are read directly out of the shipping pane bundle, so
this is a stronger check than a doc quote. **The fallback to the general
Privacy & Security pane was not needed and is not present.** I did not
*execute* the deep link (opening System Settings would take over the GUI); the
identifier/anchor pair is confirmed, the dispatch itself is not.

Also verified the two CG functions exist in the installed SDK:

```
$(xcrun --show-sdk-path)/System/Library/Frameworks/CoreGraphics.framework/Headers/CGEvent.h
399: CG_EXTERN bool CGPreflightListenEventAccess(void) API_AVAILABLE(macos(10.15));
402: CG_EXTERN bool CGRequestListenEventAccess(void) API_AVAILABLE(macos(10.15));
```

### The state machine (`internal/app`)

- `HotkeyStateDTO` and `events.HotkeyStatePayload` both gain
  `Permission string \`json:"permission"\`` — the two shapes stay consistent,
  and a Go test asserts they agree because `useSettingsSync` feeds the same
  store slot from both.
- `events.Tagged.HotkeysState` takes a fourth `permission` argument.
- New DTO `HotkeyPermissionResultDTO{prompted, permission}`.
- New bindings `RequestHotkeyPermission()`, `OpenHotkeyPermissionSettings()`,
  `RecheckHotkeyPermission()`.
- `SetHotkeyPermissionPoll(interval, timeout)` — the test override, mirroring
  the existing `SetCaptureTimeout`.

**The central rule, enforced in code and in tests:** a grant is concluded from
`Status()` (i.e. `CGPreflightListenEventAccess`) and *never* from `Request()`'s
return. `PermissionChecker.Request()` returns only `prompted bool`, so no
caller can mistake it for an answer; its sole use is choosing the frontend's
next button label.

`RequestHotkeyPermission` branches three ways: already granted → apply
immediately, no poll; not applicable → return, no poll; otherwise → arm the
bounded re-check (500 ms interval, 30 s deadline, self-cancelling on the first
grant or at the deadline, superseded rather than stacked by a second request).
On a detected grant it calls `applyHotkeys()`, which tears down and recreates
the OS registrations — a fresh `CGEventTap` under the new permission — and
emits `hotkeys:state`.

`RecheckHotkeyPermission` is the primary trigger, wired in `main.go` to
`events.Common.WindowFocus` on the main window (same shape as the existing
tray `WindowClosing` hook). It early-outs when the cached state is already
granted or not-applicable, so an ordinary window activation costs nothing; the
hook in `main.go` is pure wiring with no policy.

### Frontend

- `shared/store/settings.ts`: `HotkeyPermission` union type,
  `HotkeyState.permission` (default `"unknown"` — the banner's permission copy
  renders only on the exact string `"denied"`, so unknown shows nothing and
  nothing claims a grant the backend has not reported), and
  `HotkeyPermissionResult`.
- `shared/api/client.ts`: `requestHotkeyPermission`,
  `openHotkeyPermissionSettings` and `recheckHotkeyPermission` as thin
  delegations, imported `import type`-consistently.
- `Keybinds.tsx` banner: when `permission === "denied"`, an explanation
  (macOS requires Input Monitoring; until granted the system delivers no
  keypress while another app is focused) plus **GRANT ACCESS**, swapping to
  **OPEN SETTINGS** once a request returns `prompted: false` while still not
  granted, plus **RE-CHECK** once any request has been made. When
  `permission === "granted"` and registration still failed, a text-only
  "restart VCS for hotkeys to take effect" line — **no restart button**, as
  specified. `"not_applicable"` renders none of it.

**No new CSS and no new class names**: the banner reuses `.col`, `.row`,
`.gap-3`, `.cap`, `.cap-dim` and the existing `Button` component's
`size="sm"` / `variant` props (`.btn`, `.btn-sm`, `.btn-primary`,
`.btn-ghost`), all read out of `components.css`.

---

## Load-bearing verification

Every key assertion was verified by breaking the fix and confirming the test
failed, then restoring. All nine breakages produced failures, and all failures
named the right thing.

| # | Breakage | Result |
|---|---|---|
| 1 | Change 1: revert to unconditional `hotkeys.failed[...]` lookup | ✗ `does not repeat the banner as a per-row warning on every binding` |
| 2 | Frontend: `setPromptSpent(res.prompted)` (inverted) | ✗ `swaps to OPEN SETTINGS once the one-shot prompt is spent` **and** ✗ `keeps GRANT ACCESS when the OS did show a prompt` |
| 3 | `RequestHotkeyPermission` treats `prompted` as a grant | ✗ `TestRequestHotkeyPermissionPollDetectsGrant`: `Permission = "granted" immediately after the request, want "denied" -- a prompt is not an answer` |
| 4 | Poll emits state but does not `applyHotkeys()` | ✗ `applyHotkeys did not re-register after the grant (12 registrations, was 12)` |
| 5 | Poll does not `return` after detecting the grant | ✗ `TestPermissionPollStopsOnGrant`: poll did not finish within 5s |
| 6 | Poll deadline case removed (unbounded) | ✗ `TestPermissionPollStopsAtTimeout`: poll did not finish within 5s |
| 7 | `RecheckHotkeyPermission` made a no-op | ✗ `the focus re-check did not re-apply hotkeys after an out-of-band grant (12, was 12)` |
| 8 | `NotApplicable` early-out removed | ✗ `a re-check poll was armed on a platform whose permission state cannot change` |
| 9 | `hotkeys:state` drops `Permission` | ✗ `TestHotkeyStateCarriesPermission` (app) **and** ✗ `TestEmitter_HotkeysState` (events) |

Breakage 2 is worth calling out: it fails in *both* directions, so the test
pair pins the exact semantics rather than just "some branch happened".
Breakages 5 and 6 are the two exits of the bounded poll, and each is caught
independently — neither test passes because of the other's mechanism.

A note on test-harness hygiene found during this work: `countingRegistrar`
could not be reused for the permission tests, because `applyHotkeys` now also
runs on the poll goroutine and its unguarded counter would be a genuine data
race under `-race`. `permission_test.go` introduces a mutex-guarded
`lockedRegistrar` instead. Similarly, `fakePermission` keeps `prompted` and
`status` completely independent — a fake that derived one from the other would
have quietly agreed with the exact bug this design exists to prevent.

---

## Test output

### `gofmt -l .`

Empty (clean) across the whole repo.

### `go vet ./...`

Clean. (Build chatter from `golang.design/x/hotkey` and the Wails cgo packages
is the sandbox's `xcrun` cache warning, not a vet finding; exit status 0.)

### `go test -race -count=1 ./...`

```
?   	github.com/FPGSchiba/vcs-srs-client	[no test files]
ok  	github.com/FPGSchiba/vcs-srs-client/internal/app	2.555s
ok  	github.com/FPGSchiba/vcs-srs-client/internal/auth	1.794s
ok  	github.com/FPGSchiba/vcs-srs-client/internal/chord	2.352s
ok  	github.com/FPGSchiba/vcs-srs-client/internal/config	1.426s
ok  	github.com/FPGSchiba/vcs-srs-client/internal/control	3.720s
ok  	github.com/FPGSchiba/vcs-srs-client/internal/events	3.861s
?   	github.com/FPGSchiba/vcs-srs-client/internal/grpctest	[no test files]
ok  	github.com/FPGSchiba/vcs-srs-client/internal/hotkeys	4.029s
ok  	github.com/FPGSchiba/vcs-srs-client/internal/keybinds	3.339s
ok  	github.com/FPGSchiba/vcs-srs-client/internal/session	4.133s
ok  	github.com/FPGSchiba/vcs-srs-client/internal/state	3.077s
?   	github.com/FPGSchiba/vcs-srs-client/internal/version	[no test files]
ok  	github.com/FPGSchiba/vcs-srs-client/internal/windowstate	2.875s
ok  	github.com/FPGSchiba/vcs-srs-client/pkg/logger	2.818s
?   	github.com/FPGSchiba/vcs-srs-client/srspb	[no test files]
```

New Go tests (`internal/app/permission_test.go`, 10 tests, all passing):

```
--- PASS: TestRequestHotkeyPermissionPollDetectsGrant (0.00s)
--- PASS: TestRequestHotkeyPermissionNonPromptingStaysDenied (0.00s)
--- PASS: TestPermissionPollStopsOnGrant (0.05s)
--- PASS: TestPermissionPollStopsAtTimeout (0.06s)
--- PASS: TestRecheckHotkeyPermissionFlipsState (0.00s)
--- PASS: TestRecheckHotkeyPermissionIsIdleOnceGranted (0.00s)
--- PASS: TestRequestHotkeyPermissionAppliesWhenAlreadyGranted (0.00s)
--- PASS: TestOpenHotkeyPermissionSettingsDelegates (0.00s)
--- PASS: TestHotkeyStateCarriesPermission (0.00s)
--- PASS: TestPermissionStringWireForms (0.00s)
```

### `npx tsc --noEmit`

Clean, exit 0. Test files are type-checked; no `as any` and no
`@ts-expect-error` were used anywhere. Five existing test files needed their
`HotkeyState` literals completed with the new required `permission` field —
the field was made required rather than optional precisely so the compiler
flags every construction site.

### `npx vitest run`

```
 ✓ src/shared/components/Placeholder.test.tsx (2 tests) 24ms
 ✓ src/windows/main/screens/Players.test.tsx (1 test) 29ms
 ✓ src/shared/components/KeyChip.test.tsx (9 tests) 41ms
 ✓ src/windows/comms/RadioCard.test.tsx (2 tests) 53ms
 ✓ src/windows/main/screens/settings/SettingsScreen.test.tsx (4 tests) 70ms
 ✓ src/shared/store/useSettingsSync.test.tsx (6 tests) 179ms
 ✓ src/windows/comms/CommsApp.test.tsx (3 tests) 156ms
 ✓ src/windows/main/screens/Welcome.test.tsx (2 tests) 232ms
 ✓ src/shared/store/settings.test.ts (7 tests) 3ms
 ✓ src/windows/main/screens/settings/sections/Keybinds.test.tsx (21 tests) 611ms

 Test Files  10 passed (10)
      Tests  57 passed (57)
```

`Keybinds.test.tsx` went from 13 to 21 tests (+8: two for Change 1, six for
the permission banner).

### `npm run build`

```
vite v8.0.16 building client environment for production...
✓ 89 modules transformed.
dist/main.html                     0.47 kB │ gzip:  0.29 kB
dist/comms.html                    0.48 kB │ gzip:  0.30 kB
dist/assets/Toggle-X7Kfg63W.css   36.40 kB │ gzip:  7.90 kB
dist/assets/comms-DKr7F-9H.js      3.42 kB │ gzip:  1.38 kB
dist/assets/main-MV7Ee8Jw.js      29.99 kB │ gzip:  8.44 kB
dist/assets/Toggle-Dhc0wmXb.js   235.76 kB │ gzip: 73.40 kB
✓ built in 393ms
```

Wails bindings were regenerated (`wails3 generate bindings -ts -clean=true`,
36 methods / 37 models) so `tsc` sees the new binding functions. The
`frontend/bindings/` tree is gitignored and regenerated at build time, so it
does not appear in the diff.

---

## Nothing was removed, narrowed or weakened

No existing assertion or behaviour was relaxed to make anything compile or
pass. The only edits to pre-existing tests were **additive**: completing
`HotkeyState` literals with the new required `permission` field, and asserting
the new field in `TestEmitter_HotkeysState`. The per-row suppression in
Change 1 is a deliberate behaviour change (the point of the task), and it is
covered in both directions by the two new tests.

---

## What could not be verified in this environment

1. **The deep link actually opening.** The pane identifier and anchor are
   confirmed against the shipping `Security.prefPane` bundle, but I did not
   run `open x-apple.systempreferences:...` — that would seize the GUI. Worth
   one click on the manual macOS checklist.
2. **The real TCC behaviour end to end.** `CGPreflightListenEventAccess` /
   `CGRequestListenEventAccess` compile and link against the real headers
   here, but no GUI can launch, so the prompt, the grant, and whether
   `applyHotkeys()` alone suffices to revive a `CGEventTap` without a restart
   were not exercised against a live TCC. The "restart VCS" text exists
   precisely because step 3 may not always be enough; which of the two paths
   the OS actually takes is a manual-checklist item.
3. **A caveat on `prompted` I want on record.** Per the brief,
   `darwinPermission.Request()` returns `CGRequestListenEventAccess`'s raw
   value. In practice that call returns `false` both when the prompt has just
   been shown *and* when it was spent long ago, so on a genuine first request
   the banner may swap to OPEN SETTINGS while the prompt is still on screen.
   That is benign — OPEN SETTINGS is still a valid route, RE-CHECK is offered
   alongside, and the focus re-check plus the bounded poll will flip the state
   the moment access is granted — but it is a behaviour to watch for on the
   manual pass, and the alternative (suppressing OPEN SETTINGS on a first
   request) would risk stranding exactly the user this feature exists for.
   Documented in `permission_darwin.go`.
4. **Final binary link / `GOOS=linux` build.** Both fail for the documented
   environment reasons (`dsymutil`/`xcrun` sandbox; Wails' own
   `menu_linux.go`), unrelated to these changes. `go build` of
   `internal/hotkeys` succeeds for `GOOS=linux` with and without cgo, which is
   the part the new tag partition affects.

## One thing to be aware of

`SetHotkeyPermissionPoll` is exported and therefore bound and reachable from
the frontend, even though it is a test-only knob. This exactly mirrors the
existing `SetCaptureTimeout`, and the brief asked for that parity, so I kept
it — but if either should become unexported, both should, together.
