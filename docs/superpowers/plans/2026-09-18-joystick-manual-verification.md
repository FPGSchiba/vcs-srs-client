# Joystick keybinds — manual verification

Nothing here can be automated: every item needs real hardware, and the most
important one needs Star Citizen running. Run it on **Windows** with a HOTAS.

**Closes spec risks J1 (di8 unproven), J2 (helper-window cooperative level),
J5 (identical devices) and Definition-of-Done item 10.**

## 1. Force feedback survives — THE critical test

The one finding the spike could not prove, and the reason this design avoids
SDL. If this fails, stop and report: nothing else matters.

- [ ] Start Star Citizen with a force-feedback stick. Confirm FFB works
      (stick shakes on weapon fire / turbulence).
- [ ] Leave SC running. Start VCS.
- [ ] Confirm **FFB still works in SC**.
- [ ] Quit and restart in the other order: VCS first, then SC. Confirm FFB
      works.
- [ ] Confirm VCS still reads the stick in both orders.

If FFB breaks in either order, capture which order and report it — that is
evidence the cooperative level is not what it should be.

## 2. Background input

- [ ] Bind a joystick button to Global PTT.
- [ ] Give SC focus. Press the button. Confirm VCS registers the PTT (the
      log records `joystick bind fired` with the device and input).
- [ ] Confirm SC still receives that button itself — it must not be swallowed.

## 3. Minimise to tray

- [ ] Close VCS to tray. Press the bound button. Confirm it still fires.
      (This is what the message-only helper window buys us.)

## 4. Buttons, hats and modifiers

- [ ] Bind a plain button. Fires.
- [ ] Bind a hat direction. Fires, and only in that direction.
- [ ] **Diagonal must not cut a held PTT.** Bind Global PTT to `Hat 1 ↑`.
      Start transmitting, and while transmitting roll the hat to up-right and
      back. Confirm transmission **does not cut** at any point, on Windows and
      on Linux. (Spec §8 hat rule: a diagonal reports the diagonal plus both
      adjacent cardinals. Before that rule, Windows cut the mic here and Linux
      did not — the two backends disagreed on identical hardware.)
- [ ] Bind a diagonal directly (`Hat 1 ↗`). Capture must produce ONE chip
      reading `Hat 1 ↗` — **not** a modifier pair such as
      `Hat 1 ↑ + Hat 1 →`. Confirm it fires on the diagonal and not on a
      straight up or straight right press.
- [ ] Bind a modifier combo: hold button A, press button B, release both.
      Confirm the chip shows "A + B" and that it fires only with A held.
- [ ] Bind a **cross-device** modifier: hold a throttle button, press a stick
      button. Confirm it fires.
- [ ] Specificity: bind Global PTT to button B alone AND another action to
      A+B. Confirm A+B fires only the second action.

## 5. Multi-source PTT — the refcount

- [ ] Bind Global PTT to both a keyboard key and a joystick button.
- [ ] Hold the keyboard key, then also press the joystick button, then
      release **only the keyboard key**.
- [ ] Confirm transmission CONTINUES — it must not cut while the joystick
      button is still held.
- [ ] Release the joystick button. Confirm transmission stops.

## 6. Hot-plug and unplug

The rows below are split deliberately. The SLOW path (unplug, watch the chip
go muted, plug back in) is the one that always worked, and a checklist that
only asks for it steers the verifier straight past the bug I1 described: a
device that re-enumerates INSIDE one 3s rediscover window used to have its
dead handle reused permanently, with the UI still reporting it connected and
every binding on it silently dead until the app was restarted. Both fast rows
have to be run, and neither is optional because "the slow one passed".

### Slow re-enumeration (longer than one rediscover)

- [ ] With VCS running, unplug the stick mid-PTT. Confirm transmission stops
      and nothing latches open.
- [ ] Plug it back in. Confirm the binding works again within ~3 seconds,
      with no restart.
- [ ] Confirm the chip showed as muted/"not connected" while unplugged and
      did not disappear.

### Fast re-enumeration (inside one rediscover) — the I1 path

- [ ] Unplug the stick and plug it back in **within two seconds**, without
      waiting for the chip to go muted. Press the bound button. Confirm it
      **still fires**.
- [ ] Repeat with the app left running for a minute afterwards, pressing the
      button every so often. Confirm it never goes quietly dead.
- [ ] On Linux, confirm the log contains a `joystick device re-enumerated;
      reopening it` line naming the device, with a DIFFERENT `now=` path from
      `was=` if the kernel moved the node. Its absence on a replug that the
      binding survived is fine (same node, live handle); its absence on a
      replug the binding did NOT survive is the bug back.

