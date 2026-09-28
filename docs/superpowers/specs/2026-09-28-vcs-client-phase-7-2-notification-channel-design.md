# Phase 7.2 — Notification channel: design

**Date:** 2026-09-28
**Status:** approved
**Branch:** `feat/phase-7-2-notification-channel` (off `main` at `ed35789`)
**Parent:** [`2026-09-28-vcs-client-phase-7-decomposition-design.md`](./2026-09-28-vcs-client-phase-7-decomposition-design.md)

Phase 7.2 builds the client's general notification channel, gives it the
three consumers `internal/events/events.go:56` names — hotkeys, joystick and
audio — and fills the mixer's notification bus so an alert can be heard as
well as seen.

**The channel is the deliverable; hotkeys are merely the first caller.**
Nothing in `internal/notify` knows what a hotkey, a joystick or an audio
device is. 7.2 comes before 7.3 and 7.4 precisely so Ship, Fleet and Messages
find a channel already there instead of three consumers being retrofitted
onto one built for keybinds.

Hotkeys and joystick replace Phase 3's inline banner in the Keybinds section,
which is the ROADMAP's stated deliverable; audio and the sound engine are
additions made during design (§1.5, §1.7).

---

## 1. What was verified, and what turned out to be wrong

Everything below was established by reading source on 2026-09-28. Phase 7.1
found that R4 described a `session.json` that never existed, that R5 could
not be closed from this repo at all, and that PROTO_GAPS #7 sourced history
from a `voice:rx_active` event that does not exist. The same discipline was
applied here.

### 1.1 Confirmed

- **The payloads already carry every field the three required cases need.**
  `events.HotkeyStatePayload` (`internal/events/events.go:190`) carries
  `Registered`, `Error`, `Failed map[actionID]reason` and `Permission`
  (`unknown` | `granted` | `denied` | `not_applicable`).
  `events.JoystickStatePayload` (line 285) carries `Supported`, `Error`,
  `Devices`. **No new payload field is added by this phase.**
- **The window Registry is generic** (`internal/app/windows.go:34`). A popout
  costs two `switch` arms in `internal/app/windowfactory.go:20-36`, a Vite
  input entry and an HTML shell.
- **The prototype CSS is already ported in full.** `.toast-stack`, `.toast`
  with its `.warn`/`.alert`/`.ok` variants, `.launcher-btn .badge`,
  `.sb-bell-count`, `.statusbar .sb-btn.has-unread` and `.state-card` all
  exist in `frontend/src/shared/styles/components.css`. Every icon `NotifRow`
  needs — `bell sos sync users layout sensor refresh x` — exists in
  `Icon.tsx`. The UI cost is JSX only.
- **PROTO_GAPS #8 keeps this local-only.** Nothing in 7.2 touches
  `srs.proto`.

### 1.2 Contradicted: "this may add no new backend plumbing"

False. A popout is a separate webview with its own JS heap, so a Zustand
notification list living in the main window is invisible to the Notifications
window. Every cross-window fact in this app is already Go-owned and
event-broadcast for exactly that reason — `useWindows` ← `window:state`,
`shared/store/connection.ts` ← `connection:state`.

**The notification store must live in Go.** That is the bulk of 7.2's real
work. The narrower claim — that no new *event payload* or subsystem plumbing
is needed — does hold.

### 1.3 The dedupe problem is worse than "level-triggered snapshots re-emitted on change"

- **`hotkeys:state` has no dedupe at the emit site at all.**
  `emitHotkeyState` (`internal/app/settings.go:994`) is called from
  `AddTrigger`, `RemoveTrigger`, `ClearKeybind`, `resumeCapture` (i.e. every
  `EndCapture`), `RecheckHotkeyPermission`, `applyGrantedHotkeys`,
  `RefreshKeybinds` and `SetJoystickBackend`, plus `applyHotkeys` itself
  (`settings.go:1241`). Rebinding nineteen actions emits roughly 38 identical
  payloads. One unregisterable `Numpad7` would become 38 notifications.
- **`joystick:state` *is* edge-deduped in the manager**
  (`internal/joystick/manager.go:388,441` only notify when something visible
  changed) — **but the poll loop runs at 100 Hz**, and `tick()` notifies
  whenever `errText(pollErr)` changes. A device failing every other poll
  flaps the edge at up to ~50 Hz. Identity dedupe does not fix this: against
  a condition-backed store that flap is Raise→Resolve→Raise→Resolve, every
  one of them a genuine state change. Flap suppression is a second,
  independent mechanism. See §4.2.

### 1.4 The Keybinds banner is not purely informational

It carries `GRANT ACCESS` / `OPEN SETTINGS` / `RE-CHECK`, backed by two
pieces of *local* React state — `requested` and `promptSpent`
(`Keybinds.tsx:141-148`) — which are unobservable from the backend, because
macOS provides no way to ask whether an app's one-shot Accessibility prompt
has been spent. Moving the banner is not a copy-paste. See §6.4.

### 1.5 The "no notification engine" comment overstates what is missing

`internal/audio/manager.go:938`:

