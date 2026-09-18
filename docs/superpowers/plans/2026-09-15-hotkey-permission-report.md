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

---

# Round 2 — review response

Changes 1 and 2 were approved as-is and are untouched. This round addresses a
Critical, three Importants and five Minors raised against Change 3.

## CRITICAL — we were asking the OS for the wrong permission

**Confirmed, and it was as bad as described.** I verified the claim at the
source before touching anything:

`golang.design/x/hotkey@v0.6.1/hotkey_darwin.m:110-116`:

```c
void* registerTap(uintptr_t handle, int isMedia, int code, uint64_t flags) {
	// A keyboard event tap requires Accessibility trust. Check explicitly:
	// CGEventTapCreate can otherwise return a non-NULL but inert tap when
	// untrusted, which would look like success but never fire.
	if (!AXIsProcessTrusted()) {
		return NULL;
	}
```

and `hotkey.go:23-25`: *"hotkeys are delivered through a CGEventTap, which
requires the application to be trusted for Accessibility... Grant it in System
Settings → Privacy & Security → Accessibility."*

So the registrar gates on `kTCCServiceAccessibility`, and the shipped checker
gated on `kTCCServiceListenEvent`. On a machine already holding Accessibility
the two agree, so it was invisible locally and broken for exactly the users
the feature exists for.

### The fix — confined to `permission_darwin.go` as specified

Nothing above the seam moved: the `PermissionChecker` interface, the enum, the
poll, the focus hook, the DTOs and the entire frontend state machine are
unchanged.

| | before | after |
|---|---|---|
| `Status()` | `CGPreflightListenEventAccess()` | `AXIsProcessTrusted() != 0` |
| `Request()` | `CGRequestListenEventAccess()` | `AXIsProcessTrustedWithOptions({kAXTrustedCheckOptionPrompt: true})` |
| `OpenSettings()` | `...?Privacy_ListenEvent` | `...?Privacy_Accessibility` |
| framework | `-framework CoreGraphics` | `-framework ApplicationServices` |

Gated on Accessibility **only** — not both services — matching the library, so
users for whom registration already works are never blocked.

Two implementation details worth recording, both found by compiling rather
than assumed:

1. **`AXIsProcessTrusted` returns `Boolean`, not C99 `bool.`** It is a `uint8`
   from `MacTypes.h`, so cgo surfaces it as `C.uchar` and `bool(...)` is a
   compile error: `cannot convert ... (value of uint8 type _Ctype_Boolean) to
   type bool`. The code uses `!= 0`.
2. **The option dictionary is built in plain C over CoreFoundation**
   (`CFDictionaryCreate` with `kAXTrustedCheckOptionPrompt` / `kCFBooleanTrue`,
   `CFRelease`d before return) rather than with the suggested Objective-C
   literal and `__bridge` cast. The dictionary was the only thing needing ObjC,
   and this keeps the file compiling without `-x objective-c` and sidesteps ARC
   bridging entirely. Behaviourally identical.

`Request()` still returns only `prompted`, and the doc now states the actual
semantics: `AXIsProcessTrustedWithOptions` returns the *current trust state*,
so the design's "grant is concluded from `Status()` only" rule survives the
switch unchanged — if anything it fits better than before.

### `Privacy_Accessibility` anchor — VERIFIED, by a different route

Same machine as before (macOS 26.6.2, build 25G83). The pane identifier is
unchanged and already verified: `Security.prefPane/Contents/Info.plist` →
`CFBundleIdentifier = com.apple.preference.security`.

The anchor needed a different method, and this is the part worth reading. As
the review predicted, `kTCCServiceAccessibility` is **not** in
`PrivacyTCCServices.plist` at all:

```
$ plutil -p .../PrivacyTCCServices.plist | grep -c kTCCServiceAccessibility
0
```

so the plist route that verified `Privacy_ListenEvent` could never have
surfaced this. Instead, `Privacy_Accessibility` is a literal in the pane's own
binary, `Contents/MacOS/Security`:

