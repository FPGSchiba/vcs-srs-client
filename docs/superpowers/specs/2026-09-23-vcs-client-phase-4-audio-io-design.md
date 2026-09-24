# Phase 4 — Audio I/O — design

**Date:** 2026-09-23
**Status:** approved design, ready for implementation planning
**Builds on:** [`2026-05-31-vcs-client-design.md`](./2026-05-31-vcs-client-design.md) §3, §5, §9 (R3)
**Roadmap row:** [`docs/ROADMAP.md`](../../ROADMAP.md) — Phase 4

---

## 1. Goal

Give the client a working audio engine: open a real input and output device, clean the microphone signal, meter it, gate it behind PTT/VOX, colour it with radio effects, play bundled SFX, and let the user pick devices and levels in Settings.

Phase 5 (UDP voice) is **blocked** on the server-side protocol reference, so this phase has no network sink. Instead it ships the **complete capture → process → playback chain with a local monitoring sink**. That is not a stopgap: it makes the phase verifiable on real hardware the day it lands, and Phase 5 replaces one `Sink` implementation rather than reworking a pipeline.

---

## 2. Decisions

Settled during brainstorming. Not open for re-litigation during implementation.

| # | Decision | Rationale |
|---|---|---|
| D1 | Phase 4 ships a **local loopback proving ground** — full chain, local sink | Phase 5 is blocked; a device-to-device chain is verifiable now and Phase 5 swaps only the sink |
| D2 | **Vendored RNNoise (cgo)** for noise suppression, **pure-Go AGC** | RNNoise is self-contained MIT C with no system deps or pkg-config — compiles as part of the Go package on all three OSes. AGC is tractable and testable in Go |
| D3 | **No WebRTC APM, no speexdsp** | APM needs `libwebrtc-audio-processing` as a system library with autotools — exactly the per-OS install burden R3 warned about. speexdsp's NS is materially weaker |
| D4 | Internal format is **48 kHz, mono, float32, 480-sample (10 ms) frames** | Forced by RNNoise, which accepts only 480-sample frames at 48 kHz. Everything else follows from that |
| D5 | **Ring-buffer pipeline, one owned DSP goroutine** | Keeps cgo, locks and allocation off the realtime audio threads; makes the DSP chain a pure function over frames, testable with no hardware |
| D6 | **Shared mode, never exclusive**, on both devices | Direct echo of Phase 3.5's non-exclusive DirectInput decision: the user is running Star Citizen at the same time. Exclusive capture would break the game's audio the way `DISCL_EXCLUSIVE` would have broken its force feedback |
| D7 | Hot-plug is a **~2 s polled enumeration diff** | Matches `internal/joystick`'s polled-manager house pattern. miniaudio's notification callback fires on an OS thread with re-entrancy constraints; consistency beats a marginal gain on a 2 s UI refresh |
| D8 | **VOX, PTT start/release delays, mic passthrough and radio-effect DSP presets are in scope**; recording is not | All four act on the microphone path, which is being built this phase regardless. Recording needs received audio — Phase 5 |
| D9 | **Four level buses**: master, voice, sfx, notification | Each maps to a category the design already separates elsewhere; only levels were missing. Per-radio volume/balance/mute stay in Comms (Phase 5/6) |
| D10 | Audio settings extend the **existing** `SettingsDTO` / `GetSettings` / `SetSettings` | One settings path, one `settings:changed` event, one `useSettingsSync`. A parallel path is what caused the Comms-popout divergence documented in `SettingsScreen.tsx` |
| D11 | SFX engine is **asset-agnostic**; the sample pack is a blocking dependency on the user | The design credits real contributors (`AUDIO · Spaceharvest, JohnMckeel`); inventing or substituting their files is not ours to do |
| D12 | `.preset` entries are **built-in named configurations in code**, not files | `comms_filter_mid` / `saturated_overdrive` are DSP settings. No preset file format, no parser |
| D13 | **Hand-rolled minimal RIFF/PCM WAV decoder**, not a dependency | ~150 lines, fully unit-testable, avoids a fourth new dep on a phase that already adds two |
| D14 | The **per-OS CI matrix lands before the RNNoise vendoring** | Closes R3, which was assigned to Phase 3 and never landed. Toolchain failures should surface against an empty package, not a finished one |

