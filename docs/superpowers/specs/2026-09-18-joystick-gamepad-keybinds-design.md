# Joystick / gamepad keybinds — design

**Date:** 2026-09-18
**Status:** approved design, ready for implementation planning
**Spike:** [`2026-09-18-gamepad-joystick-bindings-spike.md`](./2026-09-18-gamepad-joystick-bindings-spike.md)
**Builds on:** [`2026-09-15-vcs-client-phase-3-settings-keybinds-design.md`](./2026-09-15-vcs-client-phase-3-settings-keybinds-design.md)

---

## 1. Goal

Let a user bind any VCS action to a gamepad / joystick / HOTAS button or hat direction, **in addition to** a keyboard chord rather than instead of one, without disturbing Star Citizen's own access to the device.

The change is **additive by construction**: an action holds a *list* of triggers. Every existing keyboard binding keeps working, every existing `config.toml` keeps loading, and the shipped keyboard input path is not reopened.

---

## 2. Decisions

These were settled before design and are not open for re-litigation during implementation:

| # | Decision | Rationale |
|---|---|---|
| D1 | An action holds a **list** of triggers, not one | Users want keyboard PTT *and* HOTAS PTT on the same action |
| D2 | **Buttons + POV hats** in v1; **no axes** | Hats are nearly free (same edge code); axes need threshold/deadzone UI and calibration — separate scope |
| D3 | **macOS is stubbed out** | Star Citizen has no macOS build and none is planned; `IOHIDManager` would need a second TCC permission distinct from Phase 3's Accessibility grant |
| D4 | Joystick triggers **support one modifier button** | User decision, overriding the spike's simpler recommendation. Drags in §5 specificity |
| D5 | **Vendor `gonutz/di8`** into the repo | Removes supply-chain risk of a 0-star single-author dep on the critical input path; DirectInput8 is frozen since 2005 so it cannot rot; MIT permits it |
| D6 | **No SDL, on any platform** | SDL unconditionally takes `DISCL_EXCLUSIVE` on every DirectInput joystick — see spike §2 |

---

## 3. Data model

`internal/chord` is **unchanged** — it stays keyboard-only and stdlib-only. A new `internal/trigger` package wraps it.

```go
package trigger

type Kind uint8

const (
    KindKey Kind = iota // keyboard chord
    KindJoy             // joystick button or hat direction
)

// Trigger is one way to activate an action.
type Trigger struct {
    Kind Kind
    Key  chord.Chord // valid when Kind == KindKey
    Joy  JoyBinding  // valid when Kind == KindJoy
}

// JoyBinding is a button (or hat direction) on one device, optionally gated
// behind a modifier button on any device.
type JoyBinding struct {
    Device   DeviceID
    Button   Button
    Modifier *JoyButton // nil = bare binding
}

// JoyButton names one physical input on one device.
type JoyButton struct {
    Device DeviceID
    Button Button
}

// DeviceID is a backend-generated stable identity. Constrained to
// [A-Za-z0-9_.-]+ so it can never contain the ':' or '+' used as field
// separators in the persisted form (§6). Backends sanitise.
type DeviceID string

// Button indexes a physical input:
//   0..127   buttons
//   128..159 hat directions, encoded 128 + hat*8 + dir
//            hat 0..3, dir 0..7 clockwise from up
type Button uint16
```

Encoding hats as high button indices follows DCS-SRS and means **one** edge-detection and capture path serves both.

`keybinds.Store` changes from `map[ActionID]chord.Chord` to `map[ActionID][]trigger.Trigger`.

### One keyboard trigger, many joystick triggers

An action holds **at most one** `KindKey` trigger and **any number** of `KindJoy` triggers. Adding a second keyboard chord replaces the first rather than appending.

This is a constraint, not an accident. `internal/hotkeys` keys both `Manager.Apply(map[string]Binding)` and `dispatcher.binds map[string]boundAction` by action ID, one entry each. Supporting two chords for one action would mean widening both — reopening the shipped, tested keyboard path that D-series decisions and §7 exist to protect. Without the constraint the overflow is *silent*: the second chord saves fine, displays fine, and never fires.

