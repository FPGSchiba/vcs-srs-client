# Phase 3 — Manual Verification Checklist

**Status: NOT YET EXECUTED — requires a human on real hardware.**

This checklist exists because Task 12 (end-to-end verification and phase
close-out) ran in a sandboxed CI-like environment with no display server and no
way to launch a GUI application. Every automated check that *can* run
headlessly (`go vet`, `go test -race`, `tsc --noEmit`, `vitest`, the Vite
production build) has been run and is reported in
`.superpowers/sdd/2026-09-15-vcs-client-phase-3-settings-keybinds/task-12-report.md`.
Nothing below has been executed by an agent. Do not treat any item here as
verified until a human has actually performed it and recorded the result.

Each item names the spec DoD number it satisfies (`docs/superpowers/specs/2026-09-15-vcs-client-phase-3-settings-keybinds-design.md`
§13) and/or the risk it closes (§12).

## Setup

1. Build and launch the app natively on the target OS: `wails3 build` then run
   the produced binary (or `wails3 dev` for a faster iteration loop on the
   items that don't depend on a packaged binary — e.g. persistence and tray
   items should be re-checked against the packaged build at least once).
2. Have the server (`vngd-srs-server`) reachable at whatever `server_url` is
   configured in `config.toml`, since several items require a live gRPC
   session (leave-on-quit, Comms popout sync).
3. Have access to `config.toml` (the OS-specific app config dir) to inspect
   persisted values directly, and to the server's log output to check for a
   clean leave vs. a dropped stream.

---

### 1. Settings screen and section rendering — DoD 1

1. Launch the app, log in as guest, click the `Settings` entry in the main
   window's nav rail.
   **Expected:** the Settings screen replaces the current screen; a section
   rail with 8 entries is visible: General, Keybinds, Audio & Sounds, Effects,
   Profiles, Notifications, Misc, Legacy (exact labels may differ slightly —
   match against `design/vcs/project/screens/settings.jsx`).
2. Click each of the 6 non-functional sections (Audio & Sounds, Effects,
   Profiles, Notifications, Misc, Legacy) in turn.
   **Expected:** each renders a stub panel with text reading "Arrives in Phase
   N" for some N (Audio & Sounds should say Phase 4; verify the others show a
   plausible phase number, not a placeholder like "TBD" or a blank).
3. Click General and Keybinds.
   **Expected:** both render real, interactive content (not stubs).

### 2. General toggles persist — DoD 2

There are 5 toggles: `start_minimized`, `minimize_to_tray`,
`show_transmitter_name`, `play_connection_sounds`, `radio_switch_as_ptt`.

1. Open Settings → General. Note the current state of all 5 toggles.
2. Flip all 5 to the opposite state.
3. Open `config.toml` in a text editor (or `cat` it) while the app is still
   running.
   **Expected:** the `[general]` table reflects all 5 new values immediately
   (writes are described as immediate-on-change, not on app quit).
4. Fully quit the app (not just close-to-tray — use the tray Quit or Cmd+Q /
   Alt+F4) and relaunch it.
   **Expected:** Settings → General shows the same 5 flipped values, not the
   defaults.
5. Repeat once more flipping back to the original values, to confirm the round
   trip both ways and leave the app in its original configured state.

### 3. Tray legibility, light/dark, close-to-tray, restore, start-minimized — DoD 3, R13

**macOS only for the light/dark check; do all sub-items on every platform you
have available.**

1. With `minimize_to_tray = true`, switch macOS to Light mode (System
   Settings → Appearance) and look at the menu bar tray icon.
   **Expected:** icon is a legible monochrome/template glyph, not a colored
   square or blank space, and is visually distinct from other menu bar icons.
2. Switch macOS to Dark mode. Look at the tray icon again.
   **Expected:** icon inverts appropriately (template icon behavior) and stays
   legible — this is the point of `SetTemplateIcon` in §8 of the spec.
3. With `minimize_to_tray = true`, close the main window (red traffic-light /
   OS close button, not Quit).
   **Expected:** the window disappears but the app keeps running (check
   Activity Monitor / Task Manager / `ps`) — it must NOT quit.
4. Click the tray icon.
   **Expected:** the main window reappears in the same position/state it was
   closed in.
5. Set `start_minimized = true` in `config.toml`, quit fully, relaunch.
   **Expected:** the app starts with no main window visible; only the tray
   icon is present. Clicking the tray icon shows the window.
6. **Linux specifically (R13):** on a desktop environment with no
   StatusNotifier/AppIndicator host (e.g. a bare X11/i3/openbox session with no
   notification daemon running), set `minimize_to_tray = true`, launch the
   app, and close the main window.
   **Expected (per spec R13, only PARTIALLY MITIGATED):** the tray icon may
   silently fail to register (this is a known, documented gap — `TrayAvailable()`
   can report true even when the tray never actually registered with DBus).
   Record whether the user is left with **no visible window and no way back**
   (the failure mode R13 describes) or whether some other affordance exists.
   This is expected to still be broken; the point of running it is to confirm
   the actual failure mode matches what R13 predicts, not to pass/fail it
   against a fixed bar.

