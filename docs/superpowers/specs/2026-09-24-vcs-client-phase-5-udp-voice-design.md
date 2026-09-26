# Phase 5 — UDP voice — design

**Date:** 2026-09-24
**Status:** approved design, ready for implementation planning
**Builds on:** [`2026-05-31-vcs-client-design.md`](./2026-05-31-vcs-client-design.md) §3, §5; [`2026-09-23-vcs-client-phase-4-audio-io-design.md`](./2026-09-23-vcs-client-phase-4-audio-io-design.md) (the `Sink` seam, D4, D9)
**Roadmap row:** [`docs/ROADMAP.md`](../../ROADMAP.md) — Phase 5

---

## 1. Goal

Make the client talk. Phase 4 ships a complete capture → process → playback chain whose only consumer is a local monitor; this phase gives it a network: an Opus-coded, authenticated, per-frequency-multiplexed UDP voice path against the VCS server, with a jitter buffer on receive and PTT-gated transmission on send.

It also closes the two gaps that make the phase demonstrable at all. A freshly connected client has **zero radios and no way to create or tune one**, so there is nothing to key up on; and every PTT action currently collapses to a single boolean, so nothing records *which* radio is transmitting. Both are fixed here, minimally — profiles and import/export stay in Phase 7.

### 1.1 Sources of truth, and how they were verified

This phase is an interop exercise across three independently-written implementations, so every protocol claim below was read from source rather than inherited from a summary.