The constraint costs nothing against the actual requirement. "Keyboard PTT **and** HOTAS PTT on the same action" is fully served by one keyboard chord plus N joystick triggers; two keyboard chords for one action is a marginal want. If it is ever asked for, the honest fix is widening `internal/hotkeys` deliberately, as its own change.

### Device identity

`DeviceID` is opaque to everything above the backend. Backends derive it to be stable across replug where the OS permits:

- **Windows:** DirectInput `InstanceGuid`, hyphens kept, braces stripped.
- **Linux:** the `/dev/input/by-id/` stable name when present; otherwise `vendor-product-name` slugged.

Each device also carries a human-readable **product name**, persisted alongside the ID (§6) so a binding for an absent device can still say *which* device it wants.

---

## 4. Multi-trigger activation semantics

An action is held while **any** of its triggers is active. The join is a **refcount, not a boolean** — this is the single most important correctness rule in this design.

> `App.Pressed` / `App.Released` today emit straight through with no refcounting. With two sources, releasing one while the other is still held would emit `HotkeyReleased` mid-transmission and **cut the user's PTT off while their finger is still down**.

Therefore `internal/app` gains a per-action held-source count:

- `Pressed(actionID)` emits `HotkeyPressed` **only** on the 0 → 1 transition.
- `Released(actionID)` emits `HotkeyReleased` **only** on the 1 → 0 transition.
- The count is per action, not per trigger, and is shared across both managers.

This is required even with a single manager, because one action may hold several joystick triggers.

---

## 5. Specificity resolution

D4 (modifiers) makes this necessary. Keyboard chords get it free — `dispatch.go` matches `b.mods != e.mods`, exact — but joystick "modifiers" are arbitrary buttons, so "no modifier held" is not knowable without knowing which buttons are modifiers.

Let `H` be the set of currently-held `(Device, Button)` pairs.

**Satisfied:** trigger `T` is satisfied iff `T.Button ∈ H` and (`T.Modifier == nil` or `T.Modifier ∈ H`).

**Suppressed:** a satisfied `T` is suppressed iff `T.Modifier == nil` and there exists another satisfied trigger `U` with `U.Modifier != nil` and (`U.Button == T.Button` or `U.Modifier == T.Button`).

**Effective active set** = satisfied minus suppressed. Edges fire on transitions of this set.

Worked example — the case SRS documents:

```
global.ptt   = Btn3
radio.1.ptt  = Btn5 + Btn3

Btn3 alone        → global.ptt fires
Btn5 + Btn3       → radio.1.ptt fires; global.ptt is suppressed
```

This is a pure function of `H` and the trigger table. It is table-driven tested with no OS involvement.

---

## 6. Persistence

`config.toml`'s `[keybinds]` table stays the same table with the same key names. Values become **string or array of strings**.

```toml
[keybinds]
global.push_to_mute = "V"                                    # unchanged
global.ptt          = ["F1", "joy:vpc-throttle-a1b2:btn12"]
channel.intercom    = ["joy:vpc-stick-c3d4:hat1.up"]         # hat direction

# modifier form; the modifier comes first and may live on another device
radio.1.ptt         = ["joy:vpc-stick-c3d4:btn5+vpc-stick-c3d4:btn3"]
radio.2.ptt         = ["joy:vpc-throttle-a1b2:btn7+vpc-stick-c3d4:btn3"]

[keybind_devices]                                            # display names only
vpc-throttle-a1b2 = "VPC MongoosT-50CM3 Throttle"
vpc-stick-c3d4    = "VPC MongoosT-50CM3 Stick"
```

**Trigger string grammar.** A value is a keyboard chord (existing canonical form, §3 of the Phase 3 spec) unless it begins with `joy:`, in which case:

```
joy:[<ref>+]<ref>
<ref>   := <device-id>:<input>
<input> :=  btn<1-128> | hat<1-4>.<up|up_right|right|down_right|down|down_left|left|up_left>
```

Parse: strip the `joy:` prefix, split on `+`. **One part is a bare binding; two parts mean the
first is the modifier and the second the main input.** Each part splits on `:` into exactly two
fields. This is unambiguous because `DeviceID` excludes both `:` and `+` by construction (§3).