### 4. Clean disconnect on tray quit — DoD 4

1. With the app connected to a live server, open the tray menu and click
   `Quit` (not window-close).
2. Immediately check the server's log output for this session.
   **Expected:** the log shows a proper client leave/disconnect (whatever the
   server logs for `sess.Disconnect`/graceful session teardown), not a
   dropped-stream / timeout / abrupt-EOF style log line. Compare against what
   a window-close-without-tray (if `minimize_to_tray` is off) or a killed
   process logs, to confirm there's a real difference.

### 5. Keybinds section lists all four categories — DoD 5

1. Open Settings → Keybinds while connected to a server with at least 2 live
   radios assigned.
   **Expected:** four groupings are visible: Global (5 actions —
   `global.ptt`, `global.push_to_mute`, `global.mute_toggle`,
   `global.emergency_broadcast`, `global.compact_overlay`), Channel (4 —
   `channel.intercom`, `channel.role`, `channel.ship`, `channel.fleet`),
   Per-Radio (one PTT row and one Select row per live radio, generated from
   the radios currently in `state.Store` — confirm the row count matches the
   number of radios, and disappears/appears if radios change), and
   Quick-Status (4 — `status.available`, `status.combat`, `status.discipline`,
   `status.afk`).
2. Change the server-side radio assignment (add/remove a radio) without
   restarting the client, if the test harness allows it.
   **Expected:** the Per-Radio row set updates to match.

### 6. Chip capture reads physical key codes, non-US layout — DoD 6, spec §3 "why e.code"

1. On a US keyboard layout, click a KeyChip, press a key, confirm the chord
   shown matches the physical key pressed.
2. **Explicitly required — this is the entire reason `e.code` was chosen over
   `e.key`:** switch the OS keyboard layout to a non-US layout (e.g. German
   QWERTZ, French AZERTY) if a physical or software non-US layout is
   available. Click a KeyChip and press a key in a position that differs
   between layouts (e.g. the key physically where `Z` sits on a US keyboard,
   which is `Y` on QWERTZ).
   **Expected:** the captured chord reflects the *physical* key position
   (`KeyZ`/`Z` in the chord, per US physical layout), not the character the
   layout would otherwise produce. If this instead captures the
   layout-remapped character, that's a regression to the `e.key` behavior the
   spec explicitly rejected.
3. Quit and relaunch; confirm the chord captured under the non-US layout
   persists correctly and re-registers as the same physical key.

### 7. Binding an already-used key steals it — DoD 7

1. Note the current binding for two different actions, A and B (any two, e.g.
   `global.mute_toggle` and `status.afk`).
2. Bind action B to the exact same chord currently bound to action A.
   **Expected:** the UI reports the steal (an inline note near action A or B
   naming the other action), and action A's row now shows "unbound" (no
   chord).
3. Reload the Keybinds section (navigate away and back, or restart) to confirm
   the steal persisted to `config.toml` and isn't just an optimistic UI change
   that reverts.

### 8. Global hotkey fires while another app has focus — DoD 8, R14

1. Bind any Global or Quick-Status action to a chord unlikely to collide with
   OS/other-app shortcuts (e.g. `Ctrl+Alt+Shift+F13` if available, or another
   low-collision combo).
2. Click into a completely different application (browser, text editor —
   anything that is NOT the VCS client) so it has OS focus.
3. Press the bound chord.
   **Expected:** `hotkey:pressed {action_id}` fires — check via a debug
   console/log line wired for this purpose, or any temporary instrumentation.
   This must work with the VCS client window unfocused/backgrounded, since
   that's the entire point of global (as opposed to in-app) hotkeys.
4. Try this again inside a fullscreen exclusive-input game if one is available
   (spec explicitly calls out Star Citizen as a case that may grab input
   exclusively, R14) to document whether the collision actually occurs in
   practice, not just in theory.

### 9. Hold-to-talk press/release — DoD 8, R11 (now resolved per spec §12)

1. Bind `global.ptt` (a `Hold`-kind action) to a chord.
2. Press and hold the chord for several seconds without releasing.
   **Expected:** exactly one `hotkey:pressed {action_id: "global.ptt"}` event
   fires on press-down, and no repeated `pressed` events fire from OS key
   repeat while held.
3. Release the key.
   **Expected:** exactly one `hotkey:released {action_id: "global.ptt"}`
   fires on release, and it fires promptly (not after some polling delay long
   enough to feel laggy for PTT — this matters especially on Windows, where
   the implementation polls `GetAsyncKeyState` at 100 Hz rather than getting a
   push interrupt; confirm perceived release latency is acceptable, roughly
   ≤ 20-30ms on top of the 100Hz poll interval).
