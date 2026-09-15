# VCS Client — Phase 3: Settings + Keybinds

**Status:** Approved (brainstorm phase)
**Date:** 2026-09-15
**Author:** Jann Erhardt (with Claude Code)
**Project:** `github.com/FPGSchiba/vcs-srs-client`
**Parent spec:** [`2026-05-31-vcs-client-design.md`](./2026-05-31-vcs-client-design.md)
**Roadmap row:** [`docs/ROADMAP.md`](../../ROADMAP.md) → Phase 3

---

## 1. Overview

Phase 3 delivers the Settings screen, a persisted keybind system, real OS-level
global hotkey registration, and a system tray. It is picked up ahead of Phase 2
(plugin SSO), which is deferred — see the roadmap for why.

Nothing in this phase makes a *sound*. Audio is Phase 4 and voice is Phase 5, so
a registered PTT hotkey fires an event that only a debug surface consumes. That
is deliberate: registering hotkeys now surfaces the OS permission model and the
key-release question while nothing depends on the answers, mirroring the parent
spec's R3 reasoning for malgo ("add a CI job one phase early to surface toolchain
issues").

## 2. Sources of truth

| Source | Path | Authoritative for |
|---|---|---|
| Design package | `design/vcs/project/screens/settings.jsx` | Section list, row layout, keybind table structure |
| Keybind chip styles | `design/vcs/project/styles.css` §"Keybind chip" (`.kbd`, `.kbd.listening`, `.kbd-row`) | Capture chip appearance |
| Parent spec | `docs/superpowers/specs/2026-05-31-vcs-client-design.md` | Architecture, state-sync rules, event conventions |

## 3. Decisions (locked)

| Decision | Value |
|---|---|
| Settings location | In-window screen in the main window (nav rail already has a `settings` key), **not** a popout |
| Section coverage | All 8 nav entries render. **General** and **Keybinds** are functional; Audio, Effects, Profiles, Notifications, Misc, Legacy render an honest "arrives in Phase N" stub |
| Hotkey depth | Real OS registration this phase, via `golang.design/x/hotkey` |
| System tray | Built this phase, using the Wails v3 native `SystemTray` API |
| Conflict policy | **Steal and report** — new binding wins, previous owner is unbound, UI names what lost it |
| Key capture source | `KeyboardEvent.code` (physical key), **not** `.key` |
| Chord canonical form | `Ctrl+Alt+Shift+Super+<Key>`, modifiers always in that order |
| Persistence | **A single `config.toml`** — `[general]` for settings, `[keybinds]` for bindings. Written immediately on change |
| Prerequisite | Wails v3 `alpha.96` → `beta.22` upgrade lands **before** any Phase 3 code (§10) |

### Why `e.code` and not `e.key`

The design prototype's `KeyChip` reads `e.key`, which is keyboard-layout
dependent — `Alt+1` yields different values across layouts, and dead keys and
non-US layouts diverge further. OS-level hotkey registration matches *physical*
keys. Capturing `e.code` (`Digit1` → `1`, `KeyD` → `D`) keeps what the user sees
in the UI aligned with what the OS actually registers. Identical behaviour on a
US layout; correct everywhere else. No visual change — capture internals only.

## 4. Package layout

```
internal/
  chord/                pure value type — NO external dependencies
    chord.go              Chord{Mods, Key}, Parse, String (canonical form)
    keycode.go            KeyboardEvent.code → chord.Key table
  keybinds/             depends on chord — pure in-memory, NO file access
    actions.go            canonical action registry
    store.go              action→Chord map, Set/Clear, conflict resolution, change fan-out
                          Snapshot() map[string]string for the config layer
  hotkeys/              depends on chord; owns golang.design/x/hotkey
    hotkeys.go            Manager.Apply(map[ActionID]chord.Chord), Suspend/Resume
    keymap.go             chord.Key → hotkey.Key constants
    registrar.go          interface seam so tests never touch the OS
  config/               existing, extended
    config.go             + [general] and [keybinds] tables
  app/                  existing, extended
    bindings.go           + settings & keybind bindings
    tray.go               tray construction + window lifecycle rules
```