---

## 3. Scope

**In:**

- malgo device lifecycle: enumerate, open, close, re-open, teardown (input + output)
- Device picker UI in Settings → Audio & Sounds, with `System Default` as a first-class value
- Noise suppression (RNNoise) and AGC (pure Go), each independently toggleable
- VU metering, published at ~20 Hz
- VOX: threshold, minimum length, hang, and a pre/post-NS evaluation toggle
- PTT start delay and release delay, driven by the existing Phase 3/3.5 keybind subsystem
- `global.push_to_mute` and `global.mute_toggle` — which exist in `internal/keybinds/actions.go` with default bindings `V` and `M` and, today, **no consumer at all**
- Mic passthrough as a persisted user setting
- Radio-effect DSP presets: voice effect and clipping effect
- SFX engine: nine effect slots, enable toggles, preview, bundled-asset loading
- Four level buses with a perceptual taper
- Per-OS CI matrix (closes R3)

**Out:**

- Any network transport, Opus encoding, or jitter buffering — Phase 5
- Recording received transmissions — needs RX audio, Phase 5
- Per-radio volume / balance / mute UI — Comms surface, Phase 5/6
- Echo cancellation — not in the design, and headsets are the assumed hardware
- User-supplied custom sample directories — Phase 7, alongside profiles
- Axis-based input of any kind — remains out per Phase 3.5 D2

---

## 4. Audio format and the Phase 5 seam

Internal format is **48 kHz, mono, float32**, in **480-sample (10 ms) frames**.

This is not a free choice. RNNoise processes exactly 480 samples at 48 kHz; anything else requires buffering around it, so the pipeline adopts its granularity. 48 kHz is also Opus's native rate, so whatever frame duration Phase 5's reference dictates (20 ms and 40 ms are both common) is an integer multiple of 10 ms.

Device-native → internal conversion is **miniaudio's built-in data converter**, requested through `DeviceConfig` (f32 / 1 channel / 48000). No hand-rolled resampling on the capture path.

**The seam.** The DSP goroutine writes processed frames to a `Sink`:

```go
type Sink interface {
    WriteFrame(frame []float32)  // len(frame) == 480; must not retain
    Close() error
}
```

Phase 4 ships exactly one implementation — the loopback monitor. Phase 5 adds an encoder sink and, if the server's frame duration differs, a repacketizer **at that boundary only**. This confines the cost of the unknown wire format to a single, named interface rather than spreading a guess through the pipeline.

---

## 5. Runtime architecture

Four threads. The two realtime ones are deliberately trivial.

```
 ┌─ capture callback (OS audio thread) ──────────┐
 │  miniaudio converts to 48k/mono/f32           │
 │  copy into captureRing; return                │   no cgo, no locks, no alloc
 └───────────────────────────────────────────────┘
                     │  480-sample frames
                     ▼
 ┌─ DSP goroutine (the only place work happens) ─┐
 │  NS (RNNoise) → AGC → gate → voice effect     │
 │  → Sink(s)                                    │
 │  mixes monitor + SFX voices → playbackRing    │
 │  accumulates VU (peak-held)                   │
 └───────────────────────────────────────────────┘
                     │
                     ▼
 ┌─ playback callback (OS audio thread) ─────────┐
 │  drain playbackRing; underrun → silence       │   pure ring drain
 └───────────────────────────────────────────────┘

 ┌─ event pump (~20 Hz ticker) ──────────────────┐
 │  audio:vu, audio:state                        │
 └───────────────────────────────────────────────┘
```

### Chain order

`NS → AGC → gate → voice effect`, and the order is load-bearing:

- **NS before AGC**, so AGC cannot amplify the noise floor during silence — the classic failure where a quiet room turns into a hiss swell between sentences.
- **Gate after AGC**, so the gate decides on a levelled signal rather than chasing raw input gain.
- **Voice effect last**, because it is coloration and belongs closest to the sink.

### Rings

