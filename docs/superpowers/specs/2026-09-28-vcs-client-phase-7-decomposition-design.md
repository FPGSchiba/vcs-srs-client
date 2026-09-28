# Phase 7 — Profiles + remaining popouts: decomposition

**Date:** 2026-09-28
**Status:** decomposition approved; sub-phases individually specced
**Scope:** umbrella document. Contains no implementation detail of its own.

This document exists so a later session can pick up any Phase 7 sub-phase
without re-deriving the survey work. It records what the phase decomposes
into, what each piece owns, the dependency order, and the findings that
apply across more than one sub-phase.

**Each sub-phase gets its own spec, plan and execution cycle.** Do not
attempt a single design covering all nine ROADMAP deliverables.

---

## 1. Why the ROADMAP row had to be split

`docs/ROADMAP.md`'s Phase 7 row lists nine headline deliverables spanning at
least four independent subsystems: radio profiles, four new popout windows,
transmission history, notification routing, OS-keychain token migration
(R4), and TLS (R5). Those share no data model, no code path and no risk
profile. Radio profiles and TLS have nothing to say to each other.

---

## 2. The four sub-phases

| Sub-phase | Contents | Spec |
|---|---|---|
| **7.1 Secure transport** | TLS on the control plane, client *and* server; R4 resolution | `2026-09-28-vcs-client-phase-7-1-secure-transport-design.md` |
| **7.2 Notification channel** | Notifications popout; routing hotkey/joystick registration failures into it | not yet written |
| **7.3 Local persistence** | Radio profiles; transmission history | not yet written |
| **7.4 Remaining popouts** | Ship Mode; Fleet C2; Messages | not yet written |

### Dependency order

**7.1 → 7.2 → {7.3, 7.4}.**

- **7.1 first** because Phases 3, 3.5, 4, 5 and 6 are all code-complete and
  none has been field-verified — five manual checklists sit unrun in
  `docs/superpowers/plans/`, and nothing in the voice path has ever been
  heard by a human. `internal/session/dial.go` fails closed on any
  non-localhost host, so the client can only reach a server on the same
  machine. Making remote verification *possible* outranks every feature in
  7.2–7.4.
- **7.2 second** because it is a dependency of the later popouts rather than
  a peer: Ship, Fleet and Messages will each want to raise notifications,
  and building the channel after them means retrofitting three consumers.
- **7.3 and 7.4 are independent of each other** and may run in either order,
  or in parallel if isolated in separate worktrees.

---

## 3. Cross-cutting findings

Established by reading source in both repos on 2026-09-28. Each contradicts
something currently written down, so they are recorded once here rather than
rediscovered per sub-phase.

### 3.1 The window Registry is already generic (helps 7.2, 7.4)

`internal/app/windows.go` `Registry` is window-id-agnostic: `Open`, `Close`,
`Toggle`, geometry persistence and the `EventWindowState` broadcast all work
for any id. Adding a popout costs two `switch` arms —
`defaultGeometry` and `windowURL` in `internal/app/windowfactory.go:20-36` —
plus a Vite entry in `frontend/vite.config.ts` and an HTML shell.

`frontend/src/shared/components/TopBar.tsx:33-39` already declares all five
launchers (`comms`, `fleet`, `ship`, `messages`, `notifications`); only
`comms` is wired. The rest render as disabled placeholders.

**The cost of these sub-phases is the UI port, not the windowing.** From the
design prototype: `screens/misc.jsx` is 688 lines (holding `ScreenProfiles`,
`ScreenMessages`, `ScreenHistory`, `ScreenNotifications`),
`screens/ship-fleet.jsx` is 585, `screens/fleet-c2.jsx` is 547.

### 3.2 Notification routing is a level-to-edge problem (7.2)

The backend payloads already carry exactly what the ROADMAP's three cases
need, so 7.2 adds no new backend plumbing:

- `events.HotkeyStatePayload` (`internal/events/events.go:191`) carries
  `Registered`, `Error`, `Failed map[actionID]reason`, and `Permission`
  (`unknown` | `granted` | `denied` | `not_applicable`).
- `events.JoystickStatePayload` (line 288) carries `Supported`, `Error`,
  `Devices`.

That maps cleanly onto the three cases the ROADMAP insists stay distinct:
global failure notifies once carrying permission state; per-binding failure
notifies per action, named; macOS `Supported: false` is informational and
must never render as an error.

