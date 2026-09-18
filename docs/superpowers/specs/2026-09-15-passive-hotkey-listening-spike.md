# Spike: passive global hotkey listening (push-to-talk must not steal the key)

- **Date:** 2026-09-15
- **Status:** research spike — recommendation only, nothing implemented
- **Branch context:** `feat/wails3-migration`
- **Scope:** `internal/hotkeys` OS layer only. `internal/chord`, `internal/keybinds`,
  `hotkeys.Manager`, `hotkeys.Registrar` and the whole permission/UI flow are unchanged
  by anything recommended here.

---

## 1. The question

The client binds global hotkeys — most importantly push-to-talk — through
`golang.design/x/hotkey@v0.6.1` (`internal/hotkeys/registrar_x.go`). Manual testing found
that **every bound key is taken exclusively**: while VCS is running, the key stops reaching
other applications. For a voice client used alongside Star Citizen that is a
product-breaking defect — a user who binds PTT to a key the game also uses loses that key
in the game.

This is not a bug in `x/hotkey`; it is what the library is for. It implements *global
hotkeys* ("this key is mine now"). We need a *global listener* ("tell me when this key
moves, and let it through"). Those are different OS mechanisms, and on two of three
platforms they are different system calls entirely.

Confirmed exclusive on all three backends:

| Platform | `x/hotkey` mechanism | Why it is exclusive |
| --- | --- | --- |
| macOS | active `CGEventTap`, callback `return NULL; // consume` (`hotkey_darwin.m:95,101`) | the callback deliberately swallows the event |
| Windows | `RegisterHotKey` (`hotkey_windows.go:57`) | exclusive by OS design — the key is claimed process-wide and is not delivered to the focused window |
| Linux/X11 | `XGrabKey` (`hotkey_x11.go:165`) | a grab; the X server redirects the key to the grabbing client |

Two further quality problems in the current implementation, found while confirming the above
and worth recording because a replacement fixes them for free:

- **Windows key-up is polled, not delivered.** `hotkey_windows.go:107-119` runs a
  `time.NewTicker(time.Second / 100)` — a **10 ms poll** — and infers release from
  `GetAsyncKeyState(...) == 0`. That is up to 10 ms of trailing open microphone per PTT
  release, and a tap shorter than one tick can be missed entirely.
- **A comment in our own source is wrong and should be fixed regardless of what we decide.**
  `internal/hotkeys/registrar_x.go:23-30` states that `x/hotkey`'s release signal is
  *"an XRecord key-up callback on Linux/X11 (confirmed by reading the library source:
  … hotkey_x11.go)"*. It is not. The mechanism is in **`hotkey_x11.c`**, not `hotkey_x11.go`,
  and that file contains **zero** occurrences of XRecord:
  ```c
  // hotkey_x11.c:121-122 (v0.6.1)
  XGrabKey(d, keycode, mods[i], DefaultRootWindow(d), False, GrabModeAsync, GrabModeAsync);
  ```
  followed by `XSelectInput(d, root, KeyPressMask)` and an `XNextEvent` loop handling
  `case KeyPress:` / `case KeyRelease:` (`:132, :143-148`). Release comes from **the grab**,
  not from XRecord. Note `owner_events = False` — the focused game does not get the key. The
  conclusion (`SupportsRelease() == true`) happens to be right; the stated reason is not.
- **Each hotkey costs several OS registrations.** On macOS `registerTap` creates **one
  `CGEventTap` per hotkey** (`hotkey_darwin.m:118-147`, `kCGEventTapOptionDefault`,
  `kCGHeadInsertEventTap`). On X11, `lockVariants` (`hotkey_x11.go:165-172`) issues a
  separate `XGrabKey` for each of four CapsLock/NumLock mask combinations — so up to **four
  grabs per binding**. With ~19 bindable actions (`internal/keybinds/actions.go`) that is up
  to 19 active session taps or ~76 key grabs live at once.

**Requirements for a replacement**

1. Pass-through: the key must still reach the focused application.
2. Key **down and up**: PTT that never releases leaves a live microphone. Non-negotiable.
3. All three platforms, ideally one code path.
4. Permission model must stay honest — see §7, this project has already been bitten once.
5. Must fit behind the existing `hotkeys.Registrar` seam so nothing above the OS layer changes.

---

## 2. Comparison table