Single-producer / single-consumer `float32` rings with atomic indices. No mutex on the audio path. Capture ring ≈ 200 ms, playback ring ≈ 100 ms. Overflow drops the **oldest** frame and increments an overrun counter; underflow emits silence and increments an underrun counter. Both counters surface in `audio:state`, so a struggling machine is diagnosable rather than mysteriously crackly.

### SFX mixing lives in the DSP goroutine

The playback callback is a pure drain. Mixing in the DSP goroutine costs at most one frame (10 ms) of SFX latency — imperceptible — and buys a callback that never touches voice state or takes a lock. A fixed pool of **8 voices**, oldest evicted, mixed without allocation.

---

## 6. Gate semantics

The gate is a **state machine over frame counts**, not wall-clock timers. Deterministic, testable with no clock and no device.

Inputs:

- **PTT** — any of `global.ptt` or `radio.N.ptt`, already refcounted across keyboard and joystick sources by Phase 3.5, so keyboard + HOTAS held together cannot cut transmission mid-word.
- **VOX** — frame RMS against `vox_threshold`, requiring `vox_min_length_ms` of sustain to open, with a hang time before closing. `vox_noise_cancel` selects whether VOX evaluates the **post-NS** or **raw** signal.
- **Mute** — `global.push_to_mute` (hold) and `global.mute_toggle` (press).

Resolution:

```
open = (PTT || VOX) && !muted
```

Mute **overrides** both, unconditionally. A user who hits push-to-mute expects silence regardless of what else is asserting.

`ptt_start_delay_ms` delays opening after press; `ptt_release_delay_ms` holds the gate open after release so word-final consonants are not clipped. Both are expressed in frames internally.

---

## 7. Level model and mixer

Four buses:

```
output = master × ( voice × monitor
                  + voice × Σ(per-radio gain × pan)   ← Phase 5/6
                  + sfx × Σ(effect voices)
                  + notification × Σ(notification voices) )
```

Mic passthrough monitors on the **voice** bus.

Levels persist as normalized **knob position** (0–1) and are converted to gain with a **perceptual taper** at mix time. A linear 0–1 wired straight to amplitude makes the top third of every knob inaudible as a change; the taper is what makes a knob feel like a knob.

Per-radio `volume` / `balance` / `muted` already exist in the design's radio model (`design/vcs/project/app.jsx`) but act on received voice. They are **not built this phase**; the mixer defines their slot so Phase 5 drops them in without reshaping it.

---

## 8. Device lifecycle and hot-plug

The device layer sits behind an interface so the entire manager is testable without hardware:

```go
type Backend interface {
    Enumerate() (inputs, outputs []DeviceInfo, err error)
    OpenCapture(id string, onFrames func([]float32)) (Stream, error)
    OpenPlayback(id string, fill func([]float32)) (Stream, error)
}
```

`backend_malgo.go` is the real implementation; `backend_fake.go` (test-only) scripts input frames and records output.

**Context.** One shared `malgo.Context` per process, lazily initialized on first use, freed on shutdown. miniaudio selects WASAPI / CoreAudio / ALSA-PulseAudio itself; we do not override the backend priority list.

**Identity.** A device persists as **ID + display name**, mirroring the `keybind_devices` precedent in `internal/config` — so a device that is currently unplugged still renders a meaningful name instead of a raw ID. `System Default` is stored as an **empty ID** and stays unresolved: a user who picks it wants to follow the OS, not to be silently pinned to whatever was default on the day they chose it.

**Hot-plug.** A ~2 s ticker re-enumerates and diffs against the previous snapshot, emitting `audio:devices_changed` on any difference.

**Device loss while open.** Bounded-backoff re-open attempts against the same ID; if the device is genuinely gone, fall back to the system default and raise `audio:state`. Never crash, never spin, never a modal dialog — the user may be mid-combat.

**Teardown order.** Stop DSP goroutine → stop streams → uninit context. A device change restarts only the affected stream.

---

## 9. Platform notes

Unlike Phase 3.5, **no platform is stubbed out** — audio works on all three.