```go
notifBuf := make([]float32, FrameSamples) // no notification engine yet (Phase 4 scope); always silent.
```

Read literally that sounds like a subsystem to build. It is not. `notifBuf`
is **already** handed to `mixer.Mix` (`manager.go:1118`); the notification bus
already has its own gain, taper and Settings → Audio slider
(`mixer.go:9,59`); and `voicePool.mixInto(dst, lookup)` (`sfx.go:250`) is
**generic over the sample lookup**, with the DSP loop running the identical
call for SFX one line earlier (`manager.go:1116`).

What is genuinely absent is a second voice pool, a second sample set, and a
`PlayNotification` mirroring `PlayEffect` — see §3.8. What is absent and
*cannot be supplied by this phase* is the sample itself: the pack is a
dependency on the user, and `internal/audio/assets/README.md` records it as
"not something to substitute".

**So every notification in 7.2 is silent — but by missing asset, not missing
code.** The distinction matters: the path becomes audible the day a WAV
lands, with no further work.

### 1.6 Two delivery traps

- `TopBar.tsx:88` renders each launcher as a `<span onClick>`. Wiring the
  `notifications` launcher edits that line, making it *new code* for
  SonarCloud's PR gate → `typescript:S1082`, the rule Phase 6 already tripped.
- `StatusBar.tsx:121` already renders the ALERTS bell, but it is completely
  inert: no handler, no count, no `.has-unread`.

### 1.7 `EventAudioState`'s comment contradicts the ROADMAP — the comment wins

`internal/events/events.go:56` claims Phase 7's notification channel "absorbs
all three uniformly" — hotkeys, joystick **and audio**. The ROADMAP's 7.2 row
names only hotkeys and joystick.

**Resolution: audio is in, and the comment is honoured rather than amended.**

The first draft of this spec deferred audio on the grounds that "nobody knows
how chattily `audio:state` fires". That was a reason to go and look, not a
reason to stop. Having looked:

- `emitStateIfChanged` (`internal/audio/manager.go:503`) runs at the end of
  every poll tick, and `PollInterval` defaults to **2 s**
  (`manager.go:291`, "design spec §8: ~2s hot-plug poll").
- It compares the **whole `State` struct** (`manager.go:63-70`), which
  includes `Overruns` and `Underruns` — **monotonic counters**. So while the
  engine is glitching, `audio:state` fires every 2 s indefinitely: ~30 per
  minute, ~1800 per hour.

So the chattiness is real, but it is a 0.5 Hz ceiling rather than the
unbounded churn the joystick's 100 Hz loop implies — and §4.1's fingerprint
already neutralises it, because the fingerprint is computed over what the
**user sees**, not over the payload. The audio adapter (§5.4) projects those
ten fields down to the four that are faults and drops both counters, both
device ids, `Running` and `Starting`. 1800 emissions per hour collapse to one
notification, with no new machinery.

Two facts make that projection stable rather than merely plausible:

- The error text comes straight from the backend with no attempt counter or
  timestamp (`m.inputErr = err.Error()`, `manager.go:1402`, and the
  symmetric `manager.go:687`).
- The bounded-backoff reopen (1 s → 30 s doubling, `manager.go:35-36`) makes
  no new attempt during the wait, so nothing rewrites the error text while a
  device is down.

What remains unobserved is narrower than "how often does it fire" — see §7 item 2.

---

## 2. Scope

**In:**

1. A general, source-agnostic notification channel owned by Go.
2. The Notifications popout, ported from the design prototype.
3. Unread surfacing that reaches a user who never opens that popout: the
   TopBar launcher badge, the status-bar ALERTS bell, and an error-severity
   toast stack.
4. Three first consumers routed through the channel: hotkey registration
   failures, joystick availability, and audio device faults — the three
   `events.go:56` names. Hotkeys and joystick replace Phase 3's inline
   Keybinds banner.
5. The notification sound engine (§3.8) — the second voice pool that finally
   fills the mixer's notification bus. Ships **silent**: the sample pack is
   an outstanding dependency on the user and must not be substituted.

**Out:** see §8.

---

## 3. Architecture

### 3.1 `internal/notify` — a pure package

Modelled on `internal/connhealth`, which exists for the same reason: one
derived model, one owner, no consumer deriving it independently. Like
`connhealth` it depends only on injected function values and an injected
clock — never on `internal/app`, `internal/hotkeys` or `internal/joystick` —
so it is unit-testable with no sockets and no goroutines but its own timers.