Each `<ref>` carries its own device, so a modifier MAY live on a different device from the main
input — holding a throttle button to qualify a stick button is a normal HOTAS pattern and this
grammar must express it.

**Write rule — existing files stay byte-identical.** An action with exactly one trigger *of kind key* is written as a bare string, exactly as today. Anything else is written as an array. A user who never binds a joystick never sees their file change shape.

**Read rule.** A bare string is a one-element list. Unparseable entries are dropped **per entry**, keeping the rest — matching current `Store.Load` behaviour. Unrecognised action IDs still round-trip verbatim.

`[keybind_devices]` is display metadata only. A missing entry degrades to showing the raw `DeviceID`; it never invalidates a binding.

---

## 7. Runtime architecture

`internal/joystick` is a **sibling** of `internal/hotkeys`, not a change to it.

```
                       ┌─────────────────────┐
config.toml ─► keybinds.Store ──┬──► hotkeys.Manager ──► Registrar (gohook)
   [keybinds]   []Trigger       │      (KindKey)
                                └──► joystick.Manager ─► Source (di8 / evdev)
                                       (KindJoy)                │
                                              both ─► hotkeys.Handler (App)
                                                          │
                                                   per-action refcount (§4)
```

### The OS seam

```go
type Source interface {
    Devices() ([]Device, error) // enumerate; drives capture UI + hot-plug
    Poll() (State, error)       // held-button set, all devices
    Close()
}
```

`State` is a snapshot of held `(DeviceID, Button)` pairs. **Every behaviour above `Source` — edge detection, the refcount, specificity — is a pure function over successive `State` values**, tested with synthetic states exactly as `dispatch.go` is tested with synthetic `keyEvent`s.

### Timing

- **Poll interval: 10 ms (100 Hz)**, a named constant. DCS-SRS ships 40 ms; 10 ms costs nothing measurable and keeps added PTT latency inaudible.
- **Rediscovery interval: 3 s.** A stick plugged in after launch must start working without a restart.

### No stale-latch watchdog

Deliberately **not** reused from `dispatch.go`. Polling makes that failure mode self-healing: a device that disappears mid-transmission reads as all-buttons-up on the next poll and releases naturally. The same mechanism covers unplug, sleep and driver reset, and is strictly better than a 120 s timeout. A device that vanishes while a trigger is active MUST produce the release edge.

---

## 8. Backends

| File | Build tag | Backend | cgo |
|---|---|---|---|
| `source_windows.go` | `windows` | vendored di8 | no |
| `source_linux.go` | `linux` | `holoplot/go-evdev` | no |
| `source_darwin.go` | `darwin` | stub → `ErrUnsupported` | no |
| `source_other.go` | `!windows && !linux && !darwin` | stub → `ErrUnsupported` | no |

### Windows

Vendored di8 lands at `internal/joystick/di8/`, trimmed to what we use, MIT licence retained verbatim with upstream attribution.

Cooperative level is **`SCL_NONEXCLUSIVE | SCL_BACKGROUND`** — the combination DCS-SRS ships and Microsoft documents as the default. Data format `Joystick2` (`JOYSTATE2`: 128 buttons, 4 POVs). **A build that requests `SCL_EXCLUSIVE` anywhere is a defect**, and the spec calls it out here so a reviewer can grep for it.

`SetCooperativeLevel` needs a top-level `HWND`. We create **our own hidden helper window** rather than reaching into Wails for the native handle — the approach SDL itself takes (`SDL_HelperWindow`). This decouples us from Wails internals and survives the main window being hidden to tray, which matters because close-to-tray is a shipped feature.

POV values arrive in centidegrees (`0..35999`, `-1`/`0xFFFF` centered); they map to the 8 directions by rounding to the nearest 45°.

### Linux

`holoplot/go-evdev`, reading `EV_KEY` for buttons and `EV_ABS` `ABS_HAT*` for hats. Devices are filtered to those declaring joystick-like capabilities so keyboards are never opened — we must not become an input sniffer.