- **Windows** — WASAPI shared mode. cgo via mingw, present on the runner.
- **macOS** — CoreAudio. Microphone access triggers a TCC prompt (`NSMicrophoneUsageDescription`); the Info.plist string must be added in this phase or the app is killed on first capture. This is a **second** TCC surface alongside Phase 3's Accessibility grant, and the two are independent: denying one says nothing about the other.
- **Linux** — miniaudio runtime-links ALSA and PulseAudio (`dlopen`) rather than linking at build time, so **no new apt dev packages are expected** in CI. This is an expectation to confirm in the first implementation task, not an assumption to build on. PipeWire is reached through its PulseAudio compatibility layer.

---

## 10. Persistence

A new `[audio]` table, purely additive. Every field gets a value in `Default()` so existing `config.toml` files load unchanged — the convention documented on the `Config` struct.

```toml
[audio]
input_device  = ""        # "" = follow system default
output_device = ""
input_device_name  = ""   # display only, like keybind_devices
output_device_name = ""
mic_passthrough = false
agc = true
noise_suppression = true
vox = false
vox_threshold = 0.35      # normalized; UI renders the design's 0-100
vox_min_length_ms = 220
vox_noise_cancel = true   # evaluate VOX post-NS rather than raw
ptt_start_delay_ms = 0
ptt_release_delay_ms = 120
voice_effect = "comms_filter_mid"
clipping_effect = ""      # "" = off

[audio.levels]
master = 0.75
voice = 1.0
sfx = 0.8
notification = 0.8

[audio.effects.tx_start]
enabled = true
file = "transmit_open.wav"
# ... one sub-table per effect id
```

Levels and thresholds persist normalized 0–1 with the UI doing the 0–100 presentation, so the on-disk format does not inherit a widget's scale.

---

## 11. Frontend surface

### Bindings

Audio settings extend the existing `SettingsDTO`. The engine is injected with the established pattern, `SetAudioBackend(*audio.Manager)`, alongside `SetSettingsBackend` and `SetJoystickBackend`.

```go
func (a *App) GetAudioDevices() AudioDevicesDTO   // {inputs, outputs} · {id, name, isDefault}
func (a *App) GetAudioState() AudioStateDTO       // mirrors GetHotkeyState / GetJoystickState
func (a *App) StartMicTest() error
func (a *App) StopMicTest() error
func (a *App) PreviewEffect(id string) error
```

### Events

```
audio:devices_changed   full device lists, from the hot-plug diff
audio:vu                {input, output} at ~20 Hz
audio:state             {running, inputError, outputError, overruns, underruns}
audio:mic_muted         bool
```

`audio:state` mirrors `hotkeys:state` and `joystick:state` deliberately, including their **informational-vs-error** distinction, so Phase 7's notification channel can absorb all three uniformly.

**VU is suppressed when unchanged.** An idle or muted mic emitting 20 identical zeros per second to every open window is pure bus noise; the meter only needs transitions.

Master spec §5.4 predicted the name `audio:device_changed` (singular). This design uses the **plural**, because the payload is the full list rather than one device.

### Settings UI

Section `audio` ("Audio & Sounds") replaces its `Deferred` stub:

- **DEVICES** — microphone select, speakers select, mic passthrough toggle
- **LEVELS** *(new panel)* — master / voice / sfx / notification knobs in a row
- **MIC TEST** — TEST MIC button + live VU
- **PROCESSING** — AGC, NS, VOX (+ threshold, min length, noise cancel), PTT start/release delay

Section `effects` ("Radio Effects") replaces its stub with all nine rows the design lists: seven sample rows (sample select, PREVIEW button, enable toggle) and two preset rows for the voice effect and clipping effect, whose selects offer built-in preset names rather than filenames.

**Approved deviation from the prototype.** `design/vcs/project/screens/settings.jsx` puts the MASTER knob inside the MIC TEST panel. Four knobs there would conflate input testing with output mixing, so all four move into a dedicated **LEVELS** panel and MIC TEST keeps only its button and VU. A knob row is an established pattern in the design's own component gallery (`design/vcs/project/screens/misc.jsx`). Approved by the user on 2026-09-23.

---

## 12. SFX engine and asset contract

