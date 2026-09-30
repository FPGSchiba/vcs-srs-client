# Phase 7.3 — Local persistence: radio profiles + transmission history

**Date:** 2026-09-30
**Status:** design approved; implementation plan not yet written
**Branch:** `feat/phase-7-3-local-persistence`
**Parent:** [`2026-09-28-vcs-client-phase-7-decomposition-design.md`](./2026-09-28-vcs-client-phase-7-decomposition-design.md)

Phase 7.3 gives the client two things it has never had: a way to save and
restore a radio setup as a shareable file, and a log of who transmitted on
what. The ROADMAP row calls them "radio profile load/save/import/export" and
"transmission history view".

**This phase is wider than that row.** At the user's explicit direction
during design, a radio profile also carries a **fully customizable Comms
layout** — per-radio block sizes in pixels and the Comms window's own size —
which means the Comms popout gains a resize/reorder editor it does not have
today. That is new UI work in a window the ROADMAP's 7.3 row does not
mention. It is recorded here as a deliberate widening rather than folded in
silently; CLAUDE.md requires explicit approval to cross a phase boundary and
it was given.

---

## 1. What was verified, and what the documents got wrong

Everything in the decomposition spec's §3 was re-derived from source in both
repos on 2026-09-30 rather than inherited. Prior sub-phases each found a
document asserting something untrue (7.1: an `R4` `session.json` that never
existed; 7.2: a spec contradicting itself on the audio fingerprint, and three
documents claiming a unit-tested path with zero coverage), so the same
suspicion was applied here.

### 1.1 Confirmed

**`voice:rx_active` does not exist.** `docs/PROTO_GAPS.md:112` says history is
"populated client-side from observed `voice:rx_active` events". A repo-wide
search finds four hits, all of them documents citing the name; nothing in
`internal/events/events.go` declares it and nothing emits it.

**The frequency-as-integer constraint is real, at both ends.**
`vcs-srs-server/state/server.go:211` is `if radio.Frequency == frequency` —
exact `float32` equality — and `vcs-srs-server/voice/protocol.go:287` derives
the comparand as `float32(p.Frequency) / 1000.0`. The client's
`voice.KHz.MHz32()` (`internal/voice/freq.go:55`) is that expression
character-for-character, with a comment forbidding float64 intermediates.
`config.Radio.FrequencyKHz` is `uint32`. A profile format that stored MHz as
a float could therefore drop a radio out of range with nothing logged
anywhere.

**Profiles and History are main-window nav screens, not popouts.**
`frontend/src/shared/components/NavRail.tsx:20-21` lists `history`
("Transmission Log") and `profiles` ("Radio Profiles");
`frontend/src/windows/main/MainApp.tsx:117-121` routes `home`, `players` and
`settings` and falls through to `<Placeholder/>` for everything else.

**The design prototype is 688 lines** in one file holding four screens, as
recorded.

### 1.2 Corrections

**There is no end-of-transmission marker anywhere on the wire.** The
decomposition spec framed the RX problem as a missing event name. It is
worse than that: nothing on either side signals that a transmission has
ended. `internal/voice/session.go:616` counts inbound BYE packets only so a
server that starts sending them is "visible rather than mysterious" — the
server never sends one. The client sends BYE exactly once, from `Close()`
(`session.go:397`), i.e. at session teardown, not per transmission. The only
existing end-of-stream signal is `rxIdleTimeout = 5 * time.Second`
(`rx.go:43`) reaping an idle stream, which is an audio-path lifetime, not a
transmission boundary. §7 is the answer.

**Prototype paths in the documents are wrong.** The prototype lives at
`design/vcs/project/`, not `design/vcs/`: the screens are
`design/vcs/project/screens/misc.jsx` and the tokens are
`design/vcs/project/styles.css`. The decomposition spec's §3.1 and
CLAUDE.md's documentation map both omit the `project/` segment.

**CLAUDE.md's `make proto` does not exist.** There is no Makefile. The repo
uses `Taskfile.yml`, whose `proto` task runs `buf generate` (`buf.gen.yaml`,
remote plugins, so it needs network). `buf` was not installed in this
environment; `go build` fails on a fresh checkout with *"no required module
provides package .../srspb"* until it is.

**PROTO_GAPS §7's "status bar history affordance" does not exist.**
`frontend/src/shared/components/StatusBar.tsx` has one navigation call,
`onNavigate?.("server")`. There is no history control there.