Three boundaries are deliberate and should not be collapsed:

**`chord` has no external dependencies.** The parser and canonical-string logic
are where bugs will concentrate, and keeping them free of `x/hotkey` means they
are testable on any platform with no OS involvement. Translation to
`hotkey.Key` lives in `hotkeys/keymap.go`, next to its only consumer.

**`hotkeys` does not know `keybinds` exists.** It takes
`Apply(map[ActionID]chord.Chord)` and registers what it is handed.
`internal/app` subscribes to keybind changes and calls `Apply`. This keeps the
registrar testable with a literal map and leaves `keybinds` with no OS-side
dependency at all.

**`keybinds` does not touch the filesystem.** It is a pure in-memory domain
package. `internal/app` reads `Store.Snapshot()`, writes it into
`config.Config.Keybinds`, and calls the existing `config.Save`. So `keybinds`
imports neither `config` nor `os`, and is testable with no temp directories.

## 5. Data model

### 5.1 Action registry

Actions are a static registry, not free-form strings:

```go
type Kind uint8      // Hold | Press
type Category uint8  // Global | Channel | PerRadio | Status

type Action struct {
    ID       ActionID  // "global.ptt", "radio.1.ptt"
    Label    string    // "Global PTT"
    Desc     string
    Category Category
    Kind     Kind
}
```

`Kind` is load-bearing. **PTT and push-to-mute are `Hold`** and need press *and*
release; everything else is `Press`. This is what makes the `x/hotkey`
release-event question a blocking unknown (R11).

Registry contents, from `settings.jsx`:

| Category | Actions | Kind |
|---|---|---|
| Global | `global.ptt`, `global.push_to_mute`, `global.mute_toggle`, `global.emergency_broadcast`, `global.compact_overlay` | first two `Hold`, rest `Press` |
| Channel | `channel.intercom`, `channel.role`, `channel.ship`, `channel.fleet` | `Press` |
| PerRadio | `radio.<id>.ptt`, `radio.<id>.select` | `ptt` = `Hold`, `select` = `Press` |
| Status | `status.available`, `status.combat`, `status.discipline`, `status.afk` | `Press` |

**Per-radio actions are dynamic**, derived from radio IDs currently in
`state.Store`. Bindings for radios the server is not currently sending are
**kept in the file but not registered** — a server that temporarily omits a
radio must not silently delete the user's bindings.

### 5.2 Chord

Canonical string: modifiers in fixed order `Ctrl`, `Alt`, `Shift`, `Super`, then
the key — e.g. `Ctrl+Alt+Delete`, `F1`, `Alt+1`. Keys are normalised: letters
uppercase, digits bare, function keys `F1`–`F24`, named keys spelled out
(`Space`, `Escape`, `ArrowUp`). A modifier-only chord is invalid and rejected.

### 5.3 `[keybinds]` in `config.toml`

Bindings live in the **same `config.toml`** as everything else, as one flat
table. Structure comes from the registry, not the file:

```toml
log_level = "INFO"
server_url = ""
ping_interval_seconds = 5

[general]
minimize_to_tray = true
# ...

[keybinds]
"global.ptt" = "F1"
"global.emergency_broadcast" = "Ctrl+E"
"radio.1.ptt" = "F2"
"radio.1.select" = "1"
```

Held on `Config` as `Keybinds map[string]string` and round-tripped whole, so
**unknown IDs from a future version survive a save** rather than being silently
dropped.

**Why one file rather than a separate `keybinds.toml`** (which the parent spec
§4.3 and the roadmap originally specified): the only real argument for splitting
is portability of keybind sets, and profiles/presets are out of scope this phase
(§14) — if they land later they want an export function, not a particular path
on disk. Against splitting: a second loader, a second atomic-save path and a
second path helper, all duplicating `internal/config`. Merging also produces the
cleaner boundary described in §4 — `keybinds` ends up with no file access at all.