```go
// Severity drives delivery, not just colour. See §5.3.
type Severity string
const (
    SeverityError Severity = "error"
    SeverityWarn  Severity = "warn"
    SeverityInfo  Severity = "info"
)

// KV is one ordered context row. Deliberately a slice, not a map: Go's
// encoding/json sorts map keys alphabetically, which is deterministic but
// is not display order. AudioSettingsDTO.EffectOrder exists for the same
// reason.
type KV struct {
    Key   string `json:"key"`
    Value string `json:"value"`
}

// Action is one button on a notification row. Kind is a closed set (§3.4)
// so a new notification source adds no frontend dispatch code.
type Action struct {
    Label   string `json:"label"`
    Icon    string `json:"icon"`
    Kind    string `json:"kind"`
    Target  string `json:"target"`
    Primary bool   `json:"primary"`
}

type Item struct {
    ID       string    `json:"id"`
    Key      string    `json:"key"`       // "" for a discrete Post
    Category string    `json:"category"`
    Severity Severity  `json:"severity"`
    Icon     string    `json:"icon"`
    Title    string    `json:"title"`
    Body     string    `json:"body"`
    Context  []KV      `json:"context"`
    Actions  []Action  `json:"actions"`
    Time     time.Time `json:"time"`
    Unread   bool      `json:"unread"`
    Resolved bool      `json:"resolved"`
}

type Snapshot struct {
    Items  []Item `json:"items"`  // newest first
    Unread int    `json:"unread"`
}
```

`Actions` is a **list** from the start, even though 7.2 only ever populates
one. The prototype's distress example carries two (`misc.jsx:421-422`), so a
singular field would force 7.4 to change the wire shape and every consumer —
the retrofit this sub-phase exists to prevent.

Store: a capped list (**200 items**, oldest dropped) plus per-key
bookkeeping. **In-memory only, lost on restart.** That is not a gap for 7.2's
three cases: `applyHotkeys()` runs at startup (`internal/app/app.go:166`) and
re-raises whatever is still true. Persistence is 7.3's subject.

### 3.2 Two entry points, because a general channel needs both

```go
func (n *Notifier) Post(item Item)              // discrete event
func (n *Notifier) Raise(key string, item Item) // condition asserted
func (n *Notifier) Resolve(key string)          // condition cleared
```

- **`Post`** — a thing that happened once and is never "resolved": a distress
  beacon, a client joining a frequency, an incoming message. Always appends.
  **No dedupe, no coalescing.** This is what 7.3 and 7.4 will mostly use.
- **`Raise`/`Resolve`** — a condition that is true or false right now.
  Identity dedupe and the coalescing window apply, and resolution is
  meaningful. This is what 7.2's three cases use, and it is the *minority*
  case in the long run.

Dedupe and flap suppression are therefore properties of the **condition
path**, not of the store. A `Post` is never coalesced, never deduped, and
never resolved.

Read/dismiss surface: `MarkRead(id)`, `MarkAllRead()`, `Dismiss(id)`,
`Clear()`.

### 3.3 The source-adapter seam

One small adapter per notification source, each turning a subsystem's DTO
into an `Item`. **`internal/notify` is not touched when a source is added.**
A reviewer should be able to grep it for "hotkey", "joystick" or "audio" and
find nothing.

Two sources tap a funnel inside `internal/app`, because each is already the
**sole** caller of its emitter — verified: `emitHotkeyState`
(`settings.go:994`) is the only caller of `events.Tagged.HotkeysState`, and
`emitJoystickState` (`settings.go:170`) the only caller of `JoystickState`.
Both already build the exact DTO an adapter needs, so tapping the funnel
means no second path can bypass the channel. These live in
`internal/app/notify_keybinds.go`.

**Audio wires differently, and deliberately so.** It has *two* emit sites,
both in `main.go` and neither inside `internal/app`:

| Site | Condition |
|---|---|
| `main.go:225` | `audio.NewMalgoBackend()` failed — no Manager exists, so an `AudioStateDTO` is hand-pushed with both error fields set |
| `main.go:251` | the Manager's `OnState` callback, for everything after that |

An adapter hung only off `OnState` would miss the most severe audio failure
there is — "audio features are disabled", the case where no Manager was ever
constructed. So the audio adapter lives in `main.go` beside `SetNotifier`,
covering both sites, in `notifyAudioState(app.AudioStateDTO)`.

(Checked and *not* a third site: `am.Start()` returning an error at
`main.go:279` reaches only slog. That is harmless, because `Start` does not
abort on enumeration failure — it logs, falls through with empty device
lists, and still reaches `emitState()` at `manager.go:720`. The only error
`Start` returns is `ErrStartInProgress` from a concurrent call, which
`main.go` cannot produce.)

Wiring: `gui.SetNotifier(n)` in `main.go`, alongside the existing
`SetConnHealth` / `SetSettingsBackend` / `SetJoystickBackend` calls. A nil
notifier is legal and inert, so every existing test constructing an `App`
without one keeps compiling.

### 3.4 Action kinds are a closed set

The prototype dispatches actions with an `if` ladder
(`misc.jsx:389-391`: `open-comms`, `open-ship`, `view-op`). Replaced by two
data-driven kinds:

| Kind | Target | Effect |
|---|---|---|
| `open-window` | a window id (`comms`, `ship`, …) | `api.openWindow(target)` |
| `navigate` | a main-window nav key (`settings`, `profiles`, …) | main window navigates; from a popout, focuses the main window first |

7.2 emits exactly one action: `navigate` → `settings`. Adding a notification
source in a later phase adds no frontend dispatch code.

### 3.5 Categories are fixed now, not per-phase

The prototype's seven categories (`misc.jsx:404-412`) with their colours move
into one shared table read by both the popout's filter and `NotifRow`'s
border: `distress`, `fleet`, `sync`, `comms`, `profile`, `system`,
`operation`.