```
$ strings .../Contents/MacOS/Security | grep -E '^Privacy' | sort -u
Privacy
Privacy_Accessibility
Privacy_LocationServices
Privacy_SystemServices
PrivacyAccessibilityServicesType
...
PrivacyTCCServicesType
```

That listing also explains *why* the two live in different places:
`Privacy_Accessibility` sits with `Privacy_LocationServices` and
`Privacy_SystemServices` — the anchors for panes that are **not** generic TCC
service tables — and the binary carries a distinct
`PrivacyAccessibilityServicesType` alongside `PrivacyTCCServicesType`.
Accessibility has its own service type, so it is handled by dedicated code
rather than driven from the plist. Same anchor namespace, different source.

**Result: verified, fallback not needed.** As before, I did not *dispatch* the
URL (that would seize the GUI); it is now item 4 on the spec's mandatory
manual pass.

### Making the switch load-bearing

This is the one place I could not do what I did for every other fix, and I
want to be explicit about why rather than imply a stronger check than exists.

I probed the live machine first. A `go test` binary is never a trusted app, so
**both** services read false here:

```
AXIsProcessTrusted()           = false
CGPreflightListenEventAccess() = false
```

The two cannot be told apart at runtime in this environment — which is
precisely the mechanism by which the original mistake survived. (Also learned:
cgo is not permitted in `_test.go` files, so the probe needed a scratch
package.)

So `internal/hotkeys/permission_darwin_test.go` (`//go:build darwin && cgo`)
pins what *can* be pinned without the OS:

- `TestDarwinPermissionGatesOnAccessibility` — asserts the implementation
  gates on `C.AXIsProcessTrusted()`, prompts via
  `AXIsProcessTrustedWithOptions`, links `ApplicationServices`, and contains
  **no** call to `C.CGPreflightListenEventAccess` / `C.CGRequestListenEventAccess`.
  A source-level assertion, used deliberately and labelled as such: it is the
  only available guard for a seam a unit test cannot execute. It checks calls,
  not prose, so the doc comments stay free to discuss the old service.
- `TestAccessibilityPaneURL` — pins the deep link and rejects any `ListenEvent`
  anchor. A well-formed URL for the wrong pane is worse than no button.
- `TestDarwinPermissionStatusIsBinary` — `Status()` must commit to
  granted/denied and never leak `PermissionUnknown`.

**Breakage check** (revert `Status()` to the preflight and the anchor to
`Privacy_ListenEvent`) — both tests fail with the right messages:

```
--- FAIL: TestDarwinPermissionGatesOnAccessibility
    Status() must gate on AXIsProcessTrusted -- the exact predicate
    golang.design/x/hotkey's registerTap uses; anything else can report
    granted while every Register still fails
    C.CGPreflightListenEventAccess gates Input Monitoring
    (kTCCServiceListenEvent), not the Accessibility trust the CGEventTap
    actually requires
--- FAIL: TestAccessibilityPaneURL
    accessibilityPaneURL = "...?Privacy_ListenEvent", want "...?Privacy_Accessibility"
```

The behavioural check — grant Input Monitoring only, confirm hotkeys stay dead
— is item 7 on the manual pass, because only a real TCC can run it.

### Naming swept

Every `Input Monitoring` reference in code, tests, banner copy, store/client
docs, the ROADMAP entry and the spec's R12 now says Accessibility. The only
surviving mentions are deliberate: `permission_darwin.go` and `permission.go`
explaining what went wrong, and the spec's wrong-service regression check.

---

## IMPORTANT — the two new `applyHotkeys()` callers skipped `writeMu`

Both now take it, matching every pre-existing caller.

`RecheckHotkeyPermission` takes it *after* its cheap early-out, deliberately:
window focus fires constantly and the common case must not queue behind a
keybind write. Lock order is `writeMu -> mu` throughout, as the existing
callers established — the `lastPerm` read releases `mu` before `writeMu` is
acquired, so the two are never held in the opposite order.

The poll goroutine applies through a new helper, `applyGrantedHotkeys(cancel)`,
which takes `writeMu` and then **re-checks `cancel` after winning the lock**.
That second check is not decoration: by the time the goroutine acquires
`writeMu`, the focus re-check may already have detected the same grant and
applied, and skipping is what stops that becoming a redundant
teardown-and-recreate.