**The prototype's profile is wider than the client, in ways this phase does
not adopt.** `ScreenProfiles` stores `layout` as one of three hardcoded CSS
grid presets (`comms.jsx:44-50`: `2×2` / `POWER` / `STRIP`) with no per-block
sizing at all, plus an `overlay` block and per-radio `enc`/`key`. The
per-block sizing in this phase is therefore an *improvement on* the design
handoff, not a port of it. Encryption (`enc`/`key`) is PROTO_GAPS #1 and is
not adopted — see §4.2.

**The prototype's history table has a Replay column** backed by a `rec` flag.
Recording is PROTO_GAPS #9 and does not exist. The column is dropped rather
than rendered permanently dead — see §8.3.

### 1.3 Also true

`internal/config/paths.go`'s `AppDataDir` doc comment already promises the
directory stores "configs, profiles, logs, and session state". Profiles are
the second of those four and have never existed. This phase makes that
sentence true.

PR #34 (Phase 7.2) is merged: `46a3dea` is on `main`.

---

## 2. Decisions

Each was put to the user during brainstorming; the alternatives considered
are recorded so a later session can see what was weighed rather than
re-opening the question.

| # | Decision | Rejected alternatives |
|---|---|---|
| D1 | A received transmission ends after a **dedicated 500 ms idle threshold**, evaluated in the existing `rxService` sweep. `rxIdleTimeout` (5 s) is untouched. | Reuse the 5 s reap (every duration inflated by 5 s, every row 5 s late, two sentences merged); log start only, no duration (drops the column that makes a log useful). |
| D2 | A profile stores a **fully customizable layout**: per-radio block `{w,h}` in px plus the Comms window `{w,h}`, as a **resizable flow grid** (wrap, reorder). | Free-form absolute canvas (needs overlap, clamping and shrink policies; blocks can be dragged off-screen); layout stored but inert (a control that changes nothing). |
| D3 | The profile's window size **applies on load**; a later manual resize updates `windows.json` as today **and marks the profile dirty**. `windows.json` stays authoritative for "what size is it right now", including at cold start. | Store-but-never-apply (the layout you get back is not the one you saved); drop window size entirely (same, with no record of the width it was designed for). |
| D4 | **Two** recovery controls: REVERT (only when dirty with an active profile — re-read the file and re-apply) and RESET (always — built-in default layout, marks dirty). | REVERT only (no escape with no profile loaded); RESET only (two steps where revert is one). |
| D5 | The log records **RX and own TX**, with **no Replay column**. | RX + TX with a permanently dead Replay column; RX only (can't answer "when did I last call it in"). |
| D6 | A **capped ring of 2000 entries**, persisted to `AppDataDir()/history.json`, flushed atomically on a ~5 s debounce and on shutdown. | In-memory only (contradicts the ROADMAP row; no after-action review); 24-hour time window (file size varies with how busy ops were; entries vanish by clock). |
| D7 | Profiles default to `AppDataDir()/profiles`, overridable by a new `profiles_dir` config key, with BROWSE and OPEN. | Default under `Documents/Vanguard/Profiles` (a second storage root, resolved per-OS, separate from everything else the client persists); fixed and non-configurable (drops "user-chosen dir" from the ROADMAP row). |
| D8 | **`config.toml` owns the live state; a profile is a snapshot of it.** | Layout in its own file beside `windows.json` (a profile load would have to write two files with no shared lock — a crash between them leaves radios from profile B with layout from profile A); the active profile file *is* the live state (deletes D4's dirty/REVERT design outright, and every frequency tune rewrites a user-visible file). |

---

## 3. Architecture

Two new pure packages, plus one binding file each in `internal/app`, plus one
callback in `internal/voice`. The shape follows `internal/notify`: the store
is Go-owned and the frontend is a view over it.

```
internal/profile/      .vcs.json document: types, codec, dir listing,
                       atomic write, validation.
                       Imports: stdlib only.

internal/history/      fixed-capacity ring, JSON codec, debounced atomic
                       flush. Takes fully-resolved entries.
                       Imports: stdlib only.

internal/app/profiles.go   bindings; maps profile <-> config; applies a
                           loaded profile; computes dirty.
internal/app/history.go    the two sources; GUID->callsign and
                           frequency->channel resolution; emits.

internal/voice/rx.go       + per-stream started/ended, + OnRX delivery.
internal/voice/session.go  + Options.OnRX, + its delivery goroutine.
```

### 3.1 Why `internal/profile` defines its own types

`config.Radio` carries only `toml:` tags. Marshalling it as JSON would emit
Go field names (`FrequencyKHz`, `IsIntercom`), and the mapping between the
file format and the config struct would be invisible — a rename in
`internal/config` would silently change a file format users have on disk and
share with each other. `internal/profile` therefore declares its own
`Radio`/`Layout`/`Block` types with explicit `json:` tags, and
`internal/app/profiles.go` owns the ~10-line mapping in both directions.

This is the same discipline `internal/config` already applies to
`Audio`/`Voice`/`Keybinds`: a raw-values layer that does not import the
package that interprets it.

### 3.2 Why both stores are Go-owned

The history event source is in Go and the ring must outlive any one window.
Profiles must be applied atomically into `config.toml` under the existing
`settingsBackend.writeMu`, which only Go holds. Neither can live in Zustand.

---

## 4. Profile file format

Files are named `<slug>.vcs.json`.

```json
{
  "schema_version": 1,
  "name": "Fleet Op — Stanton",
  "description": "Default loadout for Stanton ops. Discovery wing primary.",
  "author": "FPGSchiba",
  "created_at": "2026-09-30T18:02:11Z",
  "modified_at": "2026-09-30T21:14:03Z",
  "radios": [
    { "id": 1, "name": "Fleet Common", "frequency_khz": 118500,
      "enabled": true, "is_intercom": false },
    { "id": 2, "name": "Gunners Net", "frequency_khz": 122750,
      "enabled": true, "is_intercom": false }
  ],
  "selected_radio_id": 1,
  "layout": {
    "window": { "w": 540, "h": 720 },
    "blocks": [
      { "radio_id": 1, "w": 516, "h": 180 },
      { "radio_id": 2, "w": 253, "h": 120 }
    ]
  }
}
```

### 4.1 The frequency invariant

`frequency_khz` is an integer, always. The load path is

```
profile.Radio.FrequencyKHz (uint32)
  -> config.Radio.FrequencyKHz (uint32)                          [stored]
  -> voice.KHz(...).MHz32() -> srspb.Radio.Frequency (float32)    [wire]
```

and it never passes through `RadioDTO.Frequency` (float32 MHz, the
frontend's form) on the way in. The existing frontend tuning path
(`UpdateRadioInfo` → `persistRadios` → `voice.KHzFromMHz32`) is unchanged
and unaffected; this is a second, integer-only entry point beside it.

### 4.2 Field notes

- **`blocks` is ordered, and that order is the flow order.** There is no
  separate index field to keep consistent with the array.
- **A radio in `radios` with no entry in `blocks`** is appended at the end
  with the default block size. A profile hand-edited to add a radio still
  loads.
- **A block whose `radio_id` matches no radio** is dropped on load.
- **`enc` / `key` are not stored.** Per-radio encryption is PROTO_GAPS #1
  and is not on the wire; a stored key would be a setting the UI lets you
  change that affects nothing.
- **`schema_version` newer than this build is refused**, with a notification
  naming the file and the version. It is never partially applied.

---

## 5. Live state, and what a profile is

`config.toml` is authoritative for the live setup. It already holds `Radios`
and `SelectedRadioID`; this phase adds:

```toml
active_profile = "C:\\Users\\janne\\AppData\\Roaming\\VCS\\profiles\\fleet-op.vcs.json"
profiles_dir   = ""   # empty = AppDataDir()/profiles

[comms_layout]
  window_w = 540
  window_h = 720
  [[comms_layout.blocks]]
    radio_id = 1
    w = 516
    h = 180
```

A profile file is a serialization of exactly `Radios` + `SelectedRadioID` +
`[comms_layout]` plus metadata. Nothing else.

### 5.1 Operations

**LOAD** — read → validate → write radios, selected id and layout into
config in **one** `config.Save` under `settingsBackend.writeMu` →
`Registry.SetBounds("comms", …)` → push radios to the server through the
existing `pushPersistedRadios` path → set `active_profile`.

One config write means there is no half-applied state to recover from. The
server push comes **last**, deliberately: `persistRadios` already documents
the rule that a failed local persist must not be masked by a successful
server round trip the client itself cannot then act on.

**SAVE** / **SAVE AS** — capture live config into a document; write
atomically (write-temp + rename, the pattern `windowstate.Save` uses);
`modified_at` = now.

**DIRTY** — computed on read by comparing live config's radios, selected id
and layout against the active profile's file contents. It is **never
stored**. There is nothing to keep in sync and nothing that can go stale
across a crash, an external edit, or a config change made by another code
path.

With **no active profile there is no dirty state** — there is nothing to
compare against, so the dot is not rendered and REVERT is not offered. An
ad-hoc setup is not "unsaved", it is simply not a profile.

**REVERT** — rendered only when dirty *and* a profile is active. Re-reads
the file from disk and runs LOAD.

**RESET** — always rendered. **Affects the layout only**: it restores the
built-in default block size and window 540×720 (the `windowfactory.go:23`
default) and marks dirty. It does **not** touch radios or the selected
radio — a control that silently wiped your tuned frequencies while claiming
to reset a layout would be a trap. This is the only escape when no profile
is loaded.

**RENAME** — edits `name` and `description` in place (the prototype's pencil
button). The filename is left alone: renaming the display name must not
break a path someone else has a copy of, or that `active_profile` points at.

**DELETE** — removes the file. If it was the active profile,
`active_profile` is cleared and **live config is left untouched** — deleting
a saved copy must not wipe the radios you are currently using.

**IMPORT** / **EXPORT** — native file dialogs
(`application.OpenFileDialog` / `SaveFileDialog`, present in Wails
v3.0.0-beta.22 at `pkg/application/dialogs.go`), accepting any path. Import
copies into `profiles_dir`; export writes a copy out. Neither changes the
active profile.

**BROWSE** — `OpenFileDialog().CanChooseDirectories(true)`
(`dialogs.go:217`), writing `profiles_dir`. **OPEN** reveals the folder in
the OS file manager.

A cancelled dialog is a normal outcome, not an error.

---

## 6. Layout model and editor

The Comms popout body becomes a `flex-wrap` container; each radio is a
`RadioCard` inside a sized block.

- **Resize** — drag the block's corner. Bounds: min 240×96, max the body's
  content width.
- **Reorder** — drag the block; the flow order is the `blocks` array order.
- **Re-wrap** — resizing the OS window re-flows rows. Nothing clips, nothing
  overlaps. This is the entire reason the flow grid was chosen over a
  free-form canvas: there is no clamping rule, no collision rule and no
  shrink policy to get wrong, because none of those states is reachable.
- **Persistence** — `[comms_layout]` written on a ~500 ms debounce, so a
  drag is not 60 config writes per second.

The editor lives in the Comms window only. The Profiles screen's
`LayoutPreview` SVG renders a schematic from the stored block sizes, so the
prototype's Layout column survives with real data behind it instead of one
of three preset names.

Two toolchain constraints are honoured in the same commits that introduce
the handles, not afterwards:

- Drag handles are non-button elements carrying pointer handlers, so each
  needs `role="button"`, `tabIndex` and Enter/Space handling or SonarCloud's
  `typescript:S1082` fails the PR gate (it caught exactly this in Phase 6).
- Both window roots render inside `React.StrictMode`. Any effect that
  registers pointer listeners needs a StrictMode test **plus** a control
  proving the real-unmount path still fires once — a bare-rendered suite hid
  a total keybind-capture break through two phases.

---

## 7. Transmission history pipeline

### 7.1 RX source

`voice.Options` gains:

```go
// OnRX reports one completed received transmission.
OnRX func(RXEvent)

type RXEvent struct {
    Sender uuid.UUID
    Freq   KHz
    Start  time.Time
    End    time.Time
}
```

`rxStream` gains `started time.Time` and `ended bool`. The existing
`rxService` sweep on the decode goroutine gains one branch:

```
for each stream:
    retune(); topUp()
    if !ended && now - lastSeen >= historyIdle (500ms):
        ended = true
        queue RXEvent{started, lastSeen}
    if now - lastSeen >= rxIdleTimeout (5s):
        reap()                                   // unchanged
```

**A packet arriving on an `ended` stream before the 5 s reap clears the flag
and starts a new transmission.** Two sentences 600 ms apart are two rows,
which is the correct reading, and it falls out of the same flag rather than
needing its own mechanism.

`rxIdleTimeout` is deliberately not reused and not changed: it is an
audio-path lifetime governing when a decoder and jitter buffer are released,
and re-tuning it to suit a log would change voice behaviour to serve a view.

**A stream still open when the session closes is ended at close time**, with
`End = lastSeen`, so a transmission in flight during a reconnect produces
one truthful row rather than vanishing. Its event is queued before the
delivery goroutine stops; anything that cannot be queued is counted as an
overflow drop (§9), never blocked on.

**Delivery does not reuse `deliverLoop`.** That loop (`session.go:463`)
drains a queue typed to state changes and **returns permanently** on
`StateClosed`. RX events get their own buffered channel with a non-blocking
send that counts drops — the discipline `Session.emit` (`session.go:625`)
already uses for control events — drained by a dedicated goroutine. The
decode goroutine therefore never runs a consumer callback, and a slow
consumer costs dropped log rows rather than dropped audio.

### 7.2 TX source

`internal/app/voice.go` installs the resolved TX target set at exactly three
places, all under `voiceState.mu`: `:521` (press), `:546` (release) and
`:643` (the idle clear). All three are routed through one new
`installTXTargetsLocked` helper that diffs the new set against the
previously installed one — a frequency entering starts a transmission, a
frequency leaving ends one.

This yields one row per frequency per transmission, which is what actually
went on the wire when one PTT resolves to two radios, and it requires no
change inside `internal/voice`. Hooking the three call sites individually
would leave the fourth — whichever one a future phase adds — silently
unlogged.

Own rows carry `own: true` and take their sender name from the local
client's own callsign (`state.Store.Snapshot().Self.GetName()` — `Self` is a
`*srspb.ClientInfo` and is nil before SyncClient lands, so the getter, not
the field), so the log reads uniformly whoever was talking; the History
screen accents them the way the prototype's `self: true` rows are accented.
If self is not yet populated the
row still records — with an empty sender rather than a dropped entry, since
the frequency and duration are the parts worth keeping.

### 7.3 Resolution

**Sender GUID → callsign** goes through `state.Store.Client(guid)` →
`ClientInfo.Name` (`srs.proto:323`).

There is a hazard here. The store is keyed by the server's GUID **string**,
while voice carries a `uuid.UUID` (`App.voiceDialInputs` parses the store's
own `SelfGUID` with `uuid.Parse`, `voice.go:694`). `uuid.Parse` accepts
uppercase, brace-wrapped and unhyphenated forms that `UUID.String()` does
not round-trip to, so a naïve `sender.String()` lookup can silently miss and
render a blank sender with nothing logged. `internal/app/history.go`
therefore keeps a normalized `map[uuid.UUID]string` index, rebuilt from the
snapshot on client change, instead of string-keying the lookup.

This is reasoned from `uuid.Parse`'s accepted forms, not from an observed
server response — see §11.

**Frequency → channel name** comes from `config.Radios`. Global channels are
accepted without a tuned radio (`rxContext.global`, `rx.go:520`), so a row
can legitimately have no local radio name; those render the frequency alone
rather than a fabricated channel.

### 7.4 Storage

```json
{
  "schema_version": 1,
  "entries": [
    { "at": "2026-09-30T21:15:51Z", "sender": "Dabble",
      "guid": "…", "freq_khz": 118500, "radio": "Fleet Common",
      "dur_ms": 3200, "own": false }
  ]
}
```

`AppDataDir()/history.json`. Ring capacity 2000, oldest evicted. Flushed
atomically (write-temp + rename) on a ~5 s debounce and on shutdown. Up to
~5 s of the newest rows are lost on a hard kill; that is the accepted cost
of not rewriting a ~240 KB file several times a second.

---

## 8. Events, bindings, frontend

### 8.1 Events

```go
EventHistoryAppended = "history:appended"   // ONE entry
EventHistoryCleared  = "history:cleared"
EventProfileState    = "profile:state"      // active path + dirty
```

`history:appended` carries **one entry, not a snapshot**. This is a
deliberate divergence from `notifications:changed`, which broadcasts the
full `notify.Snapshot` — that works because `notify.DefaultCap` is small,
whereas 2000 entries at ~120 B is ~240 KB per emit, at up to several
transmissions per second. Hydration is `App.GetHistory()` on mount, the same
shape `GetNotifications()` already has.

History is main-window only; no popout consumes it. `profile:state` is
broadcast to every window because the Comms popout renders the dirty dot and
the REVERT/RESET controls.

### 8.2 Bindings

```
ListProfiles() []ProfileSummaryDTO      GetHistory() []HistoryEntryDTO
LoadProfile(path) error                 ClearHistory() error
SaveProfile() error                     ExportHistoryCSV() error
SaveProfileAs(name, desc) error
RenameProfile(path, name, desc) error
DeleteProfile(path) error
ImportProfile() error
ExportProfile(path) error
BrowseProfilesDir() error
OpenProfilesDir() error
GetProfileState() ProfileStateDTO
SetCommsLayout(LayoutDTO) error
```

### 8.3 Frontend

`ScreenProfiles` and `ScreenHistory` are ported from
`design/vcs/project/screens/misc.jsx` into
`frontend/src/windows/main/screens/`, replacing the `<Placeholder/>`
fallback at `MainApp.tsx:121` with two real arms.

Deviations from the prototype, all of them forced by what exists:

- The Profiles table's **Layout column survives with real data** — the
  `LayoutPreview` SVG is driven by stored block sizes rather than one of
  three preset names.
- The History table's **Replay column is dropped**; recording is
  PROTO_GAPS #9.
- The History screen keeps the prototype's channel filter, time segmented
  control, search box and EXPORT CSV button.

The Comms window gains the resizable block wrapper around `RadioCard`, the
dirty dot, and the REVERT/RESET controls in its chrome.

---

## 9. Error handling

Every failure routes through `internal/notify` — the channel Phase 7.2 built
for exactly this, and this phase's first consumer outside 7.2's own three
sources.

| Failure | Behaviour |
|---|---|
| Malformed or unreadable profile | Error notification naming the file and the parse error. Not loaded, not partially applied. |
| `schema_version` newer than this build | Error notification naming file and version. Refused outright. |
| `profiles_dir` cannot be created or listed | Error notification naming the path; the screen renders an empty directory rather than a blank page. |
| `config.Save` fails during LOAD | Error notification; live config unchanged, `active_profile` unchanged, nothing pushed to the server. |
| History flush fails | `Raise`d **once** on a stable key and `Resolve`d on the next success — never one notification per 5 s tick. |
| Native dialog cancelled | Normal outcome. No notification, no error. |
| RX event channel overflow | Counted, logged at Warn. Log rows are dropped; audio is not. |

---

## 10. Testing

`superpowers:test-driven-development` throughout — tests first.

**`internal/profile`** (pure, full coverage): round-trip encode/decode;
unknown fields ignored; `schema_version` rejection; radio-with-no-block
appended; block-with-no-radio dropped; atomic write leaves no partial file
when the rename fails; directory listing with a non-profile file present.

**`internal/history`** (pure, full coverage): ring eviction at capacity;
snapshot ordering; clear; load of a missing file yields an empty log, not an
error (the discipline `windowstate.Load` already uses); debounce coalescing
via an injected clock; flush-on-shutdown.

**The frequency invariant gets its own test.** Assert that
`profile → config → voice.KHz.MHz32()` is bit-identical to the server's
`float32(khz) / 1000.0` across the radio band, because that is the one
failure in this phase that is silent on both sides.

**`internal/voice`**: fake-clock test of the 500 ms end detection, the
re-start-after-end case, and that `rxIdleTimeout` reaping is unchanged. This
package needs `dangerouslyDisableSandbox` — the sandbox blocks `bind(2)` and
its tests fail with "listen udp: operation not permitted".

**`internal/app`**: LOAD applies atomically and pushes to the server last;
dirty computation; DELETE of the active profile leaves live config intact;
the normalized GUID index resolves a sender whose store key is
non-canonical; a global-channel row renders no channel name.

**Frontend**: `vitest` for both screens; StrictMode tests plus real-unmount
controls for the drag handlers.

Native file dialogs are **not** covered — see §11.

---

## 11. What this design assumes and nobody has observed

Per the decomposition spec's §4, every sub-phase must state where it depends
on real-world behaviour nobody has watched. This will be the **eighth**
unrun manual checklist in `docs/superpowers/plans/`, joining Phases 3, 3.5,
4, 5, 6, 7.1 and 7.2.

- **The 500 ms RX idle threshold is a guess, not a measurement.** It must
  sit above a real talker's worst inter-packet gap (nominally 20 ms) and
  below a natural speech pause. Nobody has ever received voice from a real
  peer on this client, so the true jitter distribution is unknown. Too low
  and one sentence splits into several rows; too high and two exchanges
  merge into one. It is a named constant so a field test can move it without
  touching logic.
- **Native file dialogs are untestable in CI** and have never been run on
  any OS from this codebase. BROWSE, IMPORT, EXPORT and OPEN are all
  unverified, on all three platforms.
- **The GUID-format hazard is reasoned from `uuid.Parse`'s accepted forms**,
  not from an observed server response. The normalized index makes it moot
  either way, which is why the design routes around it rather than measuring
  it first.
- **Cross-client interop stays blocked.** `VNGD-SimpleRadioStandalone`
  PR #253 sends no voice secret in its HELLO and cannot authenticate against
  the current server, so no RX row will ever be produced by a C# peer until
  that is fixed. Every RX row observable today would have to come from a
  second instance of this client.
- **The Comms window has never been resized by a human** in a way that
  matters, because nothing in it was resizable. Whether the flow grid
  re-wraps acceptably at real window sizes is a visual judgement no test
  makes.

---

## 12. Definition of Done

1. `internal/profile` and `internal/history` exist as pure packages with the
   coverage described in §10.
2. A profile can be saved, listed, loaded, renamed, deleted, imported and
   exported; the active profile and its dirty state are visible in both the
   Profiles screen and the Comms chrome.
3. REVERT and RESET behave as §5.1 specifies, including with no profile
   active.
4. Radio blocks in the Comms window can be resized and reordered; the layout
   survives a client restart and round-trips through a profile file.
5. Loading a profile re-pushes radios to the server and the frequencies it
   advertises are bit-identical to what the server compares against.
6. The Transmission Log shows RX and own-TX rows with sender, channel,
   frequency and duration, filtered by channel, time and search, and exports
   to CSV.
7. The log survives a client restart and is capped at 2000 entries.
8. Every failure in §9's table raises the notification it describes.
9. `go build` / `go vet` / `go test -race ./...` green with `-tags purego`;
   frontend `vitest`, `npx tsc --noEmit` and build green.
10. A manual verification checklist is written to
    `docs/superpowers/plans/` covering everything in §11.

---

## 13. Out of scope

- **Recording and replay** (PROTO_GAPS #9). The Replay column is dropped,
  not stubbed.
- **Per-radio encryption** (PROTO_GAPS #1). Not stored in the profile.
- **Server-pushed history** (PROTO_GAPS #7's suggested change). This phase
  is local-only, like every other 7.x sub-phase.
- **`srs.proto` changes.** None. The profile→server path reuses
  `UpdateRadioInfo` exactly as it exists.
- **The other three popouts** (Ship, Fleet, Messages) — those are 7.4.
- **A per-item dismiss control on the Notifications popout.** 7.2 recorded
  that as an open product decision; this phase does not settle it.

---

## 14. Toolchain

Per the decomposition spec's §5, re-confirmed on 2026-09-30:

- Branch off `main`: `feat/phase-7-3-local-persistence`.
- Every Go invocation carries `-tags purego`, with
  `GOCACHE=$TMPDIR/vcs-gocache`.
- `srspb/` must be generated before anything compiles: install `buf`
  (`go install github.com/bufbuild/buf/cmd/buf@v1.47.2`) and run
  `buf generate`. There is no Makefile; `Taskfile.yml`'s `proto` task is the
  documented entry point.
- Typecheck with `(cd frontend && npx tsc --noEmit)`. The `--prefix` form
  prints a help banner and exits 0 **without checking anything**.
- `frontend/bindings/` is gitignored and goes stale across branch switches.
  Regenerate with
  `wails3 generate bindings -ts -f "-tags purego" -clean=true`.
- **gopls is badly unreliable in this repo** — 13 false positives in Phase
  7.2 across three kinds, two of which would be hard compile errors if real.
  Never act on its diagnostics; `go build`/`go vet` is the only authority.
- `internal/voice` tests need `dangerouslyDisableSandbox` for `bind(2)`.
- `cmd | grep -v x; echo $?` reports **grep's** status, and grep exits 1 when
  it filters everything out. Use `${PIPESTATUS[0]}`.