**7.2 only ever emits `system`.** The filter dropdown populates from the
categories actually present, so it is not six empty options — but the table is
complete, so no later phase renegotiates it.

### 3.6 Event and bindings

New event `notifications:changed`, matching the existing `<domain>:<thing>`
naming, carrying the **full `Snapshot`**. Full replacement rather than a
delta, for the reason `keybinds:changed` already documents — "the list is
small, and it removes a class of frontend/backend divergence bug" — with the
200-item cap as what keeps that true.

Bindings on `App`: `GetNotifications()`, `MarkNotificationRead(id)`,
`MarkAllNotificationsRead()`, `DismissNotification(id)`,
`ClearNotifications()`.

### 3.7 Concurrency

`emitJoystickState` runs on the joystick manager's poll goroutine;
`emitHotkeyState` runs on Wails binding goroutines; the audio adapter runs on
the audio Manager's poll goroutine. The store is
goroutine-safe and follows `connhealth`'s documented lock ordering exactly —
`emitMu → mu → mutate → unlock mu → publish → unlock emitMu` — so `OnChange`
delivery is serialised and never reentrant. `OnChange` is documented as
must-not-call-back, as `connhealth.Options.OnChange` is
(`connhealth.go:116-126`).

Coalescing timers fire on their own goroutines and publish through the same
path.

### 3.8 Notification sound playback

`internal/audio/manager.go:938` reads:

```go
notifBuf := make([]float32, FrameSamples) // no notification engine yet (Phase 4 scope); always silent.
```

That comment understates how much already exists. `notifBuf` is **already
passed to `mixer.Mix`** (`manager.go:1118`), the notification bus already has
its own gain and its own Settings → Audio slider
(`mixer.go:9,59`; `AudioLevels.notification`), and — decisively —
`voicePool.mixInto(dst, lookup)` (`sfx.go:250`) is **generic over the sample
lookup**. The DSP loop already runs the identical call one line earlier for
SFX:

```go
sfxVoices.mixInto(sfxBuf, m.sfx.sampleFor)      // manager.go:1116, exists
notifVoices.mixInto(notifBuf, m.notif.sampleFor) // this phase, same shape
```

So the engine is a **second voice pool over a second sample set**, plus
`Manager.PlayNotification(id)` mirroring `PlayEffect` (`manager.go:400`) and
the same per-generation epoch discipline `sfxVoices` already carries
(`manager.go:203,668,672,805`). Every hard part — the fixed-capacity ring,
oldest-voice eviction, the `TryLock` drain that keeps `play` off the realtime
path, the limiter — is existing, tested code.

**Sound follows the toast.** Rather than a sound per severity, exactly what
toasts also sounds, which under §5 means **error severity only**. This avoids
the worst outcome — a sound with no visible cause — and it means one new
manifest slot, `notify_alert`, rather than three. Warn items reach the badge
and bell silently; info items, being raised already-read (§5.3), do nothing
at all, which falls out of that existing rule rather than needing its own.

Firing is governed entirely upstream: §4.1 means a sound plays only when an
item is genuinely created or content-changed, and §4.2's window means a
flapping device produces at most one sound per window. This is Phase 6's
`c8cdb75` concern ("dedupe connection SFX") solved in the store rather than
bolted onto the play site.

A new setting `play_notification_sounds` (default true) mirrors the existing
`play_connection_sounds`. The prototype's per-category Toast/Sound/Silent
table (`settings.jsx:245`) stays out of scope (§8).

**This ships silent, and that is the honest state, not a shortfall.**
`internal/audio/assets/README.md` records that the sample pack "is supplied
by the project … and is a **blocking dependency tracked in the Phase 4
spec**, not something to substitute", so no placeholder tone is synthesised.
`notify_alert` reports `available: false` and plays nothing — exactly how all
nine existing SFX slots ship today. **The outstanding ask on the user grows
from nine WAV files to ten.** The path becomes audible the day that file
lands, with no further code change.

---

## 4. The level-to-edge translation

Two mechanisms. They solve different problems and neither subsumes the other.

### 4.1 Identity dedupe (kills the `hotkeys:state` spam)

`Raise(key, item)` computes a fingerprint over **everything the user can
see** — severity, title, body, ordered context, actions. Then:

| Existing state for `key` | Fingerprint | Result |
|---|---|---|
| none | — | append, unread, emit |
| unresolved | identical | **total no-op** — no emit, no timestamp bump, no re-mark-unread |
| unresolved | differs | update in place, refresh `Time`, re-mark unread, emit |
| resolved | any | replace with a fresh unresolved item (a new occurrence), emit |

`Resolve(key)` on an absent or already-resolved key is likewise a total
no-op.

The identical-fingerprint no-op is the whole fix for §1.3's 38 emissions:
rebinding nineteen actions produces **one** notification.

#### Dismissing a condition that is still true

A keyed item raises an ordering problem a discrete one does not: the user
dismisses "Global hotkeys unavailable", the condition still holds, and the
very next `applyHotkeys()` re-raises it. Under the table above that is
"none → append", so the item the user just dismissed reappears within
milliseconds. That is a bug, not a feature.