`/dev/input/event*` is typically `0600 root:root`. Access failure is reported as a distinct, actionable error naming the `input` group. In practice a Linux user already running SC under Proton has working access, since Steam ships the udev rules.

### macOS

Enumerates nothing and reports **unsupported**, never *denied* — a different state, because it is not something the user can fix. The capture affordance is hidden rather than shown-and-broken.

---

## 9. Capture flow

`BeginCapture()` / `EndCapture(token)` and the capture-token generation from Phase 3 are reused unchanged, with one required extension:

> **`BeginCapture` must suspend BOTH managers.** It currently suspends only `hotkeys.Manager`. Without this, binding a joystick button transmits while you bind it.

**One affordance, either input.** A single capture mode accepts whichever arrives first — a DOM `keydown` (existing `KeyChip` path, unchanged) or a joystick button from the backend. No "keyboard or joystick?" mode choice.

**Joystick capture completes on release**, which reads modifiers with no extra UI:

- Prompt: *"Press a button — hold a second button first for a modifier."*
- Two buttons held → first-held becomes the modifier, last-pressed the main.
- One button → bare binding.
- **Tie-break:** if two buttons first appear held in the *same* poll sample, their order is
  unknowable, so the one sorting lower by `(DeviceID, Button)` becomes the modifier. This keeps
  capture deterministic instead of depending on poll alignment.
- More than two buttons held → the binding uses the first-held as modifier and the last-pressed
  as main; buttons in between are ignored.

Complete-on-release is harmless in a binding dialog. Capture detects presses by **delta against a baseline snapshot** taken when capture begins (the DCS-SRS approach), so a button already held when capture starts cannot register.

Capture is cancellable on the existing cancel paths, and the cancel-path matrix in the Phase 3 spec §7 gains the joystick rows.

---

## 10. Frontend surface

```go
type TriggerDTO struct {
    Kind       string `json:"kind"`        // "key" | "joy"
    Chord      string `json:"chord"`       // kind=key, canonical form
    Device     string `json:"device"`      // kind=joy
    DeviceName string `json:"device_name"` // display
    Label      string `json:"label"`       // "Btn 12" | "Hat 1 ↑" | "Btn 5 + Btn 3"
    Connected  bool   `json:"connected"`   // kind=joy
}
```

`KeybindDTO.Chord string` is replaced by `Triggers []TriggerDTO`.

**Display labels are rendered in Go**, not the frontend — the same discipline that keeps canonical chord formatting inside `internal/chord`, so button and hat naming has exactly one home.

**API verbs** move to the additive model:

- `AddTrigger(actionID string, …) (SetKeybindResult, error)` — replaces `SetKeybind`
- `RemoveTrigger(actionID string, index int) error` — new
- `ClearKeybind(actionID string) error` — unchanged, drops all triggers

Steal-on-conflict keeps its current spirit, evaluated **per trigger**: binding a trigger another action already holds removes it from that action and reports it via the existing `StolenDTO`. Keyboard and joystick triggers occupy separate conflict namespaces — they can never collide, so a keyboard chord never steals from a joystick binding.

**Rows** render one chip per trigger plus a `+`. Each chip has a remove affordance. A trigger whose device is absent renders **muted, naming the device** — the binding is intact and returns when the stick is reconnected.

---

## 11. Failure surfaces

Per-trigger registration failures reuse the existing `Failed map[string]string` shape in `HotkeyStateDTO`.

Phase 3's rule holds: when a global error is already shown in the banner, per-row warnings are suppressed. Joystick adds a third global state — *unsupported on this platform* — which is informational, not an error, and must not render as a failure.

**No new permission machinery on Windows.** DirectInput needs no grant, so Phase 3's TCC work is untouched. Linux access failure gets its own message and must **not** reuse the Accessibility banner copy.

The ROADMAP's Phase 7 note about routing hotkey failures through the notification channel is **extended, not duplicated**, to cover joystick failures.

### Logging

Phase 3 forbids logging key identity because the gohook listener sees every keystroke on the machine — a log line naming keys would be a keylog.