Tests: `TestRecheckHotkeyPermissionHoldsWriteMu` and
`TestPermissionPollHoldsWriteMu` hold `writeMu` as a stand-in for an in-flight
`SetKeybind`, flip the permission, and assert no apply happens until the lock
is released. Applies are counted via `UnregisterAll`, which `Manager` calls
exactly once per `registerLocked` — a per-apply counter rather than a
per-`Register` one.

## IMPORTANT — the focus re-check did not cancel the poll

`RecheckHotkeyPermission` now calls `cancelPermissionPoll()` before applying.

`cancelPermissionPoll` takes **and nils** `permCancel` in one step under
`sb.mu`, so exactly one caller can ever observe a non-nil channel and exactly
one `close` happens. That matters specifically because Wails dispatches window
hooks with `go a.handleWindowEvent(...)`, so two rapid focus events genuinely
can run this concurrently. It never waits for the goroutine — the focus path
holds `writeMu`, which that goroutine may itself be blocked acquiring, and
waiting would deadlock.

**This is where I found a test that proved nothing.** My first
`TestRecheckHotkeyPermissionCancelsPoll` waited up to 5s for the poll to
finish and then counted applies. With the cancellation removed it still
**passed** — the poll simply finished on its own 2s tick, applying a second
time on the way out, comfortably inside the 5s window, and the apply count was
sampled after that. The breakage matrix caught it; the review's instruction to
verify each fix is what surfaced it.

Rewritten so both assertions are framed against the tick interval:

1. the poll must end far sooner than one tick could arrive (`tick/4`);
2. after a **full tick interval** has elapsed, there must still be exactly one
   apply — the direct statement of "the freshly granted tap was not rebuilt".

Each was then verified to discriminate **independently**:

```
BREAK (no cancellation):                     FAIL — "re-check poll did not finish within 500ms"
BREAK (no cancellation, assertion 1 neutered):FAIL — "2 applies after a full tick interval, want exactly 1;
                                                     a late poll tick tore down and rebuilt the freshly
                                                     granted event tap"
```

## IMPORTANT — the poll was not cancelled on shutdown

`ServiceShutdown` now calls `stopPermissionPoll(shutdownPollStopTimeout)`,
before the disconnect.

I went slightly beyond the suggested one line: it cancels **and waits**,
bounded at 1s. Cancel alone still leaves a window in which the goroutine is
mid-apply, which is the stated hazard (calling into the hotkey library or
emitting a Wails event after teardown). The wait is bounded because the
goroutine may be blocked on `writeMu` behind an in-flight keybind write, and a
quit must never hang on that. Shutdown holds no locks, so waiting here cannot
deadlock the way it would on the focus path.

Tests: `TestServiceShutdownStopsPermissionPoll` asserts the poll is already
finished when shutdown returns (a non-blocking check, valid precisely because
shutdown waits); `TestStopPermissionPollIsBounded` wedges the goroutine on
`writeMu` and asserts the stop returns promptly anyway.

## MINORS

1. **`SetHotkeyPermissionPoll` and `SetCaptureTimeout` are now unexported**
   (`setHotkeyPermissionPoll`, `setCaptureTimeout`). Resolved downward, as
   directed — the parity argument was right and the fix was to fix both.
   Confirmed no frontend caller existed in `frontend/src`. Verified on the
   IPC surface: regenerating bindings went from **36 methods to 34**, and
   `grep -cE 'export function (SetCaptureTimeout|SetHotkeyPermissionPoll)'`
   over the generated `app.ts` returns **0**. The doc comment records the
   sharper reason for the new one: an exported poll-interval setter would let
   the webview call `setHotkeyPermissionPoll(1, 1e12)` and spin.
2. **Doc inconsistency fixed.** `permission.go`'s interface doc no longer
   states that a false `Request()` result means the prompt is spent. It now
   matches the darwin file: false means "not granted at the moment of the
   call", covering both "sheet on screen, unanswered" and "already refused" —
   *evidence* that System Settings may be the route, not proof. The same
   correction was applied to `HotkeyPermissionResult`'s doc in the store.