Samples ship via `go:embed` from `internal/audio/assets/`, described by a **manifest** declaring each effect id, its default filename, and the asset contract:

> **48 kHz, mono, 16-bit PCM WAV.**

Off-spec sample rates are linearly resampled at load with a logged warning — adequate for short chirps, and it keeps a mis-specified asset from being silently wrong rather than obviously wrong.

**Until the pack arrives, the directory holds only the manifest.** A missing sample is silent, logged **once**, and never a crash or a dialog. The per-effect `<select>` lists what is actually present in the embedded FS, plus `None`.

The nine slots, per the design: `tx_start`, `tx_end`, `rx_start`, `rx_end`, `intercom_start`, `intercom_end`, `encryption_beep`, plus the `voice_effect` and `clipping_effect` **presets** — which are built-in code configurations (D12), not files.

Decoding is a hand-rolled minimal RIFF/PCM reader, decoded once at load into memory.

---

## 13. Failure surfaces and logging

| Failure | Surface | Behaviour |
|---|---|---|
| Input device fails to open | `audio:state.inputError` | Fall back to system default; TX unavailable; user informed, app usable |
| Output device fails to open | `audio:state.outputError` | Fall back to system default; monitor and SFX silent; app usable |
| Device unplugged mid-use | `audio:devices_changed` + `audio:state` | Bounded-backoff re-open, then default fallback |
| macOS mic permission denied | `audio:state.inputError` | Carries the permission state and its remedial action, like Phase 3's Accessibility path |
| Missing SFX asset | log, once per asset | Silent effect; UI shows the slot as empty |
| Ring overrun / underrun | `audio:state` counters | Accumulate; never logged per-frame |

**Logging discipline**, inherited from Phase 3.5: nothing on the realtime path logs. Counters accumulate and are reported by the event pump. Per-frame logging at 100 Hz would itself cause the glitches it was meant to diagnose.

---

## 14. Testing

CI has no audio device and this development machine is macOS-only. Making the maximum surface testable without hardware is most of why the architecture in §5 was chosen.

**Without any device:**

- **DSP chain as pure functions over `[]float32`** — synthesized silence, 1 kHz sine at known amplitude, white noise, hard-clipped input. AGC converges to target RMS within N frames, does not pump on transients, limiter never exceeds full scale.
- **Gate state machine** — PTT start/release delay boundaries, VOX min-length and hang, PTT-or-VOX combination, mute overriding both. Frame-counted, no clock.
- **Fake `Backend`** — puts the whole `Manager` under test: lifecycle, hot-plug diff, device-loss fallback, re-open backoff.
- **SPSC rings** under `-race` with a concurrent producer and consumer, asserting overrun/underrun accounting.
- **WAV decoder** — golden tests over hand-built RIFF bytes, including truncated and malformed headers.
- **Voice-effect presets** — committed golden frames, so a preset change appears as a reviewable diff.
- **RNNoise** — link-and-process smoke test on a 480-sample frame, guarded on cgo availability.
- **Frontend (vitest)** — device selects, knob taper, VOX conditional rows, VU rendering from events, effect rows.

**Not reachable by automated tests**, and therefore destined for a manual checklist: real enumeration and device open on each OS, latency and glitching under load, coexistence with Star Citizen's audio, Bluetooth/USB device switching mid-session, and the macOS microphone TCC prompt.

---

## 15. CI

This closes **R3**, which the master spec assigned to Phase 3 and which never landed.

`test.yml`'s Go job is ubuntu-only today. It becomes a `{ubuntu-latest, macos-latest, windows-latest}` matrix running build + vet + `test -race`. Per D14 this lands **first**, before RNNoise is vendored, so toolchain problems surface against an empty package.

Notes:

- `CGO_ENABLED` is not currently disabled anywhere and must stay enabled on all three runners; both malgo and RNNoise are cgo.
- Tests must never construct the malgo backend. The `Backend` interface plus fake is what keeps a device-less runner green.
- **Watch item:** extending the Go job beyond ubuntu will run `internal/hotkeys`' registrar tests on macOS and Windows for the first time. Those register **real** global hotkeys; on macOS a CI runner cannot grant Accessibility. Expect to need the existing permission gating to skip cleanly there, and treat a red macOS job on those tests as a CI-scoping problem, not an audio regression.
- The Linux job is expected to need **no new apt packages** (§9), which the first task confirms.

