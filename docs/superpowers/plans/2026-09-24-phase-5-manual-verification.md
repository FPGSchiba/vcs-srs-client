# Phase 5 — Manual Verification Checklist

**Status: NOT YET EXECUTED — requires a human on real hardware, and for one
item a second machine/peer.**

This checklist exists because Task 14 (documentation and phase close-out) ran
in a sandboxed CI-like environment with no audio device, no GUI, and no C#
peer to talk to. Every automated check that *can* run headlessly (`go build`,
`go vet`, `go test -race`, the integration suite against a real headless
server, `tsc --noEmit`, `vitest`, the Vite production build) has been run and
is reported in `.superpowers/sdd/progress.md`. **Nothing below has been
executed by an agent, and — more starkly than any prior phase — nothing in
Phase 5 has ever been heard by a human.** No audio device, no real
microphone, no speakers, no C# peer has been involved anywhere in this
phase's verification. The automated suite is green and the integration tests
drive a real server, but no one has listened to a single frame. Do not treat
any item here as verified until a human has actually performed it and
recorded the result.

Each item names the spec decision or DoD item it satisfies
(`docs/superpowers/specs/2026-09-24-vcs-client-phase-5-udp-voice-design.md`),
where applicable.

## Setup

1. Build and launch the app natively on the target OS: `wails3 build` then
   run the produced binary (or `wails3 dev` for faster iteration on items
   that don't depend on a packaged binary — re-check persistence-sensitive
   items against the packaged build at least once).
