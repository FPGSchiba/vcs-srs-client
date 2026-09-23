# Spike — gamepad / joystick keybinds

**Date:** 2026-09-18
**Status:** investigation complete, recommendation below. No code written; nothing here is committed to.
**Question:** can VCS bind PTT and other actions to gamepad / joystick / HOTAS inputs without disturbing Star Citizen, and what does it cost the Phase 3 keybind architecture?

---

## 1. Verdict

**Yes, and the safe path is well-trodden.** Use **non-exclusive, background DirectInput** on Windows and **evdev** on Linux, with **no SDL anywhere**, and defer macOS.

The single most important finding is a trap rather than an obstacle: the obvious library choice — SDL, via any of its Go bindings — **unconditionally takes exclusive ownership of every DirectInput joystick it opens**, which is precisely the thing that would break force feedback in Star Citizen. Routing around it costs nothing, because the reference implementation for this exact problem already does.

---

## 2. The exclusivity trap

Three facts, each verified from a primary source:

**(a) Force feedback requires exclusive access.** Microsoft's *Cooperative Levels* page, verbatim:

> To use force-feedback effects, an application must have exclusive access to the device.

**(b) Exclusive access is mutually exclusive between applications.** Same page, explaining the purpose of the flag:

> To prevent this from happening, each application should have the `DISCL_EXCLUSIVE` flag set so that only one of them can be running at a time.

**(c) SDL takes exclusive access on every joystick it opens.** From `src/joystick/windows/SDL_dinputjoystick.c` on `main`, in `SDL_DINPUT_JoystickOpen` — a single call site, unconditional, executed for every DirectInput joystick regardless of capability:

```c
result = IDirectInputDevice8_SetCooperativeLevel(joystick->hwdata->InputDevice,
                                                SDL_HelperWindow,
                                                DISCL_EXCLUSIVE | DISCL_BACKGROUND);
```

The preceding comment states the motive plainly: *"Exclusive access is required for forces, though."* SDL grabs exclusive so that **SDL** can output force feedback — a reasonable choice for a game engine that owns the machine, and the wrong one for a background utility sitting next to a game.

Chain the three together and the mechanism is documented rather than speculative: if our client opens a HOTAS through SDL before Star Citizen does, we hold the exclusive claim, and SC's force-feedback acquire has nothing left to take. Open it after SC, and we get `DIERR_OTHERAPPHASPRIO` and read nothing.

**What is still *not* proven** — and the honest limit of this spike — is that Star Citizen specifically acquires FFB through DirectInput exclusive mode and would therefore visibly break. That was never tested with a real FFB stick. **The recommendation deliberately makes the question moot:** the proposed path never requests exclusive access at any point, so the conflict cannot arise whatever SC does internally. This is the substantive improvement over treating it as a risk to be measured — it is a risk to be designed out.

Worth noting why this trap is not common knowledge: SDL only falls back to DirectInput for devices XInput does not claim. Ordinary Xbox-style gamepads never take this path, so the overwhelming majority of SDL users never encounter it. The devices that *do* land on DirectInput are exactly flight sticks, throttles and pedals — our entire target population.

---

## 3. Prior art: DCS-SRS does this already

`ciribob/DCS-SimpleRadioStandalone` is the closest possible analogue — a background voice-comms client with joystick PTT, used by tens of thousands of flight simmers running force-feedback HOTAS alongside DCS. It is the design template, and it is unambiguous. From `DCS-SR-Client/Singletons/InputDeviceManager.cs`, applied identically to keyboards, mice and joysticks:

```csharp
device.SetCooperativeLevel(WindowHelper.Handle,
    CooperativeLevel.Background | CooperativeLevel.NonExclusive);
device.Acquire();
```

`Background | NonExclusive` — the exact inverse of SDL, and the combination Microsoft's own table lists as *"The default setting."* Years of field use by the target audience is about as strong a signal as this class of question admits.

Other details worth stealing from it:

- **Polling, not events.** The PTT thread is a plain `Thread.Sleep(40)` loop — 25 Hz — reading `GetCurrentState()`. No event stream, no callbacks.
- **Capture by delta.** To bind a button it snapshots every button on every device, then polls at 100 ms until one differs from the baseline. This is how "press a button to bind it" works without enumerating semantics per device.
- **128 buttons + 4 POV hats**, with hats folded in as button indices `128+n`.
- **Devices keyed by `InstanceGuid`**, with user-editable `whitelist.txt` / `blacklist.txt` escape hatches for devices that mis-declare their type.

---

## 4. Platform scoping — a large simplification

**Star Citizen is Windows-only.** There is no macOS build and none is planned; Linux is unofficial, via Proton. This reshapes the effort:

| Platform | SC runs? | Joystick binding matters? | Path |
|---|---|---|---|
| **Windows** | yes, natively | **critically** | non-exclusive DirectInput |
| **Linux** | via Proton | yes | evdev |
| **macOS** | **never** | barely — no game to talk over | defer / stub |

macOS carries a cost the other two do not: `IOHIDManager` is gated behind the **Input Monitoring** TCC permission — a *different* permission from the Accessibility grant Phase 3 already fights for, meaning a second prompt, a second denial path, and a second banner. Spending that on a platform that cannot run the game is poor value. **Recommend: macOS reports "joystick binding unavailable on this platform" and the capture UI hides the affordance.** This is a deliberate scope decision, not an oversight, and it should be written down as one.

On Linux the friction is different: `/dev/input/event*` is typically `0600 root:root`, requiring `input` group membership. In practice a Linux user already running SC under Proton has working joystick access, since Steam ships the udev rules — so this is mostly a diagnostic-message problem, not a blocker. It does need a clear error rather than silent nothing.

---

## 5. Library options