3. **The fake-only branch is labelled.** `{prompted: true, permission:
   "denied"}` cannot occur in production (`AXIsProcessTrustedWithOptions`
   returns current trust, so `prompted: true` implies granted). The test is
   kept, with a comment saying exactly that and why it still earns its place:
   it isolates the branch, proving the component keys OPEN SETTINGS off
   `prompted` rather than off `permission`. The shared `beforeEach` default
   carries a pointer to that note.
4. **Banner helper text now carries `fontSize: 10`**, matching the sibling
   `cap-dim` helper on the keybind rows. Both banner helpers were rendering
   noticeably larger than every other dim helper in the section.
5. **Manual-verification items are in the spec**, not only in this report:
   `§11` gains a mandatory eight-item macOS Accessibility pass, prefaced with
   the reason it cannot be automated (test binaries are never trusted, so both
   services read false and CI cannot tell them apart) and the instruction to
   run it on a machine that has *never* granted VCS Accessibility, since
   otherwise every item passes vacuously. It covers the deep link actually
   opening, the live grant end to end, whether `applyHotkeys()` alone revives
   the tap or the restart is genuinely needed, the `prompted` wrinkle, the
   wrong-service regression check, and quitting during the poll.

**Not changed, as instructed:** the button logic still offers OPEN SETTINGS on
a first request rather than suppressing it.

## Round 2 test results

`gofmt -l .` empty. `go vet ./...` exit 0.

```
go test -race -count=1 ./...        14 packages, all ok, 0 failures
```

New/changed tests, all passing:

```
internal/app        (16)  ...PollDetectsGrant, ...NonPromptingStaysDenied,
                          ...PollStopsOnGrant, ...PollStopsAtTimeout,
                          ...FlipsState, ...IsIdleOnceGranted,
                          ...AppliesWhenAlreadyGranted, ...SettingsDelegates,
                          ...CarriesPermission, ...StringWireForms,
                          ...HoldsWriteMu (x2), ...CancelsPoll,
                          ...CancelIsSafeConcurrently,
                          ...ShutdownStopsPermissionPoll, ...StopIsBounded
internal/hotkeys     (3)  ...GatesOnAccessibility, ...PaneURL, ...StatusIsBinary
```

```
npx tsc --noEmit    clean
npx vitest run      10 files, 57 tests passed
npm run build       ✓ built in 381ms
```

### Round 2 breakage matrix

| Breakage | Result |
|---|---|
| `Status()` reads Input Monitoring + anchor reverted | ✗ `TestDarwinPermissionGatesOnAccessibility`, ✗ `TestAccessibilityPaneURL` |
| `RecheckHotkeyPermission` drops `writeMu` | ✗ `the focus re-check applied hotkeys (2 applies, was 1) while writeMu was held` |
| Poll applies without `writeMu` | ✗ `the poll applied hotkeys (2 applies, was 1) while writeMu was held` |
| Focus re-check does not cancel the poll | ✗ (after the test was rewritten — **passed** before; see above) |
| `cancelPermissionPoll` closes without nilling | ✗ panic on double close, caught by `TestCancelPermissionPollIsSafeConcurrently` |
| `ServiceShutdown` does not stop the poll | ✗ `ServiceShutdown returned with the permission poll still running` |
| Focus cancel removed **and** assertion (1) neutered | ✗ assertion (2) alone still catches it |

## Still unverifiable here

Unchanged from round 1 in kind, but the list is now shorter and sharper, and
the items are written into the spec rather than only this report:

1. The deep link actually dispatching (URL verified well-formed, never opened).
2. Any live TCC behaviour: the Accessibility sheet, a real grant, and whether
   `applyHotkeys()` alone revives the tap or the restart guidance is
   load-bearing. **Nothing in this flow has yet met a real TCC.**
3. The wrong-service regression check (Input Monitoring granted, Accessibility
   not) — the one behavioural proof that the Critical is actually fixed. It
   needs a real TCC and is item 7 on the manual pass.
4. Final binary link and `GOOS=linux` full build — documented environment
   limits, unrelated to these changes.

---

# Round 3 — review response

