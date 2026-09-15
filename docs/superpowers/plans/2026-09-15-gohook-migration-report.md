# Migration report: passive hotkey listening via `robotn/gohook`

- **Date:** 2026-09-15
- **Branch:** `feat/phase-3-settings-keybinds` (PR #23)
- **Commits:** `cb2c3f6` (Change A), `a21b9f6` (Change B)
- **Input spec:** `docs/superpowers/specs/2026-09-15-passive-hotkey-listening-spike.md`

---

## 0. Headline

Both changes landed. Everything above the `hotkeys.Registrar` seam is untouched and all 328
lines of the existing `hotkeys_test.go` pass unmodified, which is exactly what the seam was
built for.

**Three findings contradict the spike and are the most important part of this report.**
None of them is a reason not to ship; all three needed a decision I did not take silently:

1. **`-tags purego` is not namespaced, and it broke gRPC.**
   `google.golang.org/protobuf@v1.34.2` reads `purego` as "this build has no `unsafe`" and
   switches `internal/impl` to a reflect fallback whose `Export.MessageStateOf` is
   `panic("not supported")`. Every gRPC call panicked at the first `proto.Marshal`. Four
   test packages went down. **Fixed by bumping protobuf to `v1.36.6`**, which removed the
   `purego`/`appengine` build tags upstream. See §6.
2. **The same tag disables the Go standard library's crypto assembly.** Correctness is
   unaffected; throughput is not. Measured on this machine: AES-GCM **2277 → 77 MB/s**
   (29× slower), SHA-256 **2001 → 297 MB/s** (6.7× slower). Still far above what a voice
   client needs, but it is a real cost nobody asked for. See §6.2 for the clean fix if it
   ever matters.
3. **gohook's `Event.Keycode` is *not* one cross-platform `VC_*` key space.** The spike's
   §3.9 hoped three keymap files would collapse into one; reading the beta's source shows
   that field is unreliable on macOS *and* Windows, not just Linux. The tables therefore key
   on the **OS's own constants**. Details and evidence in §3.

Smaller but worth stating: the `registrar_nocgo.go` shim is genuinely gone (§7), the Linux
cross-compile is still blocked but the blocker is entirely Wails (§7.2), and one comment in
`permission_darwin.go` is now stale and I deliberately did **not** fix it (§9).

---

## 1. Change A — log key events

`internal/app/settings.go`, `Pressed` / `Released`:

```go
a.logger.Info("hotkey fired", "action", actionID, "edge", "down")   // "up" in Released
```

Placed after the existing `a.settings == nil` guard, so an edge that arrives before
`SetSettingsBackend` has run stays a silent no-op rather than a nil dereference — which
matters more now than it did, because the OS stream is process-global and outlives any
rebind.

Info, as instructed. The record names **the action ID and the edge, and nothing else**. That
is now a privacy boundary rather than a style preference: after Change B this process sees
every keystroke on the machine, so an action ID ("push-to-talk fired") is the most that may
reach a log file that outlives the session.

Two tests (`internal/app/settings_test.go`):

- `TestHotkeyEdgesAreLogged` — two lines, correct action, correct edge, `level=INFO`.
- `TestHotkeyEdgesWithoutBackendDoNotPanic` — no backend wired, nothing logged, no panic.

**Load-bearing check:** deleting the `Released` log line fails the first test with
`got 1 log lines, want 2`. Restored, passes.

---

## 2. Change B — what replaced what

| Deleted | Added |
| --- | --- |
| `registrar_x.go` (113 lines, `golang.design/x/hotkey`) | `registrar_gohook.go` (346) — Registrar + stream lifecycle + event translation |
| `registrar_nocgo.go` (49) | — (no longer needed, §7) |
| `keymap_darwin.go` / `_windows.go` / `_linux.go` (359) | `keymap.go` (295) — all three tables, no build tags |
| — | `dispatch.go` (174) — chord matching, debounce, press/release latch |
| — | `session.go` + `session_linux.go` + `session_other.go` (98) — Wayland/X11 pre-flight |

Unchanged: `hotkeys.go` (`Manager`, `Registrar`, `Binding`, `Handler`), `internal/chord`
(bar one added accessor, §3.4), `internal/keybinds`, `permission_darwin.go`, every DTO,
the whole frontend, `main.go` (`NewOSRegistrar()` kept its signature).

---

## 3. Keycode mapping — and why the spike's "one table" does not survive

### 3.1 What I found reading `gohook@v1.0.0-beta1`

The spike recommended matching on `Event.Keycode` as a single `VC_*` space, with one caveat
about the Linux backend (R4). The caveat is larger than that. **Each of the three purego
backends puts the stable physical key code in a different field, and the "translated" field
is wrong on two of them:**

| backend | `Event.Rawcode` | `Event.Keycode` |
| --- | --- | --- |
| darwin (`darwin.go:418`, `:461-479`) | raw Quartz keycode from `CGEventGetIntegerValueField` — **authoritative** | `Keycode[rawToKeyDarwin[raw]]`, **falling back to `raw` on a miss** |
| windows (`windows.go:352-392`, `:684`) | Win32 VK from `KBDLLHOOKSTRUCT` — **authoritative** | `winVKToKeycode[vk]`, a generated `VC_*` table with errors |
| linux/X11 (`x11.go:352-368`) | X **keysym** under the event's modifier state — layout-dependent, unusable | evdev code (X keycode − 8) — **authoritative** |

Concrete defects in the `Keycode` path:

- **macOS `Keycode` collides with itself.** `vcaesar/keycode`'s map has no entry for
  Backspace, Insert, Home, End, PageUp, PageDown, F13+, or most of the numpad, and
  `makeKeyEvent` falls back to the raw Quartz code. Backspace's raw code is **51**, which is
  the same number as `VC_COMMA` (`0x33`). A `Backspace` binding would fire on comma.
- **Windows `winVKToKeycode` nibble-swaps the navigation cluster.** PageUp maps to `0x0E49`
  where `VC_PAGE_UP` is `0xE049`; the same for PageDown, Home, End, Insert, Delete and
  numpad divide. It also has no entry at all for numpad Enter.
- **Linux `Rawcode` is a keysym**, so it changes with the keyboard layout and with Shift.

This is the concrete form of gohook issue #41 ("doesn't stick to any key codes standard").

### 3.2 What I did instead

`eventKeyCode(keycode, rawcode uint16) keyCode` picks `Keycode` on Linux and `Rawcode`
everywhere else, and three tables map canonical `chord` key names to the **operating
system's own constants**: Quartz `kVK_*`, Win32 `VK_*`, Linux evdev `KEY_*`. Those are
ABI-stable in a way a beta library's hand-maintained translation table is not, and gohook
passes them through untranslated.

So the three keymap files did not become one table — but they did become **one file with no
build tags**, which buys something the old arrangement could not: **all three tables are
compiled into every binary and exercised by the tests on every platform.** A typo in the
Windows table is now caught by a CI run on Linux. That is worth the few kilobytes.

### 3.3 The bindable key set still widens

The old `keymap_darwin.go` rejected punctuation, Insert/Home/End/PageUp/PageDown, the whole
numpad and F21–F24. The new tables cover the entire canonical set of 102 keys, with exactly
two documented exceptions:

| platform | key | why |
| --- | --- | --- |
| darwin | `F21`–`F24` | macOS defines no `kVK_` constant; the window server has no code to report |
| windows | `NumpadEnter` | Win32 reports it as `VK_RETURN` + `LLKHF_EXTENDED`, and gohook's `keyboardProc` does not carry the extended flag onto `hook.Event` — there is nowhere for it to live |

I deliberately did **not** alias Windows `NumpadEnter` to `VK_RETURN`: that would make a
NumpadEnter binding fire on the main Enter key, which is worse than not binding it. Both
exceptions fail per action through `Manager.Failed()`, so the UI can name the binding that
did not take.

Inherent to Win32 rather than to us: with NumLock **off**, the numpad digits report the
navigation VKs (`Numpad7` arrives as `VK_HOME`). Every Windows application behaves this way
and this layer cannot correct it.

### 3.4 One addition to `internal/chord`

`chord.Keys() []string` returns the canonical key set, sorted. It exists so the coverage test
asserts against `chord`'s real set rather than a second hand-copied list that would drift.
No behaviour change; 24 lines.

---

## 4. Stream lifecycle — the decision and why

**Decision: one process-global stream, started lazily on the first `Register`, kept alive
across `UnregisterAll` / `Suspend` / `Resume` / every `Apply`. Restarted only when it has
reported itself dead, and then at most once per registration cycle.**

### 4.1 Why not tear it down on `UnregisterAll`

`x/hotkey`'s shape was N OS registrations for N hotkeys, so `UnregisterAll` genuinely meant
"tell the OS to forget them". gohook's shape is **one stream and N chords we match in Go**.
Those are different objects and conflating them is a bug:

- `Manager` calls `UnregisterAll()` immediately before re-registering on *every* rebind, so
  tearing the stream down there means a stop/start cycle per keystroke typed into the
  capture UI.
- gohook's `End()` sets `asyncon = false`, **sleeps** (10 ms default), drains and **closes**
  a package-global channel that `Start()` then reallocates (`darwin.go:248-278`). Restarting
  it is both slow and racy; doing it on a hot path is asking for the race to land.

So `UnregisterAll` empties the chord table and nothing more. Suspension is the empty table:
the stream keeps running and matches nothing.

### 4.2 Why restart at all

gohook's loop goroutine **returns for good** after emitting `HookDisabled` and never
recovers. On macOS that is exactly the "launched VCS before granting Accessibility" case: the
user grants it, window focus drives `RecheckHotkeyPermission` → `Apply`, and without a restart
hotkeys would stay dead until the app is relaunched.

So: when the reader has seen `HookDisabled`, a restart is **armed by `UnregisterAll` and
consumed by the restart**. One `Apply` can therefore trigger at most one restart no matter how
many bindings it registers — otherwise nineteen bindings against a denied permission would
mean nineteen `End`/`Start` cycles and, with `End()`'s sleep, seconds of stalled startup.

### 4.3 Why it cannot leak or double-start

- `ensureStream` holds `lifeMu` for the whole start/restart decision, so two starts cannot
  interleave.
- A restart calls `src.End()` and then **blocks on the old reader's `done` channel** before
  starting a new one. There is never more than one reader alive.
- `read`'s ordering is load-bearing: `defer close(done)` runs *after* the final
  `r.disabled.Store(true)`, so `<-done` returning guarantees the old reader's last write
  happened before `ensureStream` stores `false`. `disabled` is an `atomic.Bool` precisely so
  the reader can set it while `ensureStream` holds `lifeMu`.
- `lifeMu` is never held while calling into the dispatcher or a `Handler`.

Asserted by `TestStreamStartsOnceAcrossApplyCycles` (20 Apply cycles → `starts == 1`,
`ends == 0`), `TestSuspendResumeCyclesDoNotLeakOrDoubleStart` (50 cycles), and
`TestRestartEndsTheOldReader` (10 restarts, goroutine count returns to baseline).

### 4.4 The gap this leaves

The first `Apply` after a `HookDisabled` **succeeds** (it consumes the armed retry and cannot
yet know the restart failed). The failure surfaces on the *next* cycle. On macOS that window
is covered by `PermissionChecker`, which is a better signal anyway; elsewhere it converges on
the next `Apply`. Documented in the code.

---

## 5. Debounce and the press/release rules

`dispatch.go` owns all of it and is driven entirely by synthetic events in the tests.

**The latch.** Each action has a `latched` flag. On key-down: exact chord match, and if the
action is already latched the event is swallowed. On key-up: unlatch, and fire `Released`
only for `hold` bindings. gohook does not deduplicate a held key (issue #47) — macOS and
Windows re-deliver `KeyDown` while a key is down — so without this a held PTT key would
re-fire `Pressed` dozens of times a second.

**`KeyHold` is dropped entirely.** On X11 it *is* the auto-repeat of a still-held key
(`x11.go:101-102`); on Windows it is a synthetic "typed character" event carrying `Keychar`
and a zero keycode (`windows.go:369-380`). Neither is a transition.

**The two edges match differently, on purpose:**

- **Down requires an exact chord match**, key *and* modifier set. `Ctrl+E` must not fire on
  bare E, and bare `E` must not fire when the user types Ctrl+E at something else.
- **Up matches on the physical key alone**, among *latched* actions only. A user who releases
  Ctrl before releasing E produces a key-up with **no modifiers**; requiring an exact match
  there would drop the release and strand push-to-talk open. The latch set is what makes this
  safe — only a key we latched ourselves can be released by it.

**Lock keys are masked out.** CapsLock (bit 14), NumLock (13), ScrollLock (15) and the mouse
button bits (8–12) do not participate, or every binding would stop working the moment a user
left CapsLock on. Left/right modifier bits are matched as a pair, because the darwin and X11
backends only ever set the *left* bit while Windows sets the correct side.

**Press-only bindings are debounced too**, they just get no `Released`. Otherwise a held
mute-toggle key would toggle dozens of times a second.

**`UnregisterAll` releases what is held.** A rebind can land mid-transmission, and a user can
open the keybind UI while holding PTT. Clearing the table without releasing would leave the
app believing PTT is down with no event able to close it. Press-only bindings get no invented
release.

**Deterministic ordering.** Go map iteration is randomised; handler calls are sorted by
action ID so one key bound to several actions behaves the same every run.

**Known residual:** re-registering while a key is physically held drops the latch, so a
subsequent auto-repeat `KeyDown` fires `Pressed` again. It is consistent (the release fired
first), platform-dependent, and documented.

---

## 6. `-tags purego` — where it had to go, and what else it touched

### 6.1 Every build path covered

| path | what changed |
| --- | --- |
| `Taskfile.yml` | 26-line header explaining the tag, the fallback and why `-tags wayland` must never be set; new `task test` running `go vet`/`go test -race` with the tag |
| `build/darwin/Taskfile.yml` | `BUILD_FLAGS` — `purego` hardcoded in **both** the DEV and production branches |
| `build/linux/Taskfile.yml` | same |
| `build/windows/Taskfile.yml` | same |
| all three, `build:docker` | `-e EXTRA_TAGS="purego,…"` now always passed to the cross-compile image (it was conditional on the user supplying `EXTRA_TAGS`) |
| `build/Taskfile.yml` → `generate:bindings` | `-f '{{.BUILD_FLAGS \| default "-tags purego"}}'` — `wails3 generate bindings` type-checks the Go packages |
| `build/Taskfile.yml` → `build:server` | `-tags server,purego` |
| `.github/workflows/test.yml` | `go vet -tags purego`, `go test -tags purego -race`, `go build -tags purego`, and `wails3 generate bindings -f "-tags purego"` in the frontend job |
| `.github/workflows/release.yml` | `wails3 generate bindings -f "-tags purego"`; `wails3 task build` routes through the platform Taskfiles, which now carry it unconditionally |
| `build/config.yml` (`wails3 build DEV=true`) | no edit needed — routes through the Taskfiles |

The DEV branch previously emitted `-tags` *only* if `EXTRA_TAGS` or `OBFUSCATED` was set; it
now always emits it. `EXTRA_TAGS` still works and is appended.

`wails3 generate bindings -ts -f "-tags purego" -clean=true` was run locally and succeeded
(372 packages, 34 methods, 37 models).

### 6.2 The tag is not namespaced — two side effects

**(a) protobuf — a hard runtime panic.** `google.golang.org/protobuf@v1.34.2`
`internal/impl/pointer_reflect.go` is `//go:build purego || appengine`, and its
`Export.MessageStateOf` is `panic("not supported")`. With `-tags purego` the first
`proto.Marshal` on any gRPC call panics. This took down `internal/auth`,
`internal/control` and `internal/session`.

Upstream removed `purego`/`appengine` support in **v1.36.0**; I bumped to **v1.36.6**, which
is the protoc-gen-go version that generated the local `srspb/` anyway (`buf.gen.yaml` pins
the remote plugin to v1.34.2, and a newer runtime than generator is always fine). All tests
pass with **and without** the tag afterwards.

**This is the single most important thing to know about this change.** It would have shipped
as "gRPC panics in production, works in unit tests that don't hit the wire".

**(b) the Go standard library — crypto assembly disabled.** `crypto/internal/fips140/*`,
`crypto/md5`, `crypto/sha1` and the vendored `x/crypto` AEADs all use `purego` to select
pure-Go implementations. Correct, just slower. Measured here (darwin/arm64, 16 KB blocks):

| | without tag | with `-tags purego` | factor |
| --- | --- | --- | --- |
| AES-GCM seal | 2276.7 MB/s | 77.3 MB/s | **29× slower** |
| SHA-256 | 2000.7 MB/s | 296.8 MB/s | **6.7× slower** |

77 MB/s is ~600 Mbit/s, so this is not a functional problem for a voice client's TLS control
channel or its audio. It is a 29× per-byte CPU cost nobody intended, and it is invisible
unless someone measures.

**The clean fix, if it ever matters:** fork gohook (or vendor its three purego backends,
~2,216 lines of pure Go, MIT/Apache-2.0) under a project-private build tag such as
`vcs_purego`. That decouples our hotkey backend selection from the global `purego` tag
entirely. I did **not** do it — it means owning a fork, and it is well outside this change's
scope. Flagging it as the recommended follow-up if the crypto cost is ever judged
unacceptable.

### 6.3 CGo fallback — still a one-line change, verified

Nothing in our own code is tagged; gohook's own `//go:build` lines do the selection. Dropping
`purego` from the `BUILD_FLAGS` in the three platform Taskfiles (and the workflows) selects
the CGo/libuiohook backend. Verified on darwin: `go build ./internal/hotkeys/` with no tag
compiles, and the **entire test suite passes without the tag**.

Costs of doing so, documented in the `Taskfile.yml` header: an *active* macOS tap sitting
synchronously in the system input path, a 50 ms default poll on press *and* release, a
`log.Fatal` on a malformed event, six extra apt packages on Linux
(`libxtst-dev libx11-xcb-dev libxcb1-dev libxcb-xkb-dev libxkbcommon-dev
libxkbcommon-x11-dev`), and gohook issue #67 (a hard C23 build break on GCC 14+). It would
also *undo* the protobuf and crypto side effects of §6.2.

`-tags wayland` must never be set: gohook's CGo constraint starts with `!wayland`, so it
removes the backend on **every** OS, and its Wayland backend only sees keys while *our*
window has focus — the exact inverse of push-to-talk.

---

## 7. `registrar_nocgo.go` and the Linux cross-compile

### 7.1 The shim is gone — the purego backends really are cgo-free

Verified by building `internal/hotkeys` with `CGO_ENABLED=0 -tags purego`:

| target | result |
| --- | --- |
| linux/amd64 | ✅ |
| windows/amd64 | ✅ |
| darwin/arm64 | ✅ |
| darwin/amd64 | ✅ |

darwin included, because `permission_darwin.go` is tagged `darwin && cgo` and
`permission_other.go` picks up the no-cgo case. So the package is **not** cgo-bound anywhere;
the only cgo left is the Accessibility *permission probe*, and it is optional at compile time.

`registrar_nocgo.go` existed solely because `x/hotkey` has no usable no-cgo Linux backend.
Deleted, along with `ErrBackendUnavailable`'s old meaning — the name survives with a wider,
honest one ("the OS event stream cannot run here"), covering Wayland, no X display, and a
`HookDisabled` stream.

In practice a darwin build of this app always has cgo enabled (Wails v3 requires it), so the
no-cgo darwin arm is unreachable for a real build. I updated `permission_other.go`'s comment
to say that instead of its previous, now-false claim that a no-cgo darwin build has no hotkey
backend at all.

### 7.2 Linux cross-compile: still blocked, but not by us

```
GOOS=linux CGO_ENABLED=0 go build -tags purego .
# github.com/wailsapp/wails/v3/pkg/application
menu_linux.go:7:12: undefined: pointer
webview_window_linux.go:31:16: undefined: pointer
...
```

Entirely inside Wails v3's own `pkg/application`. `internal/hotkeys` cross-compiles cleanly to
linux/amd64; it is no longer part of the problem. Unchanged by this work, reported for
accuracy.

---

## 8. Wayland — what a user actually experiences

**Before:** nothing. gohook's X11 backend connects happily to **XWayland**, `XRecordQueryVersion`
succeeds, `HookEnabled` is reported, and then no keystroke from any native Wayland client is
ever seen. That looks perfect in development (where the VCS window has focus) and is dead in
real use — the worst possible failure mode, and the exact pattern that cost this project a
review cycle before.

**Now:** `sessionSupported()` runs as a synchronous pre-flight *before* the stream starts, so
the answer lands on the very first `Apply`. On Linux it claims a Wayland session when
`XDG_SESSION_TYPE=wayland` **or** `WAYLAND_DISPLAY` is set at all (that second clause is the
XWayland trap: `DISPLAY` is also set and everything looks healthy). Every `Register` then
fails with:

> global hotkeys are not available in a Wayland session, because Wayland provides no global
> key-listening API. Log in to an X11/Xorg session to use them

Because *every* binding fails, `Manager.Registered()` goes **false** — which is precisely the
one case that banner was written to describe — and `hotkeyStateDTO` puts `LastError` into the
DTO, so the existing `hotkeys:state` surface shows that sentence verbatim. No new UI, no new
event, no new state.

A headless or plain-SSH session (no `DISPLAY`) gets `ErrNoDisplay` by the same route.

This is not a gohook deficiency and no library fixes it: Wayland has no global key-listening
primitive by design. OBS's Wayland hotkey backend is a deliberate stub; Discord is
unsupported there; Mumble needs evdev. Shipping "X11 only, clearly signposted" is the state
of the art. The `xdg-desktop-portal` GlobalShortcuts portal does not help — every backend
implements it as a compositor grab, so the game would not get the key either.

`linuxSessionCheck` takes its three env values as parameters so it is tested from **any**
host, not only under CI's Linux runner.

---

## 9. Files I was told not to touch, and what I did

`internal/hotkeys/permission_darwin.go` is **byte-for-byte unchanged**. Accessibility,
`AXIsProcessTrusted`, `requestAXTrust`, the deep link, all of it. The spike is right that the
predicate is identical to what gohook's purego backend gates on (`axIsProcessTrusted()`,
`darwin.go:304`), so the whole permission flow stays correct with no edit at all.

**⚠️ One stale line I deliberately left:** `permission_darwin.go:11` still says a no-cgo darwin
build "has no working hotkey backend at all -- registrar_nocgo.go (!windows && !cgo) claims
that build". `registrar_nocgo.go` no longer exists and the claim is no longer true (§7.1).
Fixing it is comment-only and zero-risk, but the instruction was "stays exactly as it is", so
I stopped and am reporting it instead. **Recommend a one-line follow-up.**

I *did* make comment-only edits to two other permission-layer files, because they named files
I deleted and would otherwise document falsehoods:

- `permission_other.go` — referenced `registrar_nocgo.go` and `registrar_x.go`, and asserted
  that a no-cgo darwin build has no hotkey backend. Rewritten to the current truth. No code
  change.
- `permission.go` — the `Permission` doc named `golang.design/x/hotkey` (now removed from
  `go.mod`) and `RegisterHotKey`/`XGrabKey` as the mechanisms. Updated to gohook /
  `WH_KEYBOARD_LL` / XRecord, **keeping the conclusion and the warning verbatim**, plus a new
  sentence noting that the predicate itself did not change. No code change.

Flagging both so a reviewer can veto them.

---

## 10. Privacy

The stream carries **every keystroke on the machine**, in every application, including
passwords typed into other apps, plus mouse movement. gohook subscribes to the whole input
mask and offers no way to narrow it.

- A 20-line boxed comment at the top of `registrar_gohook.go` states the rule: **never log a
  key identity here — not `Keychar`, not `Keycode`, not `Rawcode`, not at Debug, not behind a
  flag** — and says why (the log file outlives the session and is not encrypted).
- `read()` and `toKeyEvent()` carry the same warning at the point of use.
- `keyEvent` is a three-field struct (edge, physical code, modifier set). `Keychar` — the
  actual character the user typed — is discarded at the conversion boundary and has no field
  to travel in.
- Neither `dispatch.go` nor `registrar_gohook.go` contains a single logging call.
- The only key-ish text in any error is the user's own chord string (`"Ctrl+F1"`), which comes
  from their config file, not from the stream, and is required for `Manager.Failed()` to name
  the binding that did not register.
- Change A's log line names the action, never the key (§1).

---

## 11. Testing

### 11.1 The seam

`hotkeys_test.go` (328 lines, fake `Registrar`) is **unchanged and passing** — the seam
proving its worth exactly as predicted.

### 11.2 The new, narrower seam

`Registrar` is not narrow enough to test this change: chord matching, debounce, the latch and
the stream lifecycle all live *below* it and would be untested if the only fake were a fake
`Registrar`. So `registrar_gohook.go` adds an unexported `eventSource`:

```go
type eventSource interface {
    Start() <-chan hook.Event
    End()
}
```

It splits off the one genuinely untestable thing (gohook talking to the window server) and
leaves everything else driven by **real `hook.Event` values through the real conversion, the
real matcher and the real lifecycle code**.

`fakeSource`'s channel is **unbuffered**, which is what makes every test deterministic rather
than timing-dependent: a send returns only once the reader has received it, so sending one
more event proves the previous one was fully handled. `flush()` is exactly that.

### 11.3 What is covered

`registrar_gohook_test.go` (744 lines) — auto-repeat debounce (25 downs → one `Pressed`),
repeatability across cycles, stray key-up, `KeyHold` ignored in both directions, press-only
debounce, registered-vs-unregistered from the same stream, exact modifier match on press
(both directions plus superset), left/right modifier equivalence, lock keys ignored, release
ignoring modifiers, release only affecting latched actions, stream-starts-once across 20
Apply cycles, 50 Suspend/Resume cycles, nothing delivered while suspended, nothing replayed
on resume, held hotkey released by `UnregisterAll`, press-only *not* released,
`HookDisabled` → registration failure, one restart armed per cycle, restart recovering the
stream, restart ending the old reader, unmappable key as a per-action failure, event
translation, mask translation, per-platform field selection.

`keymap_test.go` (190) — canonical-set coverage with an explicit documented exception list,
no stray non-canonical entries, no duplicate codes, 44 hand-checked literal values against the
OS headers, `osKeyCode` rejecting unknowns.

`session_test.go` (86) — seven session shapes including the XWayland trap, and that the
message is actionable.

`goleak` is not a dependency of this module, so goroutine assertions use
`runtime.NumGoroutine()` with a polling deadline plus `startCount()`/`endCount()` on the fake
source — the observable-state version of the same assertion.

### 11.4 Load-bearing verification

Every one of these was applied, the suite run, then reverted:

| # | break | caught by |
| --- | --- | --- |
| 1 | remove the auto-repeat latch check | `TestAutoRepeatFiresPressedOnce`, `…CycleIsRepeatable`, `TestPressOnlyBindingIsAlsoDebounced` |
| 2 | require exact modifiers on key-**up** | `TestReleaseIgnoresModifiers` |
| 3 | ignore modifiers on key-**down** | `TestModifierMatchIsExactOnPress`, `TestReleaseOnlyAffectsLatchedActions` |
| 4 | tear the stream down in `UnregisterAll` | 5 tests incl. `TestStreamStartsOnceAcrossApplyCycles` |
| 5 | `clear()` stops releasing what is latched | `TestSuspendReleasesAHeldHotkey` |
| 6 | `eventKeyCode` picks the wrong field | 12 tests |
| 7 | keymap typo: Windows `Numpad7` duplicates `Numpad8` | `TestKeyTablesHaveNoDuplicateCodes` |
| 8 | Wayland detection removed | `TestLinuxSessionCheck` |
| 9 | wrong non-duplicate value (darwin `F1` = `0x7B`) | `TestKeyTableSpotChecks` **and** `…NoDuplicateCodes` |
| 10 | Linux table silently loses `Insert` | `TestKeyTablesCoverTheCanonicalSet` |
| 11 | `KeyHold` treated as a key-down | `TestToKeyEventOnlyConvertsTransitions` |
| — | Change A: drop the `Released` log line | `TestHotkeyEdgesAreLogged` |

**One test was not load-bearing and I fixed it.** The first version of
`TestKeyHoldEventsAreIgnored` sent `KeyHold` events *after* a `KeyDown` — which the
auto-repeat latch swallows anyway, so it passed even with `KeyHold` treated as a transition.
Rewritten to send `KeyHold` with no preceding down (must fire nothing) and to assert a
`KeyHold` does not clear the latch. Now fails under **both** breakages — `KeyHold`-as-down
(`calls = [ptt:down], want []`) and `KeyHold`-as-up (`calls = [ptt:down ptt:up], want
[ptt:down]`) — and the reason it is written that way is in the test's comment.

### 11.5 Full output

```
$ gofmt -l .                     # (excluding node_modules)
(empty)

$ go vet -tags purego ./...
(clean, exit 0)

$ go test -tags purego -race ./...
?   github.com/FPGSchiba/vcs-srs-client            [no test files]
ok  github.com/FPGSchiba/vcs-srs-client/internal/app          6.783s
ok  github.com/FPGSchiba/vcs-srs-client/internal/auth         1.502s
ok  github.com/FPGSchiba/vcs-srs-client/internal/chord        1.387s
ok  github.com/FPGSchiba/vcs-srs-client/internal/config       1.816s
ok  github.com/FPGSchiba/vcs-srs-client/internal/control      3.015s
ok  github.com/FPGSchiba/vcs-srs-client/internal/events       2.112s
?   github.com/FPGSchiba/vcs-srs-client/internal/grpctest     [no test files]
ok  github.com/FPGSchiba/vcs-srs-client/internal/hotkeys      1.360s
ok  github.com/FPGSchiba/vcs-srs-client/internal/keybinds     2.760s
ok  github.com/FPGSchiba/vcs-srs-client/internal/session      3.827s
ok  github.com/FPGSchiba/vcs-srs-client/internal/state        2.633s
?   github.com/FPGSchiba/vcs-srs-client/internal/version      [no test files]
ok  github.com/FPGSchiba/vcs-srs-client/internal/windowstate  4.185s
ok  github.com/FPGSchiba/vcs-srs-client/pkg/logger            4.517s
?   github.com/FPGSchiba/vcs-srs-client/srspb                 [no test files]

286 passing Go tests across 12 packages (49 in internal/hotkeys).
Coverage: internal/hotkeys 89.3%, internal/chord 84.5%.

$ go test ./...                  # no tag -- the CGo fallback
all 12 packages ok

$ cd frontend && npx tsc --noEmit
(clean, exit 0)

$ npx vitest run
Test Files  10 passed (10)
     Tests  57 passed (57)

$ npm run build
✓ built in 390ms   (dist/main.html, dist/comms.html, 3 JS chunks, 1 CSS)

$ go build -tags purego -ldflags="-w -s" -o $TMPDIR/vcs-client .
15,718,626 bytes   # full binary links with the production ldflags
```

---

## 12. What I could not verify here

**Everything about actual runtime behaviour.** There is no GUI in this environment and no
second machine. Unverified, all of it Phase-0 work from the spike that still stands:

1. **Pass-through.** The whole point of the change. Verified by reading source only —
   `kCGEventTapOptionListenOnly`, unconditional `CallNextHookEx`, XRecord's protocol-level
   passivity. **Hold a bound key in a text editor and confirm the character still arrives**,
   then do it in-game.
2. **macOS Accessibility on a clean account.** That Accessibility *alone* lets a ListenOnly
   tap receive key events is a community claim Apple has never confirmed (spike §3.7.1,
   §8.1). Expected failure mode is "we over-demand a permission", not "granted but nothing
   flows" — but confirm on a fresh account.
3. **The macOS restart path.** `TestRestartRecoversTheStream` proves *our* lifecycle logic;
   it does not prove gohook's `End()` + `Start()` actually re-arms a real CGEventTap after
   `HookDisabled`. **Test by launching without Accessibility, granting it, and returning
   focus.** This is the riskiest untested path in the change.
4. **Key-up for modifier-containing chords on macOS**, which the backend *synthesises* from
   `kCGEventFlagsChanged`.
5. **Every keycode table value on real hardware.** 44 are hand-checked against the OS
   headers; the rest are structurally validated only. Numpad-with-NumLock-off on Windows and
   the macOS Insert/Help key are the likeliest surprises.
6. **Windows `LowLevelHooksTimeout` (spike R14).** A slow hook is *silently removed* on
   Win7+ with no way for the app to know. gohook's purego backend does a non-blocking
   drop-on-full send so it should never be slow, but there is no watchdog and **no test can
   produce this**. Spike Phase 2.
7. **Windows anti-cheat.** Whether Star Citizen's anti-cheat objects to `WH_KEYBOARD_LL` is
   the single highest-consequence unknown and is a potential hard blocker. Unchanged by this
   work; still needs a real machine.
8. **Fullscreen X11 games taking exclusive keyboard grabs (spike R16).** XRecord makes no
   promise about surviving another client's grab; only XI 2.1+ raw events do. If this fails,
   the `Registrar` seam makes a Linux-only swap cheap.
9. **XRecord under XWayland with a Proton-hosted game** (spike §8.2) — moot now that we
   refuse Wayland sessions outright, but it is the thing that would justify relaxing that.
10. **Latency.** Expected sub-millisecond on the event-driven purego backends vs up to 50 ms
    on the CGo poll. Not measured.
11. **`task` / `wails3 task build` end to end.** `task` is not installed here, so the
    Taskfile edits are validated as YAML and by inspection, and `wails3 generate bindings -f`
    was run directly. **The first CI run on this PR is the real check** — especially the
    Linux job, which is the one that would previously have needed six more apt packages.
12. **The cross-compile Docker image** (`wails-cross`) consumes `EXTRA_TAGS`; I could not run
    it. The env var is now always set, but whether that image forwards it correctly is
    unverified.

### Minor items noted, not acted on

- `build:server` now passes `-tags server,purego`, but the task also interpolates
  `{{.BUILD_FLAGS}}`, which can contain a second `-tags` that wins. Pre-existing scaffold
  weirdness; there is no `server`-tagged file in the repo at all, so it is inert.
- `github.com/vcaesar/go-wayland` arrives as an indirect dependency (gohook's `-tags wayland`
  backend) and is in `go.sum`, but is **never compiled** — we never set that tag. It is the
  least-proven thing in the dependency graph, so worth knowing it is present but dead.
- gohook `v1.0.0-beta1` is a **beta** with a single, bursty maintainer. Pinned exactly, with
  the reason inline in `go.mod`.