So `Dismiss` on a **keyed** item removes the item *and* records the key as
suppressed **at that fingerprint**. A later `Raise` with the identical
fingerprint stays suppressed and emits nothing. Suppression is cleared by
either of the two things that mean "this is news again":

- a `Raise` whose fingerprint **differs** (the condition changed — e.g.
  permission went `denied → granted` while registration still fails), or
- a `Resolve` (the condition actually cleared, so a later recurrence is a new
  occurrence).

`Clear()` applies the same suppression to every keyed item it removes, for
the same reason. `Post` items have no key, so neither dismissal nor
suppression has anything to attach to — dismissing one simply removes it.

### 4.2 Flap suppression, with a per-source window

Each key carries a coalescing window. A change arriving inside a key's open
window is recorded but not emitted; a trailing timer — reset on each
coalesced change, injected in tests — emits the settled state when the window
closes.

The trailing timer is not optional. Without it, a flap that simply *stops*
would leave its final state never emitted, so a joystick that settles into a
persistent error would show nothing.

**The window is a per-source parameter, not one global constant.** The first
draft of this spec used a single 2 s value, which was an artefact of having
only one poll loop in view. The three sources have genuinely different
cadences:

| Source | Underlying cadence | Window | Why |
|---|---|---|---|
| Hotkeys | event-driven, no poll | **0** | Nothing polls. Identity dedupe (§4.1) is the whole defence; a window would only delay an honest edge. |
| Joystick | 100 Hz poll (`manager.go` `tick`) | **2 s** | A flapping `Poll()` error toggles the edge at up to ~50 Hz. |
| Audio | 2 s poll (`manager.go:291`) | **10 s** | A 2 s window would *beat* against a 2 s poll, letting a device flapping at the poll rate through roughly every other tick. The window must be several times the source's own period to coalesce anything. |

`Notifier` takes the window per `Raise` call site (via the adapter), so a new
source in 7.3/7.4 declares its own rather than inheriting a value tuned for
someone else's loop.

**These values are designed, not measured.** See §7.

### 4.3 Worked example

A joystick failing every other poll at 100 Hz:

- Without either mechanism: ~50 notifications/second.
- With identity dedupe alone: still ~50 — each edge is a genuine
  Raise→Resolve→Raise state change.
- With both: at most one emitted update per 2 s window, settling on the true
  final state.

---

## 5. The cases

§§5.1–5.3 are the ROADMAP's 7.2 paragraph, which is binding: **these three
stay distinct.** §5.4 is the audio source added during design (§1.7).

### 5.1 Global hotkey failure — notifies **once**, carrying permission state

| | |
|---|---|
| Condition | `Registered == false` |
| Key | `hotkeys.global` |
| Severity | `error` |
| Category / Icon | `system` / `bolt` |
| Title | `Global hotkeys unavailable` |
| Body | `HotkeyStateDTO.Error` |
| Context | `PERMISSION: denied \| granted \| unknown \| not_applicable` |
| Actions | `navigate → settings` — `OPEN KEYBIND SETTINGS` (primary) |
| Resolves | `Registered == true` |

The permission value is **inside the fingerprint**, so a
`denied → granted-but-still-unregistered` transition updates the single item
rather than stacking a second one.

### 5.2 Per-binding failure — notifies **per action, naming the action**

| | |
|---|---|
| Condition | an entry in `Failed`, **only while `Registered == true`** |
| Key | `hotkeys.binding.<actionID>` |
| Severity | `warn` |
| Category / Icon | `system` / `bolt` |
| Title | `Binding not registered · <action label>` |
| Body | the reason from `Failed[actionID]` |
| Actions | `navigate → settings` |
| Resolves | the action leaves `Failed`, or `Registered` goes false |

The human label comes from `a.labelFor` (`settings.go:1093`) — already
present, no new plumbing. The **`Registered == true` guard is essential** and
mirrors the rule `Keybinds.tsx:332` already enforces: `registered == false`
implies `Failed` names every bound action, so raising nineteen of these
alongside §5.1's single global item would be the banner-printed-once-per-row
bug reborn in a new surface.

### 5.3 Joystick — one key, three states, and "unsupported" is never a failure

| Condition | Severity | Title |
|---|---|---|
| `Supported == false` | **`info`** | `Joystick input is unsupported on this platform` |
| `Supported && Error != ""` | `warn` | `Joystick unavailable — <error>` |
| `Supported && Error == ""` | — | resolved |

Key `joystick.global`, category `system`, icon `knob`. The unsupported item
carries **no action** — there is nothing to grant (CLAUDE.md records why
macOS is deliberately unsupported).

To make "must never render as an error" **structural rather than cosmetic**,
the store enforces one rule:

> **Info-severity items are raised already-read** (`Unread: false`).

They never increment the badge, never colour the bell, never toast. So
macOS's unsupported notice answers "why is there no joystick affordance?" for
anyone who looks, and nags nobody — enforced at the delivery layer, not just
in the colour of a border. This is the same instinct `connhealth`'s
`StateUnavailable` already encodes: "voice that never started is not voice
that broke" (`connhealth.go:28-35`).