**Trade-off, recorded:** a malformed `[keybinds]` table now makes the *whole*
config fail to decode, so one bad entry costs the user their `server_url` and
`log_level` too. `config.Load` returns the error and `main.go` falls back to
in-memory defaults, leaving the file on disk intact and hand-fixable — but the
next settings change overwrites it. Atomic writes rule out partial writes, so
this only triggers on hand-editing. See R16.

### 5.4 `config.toml` — `[general]`

```go
type General struct {
    StartMinimized       bool `toml:"start_minimized"`         // false
    MinimizeToTray       bool `toml:"minimize_to_tray"`        // true
    ShowTransmitterName  bool `toml:"show_transmitter_name"`   // true
    PlayConnectionSounds bool `toml:"play_connection_sounds"`  // true
    RadioSwitchAsPTT     bool `toml:"radio_switch_as_ptt"`     // false
}
```

Defaults match the prototype. Per the existing rule in `config.go`, every new
field gets a default in `Default()` so older config files still load — a
pre-Phase-3 `config.toml` with no `[general]` table must load cleanly.

`ShowTransmitterName`, `PlayConnectionSounds` and `RadioSwitchAsPTT` are stored
and exposed now; their consumers arrive in Phases 4/5. They are settings without
effect yet, which is different from the stub sections — the *control* is real,
only the downstream behaviour is pending.

## 6. Frontend-facing API

```go
func (a *App) GetSettings() SettingsDTO
func (a *App) SetSettings(s SettingsDTO) error

func (a *App) GetKeybinds() []KeybindDTO
func (a *App) SetKeybind(actionID, chord string) (SetKeybindResult, error)
func (a *App) ClearKeybind(actionID string) error
func (a *App) BeginCapture() error
func (a *App) EndCapture() error
```

`GetKeybinds()` returns the **registry joined with current chords** — label,
description, category, kind and bound chord in one shape. The UI needs no
separate "list the actions" call and cannot drift from the registry. Per-radio
rows are generated from the radios currently in `state.Store`.

`SetSettings` takes the whole struct rather than a stringly-typed
`SetSetting(key, value)`. The struct is five booleans and fixed, so whole-struct
writes stay type-safe end to end, and the change event resyncs every window.

```go
type SetKeybindResult struct {
    Stolen *StolenBinding // nil when there was no conflict
}
type StolenBinding struct{ ActionID, Label, Chord string }
```

### 6.1 Events

```
settings:changed   SettingsDTO
keybinds:changed   []KeybindDTO            full list, not a delta
hotkey:pressed     {action_id}
hotkey:released    {action_id}             Hold actions only
hotkeys:state      {registered, error}
```

`keybinds:changed` ships the **whole list**. It is a few dozen entries, and full
replacement eliminates a class of divergence bug — consistent with the parent
spec's R7 ("no optimistic FE-only updates").

`hotkeys:state` exists so a **failed registration is visible rather than
silent**. On macOS permission denial the app starts normally, the Keybinds
section shows a "global hotkeys unavailable" banner, and the UI does not pretend
the bindings are live.

## 7. Capture lifecycle

A registered hotkey will otherwise swallow the keypress meant to rebind it, so
this sequence must be exact:

1. User clicks a KeyChip → `BeginCapture()` → `hotkeys.Suspend()` releases **all**
   OS registrations
2. Frontend captures `keydown`, builds the canonical chord from `e.code`
3. `SetKeybind(actionID, chord)` → parse → store → steal-resolve → persist →
   emit `keybinds:changed`
4. `EndCapture()` → `hotkeys.Apply(current)` re-registers

`EndCapture` must also fire on every cancel path: Escape, click-away, window
blur, component unmount. **Escape cancels capture; it does not bind Escape.**