| Library | Platform | cgo | Buttons | Verdict |
|---|---|---|---|---|
| **`gonutz/di8`** | Windows | **no** — pure syscall | **128 + 4 POV** | **Recommended.** Exposes `SetCooperativeLevel` with `SCL_NONEXCLUSIVE \| SCL_BACKGROUND`; has `JOYSTATE2` and the `Joystick2` data format. MIT. |
| **`holoplot/go-evdev`** | Linux | **no** | unlimited | **Recommended.** 61★, actively maintained (Sept 2026), MIT. |
| Raw Input (`WM_INPUT`) | Windows | no | unlimited | Viable fallback. **No exclusivity concept at all**, `RIDEV_INPUTSINK` gives background input. Costs hand-rolled HID report-descriptor parsing. |
| `Zyko0/go-sdl3` / any SDL | all | no (embeds SDL3) | 128 | **Rejected** — inherits §2's exclusive grab. |
| `0xcafed00d/joystick` | Win + Linux | no | **32** | Rejected — legacy `winmm joyGetPosEx`; `JOYINFOEX.dwButtons` is a 32-bit mask. A Warthog throttle alone exceeds this. |
| ebiten / GLFW | all | varies | **32** | Rejected, same ceiling. |
| Webview Gamepad API | all | n/a | n/a | **Rejected** — Chromium gates it on page **visibility**, not focus. Survives unfocus but **dies on minimize-to-tray**, which is a headline Phase 3 feature. |

**The honest risk on `di8`: 0 stars, 0 forks.** We would be its first real user. Three things materially reduce that risk: it wraps DirectInput8, an API frozen since 2005 that cannot rot underneath it; it is 82 KB of thin `syscall.SyscallN` wrappers that can be read end to end in an afternoon; and it is MIT, so vendoring it is a legitimate exit. Treat it as "code we are adopting", not "dependency we are trusting" — and note that `go-ole` is already an indirect dependency via Wails, so the COM machinery is not new ground.

---

## 6. Architecture — the real cost

The good news first: **the `Handler` seam does not move.** A polling loop synthesizes `Pressed`/`Released` edges exactly as the gohook stream does, so `internal/hotkeys/dispatch.go` — the hold-to-talk latch, `releaseHeld`, `forceRelease`, the 120 s stale-latch watchdog — is source-agnostic and gets reused wholesale. The watchdog is in fact *more* valuable here, and polling makes one of its failure modes self-healing: a stick unplugged mid-transmission reads `false` on the next poll and releases naturally.

What does move is the type that names a binding. Today everything below `keybinds.Store` is typed on `chord.Chord`, a keyboard-only value:

```
chord.Chord{Mods, Key}  →  keybinds.Store  →  hotkeys.Binding  →  Registrar.Register  →  DTOs  →  capture UI  →  config.toml
```

Three ways to absorb a joystick button, in increasing order of honesty and cost:

- **A — pseudo-key inside `Chord`.** `Key = "joy:<guid>:btn12"`. Nearly free: `Parse`/`String` round-trip already works, the config format is untouched, the store and DTOs never notice. But it smuggles device identity into a package whose entire stated purpose is to be keyboard-only and stdlib-only, and it leaves `Ctrl+joy:…:btn12` expressible and meaningless.
- **B — a `Trigger` sum type.** `Trigger{Kind, Chord, Joy}`. Honest, validates each kind on its own terms, and leaves room for axes and hats later. Costs a signature change through `Store`, `hotkeys.Binding`, `Registrar.Register`, the DTOs, the capture UI, and the config round-trip. **This is the real blast radius, and it is the option I would take.**
- **C — a parallel subsystem.** A separate `[joystick_keybinds]` table and manager, unioned at the app layer. Zero risk to the shipped keyboard path, but two capture UIs and two conflict domains to keep in sync forever.

**One product question changes this decision and should be settled before any of it:** should an action hold *one* trigger or *a list*? Users routinely want a keyboard PTT **and** a HOTAS PTT bound to the same action simultaneously. If the answer is a list — `map[ActionID][]Trigger` — then joystick support becomes purely **additive** rather than a replacement, which is both better product and a gentler migration. TOML absorbs this cleanly: accept a string *or* an array on load, and keep writing a bare string whenever an action has exactly one keyboard chord, so existing `config.toml` files stay byte-identical until the user actually binds a stick.

Steal-on-conflict needs one adjustment: a keyboard chord and a joystick button can never collide, so they are separate conflict namespaces and the existing `takeLatchLocked` logic must not treat them as one.

Two further design notes: PTT latency at SRS's 40 ms poll is acceptable (SRS ships it), and 100 Hz costs nothing measurable if it feels better. Device identity should key on instance GUID with the **product** name retained for display, so a replugged stick is recognisable and an unrecognised binding degrades to a legible "Joystick not connected" rather than silently vanishing.

---

## 7. What I did not verify

- That Star Citizen visibly loses FFB when another process holds exclusive — untested, and deliberately routed around rather than resolved (§2).
- `gonutz/di8` has not been compiled or run against a real device in this repo.
- Hot-plug behaviour (stick connected *after* the client starts) was not investigated for either backend; DCS-SRS handles it with an explicit rediscovery pass, which suggests it needs deliberate handling rather than falling out for free.
- Whether SC itself reads sticks non-exclusively — irrelevant to the recommendation, but it would determine whether *two* comms tools could coexist alongside it.

---

## 8. Recommended placement

This is its own phase, not a Phase 3 amendment. It carries a new dependency, two new per-OS backends, a type change through the keybind stack, a config-format migration, and a UI addition — each of which Phase 3's spec would have to be reopened to accommodate. Sequencing it **after Phase 4 (Audio I/O)** is also the more useful order: PTT that opens a microphone is testable in a way that PTT wired to nothing is not.