### 5.4 Audio device faults — the fourth case

Not in the ROADMAP's 7.2 paragraph; added by the decision recorded in §1.7,
honouring `events.go:56`.

The adapter projects `audio.State`'s **ten** fields (`manager.go:63-70`)
down to the **four** that are faults. This projection is the whole reason
audio is safe to include:

| Field | In the fingerprint? | Why |
|---|---|---|
| `InputError`, `OutputError` | **yes** | the fault itself |
| `InputSubstituted`, `OutputSubstituted` | **yes** | audio works, but not on the chosen device |
| `Overruns`, `Underruns` | **no** | monotonic counters — the sole reason the raw event fires every 2 s during a glitch (§1.7) |
| `Running`, `Starting` | **no** | lifecycle, not a fault |
| `InputDevice`, `OutputDevice` | **no** | which device is in use is already carried by the substitution flags |

| Condition | Key | Severity | Title |
|---|---|---|---|
| `InputError != ""` | `audio.input` | **`error`** | `Microphone unavailable — <error>` |
| `OutputError != ""` | `audio.output` | **`error`** | `Audio output unavailable — <error>` |
| `InputSubstituted` | `audio.input.substituted` | `warn` | `Using the system default microphone` |
| `OutputSubstituted` | `audio.output.substituted` | `warn` | `Using the system default audio output` |

Four independent keys, not one, so a failed input and a substituted output
are separate items that resolve independently. Each resolves when its
condition clears. Body text for the substitution pair names the configured
device id that could not be opened, taken from `AudioSettings.input_device` /
`output_device`; context carries `IN USE: <resolved id>`.

Errors are **`error` severity, so they toast and sound** (§3.8): a dead
microphone in a voice-comms client is the most urgent thing this channel
carries, and today it is visible only if the user happens to open
Settings → Audio. Substitution is `warn` — audio still works, so badge and
bell are proportionate.

Coalescing window **10 s** (§4.2), not the joystick's 2 s: a 2 s window
against a 2 s poll would beat rather than coalesce.

The no-backend case (`main.go:225`, where `NewMalgoBackend` failed and no
Manager exists) arrives as an `AudioStateDTO` with **both** error fields set,
so it raises `audio.input` and `audio.output` together. That is correct —
both directions genuinely are dead — and is the case an adapter hung only off
`OnState` would have missed entirely (§3.3).

---

## 6. Frontend

### 6.1 The popout

| File | Purpose |
|---|---|
| `frontend/notifications.html` | window shell |
| `frontend/src/notifications.tsx` | React root, inside `React.StrictMode` |
| `frontend/src/windows/notifications/NotificationsApp.tsx` | header, filter, MARK ALL READ, CLEAR ALL, empty state |
| `frontend/src/windows/notifications/NotifRow.tsx` | one row |
| `frontend/vite.config.ts` | `notifications` input entry |
| `internal/app/windowfactory.go` | `defaultGeometry` + `windowURL` arms |

Ported from `design/vcs/project/screens/misc.jsx:292-402`. Default geometry
follows the prototype's popout proportions, in the same style as `comms`.

`NotifRow` renders the **generic `Item`** — category-coloured left border,
unread dot, expandable body, ordered context table, action buttons. There is
**no branch anywhere on what produced the item**. A reviewer should be able
to grep the `windows/notifications/` directory for "hotkey" and find nothing.

### 6.2 Reaching a user who never opens the popout

Replacing an always-visible inline banner with a popout would otherwise be a
regression: today a failure is visible the moment you open Keybinds; after
7.2 it would be invisible unless you open a window you have no reason to
open. Three surfaces prevent that, all with CSS already ported:

- **`TopBar.tsx`** — the `notifications` launcher stops being a disabled
  placeholder: `api.toggleWindow("notifications")` plus an unread `.badge`.
- **`StatusBar.tsx`** — the inert ALERTS bell gains the same toggle, the
  `.has-unread` class and an `.sb-bell-count`.