**Safety net:** if the frontend dies mid-capture, hotkeys would stay suspended
forever — an invisible failure that is maddening to diagnose. `BeginCapture`
arms a 10-second timeout that auto-resumes. A spurious re-arm is harmless; a
stuck-suspended state is not.

## 8. System tray and window lifecycle

Built in `main.go` after the main window, using the Wails v3 `SystemTray` API
(`app.SystemTray.New()`, verified present in beta.22):

```go
tray := wailsApp.SystemTray.New()
tray.SetTemplateIcon(trayIcon)   // macOS: adapts to light/dark menu bar
tray.SetTooltip("Vanguard Communications System")
tray.SetMenu(menu)               // Show VCS · Settings · ─── · Quit
tray.OnClick(toggleMainWindow)
```

**New asset required:** a 22×22 monochrome template tray icon derived from the
VCS radar mark drawn inline in `Welcome.tsx`. `appicon.png` is a 132 KB
full-colour app icon and is wrong for a menu bar.

### 8.1 The macOS termination trap

`main.go` currently sets:

```go
Mac: application.MacOptions{
    ApplicationShouldTerminateAfterLastWindowClosed: true,
}
```

With close-to-tray this must become **false**, or hiding the last window *quits
the app* — the exact opposite of the intent. This flag must be driven by the
`minimize_to_tray` setting rather than left a constant.

### 8.2 Lifecycle rules

- **Close on main window** → hide if `minimize_to_tray`, else quit
- **`start_minimized`** → main window created hidden; tray is the only way in
- **True quit** (tray menu, Cmd+Q) runs `sess.Disconnect` first, so the server
  sees a clean leave instead of a dropped stream
- **Popouts are independent.** Hiding main to tray does *not* close the Comms
  popout. For a voice client, "big window away, radio panel still visible" is a
  feature
- **Toggling `minimize_to_tray` at runtime** affects future close actions only;
  it does not retroactively show or hide anything

## 9. Frontend structure

```
windows/main/screens/settings/
  SettingsScreen.tsx        section rail + switch (mirrors ScreenSettings)
  sections/General.tsx      five toggles → SetSettings
  sections/Keybinds.tsx     Global, Channel, Per-Radio, Quick-Status panels
  sections/Deferred.tsx     parameterised stub panel
shared/components/
  Panel.tsx                 NEW — wrapper over existing .panel CSS
  SettingRow.tsx            NEW — wrapper over existing row styles
  KeyChip.tsx               NEW — capture chip over existing .kbd CSS
shared/store/settings.ts    NEW — settings + keybinds, hydrate then subscribe
```

`Panel`, `SettingRow` and `KeyChip` do not exist yet as components, but every
style they need is already ported — verified present in
`frontend/src/shared/styles/components.css`: `.panel` (784), `.kbd` with
`.listening`/`.unbound`/`.kbd-row` (1171–1197), `.tbl` for the per-radio table
(1217) and `.nav-item` for the section rail (653). **This screen requires no new
CSS**; the components are wrappers over styles that already exist.

The six deferred sections are **one parameterised component**, not six
hand-written ones:

```tsx
<Deferred title="AUDIO & SOUNDS" phase={4}
          items={["Device selection", "AGC", "Noise suppression", "VU metering"]} />
```

`MainApp.tsx` routes `view === "settings"` to `<Placeholder />` today; that
becomes `<SettingsScreen />`. The settings store lives in `shared/` rather than
`windows/main/` because the Comms popout will need keybind data once PTT is live.

## 10. Prerequisite — Wails v3 beta upgrade

This lands as a standalone `chore(deps)` commit **before** any Phase 3 code.

| | Current | Target |
|---|---|---|
| `github.com/wailsapp/wails/v3` | `v3.0.0-alpha.96` | `v3.0.0-beta.22` |
| `wails3` CLI (`test.yml:83`, `release.yml:51`) | `v3.0.0-alpha.96` | `v3.0.0-beta.22` |
| `@wailsio/runtime` | `^3.0.0-alpha.79` | `^3.0.0-beta.22` |

