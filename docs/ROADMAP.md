# VCS Client — Roadmap

Cross-phase tracking. Phase 1 is detailed in `docs/superpowers/specs/2026-05-31-vcs-client-design.md`. Each later phase gets its own design doc when picked up — entry created under `docs/superpowers/specs/YYYY-MM-DD-vcs-client-phase-N-<theme>-design.md`.

## Status legend
- `[ ]` not started
- `[~]` in progress
- `[x]` complete

---

## Phase 1 — Scaffold + auth (guest) + control gRPC + Comms popout shell

**Status:** `[ ]` design approved 2026-05-31 — implementation pending

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

**Status:** `[ ]`

**Headline deliverables**
- `InitAuth` → `DiscoverAuthenticationFlows` → `StartAuth` → `ContinueAuth` loop driver
- Field-definition-driven form renderer (uses `FieldDefinition.type` to pick input widget)
- Coalition / unit / role pickers after `LoginResult`
- `UnitSelect` finalisation

**Blocking deps:** none

---

## Phase 3 — Settings + keybinds

**Status:** `[ ]`

**Headline deliverables**
- Settings popout/section with audio, network, appearance, profiles sub-sections
- Keybind capture UI (listening state visible in design as `.kbd.listening`)
- Per-radio PTT + Select bindings, plus global PTT
- TOML persistence (`config.toml`, `keybinds.toml`)
- Settings exposed via Wails bindings; subscriber updates in all windows

**Blocking deps:** none

**Rationale for order:** Settings/keybinds is pulled forward of Audio because keybinds become essential the moment PTT exists.

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

---

## Cross-phase tracking notes

- **Proto gaps** that may become server work: see `docs/PROTO_GAPS.md`.
- **Operations** is **not** a proto gap — it lives on a REST endpoint owned by another system.
- Each phase entry above is intentionally short. When you pick up a phase, run `/brainstorm` to produce the detailed design doc for that phase.