Four residuals. Both Importants were real; the manual-doc one was the worst
artifact in the change set.

## IMPORTANT — the sweep missed a live procedure that reproduced the bug

**Confirmed and fixed.** `docs/superpowers/plans/2026-09-15-phase-3-manual-verification.md`
§10 was worse than a stale comment, exactly as described: it was an
*executable* procedure telling a tester to
`tccutil reset ListenEvent <bundle-id>`, expect the Input Monitoring dialog,
and grant via Privacy & Security → Input Monitoring, with the expectation
that "the banner clears and previously saved bindings register and fire".
Following it produces a guaranteed false negative — the tester grants the one
service that cannot help, sees nothing work, and concludes the feature is
broken.

My round-2 sweep grepped source files and the two docs I had edited. It never
touched this file, because I did not think of it. The lesson is the one the
review drew: after a semantic change, sweep by *concept* across the whole
repo, not by the set of files already open.

§10 is rewritten for Accessibility as a 10-step procedure:
`tccutil reset Accessibility`; the `AXIsProcessTrustedWithOptions` sheet;
Deny; OPEN SETTINGS; grant in Privacy & Security → **Accessibility**; return
to the window and confirm the focus re-check clears the banner unaided;
whether the re-apply alone revives the tap or a restart is genuinely needed;
the wrong-service regression check; quit-during-poll; and relaunch. It matches
the current banner copy and the GRANT ACCESS / OPEN SETTINGS / RE-CHECK flow,
and states the expected `permission` value at each stage.

Two callouts sit at the top, both earned rather than decorative:

- A warning that the permission is Accessibility, *not* Input Monitoring, and
  that an earlier revision of this very checklist said otherwise and thereby
  reproduced the bug. Anyone working from a printed or cached copy is told to
  stop and re-fetch. The old wrong instruction is named, so the reader can
  recognise it.
- A warning to run on an account that has **never** granted VCS Accessibility,
  because otherwise every item passes vacuously — which is how the
  wrong-service bug survived in the first place.

### Which way I resolved the duplication, and why

**The manual-verification doc §10 is now the single authority; the spec's §11
points at it.**

I had it backwards in round 2. That file is the repo's home for executable
procedures — thirteen numbered sections of them — and it is where results are
recorded ("Record pass/fail per numbered item ... directly in this file").
The spec's §11 was a five-line summary until I dropped an eight-step procedure
into it, creating a second executable copy. That is what drifted: my round-2
sweep corrected R12 and §11 while §10, the copy a human would actually run,
kept the wrong instructions.

So the procedure moved to where procedures live, and §11 shrank back to a
pointer plus the two facts that are genuinely spec-level rather than steps:
that the pass cannot be automated (a `go test` binary is never trusted, so
both services read false and CI cannot distinguish them), and that it must run
on a never-granted account. R12's back-reference now names §10 explicitly as
the single executable copy. Both directions of the cross-reference are in
place, and each says why, so the next person is told not to re-fork it.

### The fuller sweep of that file — what else it turned up

Grepped the whole 359-line file for every permission-adjacent term
(`input monitoring`, `listenevent`, `accessibility`, `tccutil`, `permission`,
`denied`, `grant`). **Section 10 was the only wrong-service content.** The
remaining hits are correct:

- §13's line "that banner means 'no global hotkeys are registered at all'
  (missing backend / denied permission), never 'one binding failed'" — still
  accurate, and in fact reinforced by the Change 1 suppression.
- The "known gaps" list — no permission content.

I did find one piece of **pre-permission-feature content worth adding to**
rather than correcting: §13 tested the partial-failure direction (per-row
reasons appear, banner does not) but had nothing for the complement that
Change 1 introduced. Without a note, a tester in the ungranted state would see
rows with no inline reasons and could file the suppression as a regression. So
§13 gained a step 6 describing the banner-state case and why absent per-row
notes are correct there. §10's step 1 is the easiest way to reach that state,
and it is cross-referenced.

Also updated §10's event shape from `hotkeys:state {registered, error}` to
`{registered, error, failed, permission}`, which had been stale since the
`failed` map landed, before this feature.