- **`ToastHost.tsx`** — the prototype's stack (`app.jsx:17-31`),
  **error severity only**, auto-dismissing at 6.5 s (the prototype's value),
  mounted in the **main window only**.

Comms deliberately gets no toast host: it is a narrow radio panel and a toast
stack would cover the radios. Recorded as a limit, not an oversight.

The **sound** (§3.8) is fired by the Go store, not by `ToastHost`, so it
reaches a user whose main window is closed or backgrounded — but it fires on
exactly the same items the toast does. One rule, two surfaces: error toasts
and sounds, warn does neither, info does nothing at all because it is raised
already-read (§5.3). So none of the four surfaces ever fires for macOS's
"joystick unsupported".

### 6.3 State

`shared/store/notifications.ts` (Zustand, mirroring the Go `Snapshot`) and
`shared/store/useNotificationsSync.ts` — hydrate once, subscribe to
`notifications:changed`, same contract as `useSettingsSync`, mounted once per
**window root**. Main and Notifications only; Comms has no notification
surface.

### 6.4 What changes in `Keybinds.tsx`

- The `!hotkeys.registered` block (lines 429-473) loses its **announcement**
  half — that is now §5.1's notification.
- Its **remediation** half — the macOS Accessibility explanation and
  `GRANT ACCESS` / `OPEN SETTINGS` / `RE-CHECK` with their `requested` /
  `promptSpent` local state — is extracted to
  `sections/keybinds/PermissionCard.tsx` and rendered on
  `hotkeys.permission === "denied"` rather than on `!hotkeys.registered`, so
  it cannot vanish while the grant is still missing. The
  `grantedButUnregistered` line ("Accessibility is granted — restart VCS")
  moves with it.
- The `joystick.supported && joystick.error` block (lines 477-485) is
  **deleted**; it is now §5.3's notification.
- The **per-row `failedReason` text (line 345) stays.** It is row-contextual
  rather than a banner, the ROADMAP names only "the inline banner", and
  deleting it would force the user to cross-reference a popout against a
  table row.

`Keybinds.tsx` is 542 lines today; the extraction is also what keeps it from
growing past the 800-line ceiling in the user's global coding-style rules.

---

## 7. Where this design rests on behaviour nobody has observed

Required by the decomposition spec §4. Phase 6 set the precedent of recording
designed-from-source values as such.

1. **The joystick flap has never been seen.** The Windows (DirectInput) and
   Linux (evdev) backends have only ever been cross-compiled, never executed
   (CLAUDE.md, Phase 3.5). The 100 Hz poll loop and the per-tick error-edge
   notify are read from `internal/joystick/manager.go`; the flap they imply is
   inferred. **The 2 s coalescing window in §4.2 is a guess sized to that read
   loop, not a measurement.**
2. **What a real audio device failure looks like is unknown** — but this is
   now a *narrow* gap, not the open-ended one the first draft claimed. The
   emission *rate* is measured from source (§1.7): bounded at one per 2 s
   poll tick, collapsing to one notification under the §5.4 projection. What
   remains unobserved is the *content*: whether malgo's error string for an
   unplugged USB microphone is one stable value or varies per attempt, and
   whether a hot-unplug yields a clean `Substituted` transition or flaps
   through an error state first. Worst case is one notification per distinct
   error string — a constant to tune, not a redesign. **The 10 s audio window
   is likewise sized to a read poll interval, not measured.**
3. **macOS Accessibility denial has never been exercised on hardware.**
   Phase 3's manual checklist is unrun. The permission transitions driving
   §5.1's fingerprint — and `promptSpent`'s "the one-shot is spent" inference
   — are read from source, not observed.
4. **The notification sound has never been heard, and cannot be.** §3.8
   builds the full playback path, but `notify_alert` has no sample and the
   pack must not be substituted, so **no human can confirm by ear that any of
   it works**. The engine is verifiable only by unit test — that
   `PlayNotification` queues into the pool, that the pool mixes into
   `notifBuf`, and that the notification bus gain is applied. That the
   resulting sound is audible, correctly levelled against voice, and not
   startling over a live transmission is **entirely unverified** and joins
   the manual checklist.
5. **Nothing in this phase can be field-verified in the environment that
   writes it.** 7.2 adds its own manual checklist under
   `docs/superpowers/plans/`, joining the six already unrun.

---

## 8. Out of scope

| Excluded | Why / where it goes |
|---|---|
| The `notify_alert` WAV itself | A dependency on the user, not work. §3.8: the pack "is not something to substitute", so no placeholder tone is synthesised. The ask grows from nine files to ten. |
| Per-category Toast / Sound / Silent settings table | Prototype `settings.jsx:245`. 7.2 ships one global `play_notification_sounds` toggle instead; a per-category matrix needs sounds that exist to mean anything. |
| Per-severity sounds | §3.8: sound follows the toast, so one slot. A distinct warn sound can be added later without touching the engine. |
| Persistence across restart | 7.3 owns local persistence. §3.1 explains why 7.2 loses nothing. |
| Server-pushed notifications | PROTO_GAPS #8. No proto change in this phase. |
| Radio profiles, transmission history | 7.3 |
| Ship Mode, Fleet C2, Messages popouts | 7.4 — they *consume* this channel |

---

## 9. Definition of done

1. `internal/notify` exists as a pure package with `Post` / `Raise` /
   `Resolve` / `MarkRead` / `MarkAllRead` / `Dismiss` / `Clear`, an injected
   clock and an injected `OnChange`, and no import of `internal/app`,
   `internal/hotkeys`, `internal/joystick` or `internal/audio`. Grepping it
   for "hotkey", "joystick" or "audio" returns nothing.
2. Identity dedupe: rebinding nineteen actions with one unregisterable chord
   produces exactly **one** notification, proven by test.
3. Flap suppression: a 50 Hz Raise/Resolve flap produces at most one emit per
   window, and a flap that stops still emits its settled state — both proven
   under an injected clock. The window is a **per-source** parameter
   (hotkeys 0, joystick 2 s, audio 10 s), not one constant.
4. `Post` is never deduped and never coalesced, proven by test.
5. Dismissing a keyed item whose condition still holds does **not** let the
   next identical `Raise` resurrect it, and a changed fingerprint or a
   `Resolve` does clear that suppression — both proven by test (§4.1).
6. The Notifications popout opens from both the TopBar launcher and the
   status-bar bell, persists geometry through the existing Registry, and
   renders every field of a generic `Item`.
7. The unread count is consistent across the main window and the popout, and
   info-severity items never contribute to it.
8. All three ROADMAP cases behave as §5 specifies, including macOS
   "unsupported" never rendering as an error and never reaching the badge,
   bell or toast.
9. Audio device faults raise as §5.4 specifies, from **both** emit sites —
   including the no-backend path at `main.go:225`, which an adapter hung
   only off the Manager's `OnState` would miss. `Overruns`/`Underruns`
   movement provably produces **no** notification, proven by test.
10. Phase 3's inline Keybinds banner is gone; `PermissionCard` renders on
   `permission === "denied"`; the per-row failure text remains.
11. `ToastHost` has a StrictMode test **and** a real-unmount control proving
   its dismiss timer is scheduled once and cleared once.
12. Every clickable non-button element added or touched carries `role`,
    `tabIndex` and Enter/Space handling via the shared `activatable` helper
    lifted out of `StatusBar.tsx:71` — TopBar's launcher (which fixes a
    pre-existing gap), the ALERTS bell, and `NotifRow`'s header.
