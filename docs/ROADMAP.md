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

**Status:** `[x]` complete 2026-09-23 — Trigger lists (keyboard chord and/or joystick), the polled `internal/joystick` manager, vendored DirectInput backend on Windows, evdev backend on Linux, macOS unsupported stub, refcounted multi-source PTT, and the Settings Keybinds multi-chip UI all landed on `feat/joystick-gamepad-keybinds`.

Numbered 3.5 rather than renumbering Phases 4–10: it was pulled in ahead of Audio at the user's request, and churning eight phase numbers to record that would cost more than it explains.

**Verification status:** the full automated suite is green — `go build ./...`, `go vet ./...`, `go test -race ./internal/...` (13 packages), clean cross-compilation of `./internal/joystick/...` for windows/amd64, linux/amd64 and darwin/arm64, and the frontend's `vitest`/`tsc --noEmit`/production build (see Task 15's report). (A *whole-tree* `GOOS=linux` build cannot be done from a macOS host at all: Wails v3's own GTK backend needs cgo and Linux C headers. That is pre-existing and unrelated to this phase — the failure is entirely inside `wails/v3/pkg/application`.) **The phase has NOT been verified on real hardware.** This environment is macOS-only, so the Windows DirectInput and Linux evdev backends have never been executed — only compiled. In particular, whether Star Citizen keeps force feedback on a real FFB stick while VCS reads the same device (spec risk J1/J2, the one finding the original spike could not prove) is still unknown, as is whether `BTN_TRIGGER_HAPPY*` buttons register on a high-button-count Linux HOTAS (risk J5 for identical devices is likewise unverified). A concrete manual checklist covering every hardware-dependent item — force-feedback coexistence in both launch orders first, then background input, tray behaviour, hot-plug, config round-trip, capture safety, Linux group permissions, and the macOS no-op path — is written up and waiting for a human to run: [`2026-09-18-joystick-manual-verification.md`](./superpowers/plans/2026-09-18-joystick-manual-verification.md). Treat Phase 3.5 as code-complete, not field-verified, until that checklist has been executed.

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

**Hardware gate:** field verification is not done until run on Windows with Star Citizen running and a force-feedback stick — SC must keep force feedback while VCS reads the same device. See "Verification status" above and the manual checklist it links.

---

## Phase 4 — Audio I/O

**Status:** `[x]` complete 2026-09-24 — malgo device lifecycle (input + output, hot-plug, bounded-backoff reopen), RNNoise noise suppression + pure-Go AGC, VU metering, the PTT/VOX/mute gate feeding `global.push_to_mute`/`global.mute_toggle` their first consumers, radio-effect DSP presets, a nine-slot SFX engine, four level buses, and the per-OS CI matrix (closes R3) all landed on `feat/phase-4-audio-io`.

**Verification status:** the full automated suite is green (`go build`/`go vet`/`go test -race ./...` across all packages, frontend `vitest`/`tsc --noEmit`/production build — see Task 18's report). **The phase has NOT been verified on real hardware** — no audio device or GUI could be exercised in the environment that closed out the phase. A concrete manual checklist covering every hardware-dependent DoD item (device enumeration and selection per OS, the macOS microphone TCC prompt and its denied/recovery paths, unplug/replug and Bluetooth mid-session, System Default following an OS change, Star-Citizen audio coexistence in both launch orders, PTT from keyboard and joystick including the Phase 3.5 refcount now made audible, AGC/NS audibly doing what they claim, the four level knobs, latency/glitching with overrun/underrun counters, and the concurrency fixes around a device-unplug racing a Stop/restart) is written up and waiting for a human to run: [`2026-09-23-phase-4-manual-verification.md`](./superpowers/plans/2026-09-23-phase-4-manual-verification.md). Treat Phase 4 as code-complete, not field-verified, until that checklist has been executed.

**The SFX sample pack is still outstanding.** The engine is asset-agnostic by design (D11) — a missing sample is silent and logged once, never a crash — but until the WAV files from the credited contributors land, every effect PREVIEW is silent: seven from this phase, plus `connect.wav` and `disconnect.wav` added by Phase 6 — nine outstanding in total. This is a real, open dependency on the user, not a soft caveat.

**Design doc:** [`2026-09-23-vcs-client-phase-4-audio-io-design.md`](./superpowers/specs/2026-09-23-vcs-client-phase-4-audio-io-design.md)

**Headline deliverables**
- malgo lifecycle (input + output device init/teardown, hot-plug diff, bounded-backoff reopen, device-loss fallback)
- Device picker UI in Settings, including `System Default` as a first-class value
- Mic AGC + noise suppression (vendored RNNoise), each independently toggleable
- VU metering, surfaced via events at ~20 Hz
- TX/RX/intercom/encryption SFX engine (asset-agnostic; samples bundle pending)
- Per-OS CI matrix in CI (closes R3 — see the master spec's §9)

**Blocking deps:** none remaining — malgo and RNNoise both build cleanly on all three OSes

---

## Phase 5 — UDP voice

**Status:** `[x]` complete 2026-09-25 — custom UDP voice client, Opus encode/decode, per-frequency multiplex, PTT-gated TX with a jitter buffer on RX, reconnect/binding-loss recovery, voice-secret auth, and the minimal radio bootstrap all landed on `feat/phase-5-udp-voice`.

**Verification status:** the full automated suite is green (`go build`/`go vet`/`go test -race ./...` across all packages, the integration suite driving a **real headless server** — 7/7 §11.1 cases passing, not just mocked — plus frontend `vitest`/`tsc --noEmit`/production build). **The phase has NOT been verified on real hardware, and more starkly than any prior phase, nothing in it has ever been heard by a human** — no audio device, no real microphone, no speakers, and no C# peer have been exercised anywhere in this phase's verification. A concrete manual checklist covering every hardware-dependent item — real mouth-to-ear latency measured against a stated target, the D7 radio-effect TX→RX move heard against a real listener, whether a quiet room sounds right given the server's sub-5-byte silent-frame drop, `maxCatchUpFrames = 3` against a real 10 ms tick budget, recovery from a genuine network change (Wi-Fi↔Ethernet, VPN toggle) within ~15 s, Star-Citizen coexistence with live two-way voice, PTT from keyboard and joystick including the Phase 3.5 refcount now carrying real audio, radio tuning surviving a restart, a 3+ participant multi-client session, and the test-frequency loopback as the cheapest single-person self-check — is written up and waiting for a human to run: [`2026-09-24-phase-5-manual-verification.md`](./superpowers/plans/2026-09-24-phase-5-manual-verification.md). Treat Phase 5 as code-complete, not field-verified, until that checklist has been executed.

**Cross-client interop with the C# peer is BLOCKED, not merely untested.** `VNGD-SimpleRadioStandalone` PR #253's `CreateHelloPacket` sends no voice secret, so against the current server every HELLO from it is rejected and it never receives audio — there is no partial interop to observe. This unblocks only once that branch adds the secret to its HELLO, matching what this client already sends.

**Design doc:** [`2026-09-24-vcs-client-phase-5-udp-voice-design.md`](./superpowers/specs/2026-09-24-vcs-client-phase-5-udp-voice-design.md)

**How it unblocked:** the server landed the voice path and its authentication in `vngd-srs-server` PR #207 (merged 2026-09-24, `3d5ca96`), and the C# client is being ported onto the same wire format in `VNGD-SimpleRadioStandalone` PR #253. Both were read at source; `srs.proto` is now byte-for-byte identical between client and server.

**Headline deliverables**
- Custom UDP client matching the server's Go reference
- Opus encode/decode — **48 kHz / 20 ms / mono / `Application.Audio` / 48 kbps / FEC off / DTX off**, a wire contract with the C# peer
- Per-frequency multiplex on a single UDP socket
- PTT-gated TX, jitter buffer
- Reconnect on `VoiceAddressUpdate`; binding-loss detection and re-HELLO recovery
- Voice-secret authentication via `ServerSyncResult.voice_secret` (**not** `VoiceHostDetails.secret`, which the server never populates)
- Minimal radio bootstrap: persisted `[[radios]]`, editable frequency, selected-radio state — without these there is nothing to transmit on
- Integration tests driving the real headless server, wired into CI
- The five accepted Phase 4 follow-ups

**Blocking deps:** none remaining.

**Carried risk:** the C# peer's `CreateHelloPacket` sends no voice secret, so PR #253 as it stands cannot complete a handshake against the current server. Cross-client interop testing is blocked on that branch, not on this one.

### Four issues found during Phase 5 — outside this phase, awaiting the user's decision

Found during implementation and review; deliberately **not fixed** as part of Phase 5, since each is either pre-existing, cross-repo, or a CI/build-config change requiring explicit approval per `CLAUDE.md`.

1. **Windows release builds ship with no audio at all.** `build/windows/Taskfile.yml` defaults `CGO_ENABLED=0`, which selects the stub malgo backend, the no-op denoiser, and the stub Opus codec — on the only platform Star Citizen runs on. Pre-existing since Phase 4; Phase 5 changes the severity from "a quality feature is missing" to "the product cannot do its job."
2. **`.github/workflows/release.yml` never runs `buf generate`**, so a release build cannot compile against the gitignored `srspb/`. Latent — no tag has ever been pushed, so the workflow has never run.
3. **A cross-repo version landmine.** `vngd-srs-server/srs/utils.go:13` is `return version == "0.1.0"` — a hardcoded string equality, not a semver range. The client sends `version.Client`, currently exactly `"0.1.0"`. **Bumping the client version breaks every login**, and nothing in either repo signals that coupling.
4. **The Windows CI leg has never run on a real runner**, and `protoc-gen-go-grpc`'s version pin has no automatic drift signal.

---

## Phase 6 — Connection-status surface

**Status:** `[x]` complete 2026-09-26 — control-plane liveness detection, the ping ticker (closing master spec §6 DoD 9), the `connhealth` dual-plane model, `connection:state`, the status bar's dual-pill, all three ConnBanner variants and a working reconnect UX landed on `feat/phase-6-connection-status`.

**Verification status:** the full automated suite is green (`go build`/`go vet`/`go test -race ./...` with `-tags purego`, frontend `vitest`/`tsc --noEmit`/production build). **The phase has NOT been verified on real hardware, and nobody has ever watched this client lose a control connection to a real server.** The 5s × 3 failure threshold, the half-open detection claim and the ~70s transport floor are all designed from the server's source, not measured; voice RTT has never been read by a human. Checklist: [`2026-09-26-phase-6-manual-verification.md`](./superpowers/plans/2026-09-26-phase-6-manual-verification.md).

**Design doc:** [`2026-09-26-vcs-client-phase-6-connection-status-design.md`](./superpowers/specs/2026-09-26-vcs-client-phase-6-connection-status-design.md)

**Four gaps this phase found and closed** (none of them recorded in this file beforehand):
1. **`ConnBanner` was live code that could essentially never fire.** Both `ConsumeUpdates` goroutines discarded the stream's terminating error, so a server restart or network drop emitted nothing and the UI reported `connected` indefinitely.
2. **The Phase 1 ping ticker was never wired.** `PingOnce` had zero callers, `PingIntervalSeconds` zero readers, and the server's `LatencyToControlMs` read zero for every VCS client.
3. **Phase 5's `voice:state` was emitted into the void** — absent from the frontend's `EV` map. `voice.Session.RTT()` was surfaced nowhere at all.
4. **The prototype's status bar rendered a server name and region not on the wire.** Filed as PROTO_GAPS #10.

**Headline deliverables**
- `internal/connhealth`: derived dual-plane model, one ticker, one `connection:state` event
- Stream-termination detection under intent and generation guards; `MarkControlLost`
- Ping ticker at 5s with a 3-failure loss threshold, mirroring `internal/voice`'s own budget
- gRPC client keepalive at 75s, respecting the server's `MinTime: 60s` enforcement floor
- Status bar `dual-pill`, rendered unconditionally (standalone included), with a fourth `unavailable` state for voice
- All three `ConnBanner` variants, in-flight reconnect state, and failure reasons surfaced
- `App.ReconnectVoice()`; connection SFX wired asset-agnostically (ships silent)

**Blocking deps:** none remaining.

**Two server behaviours made visible, not fixed** — both cross-repo, neither confirmed to fire. `SubscribeToUpdates` rejects a duplicate subscription while the old stream's cleanup is pending; a dead stream leaves the client in `serverState.Clients`. Both are on the manual checklist.

**Still outstanding:** the SFX sample pack is now **nine** files, not seven — `connect.wav` and `disconnect.wav` joined the seven from Phase 4.

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