### Suspend and resume — the same path, without touching the cable

- [ ] With the stick attached and VCS running, suspend the machine (lid
      close / sleep), wait a few seconds, resume.
- [ ] Press the bound button. Confirm it **still fires**, with no restart.
      The kernel tears down and re-adds USB well inside the 3s ticker, so
      this is the fast re-enumeration above arriving without a human pulling
      anything.
- [ ] Confirm the chip is rendered connected only while the binding actually
      works. A connected chip over a dead binding is the exact symptom to
      report.
- [ ] On Windows, if the binding is dead after a resume, check the log for
      `joystick device has not responded for the recreate threshold` — it
      should appear within ~1s of the failure and be followed by the device
      working again after the next rediscover. Silence there means the drop
      never fired.

## 7. Identical devices (only if two of the same model are available)

- [ ] Attach two identical sticks. Bind a button on each to different actions.
- [ ] Confirm each fires only its own action. If both fire, record it — that
      is spec risk J5 realised.

## 8. Config round-trip

- [ ] Open `%APPDATA%\vcs-client\config.toml`. Confirm keyboard-only actions
      are still bare strings (`global.push_to_mute = "V"`).
- [ ] Confirm joystick binds appear as arrays.
- [ ] Restart VCS. Confirm every binding survived.

## 9. Capture safety

- [ ] Hold the bound PTT button, and while holding it click `+` on a row.
      Confirm the held button does NOT get bound instantly (baseline).
- [ ] During capture, confirm pressing the button does not transmit.
- [ ] Press Escape mid-capture. Confirm keyboard hotkeys still work
      afterwards.

### Capture expiry

The capture auto-resume timer used to be an invisible safety net for a
crashed frontend. It is now a **user-visible** event, and both of the last
changes here live on this path — so it needs its own rows. Before the fix,
the timer tore the capture down silently, the row kept saying "Press a key or
joystick button", and the next press **transmitted on air** instead of
binding.

- [ ] Click `+` on a row and then do nothing at all for the full capture
      timeout (see `defaultCaptureTimeout`). Confirm the row STOPS rendering
      "Press a key or joystick button…" of its own accord.
- [ ] Immediately afterwards, press a button that is already bound to another
      action. Confirm it **fires that existing binding** — i.e. it behaves
      like a normal press — and does NOT appear to bind to the row you had
      open. A live transmission here is correct; a silent nothing is the bug.
- [ ] Confirm global keyboard hotkeys are re-armed at that point (they are
      suspended for the whole capture and must come back when it expires).

## 10. Threading and the helper window (Windows)

`internal/joystick` calls DirectInput from more than one goroutine over the
life of the process, and **there is no `runtime.LockOSThread` anywhere** —
`Manager.New`'s probe enumerates on the constructing goroutine, `Manager.Close`
closes on the closing one, `newHelperWindow` runs on the constructing one, and
the poll loop may migrate between OS threads between ticks. DI8 device-state
retrieval is free-threaded in practice, which is why this has never bitten, but
it is asserted rather than proven. These rows prove it on real hardware.

- [ ] Confirm enumeration works: the stick appears in the device list at
      startup (that call runs on the goroutine that built the Manager, not the
      poll loop).
- [ ] Confirm polling works: a bound button fires (that call runs on the poll
      loop's goroutine).
- [ ] Leave the app running for **several minutes** with the stick attached and
      confirm the bindings keep firing — a thread-affinity problem typically
      shows as input dying after the Go runtime migrates the poll goroutine,
      not immediately.
- [ ] Hide to tray, wait a minute, restore, and confirm the stick still fires:
      the helper window must survive the main window being hidden.
- [ ] Quit cleanly and confirm no hang and no crash on exit (`Close` unacquires
      and releases every device, and destroys the helper window, from a
      different goroutine again).

## 11. Backend start-up failure (Windows, hard to force)

If `CreateWindowEx` or `DirectInput8Create` ever fails, the client must stay
usable and say so rather than going silently dead.

- [ ] If you can induce it (heavy resource pressure at launch, or a temporary
      local patch), confirm the Keybinds section shows the **error banner** with
      the failing call named, that keyboard binds still work, and that the
      affordance is NOT hidden the way it is on macOS.

## 12. Linux (if available)

- [ ] Run as a user NOT in the `input` group. Confirm the error names the
      group and does not mention Accessibility.
- [ ] Add the user to `input`, re-login, confirm devices appear.

## 13. macOS

- [ ] Confirm the app builds and runs.
- [ ] Confirm the Keybinds section offers no joystick affordance and shows
      **no** permission-grant button.
- [ ] Confirm keyboard binds are completely unaffected.
