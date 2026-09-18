# VCS Client — Roadmap

Cross-phase tracking. Phase 1 is detailed in `docs/superpowers/specs/2026-05-31-vcs-client-design.md`. Each later phase gets its own design doc when picked up — entry created under `docs/superpowers/specs/YYYY-MM-DD-vcs-client-phase-N-<theme>-design.md`.

## Status legend
- `[ ]` not started
- `[~]` in progress
- `[x]` complete
- `[-]` deferred — consciously skipped; reason and unblock condition recorded on the phase

---

## Phase 1 — Scaffold + auth (guest) + control gRPC + Comms popout shell

**Status:** `[x]` complete 2026-06-02 — landed across Part 1 (foundation) and Part 2 (plans 2A backend session, 2B windowing/frameless shell, 2C pixel-faithful frontend) on `feat/phase-1-part-2-auth-control`.

**Headline deliverables**
- Wails v3 multi-window scaffold with per-window Vite entries
- `srspb/` gRPC bindings generated from `srs.proto`
- Guest login (name + password + unit_id; server resolves coalition)
- gRPC control client with `SyncClient`, `UpdateClientInfo`, `UpdateRadioInfo`, `SubscribeToUpdates`, `Ping` ticker
- Main window chrome with design tokens
- Communications popout as real OS window
- Design tokens ported from `design/vcs/project/styles.css`
- 3-platform CI build matrix

**Definition of Done:** see spec §6.

---

## Phase 2 — Plugin SSO multi-step auth

**Status:** `[-]` deferred 2026-09-15 — skipped in favour of Phase 3.

**Why deferred:** the phase needs the `vcs-vanguard-auth-plugin` finished, and that work is itself blocked on access to the existing Vanguard user-management backend. Building the client-side flow driver against an unfinished plugin would be speculative.

**Interim behaviour (shipped, not a stopgap to rip out):** guest login is the only supported sign-in path. The SSO entry point is deliberately rendered as a disabled placeholder in `Welcome.tsx` — the two-stage welcome UX stays exactly as designed, so picking this phase back up is a matter of enabling the button and wiring the flow behind it, not reworking the screen.

**Headline deliverables** (unchanged, for when it resumes)
- `InitAuth` → `DiscoverAuthenticationFlows` → `StartAuth` → `ContinueAuth` loop driver
- Field-definition-driven form renderer (uses `FieldDefinition.type` to pick input widget)
- Coalition / unit / role pickers after `LoginResult`
- `UnitSelect` finalisation

**Blocking deps:** `vcs-vanguard-auth-plugin` completion ← access to the Vanguard user-management backend.

**Unblock condition:** that access lands and the plugin's `StartAuth` / `ContinueAuth` are servable end-to-end. No client-side proto or server change is needed first — `srs.proto` already carries the full multi-step surface and is byte-identical to the server's copy.

---

## Phase 3 — Settings + keybinds

**Status:** `[x]` complete 2026-09-15 — Settings screen, persisted keybinds with steal-on-conflict, real OS-level global hotkey registration (including hold-to-talk key-release, R11 resolved), and system tray all landed on `feat/phase-3-settings-keybinds`.