**That rationale does not transfer to joystick input, and the distinction is deliberate.** A DirectInput or evdev joystick reader sees joystick buttons and nothing else; it cannot observe typing. Joystick edges therefore **DO** log device and button identity alongside the action ID, which is what makes "my HOTAS bind does nothing" diagnosable at all. Keyboard logging stays action-ID-only, exactly as today.

The Linux backend's device filter (§8) is what preserves this property: it must open only joystick-like devices. Opening a keyboard through evdev would turn this log line into a keylog and break the rule that justifies it.

---

## 12. Testing

- **Pure-logic, table-driven, no OS:** specificity resolution (§5), the per-action refcount (§4), hat encode/decode, trigger-string parse/format round-trip, the string-or-array config read/write rule.
- **Literal TOML fixtures**, not symmetric round-trips. Phase 3 proved a symmetric round-trip cannot catch a mistyped tag — the suite stayed green with a field deliberately broken. Every new persisted field gets a literal-TOML test, and each is verified to fail when the field is broken.
- **Fake `Source`** for manager-level tests: hot-plug, device-vanishes-while-held (must release), capture baseline behaviour.
- **`go test -race ./...`** — the poll loop, the manager and the refcount are concurrent.
- Backends themselves are **not** unit-tested against real hardware; they are thin and land behind the `Source` seam. This is a deliberate gap recorded in §14.

---

## 13. Out of scope

- **Axes** as triggers (D2) — needs threshold/deadzone UI and calibration.
- **macOS** (D3).
- **Force feedback output.** We never write to a device, only read. This is also what keeps us out of SC's way.
- **Rebinding SC's own controls.** We read devices; we never inject input.
- More than one modifier per trigger.

---

## 14. Risks

| # | Risk | Mitigation |
|---|---|---|
| J1 | Vendored di8 has never been run against real hardware by anyone we know of | It is a thin syscall wrapper over an API frozen since 2005; vendored so we can fix it in-tree. **Requires hardware verification before the phase is called done.** |
| J2 | `SetCooperativeLevel` on a hidden helper window may behave differently than on a real app window | Mirrors SDL's own long-standing approach; verify on hardware with SC running |
| J3 | Modifier + specificity is the subtlest logic here | Pure function, table-driven tests, worked example pinned in §5 |
| J4 | Hot-plug on Linux may need inotify rather than polling `/dev/input` | 3 s rediscovery is the v1 answer; revisit only if it proves laggy |
| J5 | Two identical sticks may produce colliding identities | Windows `InstanceGuid` differs per device; Linux `by-id` includes the path. Verify with two identical devices if available; a collision degrades to both bindings firing, not to a crash |
| J6 | Phase 3 was never verified on real hardware either, and this builds on it | The manual checklist at `plans/2026-09-15-phase-3-manual-verification.md` is still outstanding and gains joystick rows |

---

## 15. Dependencies

Requires user approval per CLAUDE.md — **already granted** for both:

- `github.com/holoplot/go-evdev` — new direct dependency (Linux only, no cgo, MIT)
- `gonutz/di8` — **vendored**, not a go.mod entry (D5)

No proto change. No server change.

---

## 16. Definition of Done

1. An action can hold any mix of keyboard and joystick triggers; all of them fire it.
2. Holding the same action from keyboard and joystick at once produces exactly one `HotkeyPressed` and one `HotkeyReleased` (§4) — releasing one source while the other is held does **not** cut PTT.
3. Specificity (§5) behaves as the worked example.
4. An existing `config.toml` with only keyboard binds loads unchanged and is rewritten byte-identical.
5. Buttons and hat directions both bind and fire; capture reads a modifier via hold-then-press.
6. A device unplugged while a trigger is held releases it.
7. A device plugged in after launch works within the rediscovery interval.
8. macOS builds, reports unsupported, and hides the affordance — no denial banner.
9. `go vet`, `go test -race ./...`, `tsc --noEmit`, `vitest`, and the frontend production build are all green.
10. **Hardware verification on Windows with Star Citizen running and a force-feedback stick: SC keeps force feedback while VCS reads the same device.** This closes J1, J2 and the spike's one unproven inference, and the phase is not done without it.