**The actual design problem is that these are level-triggered snapshots
re-emitted on change, while notifications are discrete items.** Without
dedupe keyed on state identity, every re-emit spams the user. Phase 6 hit
this exact bug and fixed it in `c8cdb75` ("dedupe connection SFX and stop
banner failure text leaking across variants") — read that commit before
designing 7.2.

### 3.3 Transmission history has no event source (7.3)

`docs/PROTO_GAPS.md` §7 states history is populated "client-side from
observed `voice:rx_active` events". **That event does not exist** — it
appears nowhere in `internal/events`.

`internal/voice/rx.go` tracks per-stream `SenderID` and `Frequency`
(`rxStreamFor`, line 551; `streamKey` at line 85) but emits nothing upward.
7.3 must therefore add RX stream start/end plumbing from `internal/voice`
through `internal/app` to the frontend, and resolve `SenderID` (a `uuid.UUID`)
to a callsign via `internal/state.Store.Client()` (`store.go:162`), whose
`Snapshot.Clients` is keyed by GUID.

Treat this as the risky part of 7.3. Radio profiles, by contrast, touch no
new subsystem.

### 3.4 Radio profiles already have their seed (7.3)

Radios live in `config.toml` today as `Config.Radios []Radio` plus
`SelectedRadioID` (`internal/config/config.go:62,72,106`). The comment above
`type Radio` states outright that this was written as "the honest seed for
Phase 7's profiles without building profiles now" — the server cannot create
a radio, so a local seed is what gives a freshly connected user anything to
transmit on.

Note the frequency constraint recorded there: `FrequencyKHz` is a `uint32`
and must stay one. The server decides whether to relay a transmission by
comparing advertised frequencies with **exact float32 equality**, so a
profile format that round-trips frequency through a float can silently drop
a radio out of range. Profile JSON must store the integer.

`ScreenProfiles` and `ScreenHistory` are **main-window nav screens**, not
popouts — `NavRail.tsx:17-27` already lists `profiles` ("Radio Profiles")
and `history` ("Transmission Log"), both currently rendering `Placeholder`.

### 3.5 Everything in 7.2–7.4 is local-only, by design

`docs/PROTO_GAPS.md` §§3–8 already record ship-mode registry, ships catalog,
roles catalog, text channels, transmission history and notifications as
local TOML/JSON with server-side equivalents deferred. No sub-phase here
touches `srs.proto`. Any proposal to do so goes through PROTO_GAPS and needs
explicit approval first.

### 3.6 Two recorded blockers are already fixed

PR #29 (`8497c14`, on `main`) added `buf generate` to `release.yml` and a
guard that hard-fails the release when any binary reports `CGO_ENABLED=0`
(`release.yml:103-108`). Phase 5's issue #1 (Windows ships with no audio)
and issue #2 (`release.yml` never runs `buf generate`) are **closed**.

Still open, and still blocking real-world verification:

- The **SFX sample pack** — nine WAV files, none delivered. All effect
  previews and both connection sounds are silent. A dependency on the user.
- **C# peer interop is blocked, not merely untested** —
  `VNGD-SimpleRadioStandalone` PR #253 sends no voice secret in its HELLO,
  so it cannot authenticate against the current server at all.

---

## 4. Verification debt applies to every sub-phase

Phases 3, 3.5, 4, 5 and 6 are code-complete and never field-verified. Five
manual checklists sit unrun in `docs/superpowers/plans/`. Nobody has heard
this client produce audio, and nobody has watched it lose a connection to a
real server.

**Every sub-phase spec must state explicitly where its design depends on a
real-world behaviour nobody has observed**, rather than assuming it. Phase 6
set the precedent: its 5s × 3 failure threshold, half-open detection claim
and ~70s transport floor are all documented as designed-from-source, not
measured.

7.1 is the sub-phase that makes the queued checklists runnable at all, since
reaching a real server is what every one of them has been waiting on.

---

## 5. Toolchain

Verified the hard way across Phases 5 and 6. Follow these rather than
rediscovering them:

- Branch off `main`. One branch per sub-phase.
- Every Go invocation carries `-tags purego`, with
  `GOCACHE=$TMPDIR/vcs-gocache` (the default GOCACHE is sandbox-blocked).
- Typecheck the frontend with `(cd frontend && npx tsc --noEmit)`.
  `npx --prefix frontend tsc --noEmit` prints a help banner and exits 0
  **without checking anything**.
- `frontend/bindings/` is gitignored and goes stale across branch switches.
  If `tsc` reports missing methods on the App bindings, regenerate with
  `wails3 generate bindings -ts -f "-tags purego" -clean=true` — those
  errors are staleness, not real.
- **gopls is badly unreliable in this repo.** During Phase 6 it reported
  existing methods as undefined on four separate occasions, each time
  contradicted by a clean `go vet`. Never act on its diagnostics.
- The sandbox blocks `bind(2)`, so `internal/voice`'s tests fail inside it
  with "listen udp: operation not permitted". Use
  `dangerouslyDisableSandbox` for that package only. (TLS tests avoid this
  by layering TLS over `bufconn` — see the 7.1 spec §8.)
- Both window roots render inside `React.StrictMode`. Any component whose
  effect cleanup has side effects needs a StrictMode test **plus** a control
  proving the real-unmount path still fires once. A bare-rendered suite hid
  a total keybind-capture break through two phases.
- SonarCloud's PR gate evaluates only the PR's own new code. Phase 6 tripped
  `typescript:S1082` — a clickable `div`/`span` with no keyboard handler. Any
  click handler on a non-button element needs `role="button"`, `tabIndex`
  and Enter/Space handling in the same commit.
- Shell trap: `cmd | grep -v x; echo $?` reports **grep's** status, not
  `cmd`'s, and grep exits 1 when it filters everything out. Use
  `${PIPESTATUS[0]}`.
- `gh pr checks` blocks until checks finish — use
  `gh api repos/OWNER/REPO/commits/SHA/check-runs`. `gh run view
  --log-failed` needs `XDG_CACHE_HOME` pointed somewhere writable.

---

## 6. What each sub-phase must not re-litigate

- The tech stack (master spec, §"Foundational decisions").
- Opus codec geometry — 48 kHz / 20 ms / 960 samples / mono / 48 kbps /
  FEC off / DTX off. It is a wire contract with the C# peer, which silently
  truncates or zero-pads anything else. Treat a change like a proto change.
- Guest-only login. The disabled SSO button in `Welcome.tsx` is a
  placeholder, not an oversight; Phase 2 is deferred pending the Vanguard
  user-management backend.
- Joystick support being Windows + Linux only.
- `srs.proto`. Changes go through `docs/PROTO_GAPS.md` and need approval.