4. Repeat for a `Press`-kind action (e.g. `status.afk`) and confirm it emits
   only `hotkey:pressed`, never `hotkey:released` — the spec's `nil`-channel
   mechanism should make this structurally impossible to violate, but confirm
   observed behavior matches.

### 10. macOS Input Monitoring permission prompt — R12

**macOS only.**

1. On a clean macOS user account (or after resetting the app's permission via
   `tccutil reset ListenEvent <bundle-id>`), launch the app and bind/trigger a
   global hotkey for the first time.
   **Expected:** the OS Input Monitoring permission dialog appears.
2. Click **Deny**.
   **Expected:** the app does not crash and does not fail to start. The
   Keybinds section shows the "global hotkeys unavailable" banner (driven by
   the `hotkeys:state {registered, error}` event per spec §6.1). Bindings can
   still be captured and saved to `config.toml`; they simply don't fire.
3. Grant the permission via System Settings → Privacy & Security → Input
   Monitoring, then relaunch.
   **Expected:** the banner clears and previously saved bindings register and
   fire correctly.

### 11. Linux R13 — no StatusNotifier host, no stranded user

Covered above as item 3.6. Restated separately here because it is its own DoD
concern (spec explicitly flags this as only partially mitigated): confirm
specifically whether, after a silent tray registration failure with
`minimize_to_tray = true`, the user has **any** way back to the window (e.g.
alt-tab, a taskbar entry, a keyboard shortcut) or is genuinely stuck. If
genuinely stuck, that is the expected (known, unfixed) outcome — file it as a
reminder that R13's concrete fix (the DBus `NameHasOwner` preflight described
in the spec) is still unapplied, not as a new bug.

### 12. Settings/keybind changes reach the Comms popout live — DoD 10

1. Open the Comms popout (separate OS window) alongside the main window.
2. In the main window, change a General toggle (e.g. flip
   `show_transmitter_name`).
   **Expected:** if the Comms popout renders anything driven by that setting,
   it updates without the popout being closed/reopened (via the
   `settings:changed` event, per spec §6.1).
3. In the main window, rebind or clear a keybind.
   **Expected:** if the Comms popout displays any keybind-derived UI, it
   updates live via `keybinds:changed` without reopening.

### 13. OS cannot register a key — per-row failure reason — spec Task 5 findings

The spec's Task 5 implementation found that `golang.design/x/hotkey` v0.6.1 has
no named-key constant for several canonical keys, including all `Numpad0`–`9`,
`F21`–`F24`, `Backspace`, and most punctuation keys, on any of the three
platform backends.

1. Attempt to bind any action to `Numpad7` (or another key from the
   unsupported list above, if `Numpad7` isn't capturable on your keyboard).
2. **Expected:** the row shows a specific, per-row failure reason (surfaced via
   `Manager.Failed()` → some UI element on that specific Keybinds row) — e.g.
   something like "no OS key mapping for Numpad7" — not a generic top-level
   banner that doesn't say which binding failed or why. Other, unaffected
   bindings must continue to show as registered/working.
3. Confirm the chord is still saved to `config.toml` (capture-and-persist
   succeeds even though OS registration fails) so the user doesn't lose the
   intended binding if key support improves later.

---

## Known, already-documented gaps this checklist is not meant to "fail" on

These are pre-existing, understood limitations — running the checklist above
should confirm the *expected* degraded behavior, not surface them as new bugs:

- **R13 (Linux tray on hosts with no StatusNotifier):** partially mitigated
  only; a genuinely silent failure with `minimize_to_tray` on is the documented
  expected outcome absent the DBus preflight fix.
- **Key coverage gap:** `Numpad0`–`9`, `F21`–`F24`, `Backspace`, and most
  punctuation keys have no OS-hotkey mapping in `golang.design/x/hotkey`
  v0.6.1 on any platform (see item 13 above and the spec's R11 entry).
- **Linux/X11 Alt & Super modifier mapping** is a best-effort default
  (`Mod1`/`Mod4`); it can be wrong on X servers with a non-default modifier
  map, and is not exercised by Wayland shortcut portals at all.
- **Linux release builds require `CGO_ENABLED=1` + libX11 dev headers** at
  build time, or hotkey registration silently downgrades to a
  build-succeeds/runtime-fails fallback (`registrar_nocgo.go`). CI has been
  updated to install `libx11-dev`; a local ad-hoc Linux build that skips this
  should reproduce the runtime failure, which is expected, not a new find.

## What to do with results

Record pass/fail per numbered item (not per DoD number) directly in this file
or a copy of it, including OS/platform and keyboard layout where relevant.
File any genuine failure (something behaving worse than the documented gaps
above predict) as a new task rather than patching it silently, per the
project's standard workflow.