**Verified empirically** in a throwaway worktree on 2026-09-15: `go get` to
beta.22 builds with **zero source changes**, and `go test -race ./...` is green
across every package. Only transitive bumps follow (`go-git` 5.19.1→5.19.2,
`tint` 1.1.2→1.1.3, `x/crypto`, `x/net`, `x/sys`, `x/text`).

**Unresolved until done:** beta ships a rewritten binding generator ("static
source analysis for richer generated TypeScript bindings"). Whether the emitted
TS shape or the `bindings/github.com/FPGSchiba/...` import path changes is only
answerable by regenerating with the beta CLI. If it shifts, it touches
`shared/api/client.ts`, `shared/api/events.ts`, `TopBar.tsx` and
`PreLoginTopBar.tsx`. Budget for it rather than assuming it is free.

**Why before Phase 3, not after:** this phase adds tray and global hotkeys — new
platform surface on all three OSes. Building that against an alpha we are about
to replace means doing the platform testing twice. The beta also ships an
explicit compatibility promise and a stable desktop API, which **retires parent
spec risk R1** ("Wails v3 still pre-stable; API churn breaks builds
mid-development") for every remaining phase. Update R1 in the parent spec as
part of this commit.

## 11. Testing

### Go (table-driven, `-race`)

| Package | Covers |
|---|---|
| `chord` | Parse/String round-trip, canonical modifier ordering, invalid input, `e.code`→key table, modifier-only rejection |
| `keybinds` | Set/Clear, steal semantics, `Snapshot()` shape, unknown-ID preservation, per-radio action generation, absent-radio bindings retained but not registered |
| `hotkeys` | `Apply`/`Suspend`/`Resume` against a fake registrar — tests never touch the OS |
| `config` | `[general]` defaults; `[keybinds]` round-trip incl. unknown IDs; pre-Phase-3 `config.toml` with neither table loads to defaults |
| `app` | `SetKeybind` surfaces `Stolen`; `BeginCapture` suspends; capture timeout auto-resumes |

### Frontend (vitest + testing-library)

- KeyChip enters listening state on click; derives the chord from `e.code`
- Escape cancels capture without binding
- Conflict note renders and names the stolen action
- `Deferred` renders its phase text and item list
- General toggle calls `SetSettings` with the changed field

### Manual checklist (per OS)

Playwright stays deferred per parent spec §6.2. Verify: tray appears;
close→hide; tray click restores; start-minimized; clean disconnect on quit;
a bound hotkey fires `hotkey:pressed` while another application is focused;
macOS permission prompt appears, and the banner shows when denied.

## 12. Risks

Numbering continues from the parent spec (R1–R10).

| # | Risk | Likelihood | Mitigation |
|---|---|---|---|
| R11 | `golang.design/x/hotkey` may not expose key-release, making hold-to-talk PTT impossible with it | M | **Highest-impact unknown.** First implementation task verifies it against the real library. If unavailable: `Kind: Hold` actions are captured and persisted but **not** registered, the UI says so plainly, and library selection becomes a Phase 4/5 decision. **We do not fake hold-to-talk with a press-toggle** — a PTT that latches when the user expects release is worse than one that visibly does not work yet |
| R12 | macOS Input Monitoring / Accessibility permission blocks registration | H | `hotkeys:state` event + in-section banner; failure never blocks startup; document the grant steps |
| R13 | Linux tray needs a StatusNotifier/AppIndicator host; some desktops have none. A silent tray failure with `minimize_to_tray` on hides the app with no way back | M | **PARTIALLY MITIGATED — the specified mitigation is not implementable on Wails beta.22.** `SystemTray.New()` never returns an error, and `systemtray_linux.go` discards `register()`'s bool at both initial registration and `NameOwnerChanged` reconnection, so a missing `org.kde.StatusNotifierWatcher` fails silently at the DBus layer with zero propagation to Go. `TrayAvailable()` therefore reports true even when the tray never registered. **What is in place:** close-to-quit is forced if `SystemTray.New()` panics, and `minimize_to_tray` is user-editable in `config.toml`, which is the recovery path if a user does get stranded. **Identified concrete fix, not yet applied:** a Linux-only preflight calling `dbus.SessionBus()` + `org.freedesktop.DBus.NameHasOwner("org.kde.StatusNotifierWatcher")` before `SetupTray`, treating false/error as tray-unavailable. `godbus/dbus/v5` is already present as an indirect dependency; applying this promotes it to direct and so needs owner approval. Task 12's manual pass must target a bare X11 session with no notification daemon |
| R14 | Global hotkeys collide with other apps; Star Citizen runs fullscreen and may grab input exclusively | H | Not solvable client-side. Resolve conflicts within our own bindings, document the cross-app case, ship defaults on less-contested keys |
| R15 | Capture suspends every hotkey; a frontend crash mid-capture leaves them all dead | L | 10-second auto-resume timeout on `BeginCapture` (§7) |
| R16 | Keybinds share `config.toml`, so a malformed `[keybinds]` table makes the whole config undecodable — losing `server_url` and `log_level` with it | L | Atomic writes make partial writes impossible, so this only occurs on hand-editing. `config.Load` surfaces the error and leaves the file intact for repair. If it proves a nuisance, decode `[keybinds]` into `map[string]string` leniently and drop unparseable entries rather than failing the whole load |

## 13. Definition of Done

1. Settings is reachable from the nav rail; all eight sections render, six showing "arrives in Phase N"
2. All five General toggles persist to `config.toml` and survive restart
3. Tray present on all three platforms; close-to-tray, click-to-restore and start-minimized honour their settings
4. Quit via tray disconnects cleanly — the server logs a proper leave, not a stream drop
5. Keybinds section lists Global, Channel, Per-Radio (from live radios) and Quick-Status with current bindings
6. Chip capture reads physical key codes; Escape cancels; the chord persists to `config.toml`'s `[keybinds]` table and survives restart
7. Binding an already-used key steals it — the previous owner shows unbound and an inline note names it
8. Bound hotkeys emit `hotkey:pressed` **while another application has focus**; `Hold` actions also emit `hotkey:released`, or R11's fallback is in effect and visible in the UI
9. Registration failure shows the banner and does not prevent startup
10. Settings and keybind changes reach the Comms popout without reopening it
11. `go test -race ./...` green, `go vet ./...` clean, `npm test` green
12. CI matrix builds all three platforms

## 14. Out of scope

- Anything the bindings would actually **trigger** — no audio, no transmission (Phases 4/5)
- The content of the six deferred sections (Audio, Effects, Profiles, Notifications, Misc, Legacy)
- Keybind profiles / presets / import-export
- In-game overlay
- TLS (still Phase 7; the client remains localhost-only — parent spec R5)
- Plugin SSO (Phase 2, deferred)

## 15. Spec self-review

Performed inline before commit.

- **Placeholders:** none. Every section is concrete; the one genuinely open
  question (beta binding-generator output, §10) is named as open with a stated
  budget rather than left as a TODO.
- **Internal consistency:** §5.1 `Kind` semantics, §7 capture suspension, §11
  test coverage and §12 R11 all agree on hold-vs-press handling. §8.1's
  `ApplicationShouldTerminateAfterLastWindowClosed` change is consistent with
  §8.2's close rules and DoD item 3.
- **Scope:** focused enough for one implementation plan, provided §10 lands as
  its own commit first.
- **Ambiguity:** "settings without effect yet" (§5.4) is called out explicitly so
  it is not confused with the stub sections of §3.
- **Revision 2026-09-15:** keybinds moved from a separate `keybinds.toml` into a
  `[keybinds]` table in `config.toml` at the user's request. Parent spec §4.3 and
  the roadmap's Phase 3 row updated to match; new risk R16 records the
  shared-file blast radius.