## IMPORTANT — one `applyHotkeys` caller still bypassed `writeMu`

Correct, and it was the easiest of the three to hit: `RequestHotkeyPermission`'s
already-granted branch fires on a button click, so a user clicking GRANT
ACCESS while a keybind write is in flight reaches it directly. It now cancels
any armed poll and holds `writeMu` across the apply, in the same style as the
other two.

The poll point was real too: a second GRANT ACCESS click after the grant had
landed left the first click's poll ticking, to re-apply and tear down the tap
the second click had just built.

The invariant is now actually true. Enumerated every call site:

| site | enclosing function | `writeMu` |
|---|---|---|
| `settings.go:193` | `SetKeybind` | held (line 184) |
| `settings.go:223` | `ClearKeybind` | held (line 214) |
| `settings.go:370` | `RequestHotkeyPermission` | held (**new**) |
| `settings.go:459` | `RecheckHotkeyPermission` | held (line 426) |
| `settings.go:615` | `applyGrantedHotkeys` (poll) | held |
| `settings.go:780` | `RefreshKeybinds` | held (line 768) |
| `app.go:111` | `SetSettingsBackend` | none — single-threaded startup wiring |

## MINOR — the two recheck paths now read the same way

`RecheckHotkeyPermission` re-reads `lastPerm` after acquiring `writeMu`,
mirroring `applyGrantedHotkeys`'s post-lock `cancel` re-check. As noted, the
window is the lock acquisition itself, which is wide open whenever a keybind
write is in flight — two focus goroutines could both clear the pre-lock
early-out and both apply, the second tearing down the tap the first had just
built. The symmetry is the bigger win: a reader comparing the two paths no
longer has to work out why only one of them re-checks.

## MINOR — three stale API names

All three updated to the AX equivalents:

- `internal/hotkeys/permission.go` — now "macOS's `AXIsProcessTrustedWithOptions`
  returns the CURRENT trust state and returns immediately", which is also more
  accurate than the old sentence was about its own API.
- `internal/app/settings.go` — "ask `AXIsProcessTrusted` again".
- `internal/app/permission_test.go` — "The real `AXIsProcessTrustedWithOptions`
  returns a value that is not the user's answer".

The comments that deliberately name Input Monitoring as a warning are
**kept**: the two in `permission_darwin.go` and `permission.go` explaining what
went wrong, the `permission_other.go` partition note, the spec's R12 history,
and the manual doc's warning banner and step 8 regression check.

## Round 3 tests

Three new tests, each verified load-bearing:

| Breakage | Result |
|---|---|
| already-granted branch drops `writeMu` | ✗ `RequestHotkeyPermission applied hotkeys (2 applies, was 1) while writeMu was held` |
| already-granted branch does not cancel the poll | ✗ `poll cancelled by the already-granted request: re-check poll did not finish within 500ms` |
| post-lock `lastPerm` re-read removed | ✗ `2 applies from two concurrent focus re-checks, want exactly 1; the second tore down and rebuilt the tap the first had just created` |

`TestConcurrentFocusRechecksApplyOnce` is the interesting one: it holds
`writeMu` so both focus goroutines are parked on the lock before either can
proceed, which is the precise interleaving the re-read exists for, and makes
the test deterministic rather than a timing hope.

`TestRequestHotkeyPermissionCancelsPoll` is framed against the tick interval
the same way the round-2 rewrite was, specifically so it cannot pass on a poll
that merely finished by itself — the failure mode I shipped and had to
correct last round.

```
gofmt -l .                     empty
go vet ./...                   exit 0
go test -race -count=1 ./...   12 packages ok, 0 failures
                               22 permission tests passing (19 app, 3 hotkeys)
npx tsc --noEmit               clean
npx vitest run                 10 files, 57 tests passed
npm run build                  ✓ built in 381ms
```

## Still unverified here

Unchanged, and now correctly routed: the deep link dispatching, live TCC
behaviour, whether `applyHotkeys()` alone revives the tap, and the
wrong-service regression check all live in manual-verification §10 as
executable steps rather than as prose in this report. Nothing in this flow has
met a real TCC.