| Component | Revision read | Notes |
|---|---|---|
| Go server | `vngd-srs-server` `3d5ca96` (PR #207, merged 2026-09-24) | `voice/protocol.go`, `voice/server.go`, `state/server.go`, `srs/srs_service.go`, plus the voice-path-authentication design doc |
| C# client | `VNGD-SimpleRadioStandalone` PR #253, branch `develop` (**open**) | `DCS-SR-Common/Network/VCSVoicePacket.cs`, `DCS-SR-Client/Network/UDPVoiceHandler.cs`, `Audio/Managers/AudioManager.cs`, `Audio/Providers/ClientAudioProvider.cs` |
| Wire contract | `srs.proto` | Verified byte-for-byte identical between both repos — sha256 `0d632e1c…56f25a` — and committed separately (see §12) |

Two corrections to the brief this phase started from, both material:

1. **The codec spec was quoted from the C# `master` branch**, which is the old DCS-SRS code. PR #253 — the branch actually being ported onto VCSPacket, namespace `Vanguard.VCS.Client` — has already moved from 16 kHz/40 ms to **48 kHz/20 ms**. See §4.
2. **`VoiceHostDetails` being unimplemented is not the whole story.** `coalition_voice_addr` and `global_voice_addr` are also empty on any standalone server, because they are served from a registry only distributed voice nodes populate. See §6.

---

## 2. Decisions

Settled during brainstorming. Not open for re-litigation during implementation.

| # | Decision | Rationale |
|---|---|---|
| D1 | **Opus 48 kHz / 20 ms / mono / `Application.Audio` / 48 kbps / FEC off / DTX off** | Frame geometry is a hard wire contract with the C# peer, which normalises every decoded frame to exactly 960 samples (§4). Application and bitrate are not on the wire but are matched anyway so a transmission sounds the same whichever client sent it |
| D2 | **Vendored libopus + thin cgo binding** at `internal/audio/opus/` | Exact mirror of `internal/audio/rnnoise/`: self-contained C, no pkg-config, no system packages on any of the three OSes. The alternatives either require `libopus-dev` everywhere or pin a 2015-era fork |
| D3 | **Integer kHz is the client's only canonical frequency**; both wire forms are derived from it | Three components compare frequencies three different ways (§5). Deriving the advertised `float32` with the *same expression the server evaluates* makes its exact-equality match true by construction rather than by luck |
| D4 | **One connected UDP socket per session, for the whole session** | The server binds a session to a source address on verified HELLO and authenticates everything afterwards by that binding. A connected socket also surfaces ICMP port-unreachable as a write error |
| D5 | **One Manager-lifetime `Sink` holding an atomic session pointer** | Phase 4 has no `RemoveSink` and never calls `Sink.Close()`, so a per-connection sink leaks on every reconnect. Honouring the documented "sinks are Manager-lifetime" intent needs neither |
| D6 | **Unanswered keepalives are the liveness probe**; three in a row trigger re-HELLO | A source-address change is otherwise *completely silent* (§7.3). The server already answers every keepalive at the bound address, so this needs nothing new on the wire |
| D7 | **Radio effects move from TX to RX**, one `Effect` instance per stream | The C# peer applies effects on receive. Left on TX, a C# listener hears us double-effected and we hear them dry, and the "no effects on global frequencies" rule is unimplementable from the sending side |
| D8 | **`dspLoop`'s tick is the only clock on the RX path** | A decode goroutine on its own 10 ms ticker and `dspLoop` on its own 10 ms ticker are two unsynchronised clocks that drift into periodic underruns |
| D9 | **One voice endpoint at a time**, resolved by a precedence chain that includes `VoiceAddressUpdate` | Standalone servers return empty addresses, so a fallback is mandatory regardless. Handling the redirect falls out of the precedence chain for free; Phase 9 then only adds the coalition/global split |
| D10 | **Minimal radio bootstrap persisted client-side** in `config.toml`, re-pushed on every connect | Server-side radio state is per-session and lost on disconnect. Persisting locally makes the radio stack survive restarts and is the honest seed of Phase 7's profiles without doing profiles |
| D11 | **Integration tests drive the real headless server as a subprocess**, built in CI | Both repos are public, so CI needs no credentials. A hand-written fake encodes *our* reading of the protocol and therefore cannot catch a misreading — the single largest risk in this phase |
| D12 | **All five accepted Phase 4 follow-ups land here** | #1/#2 are about shutdown ordering that our socket-owning session now depends on; #5 shapes our TX cadence; #3/#4 are device-lifecycle bugs that would obstruct the hardware verification this phase also owes |

---

## 3. Scope

**In:**

- `internal/voice`: packet codec, endpoint resolution, session lifecycle, TX, RX, jitter buffer
- `internal/audio/opus`: vendored libopus and its cgo binding
- Opus encode on transmit, decode on receive, at the geometry in §4
- HELLO handshake carrying the voice secret; keepalive with timestamp echo; BYE on disconnect
- Per-frequency multiplex on one socket; PTT-gated transmission; per-frequency sequence numbers
- Jitter buffer with reorder, loss concealment and bounded depth
- `VOICE_ADDRESS_UPDATE` handling: re-point the socket without disturbing the gRPC session
- Binding-loss detection and recovery (§7.3)
- Radio-effect relocation from TX to RX, per stream, suppressed on global frequencies
- Minimal radio bootstrap: `[[radios]]` in `config.toml`, editable frequency, selected-radio state
- TX frequency routing: `global.ptt` → selected radio, `radio.N.ptt` → radio N, held together as a set union
- Integration tests against the real headless server, wired into CI
- The five Phase 4 follow-ups

**Out:**

- TLS on the control connection — Phase 7, master spec risk R5
- The coalition/global endpoint split, `DistributionUpdate`, the Server Network panel — Phase 9
- Radio profiles, import/export, presets — Phase 7
- The connection-status UI surface — Phase 6. This phase *emits* honest state; Phase 6 renders it
- Transmission recording and history — Phase 7
- Per-radio volume, balance and mute — Phase 6

---

## 4. Codec geometry — why it is not a preference

The server relays opaque payloads and never transcodes, so codec settings are a client-to-client contract.

`ClientAudioProvider.cs` decodes, then **normalises every frame to exactly `OUTPUT_SEGMENT_FRAMES` = 960 samples**, truncating anything longer and zero-padding anything shorter. A 40 ms frame from us would therefore have its second half silently discarded — audible as chopped speech, with nothing logged anywhere to explain it. **20 ms at 48 kHz is not negotiable.**

| Parameter | Value | On the wire? |
|---|---|---|
| Sample rate | 48 000 Hz | Effectively — the peer's decoder is fixed at 48 kHz |
| Frame duration | 20 ms → 960 samples | **Yes, hard** |
| Channels | 1 | Yes |
| Application | `OPUS_APPLICATION_AUDIO` | No — matched for consistent timbre |
| Bitrate | 48 000 bps | No — matched for consistent timbre |
| FEC | off | No |
| **DTX** | **off** | **Effectively yes — see below** |

**DTX must stay off.** Opus DTX emits 1–2 byte frames during silence, and the server drops any voice payload of **5 bytes or fewer** without relaying it (`voice/server.go`, `len(packet.Payload) > 5`). Enabling DTX would present as speech that cuts out only during pauses — a failure almost impossible to diagnose from the client side. The C# peer does not enable it either.

Phase 4's pipeline is 48 kHz / 10 ms (D4 there, forced by RNNoise), so transmit accumulates exactly two frames and **no resampler exists anywhere in this phase**.

### 4.1 Datagram budget

The server reads into a fixed 1024-byte buffer and **silently truncates** anything larger — it does not reject it. At 48 kbps VBR a 20 ms mono frame is roughly 120 bytes, so a packet is about 150 bytes against a 1024-byte ceiling. Comfortable, but the encoder output length is asserted against `1024 - HeaderSize` before send, because the failure mode is invisible.

---

## 5. Frequency: one canonical integer

Three components compare frequencies three different ways:

| Component | Comparison |
|---|---|
| Go server, relay decision | **Exact `float32` equality**: `radio.Frequency == float32(pkt.Frequency)/1000.0` |
| C# client, receive filter | `Math.Abs(radios[i].FrequencyHz - listeningFrequency) < 1.0` — 1 Hz tolerance |
| Wire | 24-bit unsigned kHz, big-endian |

Rather than satisfy each separately, the client holds **`FrequencyKHz uint32` as its only canonical form** and derives everything from it:

- **packet field** ← the integer, directly
- **`UpdateRadioInfo`** ← `float32(kHz) / 1000.0` — *the identical expression the server evaluates on the packet*, so the server's exact-equality match holds by construction
- **receive matching** ← integer comparison; no float arithmetic on the RX path at all
- **a frequency arriving from the server** as `float32` MHz ← `uint32(math.Round(float64(f) * 1000))`

We round where `VcsVoicePacket.SetFrequencyHz` truncates. On the kHz grid the two agree; off-grid, rounding is the only rule that round-trips. A table test pins this across a frequency set (§11).

---

## 6. Endpoint resolution

`SyncClient` serves `coalition_voice_addr` and `global_voice_addr` from `getVoiceAddresses`, which delegates to the voice-control registry. That registry is populated **only** by distributed voice nodes calling `RegisterVoiceServer`. A standalone server never registers itself, so **both fields are empty strings** on every standalone deployment — which is every deployment today.

Resolution order, highest precedence first:

1. live `VoiceAddressUpdate.coalition_voice_addr`
2. `ServerSyncResult.coalition_voice_addr`, **when non-empty**
3. `[voice] host` / `port` in `config.toml`
4. the host from `server_url`, port **5002** (`state.DefaultPort`)

`internal/control/stream.go`'s `default:` branch currently discards `VOICE_ADDRESS_UPDATE`. It gains a real case that stores the new address and secret and signals the voice session, which dials a fresh socket and re-HELLOs. The gRPC session is never touched.

---

## 7. Session lifecycle

```
Idle ──▶ Resolving ──▶ Handshaking ──▶ Connected ──┬─▶ Rebinding ──▶ Handshaking
                            │                      │
                            └──── exhausted ───────┴─▶ Retrying ──▶ Resolving
                                                              Closed ◀── Disconnect
```

The voice session starts only once `SyncClient` has yielded `voice_secret`; without it the HELLO cannot be built.

### 7.1 Handshake

HELLO carries the 43-character base64url secret as raw UTF-8 at payload `[0:43]`. Bytes beyond are reserved and ignored by the server, so a longer payload stays valid.

We reuse the C# retry ladder — **5 attempts at 1.5 / 3 / 4.5 / 6 / 7.5 s, resending HELLO on each attempt** — with one deliberate divergence. On exhaustion C# logs *"proceeding with keepalive path"*; that cannot work against this server, because keepalives from an unbound address are dropped. We instead emit a voice-disconnected event carrying the reason and re-run the whole ladder every 15 s. The common causes are transient — server restarting, secret not yet propagated to a voice node — so permanent surrender is the wrong response.

### 7.2 Keepalive

Every **5 s**. The server's liveness threshold is 60 s with cleanup every 30 s, so this gives roughly twelve chances to survive a bad patch.

The client's keepalive payload **echoes the server's last 8-byte big-endian Unix-ms timestamp**, or is empty before the first reply. This echo is not optional decoration: it is the only input to the server's per-client latency map (`GetClientLatencyMap`). We separately time our own send-to-reply for a local RTT figure that Phase 6 will want.

### 7.3 Binding loss — the failure this protocol is most prone to

A keepalive can never rebind, by design. Any source-address change — NAT rebind, network switch, VPN toggle, roaming — therefore leaves the client transmitting into a void with **no error from anywhere**: our writes succeed, the server drops every packet, and the UI keeps saying connected.

Detection reuses what the protocol already provides. The server answers *every* keepalive, at the bound address:

- **three consecutive unanswered keepalives (≈15 s)** → assume the binding is dead, re-HELLO on the same socket
- **the retry ladder also failing** → close the socket, dial a fresh one (new source port), HELLO again

One mechanism covers NAT rebind, network change and server restart.

### 7.4 Shutdown

`BYE` on graceful disconnect, then close. `BYE` from an unbound address is inert server-side, so this is best-effort by nature; the 60 s liveness sweep is the backstop.

---

## 8. Runtime architecture

New package `internal/voice`: `packet.go`, `endpoint.go`, `session.go`, `tx.go`, `rx.go`, `jitter.go`. New leaf package `internal/audio/opus` holding vendored C, the cgo binding and a `VENDOR.md`, laid out exactly as `internal/audio/rnnoise` is.

### 8.1 Transmit

`WriteFrame` runs **on the DSP goroutine inside a 10 ms budget**, so it does memcpy and a non-blocking channel send and nothing else:

1. append two 480-sample frames into a 960-sample accumulator
2. when full, take a buffer from a small free list, copy, and **non-blocking** send to `txPCM` (8 slots ≈ 160 ms); when full, drop and count
3. return

Opus encoding and the socket write both happen on a separate `txLoop` goroutine, which emits one packet per active TX frequency — same payload, different `Frequency` field, **per-frequency 24-bit sequence counter**. Receivers key their jitter buffers by `(sender, frequency)`, so a shared counter would show them artificial gaps. Gaps must be tolerated regardless, since real loss exists and the C# peer's counter policy is its own business.

**Gate-close is a problem the `Sink` interface cannot express.** `WriteFrame` is only called while the gate is open, so a partially-filled accumulator would be glued onto the front of the *next* transmission. The sink reads an atomic generation counter, bumped on PTT release, and resets the accumulator when it changes — an ordinary atomic load on the DSP goroutine, discarding at most 10 ms of stale audio.

### 8.2 Receive

`rxLoop` drains the socket and must never block, so it only parses and enqueues:

- **HELLO_ACK** → handshake satisfied
- **KEEPALIVE** → store the echoed timestamp, record RTT, reset the unanswered counter
- **VOICE** → filter, then enqueue by `(senderID, frequencyKHz)` into that stream's jitter buffer, which holds **encoded** payloads ordered by sequence
- **BYE** → the server never sends one; ignored and counted

Drop rules, applied in order: ignore our own `SenderID` **except** on a server test frequency, where the echo is the intended loopback; drop frequencies matching no enabled radio and no global frequency (defence in depth — the server already filters).

`decodeLoop` keeps each stream's PCM ring topped up to a target depth, woken by packet arrival and by ring drain — **never by a ticker of its own** (D8). It decodes, applies that stream's radio effect, and writes PCM.

`Mixer.Mix` gains a fourth bus fed by `ReadInto`, called from `dspLoop`, which only sums pre-decoded rings: bounded work, no decode, no allocation inside the DSP budget. The mixer's existing doc already anticipates this ("Phase 5 adds received voice to the voice bus alongside monitor"); it is an addition, not a reshape.

Streams are torn down after an idle timeout.

### 8.3 Jitter buffer

Per `(sender, frequency)`. Target playout delay **60 ms**, configurable as `[voice] jitter_buffer_ms`; maximum depth **500 ms**, after which the oldest is discarded. Reorders by 24-bit sequence with wraparound handling, drops duplicates, and feeds the decoder a nil frame for loss concealment when a gap's delay budget expires. For reference, the C# peer primes at 50 ms and caps at 2500 ms; 500 ms is ample and bounds memory per talker.

### 8.4 Attachment to the audio pipeline

Exactly **one** `Sink` is registered at startup and never removed. It holds an `atomic.Pointer` to the current voice session: nil when disconnected, in which case `WriteFrame` returns immediately. Connect and disconnect are one atomic store each. This needs no `RemoveSink` and no `Close()`, and honours the seam's documented Manager-lifetime intent instead of fighting it.

---

## 9. PTT routing and the radio prerequisite

### 9.1 TX routing

`internal/app` gains a refcounted **active-TX set**: `global.ptt` contributes the selected radio, `radio.N.ptt` contributes radio N, and holding both is a set union. `Manager.SetPTT` keeps its exact current meaning — the set being non-empty — so Phase 3.5's multi-source press refcount is untouched.

`radio.N.select` has had no consumer since Phase 3; it now sets the selected radio, as does clicking a radio card. The selection is exposed as app state with a binding and an event.

### 9.2 Radio bootstrap

A new `[[radios]]` array in `config.toml` (`id`, `name`, `frequency_khz`, `enabled`, `is_intercom`), seeded with a small default set on first run. Server-side radio state is created empty by `AddClient` and lost on disconnect, so the client re-pushes its set via `UpdateRadioInfo` on every connect.

`LcdFreq` becomes editable, as the design prototype always specified — it is read-only today only because Phase 1 had nothing to tune. Edits commit through `UpdateRadioInfo` and persist locally.

### 9.3 A pre-existing frontend bug this makes load-bearing

`CommsApp.tsx` renders `Object.values(radios)[0]` — the first entry of the *whole* radios map, not the local client's. Harmless while nobody else is connected; wrong the moment voice makes multi-client sessions real. Fixed here.

---

## 10. Phase 4 follow-ups absorbed

| # | Item | Why here |
|---|---|---|
| 1 | `manager.go:614`'s false safety comment | It claims a use-after-free hazard was removed; it was *relocated* — `main.go` closes the backend with no join, and a slow sink is exactly what makes `Stop()`'s bounded join abandon `dspLoop`. This phase adds the first real sink, so the comment becomes actively misleading |
| 2 | `main_wiring_test.go` asserts nothing about defer ordering | Swapping `defer backend.Close()` and `defer am.Stop()` compiles and passes, which is worse than the bug it replaced. Guards the shutdown ordering the socket-owning session now depends on |
| 3 | `manager.go:989` `pollOnce` state emit lacks an epoch check | Device-lifecycle correctness |
| 4 | `manager.go:1031` backoff reset in the wrong branch | A device change can wait up to 30 s — obstructive during the hardware verification this phase owes |
| 5 | `dspLoop` latency ratchet | Genuine DSP work, not a tidy-up: the ticker-driven loop reads one frame per tick, so dropped ticks ratchet latency until `Drain()` discards ~160 ms. It shapes our TX packet cadence, making us bursty for every receiver's jitter buffer |

---

## 11. Testing

### 11.1 Integration, against the real server, in CI

A CI step checks out `vcs-srs-server` (public, no token) and builds it with `-tags headless`. Tests write a `config.yaml` carrying one coalition with a password, plus a generated ECDSA P-256 keypair in PEM, into `t.TempDir()`, start the binary, wait for its ports, and drive the **real client stack end-to-end**: guest login → `SyncClient` → `voice_secret` → HELLO → relay → decode → playback ring.

Locally the suite skips unless `VCS_SERVER_BIN` is set, so `go test ./...` stays green on a machine with no server.

This is the only fixture that can catch the failure mode that actually threatens this phase: a **plausible misreading** of the protocol. Any hand-written server would encode the same misreading and pass.

Cases:

- happy-path relay between two clients on a shared frequency
- wrong secret rejected; no binding created, no ACK
- VOICE from an unbound source address dropped
- keepalive echo updating the server's latency map
- the test-frequency loopback echo
- BYE from the bound address disconnecting the session
- **re-HELLO recovery after a simulated source-address change** — provoked by rebinding the client socket; the case D6 exists for, and the one worth the whole exercise

### 11.2 Unit

- **Packet** encode/parse against golden byte vectors, cross-checked against both the Go server's `SerializePacket` and the C# `EncodePacket` layout, including the UUID big-endian ordering both sides convert for
- **Jitter buffer** reorder / duplicate / gap / late arrival / overflow / sequence wraparound — a pure data structure, no server needed
- **Opus** round-trip: 960 samples in, 960 out, energy preserved; encoded length under the §4.1 budget
- **Frequency** table test asserting our advertised `float32` equals the server's computed `float32` across a frequency set — the exact-equality trap of §5, pinned
- **TX accumulator** generation reset across a gate close
- **Endpoint resolution** precedence, including the empty-string standalone case

### 11.3 Manual

A hardware checklist in the style of Phases 3 / 3.5 / 4, covering real mouth-to-ear latency, audio quality against a C# peer once RV1 clears, behaviour across a genuine network change, and Star Citizen coexistence.

---

## 12. Proto change

`srs.proto` is synced to the server's contract in **its own commit** ahead of any Phase 5 code, verified byte-for-byte against `vngd-srs-server` `3d5ca96` (sha256 `0d632e1c…56f25a`). Additive only: `ServerSyncResult.voice_secret = 6` and `VoiceAddressUpdate.voice_secret = 3`, plus the server's `UNIMPLEMENTED` annotations on `ServerInitializationResponse`, `DistributionUpdate` and `VoiceHostDetails.secret`.

Surfaced explicitly per CLAUDE.md rather than folded into implementation.

`docs/PROTO_GAPS.md` is corrected alongside it. Its per-radio-encryption row proposed deriving an encryption key from `VoiceHostDetails.secret` — a field the server has now annotated as never populated. The row is amended to say so, and to record that the live `voice_secret` is *not* a drop-in replacement for that purpose: it travels in cleartext in the HELLO payload and inherits the unclosed replay exposure of RV4, so per-radio encryption needs its own key material negotiated over the TLS control path.

---

## 13. Risks

| # | Risk | Mitigation |
|---|---|---|
| RV1 | **The C# peer cannot currently complete a handshake.** PR #253's `CreateHelloPacket(Guid, uint)` sends no payload, so against the current server every HELLO is rejected with `missing or short secret`. Its retry ladder then falls through to a keepalive path that is also dropped | Entirely outside this repo. Cross-client interop is untestable until that branch adds the 43-byte payload; raise it with whoever owns the branch. Our own integration tests do not depend on it |
| RV2 | **Hardware-verification debt compounds.** Phases 3, 3.5 and 4 are all code-complete but unverified | A Phase 5 checklist in the same style. The honest position is that the pile is growing and needs a dedicated verification pass |
| RV3 | **libopus vendoring** enlarges the cgo surface on all three OSes and lengthens CI builds | The per-OS matrix from Phase 4 already exists and already builds cgo. Land the vendoring against an empty binding first, as D14 did for RNNoise |
| RV4 | **HELLO replay stays open**, by the server's documented decision — a captured HELLO stays valid for the life of the session | Inherited, not introduced. Nothing client-side closes it; it needs transport encryption. Recorded so the path is not read as fully hardened |
| RV5 | **Silent truncation above 1024 bytes** and **silent drop at ≤5 bytes** are both server behaviours with no error | DTX off (D1) and an asserted encoder output length (§4.1) |
| RV6 | **A server release can red the client's CI**, since integration tests build the server from source | That coupling is the point — it is an interop canary. Pin to a tag rather than a branch if it proves noisy |

---

## 14. Dependencies

| Dependency | Kind | Status |
|---|---|---|
| vendored libopus + cgo binding | new, in-tree | **approved** during brainstorming |
| `github.com/google/uuid` | new Go module | **proposed, needs sign-off** — see below |
| `vcs-srs-server` built from source in CI | build-time only, not a Go dependency | **approved** — deliberately *not* imported as a module, which would force `grpc` 1.66 → 1.77 on the client as a side effect of a test |

**On `github.com/google/uuid`.** The client never *generates* a session id — it receives one as a string from `InitAuth` and must place its 16 bytes into the packet header. That is parsing, not generation, and it is roughly twenty lines to hand-roll.

The case for taking the dependency anyway: the byte order is RFC 4122 big-endian, which is exactly the conversion the C# peer has to perform by hand in both `EncodePacket` and `DecodePacket` and is an easy thing to get subtly wrong; the server already uses this module, so both ends parse identically by construction; and it is a small, zero-dependency, widely-audited module. It is also what `rules/common/development-workflow.md` asks for — prefer a battle-tested library over hand-rolled utility code.

The case against: it is a new module on the client for one function's worth of work, and hand-rolling keeps the dependency count at zero for something fully covered by the golden-vector tests in §11.2.

**Recommendation: take the dependency.** Flagged here rather than assumed because CLAUDE.md requires explicit approval for any new Go or npm dependency.

---

## 15. Definition of Done

1. A client connects, keys up, and is heard by a second client on the same frequency, against a real server.
2. Transmission is gated by PTT; `global.ptt` uses the selected radio, `radio.N.ptt` uses radio N, and holding both transmits on both.
3. Received audio is jittered, decoded, radio-effected per stream, and mixed into the voice bus; global frequencies bypass effects.
4. A wrong or missing secret fails the handshake with an honest, distinguishable event.
5. A source-address change is detected within ~15 s and recovered by re-HELLO without user action.
6. A `VoiceAddressUpdate` re-points the voice socket without disturbing the gRPC session.
7. Radios persist across restarts, are tunable in the UI, and are re-pushed on every connect.
8. `go build`, `go vet`, `go test -race ./...` green across all packages with `-tags purego`; frontend `vitest`, `tsc --noEmit` and production build green.
9. Integration tests pass in CI against the real headless server on the per-OS matrix.
10. A manual hardware-verification checklist is written and committed.