2. A headless server instance reachable at a known local address, with at
   least two guest accounts able to join the same coalition (three or more
   for item 9's multi-client case). `VCS_SERVER_BIN` is the same binary the
   automated integration suite uses.
3. A wired or Bluetooth headset (mic + output) — see Phase 4's checklist for
   device setup; this phase assumes Phase 4's device selection already works.
4. Star Citizen, or a stand-in that opens an exclusive-feeling WASAPI/CoreAudio
   stream, for the coexistence item (§6) — carried over from Phase 4, now
   with real voice traffic flowing rather than passthrough only.
5. A joystick/gamepad with a bindable button, on Windows or Linux (Phase 3.5
   scope), for item 7.
6. Network conditions you can actually change mid-session for item 5:
   Wi-Fi and Ethernet both available on the test machine, and/or a VPN you
   can toggle.
7. **Cross-client interop with the C# peer cannot be tested at all right
   now.** `VNGD-SimpleRadioStandalone` PR #253's `CreateHelloPacket` sends no
   voice secret. Against the current server every HELLO from that branch is
   rejected and it never receives audio. This is not a setup step you can
   complete — item 2 below documents it as blocked, not as "skip if you don't
   have a C# build."

---

### 1. Real mouth-to-ear latency, measured — design §5 realtime budget

1. With two clients connected to the same server, tuned to the same
   frequency, measure round-trip or one-way latency from speech at the
   sender's mouth to audible output at the receiver's ear. A simple method:
   clap or say a sharp consonant near both a reference recorder and the
   transmitting mic simultaneously, then compare timestamps in the recording
   of the received audio.
   **Expected / target:** total mouth-to-ear latency in the low hundreds of
   milliseconds — record the actual measured number. There is no hard target
   pinned in the design doc beyond "realtime," so this item's job is to
   produce the first real number, not just a pass/fail. Flag it as a finding
   if it is perceptibly laggy (walkie-talkie-bad, not just non-zero).
2. Repeat with NS/AGC/effects enabled (the normal operating configuration)
   and note whether DSP processing adds a perceptible increment.

### 2. Cross-client interop with the C# peer — BLOCKED, not merely untested

**This item cannot be executed today.** Do not attempt to "test around" it.

`VNGD-SimpleRadioStandalone` PR #253's `CreateHelloPacket` does not send the
voice secret the server now requires (`ServerSyncResult.voice_secret`).
Every HELLO the C# branch sends is rejected by the server's authentication
check, so a C# client on that branch never receives audio from us and we
never receive audio from it — there is no partial interop to observe, just a
silent handshake failure.

**What has to change before this is testable:** PR #253 needs to add the
voice secret to its `CreateHelloPacket`, matching what this client's
`internal/voice` HELLO already does. That is server-team/C#-team work, not
ours. Once that lands, this item should cover at minimum:
- HELLO handshake succeeds both directions.
- Audio sent from VCS is audible and undistorted on the C# client.
- Audio sent from the C# client is audible and undistorted on VCS.
- The D7 effect-placement fix (item 3 below) sounds correct from the C#
  side specifically, since that is the side D7 exists to protect.

Record this item's status as **BLOCKED on VNGD-SimpleRadioStandalone PR
#253**, not as "not yet run."

### 3. Radio-effect placement (D7: TX → RX) — unverified against a real listener

Design decision D7 deliberately moved radio-effect processing from the
sender's TX path to the receiver's RX path, specifically because the C#
peer applies effects on receive: left on TX, a C# listener would hear us
double-effected (our TX effect plus their RX effect) while we would hear
them completely dry (no effect at all, since they apply on receive and we
were listening for a TX-side effect that never came).

**This has never been verified against a real listener of any kind** — C#
or another VCS instance. Since item 2 (C# interop) is blocked, this item at
minimum needs a **second VCS client**:

1. Two VCS clients, same server, tuned to the same non-global frequency.
   Transmit from client A with the radio-effect preset engaged.
   **Expected:** client B hears exactly one instance of the effect (applied
   on B's RX path), not zero and not double.
2. Confirm the effect is absent (dry) on any GLOBAL frequency transmission,
   per the "no effects on global frequencies" rule D7 exists to make
   implementable from the receiving side.
3. If and when item 2 unblocks, repeat against the C# peer specifically —
   that is the pairing this decision was made for.

### 4. A quiet room — the sub-5-byte relay floor, heard rather than reasoned about

The server relays only voice payloads longer than 5 bytes. A genuinely
silent Opus frame encodes to 2–3 bytes even with DTX off (confirmed live
during Task 12's integration testing against the real server), so frames
from a silent room are **silently discarded** — no error, no log line, no
counter anywhere on either side. The deliberate decision, made and recorded
during this phase, was **not** to pad silent frames client-side: the C# peer
encodes silence identically, and padding would make us behave differently
from the peer on the wire. This is being recorded as a protocol
characteristic, not a defect — but nobody has listened to it happen.

1. Key up (PTT engaged) in a genuinely quiet room and stay silent for
   several seconds, then speak a short phrase, then go silent again.
   **Expected:** ask, plainly — does the quiet room sound right? Is there
   anything a listener would call a glitch, a stutter, or an odd silence
   during the quiet stretch? Is the transition INTO speech (silence →
   voice) clean, with no clipped first syllable or pop? Is the transition
   OUT of speech (voice → silence) clean, with no tail artifact?
2. Compare against a stretch where you speak continuously (no natural
   pauses) — confirm that case sounds normal, to isolate whether any
   artifact found in step 1 is specific to the silence-drop behaviour.
3. Record a plain pass/fail on "does a quiet room sound right" — this is a
   subjective human judgment call the automated suite structurally cannot
   make, since the frames in question never reach the wire to be tested.

### 5. `maxCatchUpFrames = 3` against a real 10 ms budget

Task 2's catch-up formula (`max(1, min(framesPresent, 1+maxCatchUpFrames))`)
was verified for correctness (no over-read, no spurious silent frame) and
for zero allocation, but `maxCatchUpFrames = 3` — up to 4 chain passes per
DSP tick in the worst case — has never been measured against a real 10 ms
tick budget with a live socket sink registered (i.e. with Phase 5's TX path
actually feeding the voice UDP socket, not just an internal/audio sink).

1. With the app fully wired (audio device open, voice session connected,
   TX actively sending), deliberately stall the DSP loop briefly — e.g.
   pause the process under a debugger for ~40 ms, or induce a scheduling
   hiccup with background load — then resume, forcing a multi-frame
   catch-up burst.
   **Expected:** no audible glitch, pop, or dropout during or immediately
   after the catch-up burst; the 10 ms tick budget is not visibly exceeded
   (no growing latency, no watchdog/overrun log if one exists).
2. Under sustained ordinary load (game + other apps running, per Phase 4's
   item 12), confirm TX audio quality holds up the same way passthrough did
   in Phase 4 — this item is specifically about the catch-up path now that a
   real socket sink is attached, not a re-run of Phase 4's item 12.

### 6. Recovery from a genuine network change — binding-loss path, real hardware

Task 7 covers this with an integration test against injected binding loss,
but the real detection and re-HELLO path — driven by an actual OS-level
network change — has never run.

1. With a voice session actively connected and transmitting/receiving on a
   live frequency, switch the machine's active network path — e.g. disable
   Wi-Fi and switch to a wired Ethernet connection, or toggle a VPN
   connection on/off.
   **Expected:** the client detects the binding loss (the old socket stops
   receiving), re-dials, and re-establishes the voice session. Per the
   design's binding-loss path, voice should return within roughly **15
   seconds** with **no user action** — no manual reconnect click, no app
   restart.
2. Confirm control (gRPC) state and voice state both recover consistently —
   the UI should not show "connected" while voice is silently dead, or vice
   versa.
3. Repeat at least once with the opposite direction of the network change
   (e.g. Ethernet → Wi-Fi) to rule out an asymmetric fix.

### 7. Star Citizen audio coexistence, both launch orders

This is Phase 4's item 6, re-run now with **real two-way voice traffic**
flowing (not just mic passthrough/TEST MIC) — the concern is whether a live
UDP voice session changes the coexistence story at all (e.g. via CPU load
from the codec, or unexpected device re-opens on voice connect).

1. Start Star Citizen first, confirm its audio is normal, then start VCS,
   connect to a server, tune a radio, and hold a real conversation with
   another client over PTT while SC plays audio.
   **Expected:** SC's audio is completely unaffected — no dropouts, no
   glitching, no reinitialization — and voice TX/RX both work cleanly.
2. Reverse the order: VCS first (connected, transmitting/receiving), then
   launch SC.
   **Expected:** SC comes up normally; VCS's voice session is unaffected by
   SC's device open.
3. With both running for several minutes and active back-and-forth voice
   traffic, confirm neither app's audio glitches, drops, or gets muted by
   the other.

### 8. PTT from keyboard and joystick, including holding two at once

This is Phase 3.5's refcount test, now carrying real transmitted audio
instead of an internal passthrough/VU indication.

1. Hold a keyboard-bound PTT trigger while connected to a server with
   another client listening. Confirm the listening client hears the
   transmission open and close with the configured delays.
2. Repeat with a joystick-bound PTT trigger.
3. Bind Global PTT to both a keyboard key and a joystick button. Hold the
   keyboard key, then also press the joystick button, then release only the
   keyboard key.
   **Expected:** the listening client's received audio CONTINUES — the
   transmission must not cut while the joystick button is still held.
4. Release the joystick button. Confirm the listening client hears the
   transmission end (respecting the release delay).

### 9. Radio tuning persisting across a restart

1. Tune a radio to a non-default frequency, select it, and set any other
   per-radio state the UI exposes. Quit the app fully (not just
   disconnect).
2. Relaunch and reconnect.
   **Expected:** the tuned frequency and radio selection are restored from
   `config.toml`'s `[[radios]]` table without any manual re-entry, and
   voice on that frequency works immediately without needing to re-tune.

### 10. Multi-client session, 3+ participants, overlapping frequencies

1. Connect three or more clients to the same server. Tune at least two of
   them to the same frequency and at least one to a different frequency (or
   a global frequency).
   **Expected:** clients on the shared frequency hear each other; the
   client on the different frequency does not hear traffic meant for the
   other frequency (unless it is global). Confirm no audio from one
   frequency leaks onto another, and that the jitter buffer / mixer handles
   two simultaneous talkers on the same frequency without one silently
   dropping the other.
2. Have two of the clients transmit at close to the same time (deliberate
   overlap).
   **Expected:** both are audible (mixed) or the behavior is at least
   graceful (no crash, no stuck gate) — record what actually happens, since
   the design does not specify a priority/ducking scheme for simultaneous
   talkers.

### 11. The test-frequency loopback as a self-check

The server has a designated test frequency where the drop rule for "ignore
our own SenderID" is inverted — a client transmitting on the test frequency
hears its own echo back. This is a self-check any single person can run
without a second participant.

1. Tune to the server's test frequency and key up, speaking a short phrase.
   **Expected:** you hear your own transmission echoed back, confirming the
   full TX → server → RX round trip works end-to-end without needing a
   second client. Use this as the first, cheapest sanity check before
   attempting any of the two-client items above.

---

## Known, already-documented gaps this checklist is not meant to "fail" on

These are pre-existing, understood limitations — running the checklist above
should confirm the *expected* behavior, not surface them as new bugs:

- **Cross-client interop (item 2) is structurally blocked**, not a gap in
  this checklist's coverage — it cannot be executed until
  `VNGD-SimpleRadioStandalone` PR #253 adds the voice secret to its HELLO.
  Do not attempt workarounds (e.g. hand-patching a local C# build) as part
  of routine verification; if that becomes necessary it is its own tracked
  effort.
- **The sub-5-byte silent-frame drop (item 4)** is a deliberate protocol
  characteristic, not a bug: padding client-side was explicitly rejected
  because it would make this client behave differently from the C# peer on
  the wire. Item 4 exists to confirm it *sounds* fine, not to demand it be
  "fixed."
- **No echo cancellation** remains out of scope (carried from Phase 4) —
  running open speakers + mic and hearing an echo is expected, not a bug.
- **This is the fourth consecutive phase accruing hardware-verification
  debt** (Phases 3, 3.5, and 4 all shipped with unexecuted manual
  checklists before this one). Phase 5 additionally requires a second
  client and, for the fullest coverage, a C# peer that cannot currently
  authenticate at all.
- **Enabling VOX does not transmit in Phase 5.** Settings presents VOX as a
  transmit mode, and internal/audio's gate genuinely opens for it
  (`gate.go`: `pttOpen || voxOpen`), so `dspLoop` calls `Sink.WriteFrame` on
  a VOX-opened gate exactly as it does for PTT. But
  `App.SetTXFrequencies` is only ever driven by the PTT press/release path
  (`internal/app/audio.go`'s `dispatchAudioPressed`/`dispatchAudioReleased`)
  -- VOX never calls it. With no PTT ever pressed, the voice session has no
  TX targets at all and every VOX-opened frame is silently dropped as
  `DroppedNoTarget`: VOX transmits nothing. Wiring VOX to actually key the
  radio is unplanned scope for this phase and was deliberately not done
  (see the wave-B fix-round notes for I3). The gate-open path that DOES run
  today for VOX exists for local metering and monitoring only (VU levels,
  mic passthrough), not for transmission -- do not read a moving VU meter
  under VOX as evidence that anyone else can hear it, they cannot.
  Wave B's I3 fix additionally closes a related, more dangerous bug (a
  stale PTT target surviving indefinitely and letting a LATER VOX trigger
  key an old frequency with no key held); that fix does not enable VOX
  transmit, it only guarantees VOX's current no-op is a clean no-op rather
  than an invisible one.
- **Selecting a radio and immediately quitting can lose that one selection
  on restart.** `queueSelectedRadioPersist` (`internal/app/voice.go`)
  deliberately moved the selected-radio `config.Save` off gohook's event-
  reader goroutine and onto a fresh, unwaited goroutine per selection --
  correctly, since a blocking save there can stall the OS key stream, which
  `internal/hotkeys/dispatch.go` documents as a cause of a stranded-open
  mic. The trade-off is that nothing waits for that goroutine: quitting
  (or crashing) before it runs can cut the persist off entirely.
  `config.Save` is write-temp-then-rename, so the worst case is bounded and
  never corrupts the config -- either the rename already happened and the
  selection is saved, or it did not and `config.toml` is untouched at its
  previous, still-valid selection, at most leaving behind a stray `.tmp`
  file. A tester who selects a radio and immediately quits may see that
  selection not survive the restart; this is expected, not a bug.

## Four issues found during this phase that are OUTSIDE its scope

These were found during Phase 5 implementation and review but are not Phase
5 defects — they are pre-existing or cross-repo problems awaiting the user's
decision. They are **not** manual-verification items; they are recorded here
for visibility and are tracked primarily in `docs/ROADMAP.md`'s Phase 5
entry:

1. **Windows release builds ship with no audio at all** —
   `build/windows/Taskfile.yml` defaults `CGO_ENABLED=0`, selecting the stub
   malgo backend, the no-op denoiser, and the stub Opus codec, on the only
   platform Star Citizen runs on. Pre-existing since Phase 4; Phase 5 raises
   the severity from "a quality feature is missing" to "the product cannot
   do its job."
2. **`.github/workflows/release.yml` never runs `buf generate`**, so a
   release build cannot compile against the gitignored `srspb/`. Latent — no
   tag has ever been pushed, so the workflow has never actually run.
3. **A cross-repo version landmine** — `vngd-srs-server/srs/utils.go:13` is
   `return version == "0.1.0"`, a hardcoded string equality rather than a
   semver range. The client sends `version.Client`, currently exactly
   `"0.1.0"`. Bumping the client version breaks every login, and nothing in
   either repo signals that coupling.
4. **The Windows CI leg has never run on a real runner**, and
   `protoc-gen-go-grpc`'s version pin has no automatic drift signal.

## What to do with results

Record pass/fail per numbered item (not per DoD/decision number) directly in
this file or a copy of it, including OS/platform, device make/model, and
server build/commit used. File any genuine failure (something behaving
worse than the documented gaps above predict, or any deviation from a stated
expectation above) as a new task rather than patching it silently, per the
project's standard workflow.