**Verification status:** the full automated suite is green (`go vet`, `go test -race ./...`, `tsc --noEmit`, `vitest`, frontend production build — see Task 12's report). **The phase has NOT been verified on real hardware** — no GUI could be launched in the environment that closed out the phase. A concrete manual checklist covering every hardware-dependent DoD item (tray legibility in light/dark, close-to-tray/restore, global hotkeys firing with another app focused, non-US keyboard layout capture, macOS permission-prompt denial, Linux no-StatusNotifier-host behaviour, and more) is written up and waiting for a human to run: [`2026-09-15-phase-3-manual-verification.md`](./superpowers/plans/2026-09-15-phase-3-manual-verification.md). Treat Phase 3 as code-complete, not field-verified, until that checklist has been executed.

**Design doc:** [`2026-09-15-vcs-client-phase-3-settings-keybinds-design.md`](./superpowers/specs/2026-09-15-vcs-client-phase-3-settings-keybinds-design.md)

**Prerequisite:** Wails v3 `alpha.96` → `beta.22` upgrade lands as its own `chore(deps)` commit before implementation (spec §10). Verified to build and pass `go test -race ./...` with zero Go source changes; the open cost is the regenerated TypeScript bindings.

**Headline deliverables**
- Settings popout/section with audio, network, appearance, profiles sub-sections
- Keybind capture UI (listening state visible in design as `.kbd.listening`)
- Per-radio PTT + Select bindings, plus global PTT
- TOML persistence — single `config.toml` with `[general]` and `[keybinds]` tables
- Settings exposed via Wails bindings; subscriber updates in all windows

**Blocking deps:** none

**Rationale for order:** Settings/keybinds is pulled forward of Audio because keybinds become essential the moment PTT exists.

---

## Phase 3.5 — Joystick / gamepad keybinds

**Status:** `[~]` in progress — started 2026-09-18 on `feat/joystick-gamepad-keybinds`.

Numbered 3.5 rather than renumbering Phases 4–10: it was pulled in ahead of Audio at the user's request, and churning eight phase numbers to record that would cost more than it explains.

**Design doc:** [`2026-09-18-joystick-gamepad-keybinds-design.md`](./superpowers/specs/2026-09-18-joystick-gamepad-keybinds-design.md)
**Spike:** [`2026-09-18-gamepad-joystick-bindings-spike.md`](./superpowers/specs/2026-09-18-gamepad-joystick-bindings-spike.md)

**Headline deliverables**
- An action holds a *list* of triggers — keyboard chord and/or joystick button — instead of one. Purely additive; existing `config.toml` files load unchanged and are rewritten byte-identical.
- `internal/trigger` (Trigger / JoyBinding value types) and `internal/joystick` (polled OS source, sibling to `internal/hotkeys`)
- Non-exclusive background DirectInput on Windows via vendored `gonutz/di8`; `holoplot/go-evdev` on Linux; macOS reports unsupported
- Buttons and POV hat directions, with an optional modifier button that may live on a different device
- Per-action press refcount so keyboard + joystick held together cannot cut PTT mid-transmission

**Why no SDL:** SDL unconditionally takes `DISCL_EXCLUSIVE` on every DirectInput joystick it opens, and Microsoft documents exclusive access as both required for force feedback and mutually exclusive between applications — so an SDL-based client would likely cost Star Citizen its force feedback. DCS-SRS, the closest prior art, uses `Background | NonExclusive`. See the spike for the evidence.

**Blocking deps:** none

**Hardware gate:** the phase is not done until verified on Windows with Star Citizen running and a force-feedback stick — SC must keep force feedback while VCS reads the same device.

---

## Phase 4 — Audio I/O

**Status:** `[ ]`

**Headline deliverables**
- malgo lifecycle (input + output device init/teardown)
- Device picker UI in Settings
- Mic AGC + noise suppression
- VU metering, surfaced via events at low rate
- TX/RX/intercom/encryption SFX engine (samples bundled)
- Per-OS smoke build job in CI (added one phase early to surface toolchain issues)

**Blocking deps:** malgo on each OS

---

## Phase 5 — UDP voice

**Status:** `[ ]` **— BLOCKED**

**Headline deliverables**
- Custom UDP client matching the server's Go reference
- Opus encode/decode
- Per-frequency multiplex on a single UDP socket
- PTT-gated TX, jitter buffer
- Reconnect on `VoiceAddressUpdate`
- `VoiceHostDetails.secret` authentication

**Blocking deps:** **server-side Go protocol reference from user** — packet structure, framing, codec, sample rate / frame size, multiplex layout, secret handshake, `coalition_voice_addr` vs `global_voice_addr`.

---

## Phase 6 — Connection-status surface

**Status:** `[ ]`

**Headline deliverables**
- ConnBanner integrated with real control + voice state
- Status bar `dual-pill` for distributed mode (still shown in standalone form here; full distributed UI in Phase 9)
- Reconnect flow UX (manual reconnect button on banner)
- Standalone path fully wired

**Blocking deps:** Phase 5

---

## Phase 7 — Profiles + remaining popouts

**Status:** `[ ]`

**Headline deliverables**
- Radio profile load/save/import/export (JSON files in user-chosen dir)
- Ship Mode popout (component registry from local TOML for now)
- Messages popout (text channels mirroring radio frequencies; local ring buffer)
- Notifications popout (local + future server-pushed alert channel)
- Route hotkey-registration failures through the notification channel,
  replacing Phase 3's inline banner in the Keybinds section. Two distinct
  cases, and they must stay distinct: a GLOBAL failure (nothing registered at
  all -- a denied macOS Accessibility grant, a missing backend) notifies
  **once**, carrying the permission state and its remedial action; a
  PER-BINDING failure (a chord `internal/chord` accepts but the OS cannot
  register, e.g. `Numpad7`) notifies **per action**, naming the action. The
  banner exists today because there is nowhere else to put this; once the
  notification channel lands it is the right home, since a registration
  failure is exactly the kind of thing the user must learn about without
  having Settings open. See the Phase 3 spec's R12 for the origin.
  **This covers joystick failures too** (Phase 3.5): the same two cases, plus
  a third global state that is informational rather than an error —
  "joystick input is unsupported on this platform" on macOS, which must never
  render as a failure.
- Fleet Mode popout (C2 view)
- Transmission history view (local JSON ring buffer)
- OS-keychain migration for session token (closes R4)
- TLS for gRPC control connection (closes R5)

**Blocking deps:** Phase 4 for audio-aware UI

---

## Phase 8 — Operations REST API integration

**Status:** `[ ]`

**Headline deliverables**
- `internal/operations` HTTP client (uses `net/http`)
- Endpoint URL configurable in `config.toml`
- Operations list view, calendar view, detail view
- Stale-while-revalidate caching via TanStack Query on the frontend side

**Blocking deps:** Operations REST API endpoint specification from server team

---

## Phase 9 — Distributed voice

**Status:** `[ ]`

**Headline deliverables**
- Honour `VoiceAddressUpdate` events: switch UDP endpoints without dropping the gRPC session
- Honour `DistributionUpdate` to refresh voice-host list
- Server Network panel from `DistributionUpdate` payloads + live ping data per voice host
- Distinguish control vs voice connection state in status bar exactly per design

**Blocking deps:** server-side distributed-mode reference

---

## Phase 10 — Polish

**Status:** `[ ]`

**Headline deliverables**
- Auto-update prompt wired (`minio/selfupdate`, check on startup, prompt non-blocking)
- Code signing for at least Windows (macOS / Linux if certs available)
- Crash reporter
- In-app log viewer

**Blocking deps:** code-signing certificates

**TODO (final step — code-quality pass before v1):**
- [ ] Integrate **React Doctor** (frontend health/diagnostics) and act on its findings.
- [ ] Resolve outstanding **SonarQube / SonarCloud** issues (the SonarCloud quality gate has been failing on PRs) and get the gate green.
- Do this as the *last* step so the whole codebase is in its final shape when audited.

---

## Cross-phase tracking notes

- **Proto gaps** that may become server work: see `docs/PROTO_GAPS.md`.
- **Operations** is **not** a proto gap — it lives on a REST endpoint owned by another system.
- Each phase entry above is intentionally short. When you pick up a phase, run `/brainstorm` to produce the detailed design doc for that phase.
