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

- [ ] With VCS running, unplug the stick mid-PTT. Confirm transmission stops
      and nothing latches open.
- [ ] Plug it back in. Confirm the binding works again within ~3 seconds,
      with no restart.
- [ ] Confirm the chip showed as muted/"not connected" while unplugged and
      did not disappear.

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

## 10. Linux (if available)

- [ ] Run as a user NOT in the `input` group. Confirm the error names the
      group and does not mention Accessibility.
- [ ] Add the user to `input`, re-login, confirm devices appear.

## 11. macOS

- [ ] Confirm the app builds and runs.
- [ ] Confirm the Keybinds section offers no joystick affordance and shows
      **no** permission-grant button.
- [ ] Confirm keyboard binds are completely unaffected.