---

## 16. Risks

| # | Risk | Likelihood | Mitigation |
|---|---|---|---|
| A1 | RNNoise / malgo cgo builds fail on Windows or macOS | M | Per-OS matrix lands first (D14); RNNoise is self-contained C with no system deps |
| A2 | Internal 48 kHz / 10 ms framing disagrees with the server's wire format | M | Confined to the `Sink` seam (§4); 10 ms divides both 20 ms and 40 ms |
| A3 | SFX sample pack never arrives | M | Engine is asset-agnostic (D11); missing samples are silent and logged, not fatal |
| A4 | Linux PulseAudio/PipeWire device-name churn breaks saved selections | M | Polled diff tolerates renames; unknown saved ID falls back to default with a health event, never a hard failure |
| A5 | DSP presets tuned against loopback only | H | Accepted deliberately — re-tune against real received voice in Phase 5/6. Recorded so it is not mistaken for settled |
| A6 | **Third consecutive phase accruing hardware-verification debt** | H | Named as a trend, not a footnote. Phases 3 and 3.5 both have unexecuted manual checklists; Phase 4 adds a third. The loopback design (D1) at least makes this phase self-verifiable by a single user with a headset, unlike 3.5 which needs a force-feedback HOTAS and a running copy of Star Citizen |
| A7 | macOS mic TCC prompt not wired, app killed on first capture | L | `NSMicrophoneUsageDescription` added in this phase; covered by the manual checklist |

---

## 17. Dependencies

**New Go dependencies** (approved by the user on 2026-09-23, per CLAUDE.md):

| Dependency | Form | Why |
|---|---|---|
| `github.com/gen2brain/malgo` | go.mod, pinned | Already the master spec's decided choice (§3) |
| RNNoise | **vendored C, in-tree** | Same precedent as `internal/joystick/di8`; MIT; no system deps |

**Blocking on the user:**

- **The SFX sample pack** — the seven WAV samples matching the §12 contract (the remaining two of the design's nine rows are code presets, per D12), from the contributors credited in the design (`AUDIO · Spaceharvest, JohnMckeel`). The phase ships complete without it; the effects are simply silent until it lands.

**Not blocking, but wanted during this phase:**

- **The Phase 5 server protocol reference** — packet structure, codec, framing, sample rate / frame size, multiplex layout, secret handshake. Phase 4's format choices are insulated by the `Sink` seam, but knowing the real numbers before Phase 5 starts avoids a repacketizer existing purely to paper over a guess.

---

## 18. Definition of Done

1. `go build ./...`, `go vet ./...` and `go test -race ./...` are green on **all three** CI runners.
2. Settings → Audio & Sounds lists real input and output devices, including `System Default`, and selecting one takes effect without restarting the app.
3. Unplugging and replugging the selected device is handled within ~2 s: the list updates, the app falls back, and it recovers — with no crash and no modal.
4. TEST MIC drives a live VU meter that visibly tracks speech.
5. Mic passthrough plays the user's own voice back through the selected output.
6. Toggling AGC produces an audible, measurable levelling change; toggling NS audibly reduces steady background noise.
7. Holding a PTT trigger — keyboard **or** joystick, per Phase 3.5 — opens the gate, honouring start and release delays.
8. VOX opens the gate on speech at the configured threshold and closes after the hang time.
9. `V` (push-to-mute) and `M` (mute toggle) silence the mic path and emit `audio:mic_muted` — their first consumers since Phase 3.
10. Each of the four level knobs audibly and independently changes its bus.
11. Effect PREVIEW plays the configured sample, or is silently and logged-once absent when the pack has not landed.
12. Settings changes propagate to every open window through the single `settings:changed` path.
13. The per-OS CI matrix is in `test.yml` and R3 is marked retired in the master spec.
14. A manual hardware-verification checklist is written to `docs/superpowers/plans/`, in the shape of the Phase 3 and 3.5 checklists.