| | pass-through | key-up | macOS permission | Windows mech | Linux mech | Wayland | license | maintenance |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **`golang.design/x/hotkey` (current)** | ❌ consumes | ⚠️ `Keyup()`, but **polled** on Windows (10 ms `GetAsyncKeyState`) | Accessibility (`AXIsProcessTrusted`) | `RegisterHotKey` (exclusive) | `XGrabKey` (exclusive) | X11 only | MIT | low |
| **`robotn/gohook` — CGo backend (default)** | ✅ passes through unless opted out | ✅ `EVENT_KEY_RELEASED` | Accessibility, **and it prompts** (`AXIsProcessTrustedWithOptions` + `kAXTrustedCheckOptionPrompt`) | `SetWindowsHookEx(WH_KEYBOARD_LL)` + `CallNextHookEx` | XRecord (passive) | X11/XWayland only | **MIT wrapper over LGPLv3 vendored C** | single maintainer, bursty |
| **`robotn/gohook` — `-tags purego` (beta)** | ✅ **cannot consume** (ListenOnly tap) | ✅ | Accessibility, no prompt (`AXIsProcessTrusted`) — ⚠️ see §7 risk | `SetWindowsHookEx(WH_KEYBOARD_LL)` + `CallNextHookEx` | XRecord over pure-Go xgb | X11/XWayland only | MIT/Apache-2.0, no LGPL | ~2 months old (PR #72) |
| **`robotn/gohook` — `-tags wayland`** | ✅ (observer only) | ✅ | n/a | n/a | n/a | **focused-surface only — useless for PTT** | MIT | ~2 months old |
| **`moutend/go-hook`** | ✅ `CallNextHookEx` unconditional | ✅ all four WM_*KEY* messages | n/a | `SetWindowsHookEx(WH_KEYBOARD_LL)` | — | — | MIT | **abandoned 2020**; heap-corrupting message pump — **reject** |
| **`MarinX/keylogger`** (Linux evdev) | ✅ never calls `EVIOCGRAB` | ✅ `Value==0` | n/a | — | `/dev/input/eventN` | ✅ **yes** (below the display server) | MIT | active 2026-08; `O_RDWR`, name-substring device matching |
| **`holoplot/go-evdev`** (better evdev) | ✅ `Grab()` exists, we never call it | ✅ | n/a | — | `/dev/input/eventN`, `O_RDONLY` | ✅ **yes** | MIT | active 2026-09 |
| **Roll our own** | ✅ by construction | ✅ | ListenOnly tap → Input Monitoring per Apple (§3.7.1) | **Raw Input** (`WM_INPUT` + `RIDEV_INPUTSINK`) | **XI2 raw** (survives others' grabs) | only via evdev | ours | ours — ~3–5 days + forever |
| **Wails v3 `GlobalShortcutManager`** | ❌ exclusive everywhere | ❌ **no key-up at all** | n/a | `RegisterHotKey` | `XGrabKey` | portal, `Activated` only | MIT | already a dependency — **unsuitable** |

Legend: ✅ verified by reading source; ❌ verified defect; ⚠️ open risk.

---

## 3. gohook: what the source actually says

Read at `master` / tag `v1.0.0-beta1` unless noted. gohook has **two independent
implementations** of every backend; this matters and is not visible from the README.

### 3.1 Two backend families, selected by build tag

Verified by reading the `//go:build` lines:

| file | build constraint | selected by |
| --- | --- | --- |
| `hook_cgo.go`, `extern.go` | `!wayland && !(darwin && purego) && !(windows && purego) && !(linux && purego)` | **default** — CGo + vendored libuiohook C |
| `darwin.go` | `darwin && purego` | `-tags purego` |
| `windows.go` | `windows && purego` | `-tags purego` |
| `x11.go` | `linux && purego && !wayland` | `-tags purego` |
| `wayland.go` | `linux && wayland` | `-tags wayland` |
| `hook.go`, `event.go`, `keycode.go`, `tables.go` | *(none)* | always |

Note the trap: the CGo constraint *starts* with `!wayland`, so `-tags wayland` disables the
CGo backend on **every** OS, not just Linux. `-tags wayland` on macOS/Windows produces a
build with no backend at all (`undefined: Start`). Only ever pair it with `GOOS=linux`.

**The purego backends are new.** They exist only on `master` and in tag `v1.0.0-beta1`
(2026-07-07, PR #72). Stable `v0.42.3` does not contain them — verified by listing the repo
tree at each tag.

### 3.2 Pass-through — macOS

**CGo backend** (`hook/darwin/hook_c.h:1111-1116`):

```c
hook->port = CGEventTapCreate(
        kCGSessionEventTap,         // kCGHIDEventTap
        kCGHeadInsertEventTap,      // kCGTailAppendEventTap
        kCGEventTapOptionDefault,   // kCGEventTapOptionListenOnly See Bug #22
        event_mask,
        hook_event_proc,
        NULL);
```

An **active** tap, but the callback returns the event (`hook_c.h:1053-1061`):

```c
CGEventRef result_ref = NULL;
if (event.reserved ^ 0x01) {
        result_ref = event_ref;
} else {
        logger(LOG_LEVEL_DEBUG, "%s [%u]: Consuming the current event. ...");
}
return result_ref;
```

Every key handler sets `event.reserved = 0x00` before dispatch (`hook_c.h:349, 450`), so the
default is pass-through and consumption is opt-in. Furthermore gohook **never writes
`reserved` back into the C struct** — it copies events out to Go as JSON and drops them —
so through gohook's API consumption is not reachable at all. (This is why gohook issues
#16 and #23, "Consume Events?" / "block system wide input", are still open since 2020.
Irrelevant to us: we want the opposite.)

**purego backend** (`darwin.go:312-313`) — strictly better for our purpose:

```go
port := cgEventTapCreate(cgSessionEventTap, cgHeadInsertEventTap,
        cgEventTapOptionListenOnly, cgEventMask(), cgCallbackPtr, 0)
```

A **listen-only** tap *structurally cannot* consume: the window server ignores the callback's
return value. `darwin.go:394` states it plainly: `// ListenOnly taps ignore the return value,
but pass the event through.`

### 3.3 Pass-through — Windows

`hook/windows/hook_c.h:672` installs the hook:

```c
keyboard_event_hhook = SetWindowsHookEx(WH_KEYBOARD_LL, keyboard_hook_event_proc, hInst, 0);
```

and the callback (`hook_c.h:301-302`):

```c
if (nCode < 0 || event.reserved ^ 0x01) {
        hook_result = CallNextHookEx(keyboard_event_hhook, nCode, wParam, lParam);
}
```

`WH_KEYBOARD_LL` + `CallNextHookEx` is the canonical **pass-through** mechanism — the polar
opposite of `RegisterHotKey`. It swallows only when `event.reserved == 0x01`, which, per
§3.2, gohook never sets. The purego backend does the same via `golang.org/x/sys/windows`
(`windows.go:349, 430`: `procCallNextHookEx.Call(...)`, called unconditionally).

### 3.4 Pass-through — Linux/X11

XRecord, not XGrabKey. `hook/x11/hook_c.h:873-887`:

```c
XRecordClientSpec clients = XRecordAllClients;
hook->data.range = XRecordAllocRange();
hook->data.range->device_events.first = KeyPress;
hook->ctrl.context = XRecordCreateContext(hook->data.display, XRecordFromServerTime, &clients, 1, ...);
```

XRecord is a **passive observer**: it is handed a copy of events the server is already
delivering. It cannot alter delivery. The source says so at `hook/x11/hook_c.h:772`:

```c
// TODO There is no way to consume the XRecord event.
```

So on X11, pass-through is guaranteed by the mechanism, not by a flag. The purego backend
uses the same extension through pure-Go `jezek/xgb` (`x11.go:191-196`,
`rng.DeviceEvents = record.Range8{First: xproto.KeyPress, Last: xproto.MotionNotify}`).

> Note: `x/hotkey`'s Linux backend uses `XGrabKey`. The *release* detection the project's
> `registrar_x.go:23-30` comment attributes to "an XRecord key-up callback" is separate from
> the grab; the grab is still what makes the key exclusive.

### 3.5 Key release — yes, on all backends

| backend | key-down | key-up | evidence |
| --- | --- | --- | --- |
| CGo macOS | `process_key_pressed` | `process_key_released` → `event.type = EVENT_KEY_RELEASED` | `hook/darwin/hook_c.h:444-464`, dispatched from `hook_event_proc` `case kCGEventKeyUp:` (`:942`) |
| CGo Windows | `WM_KEYDOWN`/`WM_SYSKEYDOWN` | `WM_KEYUP`/`WM_SYSKEYUP` → `EVENT_KEY_RELEASED` | `hook/windows/hook_c.h:264-289` |
| CGo X11 | `data->type == KeyPress` | `data->type == KeyRelease` → `EVENT_KEY_RELEASED` | `hook/x11/hook_c.h:278, 373, 421` |
| purego macOS | `cgEventKeyDown` → `KeyDown` | `cgEventKeyUp` → `KeyUp` | `darwin.go:401-404` |
| purego Windows | `wmKeyDown, wmSysKeyDown` | `wmKeyUp, wmSysKeyUp` → `processKeyReleased` | `windows.go:342-344, 383-389` |
| purego X11 | `xproto.KeyPress` | `xproto.KeyRelease` → `e.Kind = KeyUp` | `x11.go:334-336, 381` |
| purego Wayland | `wl_keyboard.key` state 1 | state 0 → `KeyUp` | `wayland.go:251-255` |

Reaching Go, these surface as the exported constants in `hook.go:28-30`:

```go
KeyDown = 4 // 3
KeyHold = 3 // 4
KeyUp   = 5 // 5
```

Modifiers on macOS deserve a note: Quartz reports modifier changes as
`kCGEventFlagsChanged`, not key up/down. Both backends synthesise discrete
`KeyDown`/`KeyUp` from the flag state — CGo at `hook_c.h:466+` (`process_modifier_changed`,
"any changes to the modifier keys will require a key state change to be fired manually"),
purego at `darwin.go:425-455` (`modifierEvent`). So a modifier-only chord still gets a
release edge. Good, but it is synthesised, so it is worth an explicit manual test.

**Auto-repeat caveat.** Held keys generate repeats. The purego X11 backend explicitly folds
these into `KeyHold` (`x11.go:101-102`, `// X delivers auto-repeat as additional KeyPress
events`), but gohook open issue **#47** (2024-04-04, still unanswered) reports repeated
`KeyDown` on macOS-hold and on Windows single-press. **We must debounce**: latch PTT on the
first `KeyDown` and ignore further downs until a matching `KeyUp`. This is a handful of lines
in our Registrar and is required regardless of which library we pick. (`x/hotkey` does this
for us today — `hotkey_darwin.m:91-94`, *"The tap repeats keyDown while the key is held; fire
once"*, guarded by a `t->down` latch — so it is work we take **on**, not work we inherit.)

### 3.6 Wayland — the honest answer

gohook has a Wayland backend, and its own doc comment is the clearest statement of the
problem (`wayland.go:26-38`, verbatim):

```
│  Wayland deliberately has NO global keylogging/mouse-hooking primitive.   │
│  A wl_seat only delivers wl_keyboard / wl_pointer events to a surface     │
│  while THAT surface holds input focus. There is no XRecord equivalent.    │
│                                                                           │
│  Consequences for this backend:                                           │
│    • It captures input only while a surface owned by this process has     │
│      keyboard/pointer focus (e.g. a robotgo/GUI window). Without a        │
│      focused surface no key/pointer events are produced — by design.      │
│    • TRUE global capture on Wayland requires the xdg-desktop-portal        │
│      InputCapture / RemoteDesktop portals plus the libei (EI) protocol.   │
│      See SUGGEST_WAYLAND.md for the recommended path and a survey of      │
│      compositor support (GNOME/KDE = yes, wlroots/Hyprland = not yet).    │
```

**So: `-tags wayland` is worthless for push-to-talk.** PTT is defined by working while the
*game* has focus; a backend that only reports keys while *our* window has focus inverts the
requirement.

(`SUGGEST_WAYLAND.md` referenced above **does not exist** in the repository — verified
against the root tree. A doc comment citing a missing file is a small but real polish signal
for this beta.)

What actually happens on a Wayland desktop:

- **`-tags wayland`:** no error, no crash — **silent absence of events** whenever another
  application has focus. The worst possible failure mode: looks fine in dev when you're
  testing with the VCS window focused, dead in real use.
- **Default or `-tags purego` (XRecord) on a Wayland session:** the X11 code connects to
  **XWayland**, which is a real X server, so `XOpenDisplay`/`XRecordQueryVersion` succeed and
  no error is reported. But XWayland only receives input the compositor routes to it — i.e.
  only while an X11/XWayland client has focus. Native Wayland clients' keystrokes are never
  seen.
  - ⚠️ **Undetermined:** whether a Proton/Wine-hosted Star Citizen (an XWayland client) would
    in practice have its keystrokes visible to XRecord. It is *plausible* — the compositor
    forwards input to XWayland, and XRecord records what the X server delivers — but I did not
    test it and found no authoritative source. **Do not ship on this assumption.** If Linux
    PTT matters, this needs an empirical test on a GNOME and a KDE Wayland session with an
    XWayland game in focus.

Either way the honest summary for Linux is: **X11 session = works; Wayland session = does
not work reliably, and fails silently.** That is not a gohook deficiency — it is the Wayland
security model, and it affects *any* option we choose (see §5).

### 3.7 macOS permission — precisely

`hook_run()` gates on accessibility (`hook/darwin/hook_c.h:1067-1068`). The implementation is
`hook/darwin/input_c.h:45-64`:

```c
bool is_accessibility_enabled() {
        bool is_enabled = false;
        if (AXIsProcessTrustedWithOptions != NULL) {
                const void * keys[] = { kAXTrustedCheckOptionPrompt };
                const void * values[] = { kCFBooleanTrue };
                CFDictionaryRef options = CFDictionaryCreate(...);
                is_enabled = AXIsProcessTrustedWithOptions(options);
        }
        ...
}
```

Two verified facts:

1. **It is `kTCCServiceAccessibility`, not Input Monitoring.** `AXIsProcessTrustedWithOptions`
   is the Accessibility trust API — the *identical* predicate the project already uses in
   `internal/hotkeys/permission_darwin.go` (`C.AXIsProcessTrusted()` / `requestAXTrust()`).
   I grepped the entire gohook tree for `IOHIDCheckAccess`, `IOHIDRequestAccess`,
   `kIOHIDRequestTypeListenEvent`, `CGPreflightListenEventAccess`, `CGRequestListenEventAccess`,
   `ListenEvent` and `InputMonitoring`: **zero matches**. Nothing in gohook asks for Input
   Monitoring.
2. **The CGo backend PROMPTS.** `kAXTrustedCheckOptionPrompt: kCFBooleanTrue` means calling
   `hook.Start()` while untrusted raises the system Accessibility sheet as a side effect. That
   collides with this project's deliberate, explicitly-documented permission flow
   (`permission.go`, `PermissionChecker.Request`), which owns when the prompt appears.
   The **purego** backend does not do this — `darwin.go:304` uses the non-prompting
   `axIsProcessTrusted()` and, when untrusted, emits `Event{Kind: HookDisabled}` and returns.
   That is a clean signal we can map onto the existing `PermissionDenied` state.

### 3.7.1 Does a ListenOnly tap need Accessibility or Input Monitoring?

This was the biggest open risk in the first draft of this spike, because it is the *exact*
shape of the bug that cost the project a review cycle: a permission model asserted rather
than verified, indistinguishable on a developer machine that already holds Accessibility.
It is now answered, and the answer is reassuring — but it is subtler than a yes/no.

**Apple's only statement says Input Monitoring.** WWDC 2019 Session 701, *Advances in macOS
Security*:

> "…where a **listen-only event requires authorization for input monitoring, a modifying
> event [tap] requires authorization for accessibility features**."

The same session names `IOHIDCheckAccess(kIOHIDRequestTypeListenEvent)` for checking without
prompting, and `IOHIDRequestAccess` for prompting. The CoreGraphics equivalents are
`CGPreflightListenEventAccess()` / `CGRequestListenEventAccess()`
(`<CoreGraphics/CGEvent.h>`, `API_AVAILABLE(macos(10.15))`).

**Apple's reference documentation was never updated to match**, which is almost certainly the
origin of the earlier wrong answer on this project. The macOS 26.5 SDK comment above
`CGEventTapCreate` still carries pre-Catalina text about *"access for assistive devices …
`AXMakeProcessTrusted`"* and never mentions Input Monitoring; `IOHIDLib.h`'s description of
`kIOHIDRequestTypeListenEvent` mentions only `IOHIDManager`/`IOHIDDevice`, never event taps.
**The tap ↔ ListenEvent linkage exists only in the WWDC session.** Anyone reading only the
headers will reach the wrong conclusion — in either direction.

**In practice Accessibility appears to be a superset, but Apple has never said so.** The
community position (e.g. Apple Developer Forums thread #122492 — note: *no* Apple staff
reply) is: *"If the app already has the Accessibility permission, the app already has
permission for Input Monitoring… If the app is granted the Input Monitoring permission,
`AXIsProcessTrusted` returns false."* KeyCastr documents Accessibility as a manual fallback.
**Treat this as observed behaviour, not a contract.**

**What this means for us — and why it is not the old bug.** gohook's purego backend gates on
`axIsProcessTrusted()` (`darwin.go:304`). Given the above, that gate is *conservative*:

| user has granted | tap would work? | gohook starts? | outcome |
| --- | --- | --- | --- |
| Accessibility | yes | yes | ✅ correct |
| Input Monitoring only | probably yes | **no** | ⚠️ we over-demand — user is told to also grant Accessibility |
| neither | no | no | ✅ correct |

The failure mode is **"we ask for more than strictly necessary"**, never *"we report granted
while events do not flow"*. That second row is the one `permission_darwin.go` documents as the
previous disaster, and it does not occur here. Since our UI already directs users to the
Accessibility pane, and `AXIsProcessTrusted` is already this project's single source of truth,
**the existing permission plumbing stays correct as-is.**

What we must *not* do is "improve" it by switching the gate to
`CGPreflightListenEventAccess`. That would re-create the exact reverted bug: it can report
granted (Input Monitoring held) while gohook's own `axIsProcessTrusted()` check refuses to
start the tap. The two must not disagree — which is precisely the invariant
`permission_darwin.go:95-105` was written to preserve.

**No `Info.plist` key is needed.** `NSInputMonitoringUsageDescription` **does not exist** —
verified four ways: it appears in zero files across the Xcode installation (control,
`NSMicrophoneUsageDescription`: 10 files); Apple's DocC JSON returns **404** for it (control
returns 200); TCC's own `Localizable.loctable` has `REQUEST_ACCESS_SERVICE_kTCCService*`
entries for ~40 services but **none** for `ListenEvent` or `Accessibility` — both bypass the
purpose-string alert entirely; and KeyCastr ships with no `*UsageDescription` key at all.
**Add no key.** What matters instead is a proper bundle with a stable code-signing identity —
TCC grants are keyed to it, and an ad-hoc or changing signature makes the grant evaporate
between builds.

⚠️ Residual unknown → §8.1. The documentary question is closed; the runtime behaviour on a
clean machine is not.

### 3.8 The `// See Bug #22` comment — resolved, and it points the other way

This was worth chasing because if listen-only were broken, any hand-rolled implementation
would inherit the breakage. It is not broken.

**Bug #22 is Google Code issue 22 of JNativeHook — "Unable to stop propagation"**
(filed 2012-09-08; archived at
`https://storage.googleapis.com/google-code-archive/v2/code.google.com/jnativehook/issues/issue-22.json`).
The reporter wanted the *opposite* of what we want:

> "I am unable to stop key event propagation. … It would be very beneficial to be able to
> stop the event from propagating once captured."

The comment was planted in 2012 (`kwhat/jnativehook` commit `93f2eaa`, *"Identified location
for a solution to bug #22 on osx."*) while the code still used ListenOnly, and the flip
landed in 2014 (`kwhat/libuiohook` commit `5f31e08`, *"Flipped OS X tap from passive to
active enabling event consumption"*):

```c
-  kCGEventTapOptionListenOnly,  // kCGEventTapOptionDefault See Bug #22
+  kCGEventTapOptionDefault,     // kCGEventTapOptionListenOnly See Bug #22
```

**Conclusion: listen-only was abandoned solely because it cannot suppress events — a feature
we explicitly do not want.** There is no evidence of listen-only missing events or delivering
degraded data. The comment is a marker of a deliberate capability trade, and for our use case
the trade should be made in the other direction.

(Beware a stale pointer: current libuiohook master rewrites the comment to cite
`github.com/kwhat/jnativehook/issues/22`. That GitHub issue is *"Java listeners aren't
notified in RC3 and RC2 builds"* from 2014-11-29 — the Google Code issues were never migrated
and the numbering coincidence is misleading.)

**Why this matters beyond archaeology:** the active tap gohook's CGo backend inherited sits
*synchronously in the system input path*. If the callback is slow the OS disables the tap
(`kCGEventTapDisabledByTimeout`, handled at `hook_c.h:1040-1044` by restarting it). A
listen-only tap does not carry that liability. For a voice client running alongside a game,
"our process can add latency to every keystroke on the machine, system-wide" is a real
downside of the CGo path and a real argument for purego.

### 3.9 API shape

Two APIs. **Use the first; avoid the second.**

**(a) Raw channel — recommended.**

```go
s := hook.Start()   // chan Event, buffered 1024
defer hook.End()
for e := range s {
    // e.Kind (KeyDown/KeyHold/KeyUp), e.Keycode, e.Rawcode, e.Mask, e.When
}
```

`Event` (`hook.go:56-75`) carries `Kind`, `When`, `Mask` (modifier bitmask), `Keycode`
(libuiohook `VC_*` virtual code), `Rawcode` (platform-native: Win32 VK, X keysym, macOS
Quartz keycode), and `Keychar`.

**(b) `Register` / `Process` — do not use.** `hook.Register(when, []string{"q","ctrl"}, cb)`
plus `<-hook.Process(s)`. Three defects found by reading it:

- **Inverted guard introduced in the beta.** `hook.go:155-157`:
  ```go
  if keyRegistered(ev.Keycode, keys[v]...) {
      continue
  }
  ```
  `keyRegistered` returns true when the event's keycode **is** one of the registered keys —
  so the callback is skipped exactly when the chord's key fires. It also returns `true` for an
  empty key list, breaking the library's own `hook.Register(hook.MouseDown, []string{}, ...)`
  example. I diffed `v0.42.3` (which has neither `keyRegistered` nor the `continue`) against
  `v1.0.0-beta1`: **this is a regression in the beta.**
- **Unsynchronised global state.** `pressed`, `keys`, `upkeys`, `cbs`, `events` are
  package-level maps written by `Register` from one goroutine and by `Process` from another,
  with no mutex (`hook.go:77-91`). Data race.
- **`resetState()` (`hook.go:248-256`) does not clear `upkeys`** — it clears `pressed`,
  `uppressed`, `used`, `keys`, `cbs`, `events` but not `upkeys`. State leaks across
  Start/End cycles, which our `Suspend`/`Resume` does on every rebind.

Taking the raw channel sidesteps all three. We already own chord matching and press/release
semantics in `internal/chord` and `hotkeys.Manager`; we want the event stream, not gohook's
opinion about chords.

**Mapping to `chord.Chord`.** This is unusually good news. `Event.Keycode` is libuiohook's
`VC_*` virtual code — **one code space across all three platforms** (`hook/iohook.h`, 171
`VC_*` constants). Spot-checked against `internal/chord`'s canonical set:

| canonical key | `VC_*` | value |
| --- | --- | --- |
| `A` | `VC_A` | `0x001E` = 30 |
| `Escape` | `VC_ESCAPE` | `0x0001` = 1 |
| `Space` | `VC_SPACE` | `0x0039` = 57 |
| `F1` | `VC_F1` | `0x003B` = 59 |
| `F13` … `F24` | `VC_F13`…`VC_F24` | `0x005B`…`0x006B` |
| `ArrowUp` | `VC_UP` | `0xE048` |
| `Numpad0` | `VC_KP_0` | `0x0052` |
| `;` | `VC_SEMICOLON` | `0x0027` |

That covers **everything** in our canonical set, including the keys the current
implementation has to reject. Today `internal/hotkeys` needs three separate keymap files
(`keymap_darwin.go` 119 lines, `keymap_windows.go` 115, `keymap_linux.go` 125 = **359 lines**)
purely because `x/hotkey` exports differently-named, differently-scoped constants per
platform — and `keymap_darwin.go`'s own comment concedes that *"punctuation, navigation keys
(Insert/Home/End/PageUp/PageDown), the numpad, and F21–F24 have no equivalent here"*.
Moving to `VC_*` collapses those three files into **one** table and **widens** the bindable
key set.

Two caveats:

- Do **not** route through `hook.Keycode` / `github.com/vcaesar/keycode`. That map stops at
  `f12` and has no numpad-`Enter`-style coverage beyond a few entries. Map
  `chord.Chord` → `VC_*` ourselves, one table.
- ⚠️ **Inconsistency in the purego X11 backend.** `x11.go:55-58` documents and implements
  `Keycode = X keycode − 8`, i.e. **raw evdev**, not `VC_*`. For the main alphanumeric block
  evdev and `VC_*` coincide (`KEY_A` = `VC_A` = 30), but for extended keys they diverge:
  evdev `KEY_UP` = 103 vs `VC_UP` = `0xE048` = 57416. So **arrow keys, numpad Enter, right
  Alt/Ctrl and similar will not match a `VC_*` table on the purego X11 backend.** This is a
  concrete portability bug in the beta. Mitigation: keep a small Linux-purego translation for
  extended keys, or restrict Linux to the CGo backend. Flagged as verified-by-source but
  **not verified by running it.**

**Yes, it delivers *all* key events, and that is worth stating plainly.** Both backend
families subscribe to the full input mask — including `kCGEventMouseMoved` and
`kCGEventScrollWheel` (`hook/darwin/hook_c.h:1086-1095`, `darwin.go:350-366`) — and there is
no API to narrow it. Consequences:

- **Privacy.** An always-on global keystroke stream is, mechanically, a keylogger's data
  path. Nothing leaves the process, but the codebase acquires a component that sees every
  keystroke on the machine including passwords typed into other apps. That deserves an
  explicit design note, a narrow internal API (translate to `VC_*`, match against bound
  chords, discard immediately — never log `Keychar`, never buffer), and probably a line in
  user-facing docs. It is also why the macOS Accessibility prompt is not a formality.
- **Performance — and here the two backends differ sharply.**
  - *CGo backend:* every event, **including every mouse-move sample**, does a
    `calloc(200)` + `sprintf` of a JSON string in the C callback, a channel send, then a
    `json.Unmarshal` in Go (`event/dispatch_proc.h`, `extern.go:19-46`). On a 1000 Hz gaming
    mouse that is a meaningful, continuous allocation and parse load — in a process that is
    also doing real-time audio.
  - *purego backends:* a struct built in Go and a non-blocking channel send that **drops on
    full** rather than stalling the input path (`darwin.go:579-591`, `windows.go:666-678`).
    Far cheaper.
- **Latency — a decisive difference.** The CGo backend does **not** push events to Go as they
  happen. `Start(tm ...int)` spawns a goroutine that calls `C.pollEv()` and then sleeps
  `tm` milliseconds, **default 50** (`hook_cgo.go:43-66`). So PTT press *and* release can each
  be delayed up to ~50 ms — clipped first syllable, trailing open mic. You can pass
  `hook.Start(1)`, but that is a 1 kHz busy-poll spinning a goroutine forever. The purego
  backends are event-driven with no poll at all (`darwin.go:230-235`: *"The optional timeout
  argument is accepted for API parity … but is ignored: this backend is event-driven"*).

### 3.10 `log.Fatal` in the hot path — CGo backend only

`extern.go:19-27`:

```go
func go_send(s *C.char) {
    str := []byte(C.GoString(s))
    out := Event{}
    err := json.Unmarshal(str, &out)
    if err != nil {
        log.Fatal("json.Unmarshal error is: ", err)
    }
    ...
```

A single malformed event string **terminates the entire application** — no error return, no
recovery, mid-call. The `sprintf` into a fixed `calloc(200)` buffer upstream makes truncation
at least conceivable. This alone is a strong argument against the CGo backend for a desktop
app users leave running for hours. The purego backends have no JSON layer and no `log.Fatal`.

### 3.11 Practicalities — verified by building

I created a throwaway module under `$TMPDIR` (the project's `go.mod` was **not** touched) and
compiled against `v1.0.0-beta1`:

| target | tags | `CGO_ENABLED` | result |
| --- | --- | --- | --- |
| darwin/arm64 | *(none)* | 1 | ✅ builds |
| darwin/arm64 | `purego` | 1 | ✅ builds |
| darwin/arm64 | `purego` | 0 | ✅ builds |
| linux/amd64 | `purego` | 0 | ✅ builds |
| windows/amd64 | `purego` | 0 | ✅ builds |
| linux/amd64 | `wayland` | 0 | ✅ builds |
| linux/amd64 | *(none)* | 0 | ❌ `undefined: Start`, `undefined: End` |
| windows/amd64 | *(none)* | 0 | ❌ `undefined: Start`, `undefined: End` |
| linux/amd64 | *(none)* | 0, `v0.42.3` | ❌ `undefined: addEvent`, `undefined: Start`, `undefined: KeyHold` |

Notable: **`-tags purego` fixes a gap the project already has.** `registrar_nocgo.go` exists
precisely because `x/hotkey` has no usable no-cgo Linux backend; gohook's purego backend
removes that constraint, and `CGO_ENABLED=0` cross-compilation to Linux and Windows works
from a macOS host with no cross C toolchain. (Wails v3 itself still needs CGo on macOS, but
build tags are per-file — `-tags purego` only changes which *gohook* file compiles. CGO can
stay enabled.)

Other practicalities:

- **Module size:** 700 KB source, 50 files. Vendored C is 420 KB in `hook/` + 24 KB in
  `event/` — 23 header files, 10,729 lines, largest `hook/x11/input_c.h` (1,958 lines).
- **Vendoring:** libuiohook is vendored **in full, header-only** (`hook/{darwin,windows,x11}/*_c.h`,
  `#include`d from one cgo preamble line). **No system libuiohook package is needed.**
  There is no `hook/wayland` — that backend is pure Go.
- **Linux CGo dev packages** (from `hook_cgo.go` LDFLAGS `-lX11 -lXtst -lX11-xcb -lxcb
  -lxcb-xkb -lxkbcommon -lxkbcommon-x11`): `libx11-dev`, `libxtst-dev`, `libx11-xcb-dev`,
  `libxcb1-dev`, `libxcb-xkb-dev`, `libxkbcommon-dev`, `libxkbcommon-x11-dev`, plus
  `gcc`/`libc6-dev`. **`-tags purego` needs none of these.** Concretely: `.github/workflows/test.yml:31`
  and `release.yml:48` currently install `libgtk-*-dev libwebkitgtk-*-dev libsoup-3.0-dev libx11-dev`
  — so the CGo path would require **six additional `apt` packages** in both workflows, while
  `-tags purego` leaves CI untouched.
- **Go version:** gohook `go.mod` requires `go 1.25.0`; the project is on `go 1.25.0`. Fine.
- **Dependencies pulled:** `ebitengine/purego` (★3.9k, Apache-2.0, actively maintained),
  `jezek/xgb` (★183, pure-Go X11), `vcaesar/keycode` (★4), `vcaesar/tt` (test helper),
  `golang.org/x/sys`, and — only under `-tags wayland` — `vcaesar/go-wayland`
  (★0, a maintainer-owned fork of `rajveermalviya/go-wayland`; the least-proven dependency,
  and one we would not compile at all).

### 3.12 License — surprising, but a non-issue for *this* project

The top-level `LICENSE` is **MIT** ("Copyright (c) 2016 go-ego Project Developers"). But
`hook/LICENSE` is the **GPLv3** text, and every vendored C header carries:

> Copyright (C) 2006-2017 Alexander Barker. This program is free software: you can
> redistribute it and/or modify it under the terms of the **GNU Lesser General Public
> License** … version 3 … or (at your option) any later version.

So the CGo build statically links **LGPLv3** code into the shipped binary. For a
closed-source product that would carry relinking/notice obligations.

**It does not apply here.** This repository's own `LICENSE` is **GPLv3** (verified: first
line, *"GNU GENERAL PUBLIC LICENSE Version 3, 29 June 2007"*), and LGPLv3 is explicitly
upward-compatible with GPLv3. Linking is fine on either path.

Recorded anyway because it is genuinely non-obvious (an MIT badge over LGPL C), and because
it would matter immediately if VCS were ever relicensed or a component were extracted for
reuse elsewhere. The purego backends contain **no libuiohook code** and are MIT/Apache-2.0,
so they sidestep the question entirely — a minor bonus, not a deciding factor.

*(Not legal advice.)*

### 3.13 Project health

- **★415, 56 forks**, not archived. Single maintainer (`vcaesar`).
- **Releases:** 18 total; `v1.0.0-beta1` 2026-07-07, `v0.42.3` 2025-12-04, `v0.42.2`
  2025-05-23, `v0.42.0` 2025-02-08, `v0.41.0` 2023-08-18, `v0.40.0` 2022-01-02. Bursty —
  18-month gaps in 2022–2023 and 2023–2025. **No stable v1.0.0.**
- **Last commit:** 2026-07-08 (PR #73). ~2 months quiet as of 2026-09-15.
- **13 open issues.** The ones that matter to us:
  - **#67** (2025-07-14, 14 months, **zero comments**) — hard Linux compile failure on
    GCC 15 / C23: `conflicting types for 'load_input_helper'`. Any modern Linux CI image will
    hit this **on the CGo path**. `-tags purego` avoids it.
  - **#47** (2024-04-04, unanswered) — duplicate `KeyDown` on hold (macOS) and on single
    press (Windows). → we must debounce; see §3.5.
  - **#30** (2021) — "Registering F12 triggers every function key". Maintainer: *"I will add
    the feature in the future."* Concerns `Register`/`Process`, which we are not using.
  - **#41** — "Framework doesn't stick to any key codes standard". Consistent with the
    evdev-vs-`VC_*` divergence found in §3.9.
  - **#28** — "Does it work on Wayland?" Maintainer: *"Not supported now, maybe supported
    later."* Still open despite the new backend.
  - **#16 / #23** (2020) — event consumption not exposed. Confirms §3.2; irrelevant to us.
- **Real maintenance in 2025–2026:** #71 "Fix unset_modifier_mask to handle duplicate keyup
  events", #69 "Fix linux build", #64 GCC 15.1.1 build args, #61 macOS rawcode mapping, and
  the whole pure-Go rewrite (#72). Preceded by a five-year gap back to 2020.

Read honestly: **maintenance is real but thin, single-maintainer, and bursty**, with issues
sitting unanswered for years.

---

## 4. Patching `x/hotkey` ourselves — assessed

The intuition — "on macOS it's just `return event` instead of `return NULL`" — is correct,
and it is also the *only* platform where it is true.

**macOS: genuinely one line.** `hotkey_darwin.m:95,101` returns `NULL` to consume. Changing
that to return `event` makes the tap pass through. (Better still: also flip
`CGEventTapCreate` to `kCGEventTapOptionListenOnly` so it cannot consume and stops sitting
synchronously in the input path — §3.8.)

**Windows: cannot be patched. The mechanism is wrong.** `RegisterHotKey` has no
pass-through mode. It is a registration against the system's hotkey table; when it matches,
the OS posts `WM_HOTKEY` to the registering thread **and does not deliver the key to the
focused window**. There is no flag, no return value, no callback. Making it pass through
means **deleting it and writing `SetWindowsHookEx(WH_KEYBOARD_LL)` + `CallNextHookEx` +
a message pump** — a different API, a different threading model (the installing thread must
own a `GetMessage` loop), and a different event model (raw VK codes, so we do our own chord
matching and our own key-repeat debouncing). **Confirmed: this is a wholesale replacement,
not a patch.**

**Linux/X11: cannot be patched. The mechanism is wrong.** `XGrabKey` *is* the grab. There is
no non-grabbing mode. `XAllowEvents`/`GrabModeSync` lets you *replay* a grabbed key
(`XAllowEvents(dpy, ReplayKeyboard, ...)`) — but that is a synchronous-grab dance that
serialises the X input queue behind our process and is notoriously fragile with games and
with any other client that grabs. The correct mechanism is **XRecord** (passive, exactly
what gohook uses) or `XInput2` raw events. **Confirmed: wholesale replacement.**

So "patch `x/hotkey`" is really: keep one line on macOS, rewrite both other backends. At that
point we have written ~80% of gohook's purego backends ourselves, with none of the testing —
and we would have to maintain a fork of a third-party library, or vendor it, forever. The
honest version of this option is not "patch the library" but "write our own OS layer"
(§8.4), and the one-line macOS insight survives into that option.

---

## 5. Alternatives

### 5.0 The headline

**There is no maintained cross-platform Go library for passive global key listening other
than gohook.** An exhaustive `gh search` across `libuiohook`, `"global keyboard hook"`,
`keylogger`, `global-hotkey`, `hotkey`, `evdev`, `"push to talk"` and `libei`
(`language:go`) surfaced exactly one other Go wrapper of libuiohook —
`ironpark/go-libuiohook`, **1 star, alive for two days in July 2024** — and nothing else
cross-platform. Every other credible option is **single-platform**, so "not gohook" means
"assemble and maintain three platform layers ourselves".

### 5.1 `github.com/moutend/go-hook` (Windows only) — **reject**

MIT · no cgo · no deps · 95★ · **last commit 2020-05-20** (≈6 years).

- **Pass-through: yes.** `pkg/keyboard/keyboard_windows.go:30` returns
  `win32.CallNextHookEx(0, code, wParam, lParam)` unconditionally, on every path.
- **Key-up: yes.** The handler forwards `Message: types.Message(wParam)` verbatim
  (`:24-27`), and `pkg/types/constant.go:38-41` defines `WM_KEYDOWN`/`WM_KEYUP`/
  `WM_SYSKEYDOWN`/`WM_SYSKEYUP`.
- **Permission:** none. **Platforms:** Windows only (`keyboard_func.go`, `//+build !windows`,
  returns `"keyboard: not supported"`).

**Rejected on memory safety, not on scope.** The hook proc is correct; the message pump
around it is not, and you cannot use one without the other:

1. `keyboard_windows.go:61` declares `var msg *types.MSG` (nil) and then calls
   `win32.GetMessage(&msg, …)`, which forwards `uintptr(unsafe.Pointer(lpMsg))` — handing
   `GetMessageW` the address of an **8-byte pointer variable** into which the OS writes a
   full `MSG` struct. Heap corruption on every message.
2. The `MSG` layout is wrong for 64-bit regardless (`pkg/types/win32.go:34-41` declares
   `WParam uint32` / `LParam uint32`; the real `WPARAM`/`LPARAM` are 8 bytes on Win64).
3. `win32_windows.go:51` passes `wMsgFilterMin` twice to `GetMessage` (the second should be
   `wMsgFilterMax`); `:68-72` `GetModuleHandle` calls `procSetWindowsHookExW`.

Additionally it does a **blocking channel send from inside the low-level hook callback**
(`:24`), which is exactly what trips Windows' `LowLevelHooksTimeout` (see §5.4).
A heap corruptor is not an acceptable foundation for a PTT feature.

### 5.2 Linux evdev — `MarinX/keylogger`, and the better `holoplot/go-evdev`

**`MarinX/keylogger`:** MIT · no C deps · 250★ · last commit 2026-08-21 · lightly maintained.

- **Reads `/dev/input/eventN`: yes** — `keylogger.go:38`
  `os.OpenFile(devPath, os.O_RDWR, os.ModeCharDevice)`; discovery walks
  `/sys/class/input/event%d/device/name` (`:52-72`).
- **Pass-through: yes.** It never calls `EVIOCGRAB` (grepped: zero matches). Reading an evdev
  node is pure observation; only `EVIOCGRAB` is exclusive.
- **Key-up: yes.** `input_event.go:58-64`, `KeyPress() { Value == 1 }`,
  `KeyRelease() { Value == 0 }` on `EvKey`. (Value 2 is autorepeat and matches neither.)
- **Wayland: YES — and it is the only option here that does.** evdev is the kernel character
  device, entirely *below* the display server. Nothing in the source touches X11 or Wayland.

**The permission is the blocker.** `/dev/input/event*` is `crw-rw---- root:input` (0660):
systemd `rules.d/50-udev-default.rules.in:51` sets `SUBSYSTEM=="input", GROUP="input"`, and
`src/udev/udev-node.c:621-623` applies 0660. Crucially, `70-uaccess.rules.in` has exactly one
input rule — line 73, `ENV{ID_INPUT_JOYSTICK}=="?*"` — **joysticks only**. Keyboards get no
`uaccess` tag, so `73-seat-late.rules.in` never ACLs them to the seat user. **On a stock
Ubuntu or Fedora desktop a normal logged-in user cannot read keyboard evdev nodes.** It needs
root, `usermod -aG input $USER` plus a re-login, or a shipped udev rule. That carve-out is
deliberate: granting keyboards would hand every session app a keylogger.

Defects: `O_RDWR` is hardcoded (it wants write access for LED output, which we do not want);
device discovery is **substring matching** on the kernel device name
(`allowedDevices = {"keyboard", "logitech mx keys"}`, `:32-33`), so a keyboard whose name
lacks the literal string "keyboard" is simply never found; and there are **no build tags** —
it compiles on Windows/macOS while being functionally Linux-only, so we would have to guard
it ourselves.

**`github.com/holoplot/go-evdev` is the better choice if we go this route:** MIT, 61★, last
commit 2026-09-09, actively maintained. `OpenWithFlags(path, flags)` permits `O_RDONLY`
(`device.go:26`); `InputEvent{Time, Type, Code, Value}` (`types.go:23-28`); `Grab()`/
`Ungrab()` exist explicitly via `EVIOCGRAB` (`device.go:335-341`) and we simply never call
them; and it ships generated `codes.go`/`names.go` plus real capability bitmaps
(`bitmap.go`, `mask.go`) for correct device selection instead of name matching.

**Verdict:** the only thing that solves Wayland, at the cost of a Linux-only code path and a
`usermod -aG input` step in our install docs. A credible **Phase 3** addition; not a
foundation.

### 5.3 Other Go wrappers of libuiohook — none viable

| Library | Verdict |
| --- | --- |
| `ironpark/go-libuiohook` | 1★, created 2024-07-04, last push 2024-07-05. Dead. |
| everything else named `uiohook` | Node / Rust / Dart / C# / Lua / Python — not Go. |

**In Go, libuiohook means gohook.** That is a concentration risk worth naming explicitly (R11).

### 5.4 Rolling our own per-platform layer

Viable, and on two platforms it would be *better* than gohook. The question is cost.

**macOS — listen-only `CGEventTapCreate`.** Exactly what gohook's purego backend already
does. Permission: see §3.7.1 — this is where the interesting finding is.

> **Implementation trap worth recording even if we adopt gohook.** KeyCastr
> (`KCEventTap.m:131-141`), which runs precisely this configuration (`kCGSessionEventTap` +
> `kCGHeadInsertEventTap` + `kCGEventTapOptionListenOnly` + KeyDown|KeyUp) and demonstrably
> delivers key-up, carries this warning:
>
> > "We have to try to tap the keydown event independently because `CGEventTapCreate` will
> > succeed if it can install the event tap for the flags changed event, which apparently
> > doesn't require universal access to be enabled. Thus, the call would succeed but KeyCastr
> > would be, um, useless."
>
> `CGEvent.h` explains it: disallowed bits are **cleared from the mask**, and only an
> *empty* resulting mask returns NULL. **gohook's purego backend requests keyboard *and*
> mouse in one combined mask** (`darwin.go:350-366`), so `port != 0` is **not** a reliable
> permission signal there — it could succeed on the mouse bits while the keyboard bits are
> silently stripped, yielding mouse events and no keys. gohook mitigates this by checking
> `axIsProcessTrusted()` *first* (`darwin.go:304`), so in practice it fails closed. But the
> combined mask means we must not treat "the tap was created" as proof, and if we ever roll
> our own we should create a **keyboard-only tap** so that NULL is meaningful.

**Windows — `WH_KEYBOARD_LL` works, but Microsoft recommends Raw Input.** From the
[LowLevelKeyboardProc docs](https://learn.microsoft.com/en-us/windows/win32/winmsg/lowlevelkeyboardproc),
verbatim:

- *"This parameter can be one of the following messages: WM_KEYDOWN, WM_KEYUP, WM_SYSKEYDOWN, or WM_SYSKEYUP."* → key-up ✅
- *"…may return a nonzero value to prevent the system from passing the message to … the target window procedure."* → returning `CallNextHookEx`'s value passes through ✅
- *"…the thread that installed the hook must have a message loop."*
- ⚠️ **the operational landmine:** *"If the hook procedure times out, the system passes the message to the next hook. However, **on Windows 7 and later, the hook is silently removed without being called. There is no way for the application to know whether the hook is removed.**"* The `LowLevelHooksTimeout` cap has been 1000 ms since Win10 1709.
- Microsoft's own steer: *"**In most cases where the application needs to use low level hooks, it should monitor raw input instead.** This is because raw input can asynchronously monitor mouse and keyboard messages that are targeted for other threads more effectively than low level hooks can."*

**Raw Input (`RegisterRawInputDevices` + `WM_INPUT`) is strictly better**: `RIDEV_INPUTSINK`
(*"enables the caller to receive the input even when the caller is not in the foreground"*);
`RAWKEYBOARD.Flags` gives `RI_KEY_MAKE` (0, down) / `RI_KEY_BREAK` (1, up); suppression
requires `RIDEV_NOLEGACY`, which suppresses legacy messages only *"for that device for the
application"* — i.e. for us, never for the game; and there is no `LowLevelHooksTimeout` and no
silent removal. There is no ready-made Go raw-input keyboard library, but the bindings exist
(`lxn/win`, `zzl/go-win32api`); a message-only window plus a `WM_INPUT` loop is roughly
150–250 lines.

**Linux/X11 — XInput2 raw events beat XRecord.** Both are passive. XRecord's passivity is
guaranteed by the protocol spec itself (`xorgproto`, `specs/recordproto/record.xml:280-288`):

> "Event filtering … does not provide a mechanism for in-place, synchronous event
> substitution, modification, or withholding."

But XI2 raw events are better for our case (XI2 protocol spec, §RawEvent,
`specs/XI2proto.txt:2572-2583`):

> "**RawEvents are sent exclusively to all root windows.** Clients supporting XI 2.0 receive
> raw events when the device is not grabbed… **Clients supporting XI 2.1 or later receive raw
> events at all times, even when the device is grabbed by another client.**"

That last clause means an XI2 listener **keeps working even if the game takes an exclusive
keyboard grab** — XRecord makes no such promise, and fullscreen games do grab. Raw delivery is
an *additional* path to root windows, so focus-based delivery to the game is untouched. No
privilege beyond a valid `$DISPLAY`.

Costs: raw events are impoverished — `detail / sourceid / flags / valuators`, with no event
window, no focus info, no `mods`/`group`, and `detail` is a raw keycode with no keysym. We
translate via xkbcommon and track modifiers ourselves. For PTT that is arguably a *feature*
(the game's keyboard layout cannot confuse us). **Go tooling gap:** `jezek/xgb` ships a
`record` subpackage but **no `xinput`**, so pure-Go XRecord is available and pure-Go XI2 is
not — XI2 means cgo or hand-rolled protocol encoding.

> Also noted: libuiohook calls `XSynchronize(data_display, True)` (`input_hook.c:891`) with
> the comment *"Make sure the data display is synchronized to prevent late event delivery!
> See Bug 42356"*. A latency trap that any hand-rolled XRecord implementation must not skip.

**Linux/Wayland — evdev is the only option that meets both requirements.** See §5.2 and
§3.6. The `xdg-desktop-portal` `GlobalShortcuts` portal does define `Activated` *and*
`Deactivated`, so it has both edges — **but every backend implements it as a compositor
grab**, so the game does not get the key, and the user cannot even choose which key (the
Hyprland protocol XML: *"A global shortcut is anonymous, meaning the app does not know what
key(s) trigger it. The shortcut's keybinding shall be dealt with by the compositor."*).
That fails our core requirement. Support floors are also poor: KDE Plasma 5.27+;
**GNOME needs 48+** for key-up specifically (`AcceleratorDeactivated` was dead code until
gnome-shell commit `fecd5cdd`, 2025-01-28, *"Actually emit AcceleratorDeactivated signal"*);
wlroots/Sway — `xdg-desktop-portal-wlr` does not implement the portal at all.

**Sizing:** ~3–5 days for macOS (ListenOnly tap) + Windows (Raw Input) + Linux/X11 (XI2, cgo)
+ Linux/Wayland (evdev), plus keymaps and permission plumbing, and then we own all of it
forever.

### 5.5 What comparable products actually do

Useful because it calibrates how hard the Linux problem really is:

- **Mumble** — three tiers, in order: **evdev** (`GlobalShortcut_unix.cpp:90-111`), then
  **XI2 raw** (`:118-155`: `XI_RawKeyPress | XI_RawKeyRelease | …`,
  `evmask.deviceid = XIAllDevices`, selected on every root window), then polling
  (*"No XInput support, falling back to polled input. This wastes a lot of CPU resources"*).
  It touches `EVIOCGRAB` only as a probe and immediately releases it (`:371-377`).
  **No XRecord anywhere.** No portal backend merged — PR #5976 is still an open draft,
  with maintainers objecting that the portal cannot distinguish devices or left/right
  modifiers.
- **OBS** — `xcb_query_keymap` polling for keys (`libobs/obs-nix-x11.c:1051`); XI2 raw for
  mouse buttons only; Wayland **deliberately stubbed** (`libobs/obs-nix-wayland.c:261-269`):
  ```c
  // This function is only used by the hotkey thread for capturing out of
  // focus hotkey triggers. Since wayland never delivers key events when out
  // of focus we leave this blank intentionally.
  return false;
  ```
- **Discord** — polls X keyboard state on X11 (passive, so the game keeps the key);
  **unsupported on Wayland**. The standing community answer is XWayland or the
  `Rush/wayland-push-to-talk-fix` shim, whose README reads: *"Read specific key events via
  evdev (needs sudo)…"* and *"`sudo usermod -aG input <your username>`"*.

Two conclusions. First, **nobody solves Wayland PTT without evdev and a group change** — our
gap is the industry's gap, and shipping "X11 only, clearly signposted" is the normal state of
the art. Second, **nobody uses XRecord**; the serious implementations prefer evdev or XI2.
That is a real argument for eventually replacing gohook's Linux backend (§5.4) even if we
adopt gohook now.

### 5.6 Wails v3's built-in `GlobalShortcutManager` — documented so nobody proposes it

The project already depends on `wailsapp/wails/v3 v3.0.0-beta.22`, which ships a global
shortcut API. It is **unsuitable on both counts**:

- **No key-up at all.** The signature is `Register(accelerator string, callback func()) error`
  — a single fire, no release edge. PTT is not expressible.
- **Exclusive on every platform.** macOS: Carbon `RegisterEventHotKey`
  (`global_shortcut_darwin.go:46`). Windows: `RegisterHotKey` (`:68`). X11: `XGrabKey`
  (`global_shortcut_linux_x11.go:60`). Wayland: the portal, handling `Activated` only with no
  `Deactivated` handler in the file.

It is the same class of thing as `x/hotkey`, with strictly less capability.

### 5.7 Rejected on supply-chain grounds

`gvalkov/golang-evdev` (archived), `bnema/libei-go-bindings` (0★ — and libei is *emulation*,
whose receiver contexts pair with the exclusive-by-design InputCapture portal, so it is the
wrong tool regardless), `makenowjust/hotkey` / `Nyx2022/winhotkey` / `jeet-parekh/hotkeys`
(Windows `RegisterHotKey`, exclusive and press-only), and a cluster of
`kindlyfire/go-keylogger`, `SaturnsVoid/*`, `EgeBalci/EGESPLOIT`, `uknowsec/keylogger`,
`vgo0/gologger` — archived, PoC, or malware-adjacent. Not candidates.

---

## 6. Risks

| # | Risk | Severity | Mitigation |
| --- | --- | --- | --- |
| R1 | **macOS ListenOnly permission.** Apple's only statement (WWDC 2019 S701) says a listen-only tap is governed by **Input Monitoring**, while gohook gates on **Accessibility** (§3.7.1). | **Medium** (downgraded from High once §3.7.1 was resolved) | The gate errs *conservatively*: worst case we over-demand a permission; we never report "granted" while events do not flow. Existing `permission_darwin.go` stays correct — **do not** switch it to `CGPreflightListenEventAccess`, that re-creates the reverted bug. Confirm empirically on a clean account (Phase 0). |
| R2 | purego backends are ~2 months old, beta-tagged, effectively unreviewed | High | Pin the exact version; own manual test matrix; CGo backend is a tag-flip away as fallback. |
| R3 | Wayland: silent no-events. Fails invisibly, including in dev. gohook does **no** runtime session detection — grepping the tree for `XDG_SESSION_TYPE` and `WAYLAND_DISPLAY` returns zero matches, so the backend selection is a compile-time tag with no runtime guard. | High | Detection is entirely ours: check `XDG_SESSION_TYPE=wayland` / `WAYLAND_DISPLAY` at startup and surface an explicit "global hotkeys unavailable on Wayland" state through the existing `Registered()` / `Failed()` UI path. Never fail silently. |
| R4 | purego X11 `Keycode` is evdev, not `VC_*` — arrows/numpad/right-modifiers mismatch (§3.9) | Medium | Linux-specific translation table, or use the CGo backend on Linux. Test every canonical key on Linux. |
| R5 | Always-on global keystroke stream = keylogger data path | Medium | Narrow internal API; translate and discard immediately; never log `Keychar`; document it. Security review before merge. |
| R6 | Key auto-repeat produces duplicate `KeyDown` (issue #47) → PTT re-latching | Medium | Debounce in our Registrar: latch on first down, ignore until matching up. Required regardless of library. |
| R7 | CGo path: LGPLv3 statically linked (§3.12) | **None** — this repo is GPLv3, which LGPLv3 is compatible with | No action. Would matter only on relicensing or component extraction. |
| R8 | CGo path: `log.Fatal` on malformed event kills the app (§3.10) | Medium | `-tags purego` avoids it entirely. |
| R9 | CGo path: 50 ms default poll latency on PTT press/release (§3.9) | Medium | `-tags purego` (event-driven), or `hook.Start(1)` and accept a 1 kHz busy-poll. |
| R10 | CGo path: Linux build break on GCC 14+/C23 — `conflicting types for 'load_input_helper'` (issue #67, open 14 months, zero comments). Present in stable `v0.42.3`; fixed on master/`v1.0.0-beta1`. | Medium | `-tags purego`, or take the beta, or pin CI to GCC ≤ 13. Note this means **the stable tag cannot be used on a modern Linux image at all** — another push toward the beta. |
| R11 | Single maintainer, bursty cadence, ~2 months quiet | Medium | MIT + fully vendored → we can fork. The three purego backends we would use are 2,216 lines of readable Go with no C (2,718 including `wayland.go`). |
| R12 | Stale-key risk: a `KeyUp` missed while the app is backgrounded or the tap is re-armed leaves the mic open | **High (product)** | Watchdog: hard-release PTT after N seconds without a `KeyUp`, and on `HookDisabled`, focus-loss, and tap re-enable. Independent of library choice. |
| R13 | `-tags wayland` accidentally applied to a macOS/Windows build silently removes every backend (§3.1) | Low | Never set it globally; if used at all, guard with `GOOS=linux`. |
| R14 | **Windows: `LowLevelHooksTimeout`.** Per Microsoft, on Windows 7+ a slow `WH_KEYBOARD_LL` hook is *"silently removed without being called. **There is no way for the application to know whether the hook is removed.**"* PTT would die mid-session with no signal. Cap is 1000 ms since Win10 1709. | **High (product)** | Never block in the callback — gohook's purego backend already does a non-blocking drop-on-full send (`windows.go:666-678`). Add a watchdog that periodically verifies liveness and re-installs. Longer term, Raw Input (§5.4) has no such failure mode. |
| R15 | **macOS: `CGEventTapCreate` succeeds on a partially-stripped mask.** Disallowed bits are cleared, and only an *empty* mask returns NULL; gohook requests keyboard **and** mouse in one mask (§5.4, the KeyCastr trap). | Medium | gohook checks `axIsProcessTrusted()` first, so it fails closed in practice. Never treat "tap created" as proof of permission; `AXIsProcessTrusted` remains the single source of truth. |
| R16 | **Fullscreen games take exclusive X11 keyboard grabs**, which XRecord makes no promise about; only XI 2.1+ raw events are documented to survive another client's grab (§5.4). | Medium (Linux only) | Test with a fullscreen game on X11. If broken, replace the Linux backend with XI2 raw (§5.4) — the seam makes this a per-platform swap. |

---

## 7. Recommendation

**Replace `golang.design/x/hotkey` with `github.com/robotn/gohook@v1.0.0-beta1`, consumed
through the raw `hook.Start()` event channel, built with `-tags purego`, behind the existing
`hotkeys.Registrar` seam.**

Linux ships **X11-only, explicitly signposted**; Wayland is out of scope for this change and
is not solvable by any library (§5.5 — neither Discord, Mumble nor OBS solves it either).

### Why

1. **It is the only option that solves the actual problem on all three platforms with one
   dependency.** Pass-through is verified in source on every backend, and on macOS-purego and
   X11 it is guaranteed *by mechanism* rather than by a flag we could regress.
2. **Key-up is delivered everywhere**, verified per backend (§3.5). PTT is satisfiable.
3. **`VC_*` is one cross-platform key space.** This *deletes* 359 lines of three-way keymap
   code and *widens* the bindable set to numpad, punctuation and F13–F24 — keys
   `keymap_darwin.go` currently has to reject. That is a rare case of the safer choice also
   being the smaller one.
4. **`-tags purego` dominates the CGo default on every axis that matters here:** listen-only
   (cannot consume, ever, and never sits in the synchronous input path); event-driven instead
   of a 50 ms poll; no JSON and no `log.Fatal`; no C toolchain and no six extra X11 dev
   packages in CI; no GCC-14+ build break; non-prompting `AXIsProcessTrusted`, which leaves
   this project's carefully-built permission flow in charge of when the sheet appears. It also
   *fixes* the existing `CGO_ENABLED=0` Linux gap that `registrar_nocgo.go` documents — that
   file can be deleted.
5. **Both scares resolved in our favour.** `Bug #22` (§3.8): listen-only was abandoned only
   because it *cannot suppress* events — nothing suggests it is lossy. The macOS permission
   question (§3.7.1): gohook's Accessibility gate errs conservatively, so the existing
   `permission_darwin.go` stays correct untouched, and the dangerous direction of error — the
   one that burned this project before — does not arise.
6. **One listener instead of nineteen**, and a real key-up on Windows instead of a 10 ms
   `GetAsyncKeyState` poll (§1). gohook gives us a single stream that we fan out in Go —
   fewer OS objects, less input-path latency, and `Suspend`/`Resume` becomes a boolean rather
   than tearing down and rebuilding 19 registrations (≈76 X11 grabs) on every rebind.
7. **The blast radius is one file.** `hotkeys.Manager`, `internal/chord`, `internal/keybinds`,
   the permission flow, the DTOs, the UI, and all 328 lines of `hotkeys_test.go` are unchanged
   — they test against fake `Registrar`s. This is exactly the seam the project built for this.
   Verified by grepping every non-test call site of the OS-layer constructors: `NewOSRegistrar()`
   has exactly **one** (`main.go:95`), `NewPermissionChecker()` exactly one
   (`internal/app/app.go:87`), and `SupportsRelease()` **none** outside the package. Replacing
   `registrar_x.go` + the three keymap files changes nothing above them.

### What it does not solve

**Wayland.** Nothing in the Go ecosystem solves Wayland global hotkeys today; it is a
deliberate platform restriction, not a library gap. Accept X11-only on Linux for now, detect
Wayland explicitly, and tell the user. Do not let it fail silently — that is R3, and silent
failure on a developer machine is precisely the pattern that cost this project a cycle before.

### The main alternative considered, and why not

A **hybrid** is defensible and was seriously weighed: gohook `-tags purego` for macOS and
Windows (both are exactly the right API), with a **hand-rolled Linux layer** — XI2 raw events
on X11, `holoplot/go-evdev` on Wayland. It is technically better on Linux: XI 2.1+ raw events
survive another client's keyboard grab (R16) and evdev is the only thing that works under
Wayland at all.

**Not now**, for three reasons: Linux is a secondary target for this client; XI2 has no
pure-Go binding (`jezek/xgb` ships `record` but no `xinput`), so it reintroduces cgo on the
one platform where `-tags purego` just removed it; and evdev requires `usermod -aG input`,
which is a support burden we should not take on speculatively. The `Registrar` seam makes
this a **cheap later swap** — that is the point of the seam. Adopt it if R16 fails in testing,
or if Linux users actually ask for Wayland.

### Sequenced plan

**Phase 0 — de-risk before committing (blocking, ~half a day).** A throwaway binary, not
project code:
1. **macOS permission, on a clean user account.** With neither permission granted, confirm a
   `-tags purego` listener receives nothing. Grant **only Accessibility** → confirm it *does*
   receive key events (this is the community claim in §3.7.1 that Apple has never confirmed;
   it is the load-bearing assumption). Grant **only Input Monitoring** → expect gohook to
   refuse, which is the acceptable conservative failure. **Under no circumstances ship a
   `Status()` that reports "granted" while events do not flow** — that is the exact failure
   `permission_darwin.go` already documents.
2. Verify pass-through concretely: hold the PTT key in a text editor and in a game, and
   confirm the character still arrives.
3. Verify key-up fires for a plain key, for a modifier-only chord (synthesised from
   `kCGEventFlagsChanged`, §3.5), and after alt-tabbing away mid-press.
4. Measure press→event latency (expect sub-millisecond on purego).
5. Linux/X11: verify the evdev-vs-`VC_*` divergence (R4) on arrows and numpad, and test with a
   **fullscreen** game to probe the grab question (R16).
6. Windows: verify `WH_KEYBOARD_LL` pass-through, and investigate the anti-cheat/UIPI question
   (§8.4) — this is the one that could still be a hard blocker.

**Phase 1 — implement.** New `registrar_hook.go` implementing `Registrar` over **one** shared
`hook.Start()` stream, fanned out to registered actions. `Register` becomes "record
`actionID → (chord, hold, handler)` in a map"; `UnregisterAll` clears the map; the OS listener
starts on first entry and is **never stopped on rebind** — `End()` sleeps, drains and closes a
package-global channel (`darwin.go:248-278`), and restart is racy. One `chord.Chord` → `VC_*`
table replacing the three keymap files. Debounce auto-repeat (R6). Add the stale-key watchdog
(R12) and the Windows hook-liveness watchdog (R14). Delete `registrar_nocgo.go`. Fix the
incorrect comment at `registrar_x.go:23-30` on the way past (§1).

**Phase 2 — harden.** Wayland detection surfaced through `Registered()`/`Failed()` (R3).
Security review of the keystroke path (R5). Pin gohook to `v1.0.0-beta1` exactly, with a
comment explaining why the beta and not the stable tag (R10), and record the fallback.

### Fallback

The **CGo backend** (drop the `purego` tag) — a one-line change. Same pass-through guarantee,
same `VC_*` codes, Accessibility permission identical to what the project already implements.
Costs: a C toolchain, six extra Linux dev packages in CI, the 50 ms poll (mitigate with
`hook.Start(1)`), the `log.Fatal` (§3.10), and pinning CI to GCC ≤ 13 (R10). Still strictly
better than the status quo, which is broken.

---

## 8. Explicit unknowns

Recorded deliberately. An honest unknown is more useful than a confident wrong answer — which
is what cost this project a review cycle when an earlier revision asserted the macOS
permission model from general knowledge instead of reading what the library gated on
(`permission_darwin.go` documents that incident).

### 8.1 Empirical confirmation of the macOS permission behaviour

The *documentary* question is answered in §3.7.1 — Apple's only statement on the subject says
a listen-only tap is governed by Input Monitoring — and the analysis there concludes that
gohook's Accessibility gate errs in the **safe** direction. What remains genuinely unverified
is the runtime behaviour on a clean machine:

- that Accessibility alone actually lets a ListenOnly tap receive key events (every source for
  this is community, never Apple — see §3.7.1);
- that the combined keyboard+mouse tap mask (§5.4, the KeyCastr trap) does not produce a
  partially-stripped tap in some state we have not thought of.

Still a **blocking Phase 0 test**, but on the strength of §3.7.1 the expected failure mode is
"we over-demand a permission", not "we report granted while nothing works". The second is the
one that burned this project before; it is not the exposure here.

### 8.2 XRecord under XWayland with a Proton-hosted game

Undetermined; see §3.6. Plausible in theory, untested. Do not ship Linux PTT on this
assumption.

### 8.3 Runtime behaviour in general

Everything in this document is from reading source, plus compile-verification of the build
matrix in §3.11. **No backend was executed.** Specifically unverified at runtime:
pass-through, key-up delivery, the `Register`/`Process` inversion (§3.9b, read but not run),
the evdev/`VC_*` divergence (R4), and all latency figures.

### 8.4 Windows: UIPI / elevated windows, and anti-cheat

**Not documented by Microsoft.** Neither the `SetWindowsHookEx` page nor the
`LowLevelKeyboardProc` page says anything about integrity levels or elevated windows. The
folklore answer is that a non-elevated `WH_KEYBOARD_LL` hook does not see input directed at
elevated windows; I found no authoritative source and **will not assert it**. (What *is*
documented, and is favourable, is the Store-app case: *"window hook DLLs are not loaded
in-process for the Windows Store app processes … The notification is delivered on the
installer's thread for these hooks: … WH_KEYBOARD_LL"*.)

Separately and more importantly: I did not determine whether Star Citizen runs elevated, or
whether its anti-cheat interferes with low-level keyboard hooks. **An anti-cheat that objects
to `WH_KEYBOARD_LL` would be a hard blocker on Windows** — the single highest-consequence
unknown remaining, and outside what source reading can answer. It is in the Phase 0 list.

### 8.5 `NSEvent` global monitors (the macOS alternative not taken)

Two things I could not establish, recorded because they would matter if we ever revisit
`addGlobalMonitorForEventsMatchingMask:` instead of an event tap:

1. **Whether modern macOS delivers `NSEventMaskKeyUp` to a global monitor.** The API
   reference's Special Considerations enumerates monitorable types and lists `NSKeyDown` but
   **not** `NSKeyUp` — pointedly, since all three mouse-*up* types *are* listed, and Apple's
   archived guidance says *"Other OS X technologies such as CGEventTap … allow for monitoring
   key events of NSKeyUp"*. But that list is scoped "In OS X v10.6" and was never revised.
   **Undetermined for modern macOS.** (Passivity, at least, is guaranteed — `NSEvent.h`:
   *"you can only observe the event; you cannot modify or otherwise prevent the event from
   being delivered to its original target application."*)
2. **Whether `NSEvent` global monitors additionally require Input Monitoring on 10.15+.**
   Apple documents only Accessibility for this API (`NSEvent.h`, referencing
   `AXIsProcessTrusted`); the contrary claim comes from the Karabiner author and is
   unconfirmed by Apple.

### 8.6 XRecord and auto-repeat

Whether XRecord's observed auto-repeat is suppressed by the recorder's own
`XkbSetDetectableAutoRepeat` is not addressed by the spec. libuiohook calls it
(`input_hook.c:806-815`), which suggests it matters. If we use XRecord, **test held-key
behaviour empirically** — spurious release/press pairs would make PTT stutter, which is
exactly the R6 failure in a different disguise. (XI2 raw events should not have this problem,
since they come from the driver — but that is expectation, not verification.)

### 8.7 gohook release-date detail

Two sources disagree slightly on when `v0.42.3` was cut (tag metadata vs. commit date) and
therefore on whether the GCC-14 fix landed just before or just after it. What is **not** in
doubt: issue #67 is open, the fix is present on master and `v1.0.0-beta1`, and the
recommendation takes the beta regardless. Not worth resolving.

---