13. The notification sound engine is wired end to end: `PlayNotification`
    queues into a second voice pool, the pool mixes into `notifBuf`, the
    notification bus gain applies, and `play_notification_sounds` gates it.
    Error-severity items play; warn and info do not. Proven by unit test —
    **`notify_alert` has no sample, so nothing is verifiable by ear (§7 item 4)**.
14. `events.go:56`'s comment is confirmed accurate rather than amended: all
    three sources are now absorbed.
15. Automated suite green: `go build` / `go vet` / `go test -race ./...` with
    `-tags purego`, `npx tsc --noEmit`, `vitest`, frontend production build.
16. A manual verification checklist is written to
    `docs/superpowers/plans/` covering everything §7 says is unobserved.
17. `docs/ROADMAP.md`'s 7.2 row is updated; PROTO_GAPS #8 is confirmed
    unchanged.

---

## 10. Testing

`internal/notify`, table-driven under an injected clock:

- the 38-emission rebind collapsing to one notification;
- a 50 Hz flap collapsing to one emit per window;
- a flap that stops still emitting its settled state;
- `Post` never coalesced, never deduped;
- dismiss-then-identical-raise staying suppressed, and both suppression
  clears (differing fingerprint, and `Resolve`) working;
- resolve-then-recur producing a fresh unresolved item, not a revived one;
- info items never counting toward unread;
- the 200-item cap dropping oldest;
- concurrent `Raise` from two goroutines under `-race`.

`internal/app`: adapter tests driving `HotkeyStateDTO` / `JoystickStateDTO`
through the funnels and asserting the resulting items, including §5.2's
`Registered == true` guard.

Audio adapter (`main.go`'s `notifyAudioState`, exercised through a test
seam rather than `main`): the §5.4 projection — four independent keys raising
and resolving independently; **`Overruns`/`Underruns` movement producing no
notification at all**, which is the single most important audio test; and the
no-backend DTO (both error fields set) raising both error keys.

`internal/audio`, the sound engine (§3.8): `PlayNotification` queueing into
the notification voice pool; the pool mixing into `notifBuf`; the
notification bus gain applying; an unavailable id being a silent no-op; and
`PlayNotification` on a stopped Manager dropping rather than lingering — each
mirroring the existing `PlayEffect` / `sfx` tests. **These prove the path,
not the sound: `notify_alert` has no sample, so nothing here is verifiable by
ear (§7 item 4).**

Frontend (`vitest`): `NotifRow` rendering each severity and an action;
`NotificationsApp` filter, mark-all-read, clear-all and empty state;
`ToastHost`'s StrictMode pair (DoD 11); badge and bell counts; keyboard
activation of all three clickable non-buttons.

---

## 11. Toolchain

Per the decomposition spec §5, unchanged. Restated only where 7.2 differs:

- Branch `feat/phase-7-2-notification-channel` off `main` at `ed35789`.
- Every Go invocation carries `-tags purego` with
  `GOCACHE=$TMPDIR/vcs-gocache`.
- Typecheck with `(cd frontend && npx tsc --noEmit)` — the `npx --prefix
  frontend` form exits 0 without checking anything.
- `frontend/bindings/` is gitignored and goes stale across branch switches.
  **This phase adds five new `App` methods (§3.6), so regenerating with
  `wails3 generate bindings -ts -f "-tags purego" -clean=true` is a required
  step, not a staleness workaround.**
- gopls is unreliable in this repo; never act on its diagnostics.
- `internal/notify` uses timers, not sockets, so it needs no sandbox
  exception. `internal/voice` still does.
- **No `Co-Authored-By` trailers** — attribution is disabled globally per
  `~/.claude/rules/common/git-workflow.md`. Phase 7.1 added them by mistake
  across ~15 commits.
