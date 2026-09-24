# Phase 4 — Manual Verification Checklist

**Status: NOT YET EXECUTED — requires a human on real hardware.**

This checklist exists because Task 18 (end-to-end verification and phase
close-out) ran in a sandboxed CI-like environment with no audio device and no
way to launch a GUI application. Every automated check that *can* run
headlessly (`go build`, `go vet`, `go test -race`, `tsc --noEmit`, `vitest`,
the Vite production build) has been run and is reported in
`.superpowers/sdd/2026-09-23-vcs-client-phase-4-audio-io/task-18-report.md`.
Nothing below has been executed by an agent. Do not treat any item here as
verified until a human has actually performed it and recorded the result.

Each item names the spec DoD number it satisfies
(`docs/superpowers/specs/2026-09-23-vcs-client-phase-4-audio-io-design.md`
§18) and/or the risk it closes (§16), where applicable.

## Setup

1. Build and launch the app natively on the target OS: `wails3 build` then
   run the produced binary (or `wails3 dev` for faster iteration on items
   that don't depend on a packaged binary — re-check persistence-sensitive
   items against the packaged build at least once).
2. A wired or Bluetooth headset (mic + output) is required for most items
   below; a second, different input/output pair (e.g. laptop mic/speakers)
   is useful for the device-switching items.
3. Star Citizen, or a stand-in that opens an exclusive-feeling WASAPI/CoreAudio
   stream, is required for the coexistence item (§6) — this is the single
   most important item in this checklist.
4. Access to `config.toml` (the OS-specific app config dir) to inspect
   persisted `[audio]` values directly.
5. The OS-level microphone-in-use indicator for your platform: macOS menu-bar
   orange dot, Windows Settings → Privacy → Microphone "recently accessed"
   list / taskbar indicator, or your Linux DE's equivalent (e.g. GNOME's
   status-area mic icon) if it has one.
6. **The SFX sample pack may not have landed yet.** If `internal/audio/assets/`
   holds only the manifest, skip the PREVIEW audio-content checks (§11) and
   only confirm the silent/logged-once fallback behaviour — this is a known,
   accepted gap (A3), not a bug to file.

---

### 1. Device enumeration and selection — DoD 2

Run this on every OS you have available.

1. Launch the app, log in as guest, open Settings → Audio & Sounds → DEVICES.
   **Expected:** the microphone and speakers selects list every real input
   and output device currently attached, plus a `System Default` entry for
   each, matching what the OS's own sound settings shows.
2. Select a specific (non-default) input device.
   **Expected:** takes effect immediately — no restart, no "Apply" step. TEST
   MIC's VU (item 4) should now visibly respond to that specific device.
3. Repeat for the output device, confirmed via mic passthrough (item 5) or an
   effect preview (item 11) audibly moving to the new output.
4. Select `System Default` for both.
   **Expected:** the app follows whatever the OS calls default at the time,
   not a device pinned at selection time (see item 5 for the live-switch
   case).

### 2. macOS microphone permission — DoD (A7), spec §9 second TCC surface

**macOS only. Run on an account that has NEVER granted VCS microphone
access** — a machine that already holds the grant passes every item below
vacuously.

> **This is a second, independent TCC surface from Phase 3's Accessibility
> grant** (used for global hotkeys). The two are unrelated: denying one says
> nothing about the other. Do not assume Accessibility being granted implies
> anything here, or vice versa.

1. Reset the grant: `tccutil reset Microphone <bundle-id>` (or use a clean
   macOS user account). Launch the app and trigger the first capture attempt
   (open Settings → Audio & Sounds and click TEST MIC, or otherwise cause
   `OpenCapture` to run).
   **Expected:** the standard macOS microphone permission sheet appears,
   naming VCS and showing the `NSMicrophoneUsageDescription` string ("VCS
   needs microphone access to transmit your voice over the radio."). The app
   must **not** be killed by the OS — that is the exact crash this string
   exists to prevent (verify by first temporarily removing the key and
   confirming the process is in fact terminated on capture, then restoring
   the key you shipped in this phase).
2. Click **Deny**.
   **Expected:** no crash. `audio:state.inputError` carries the permission
   state and remedial action (spec §13), the app stays usable, TEST MIC
   plainly shows "no input" rather than silently doing nothing, and other
   audio (output-only: SFX, effect preview) still works.
3. Recovery: open System Settings → Privacy & Security → Microphone, grant
   VCS access, switch back to the VCS window.
   **Expected:** capture recovers — confirm whether this requires a
   restart or self-heals on next capture attempt / window focus, and record
   which. (Unlike Accessibility in Phase 3, there is no documented
   window-focus re-check built for this permission — treat "does it recover
   without restart" as an open question this item answers.)
4. Relaunch with the grant in place.
   **Expected:** no prompt, TEST MIC and PTT capture work immediately.

### 3. Unplug/replug mid-session — DoD 3, spec §8 hot-plug

1. With the app running and the input device selected and actively
   capturing (TEST MIC or PTT held), physically unplug that input device.
   **Expected:** within ~2 s the device list updates (`audio:devices_changed`),
   capture falls back to the system default (or reports `inputError` if none
   is available), and the app does **not** crash, hang, or show a modal.
2. Plug the same device back in.
   **Expected:** within ~2 s it reappears in the list and, if it was the
   selected device, capture resumes on it without user action.
3. Repeat both directions for the output device (unplug while monitoring
   mic passthrough or an effect is playing; confirm fallback and recovery).

### 4. Bluetooth headset connect/disconnect mid-session — spec §9, A4

1. With a wired device selected and audio flowing, connect a Bluetooth
   headset.
   **Expected:** it appears in the device lists within ~2 s; selecting it
   works like any other device.
2. With the Bluetooth headset selected and in active use, disconnect it
   (power off, move out of range, or OS-level disconnect).
   **Expected:** same fallback behavior as item 3 — no crash, falls back to
   system default or reports the error state.
3. Reconnect. Confirm it reappears and can be reselected. Note whether the
   device ID is stable across a reconnect (some Bluetooth stacks reassign
   IDs) — if the saved selection is lost and falls back to default instead
   of re-matching, that is the A4 device-name-churn risk in practice; record
   it as a finding rather than a surprise.

### 5. `System Default` follows an OS default-device change — spec §8 "Identity"

1. Set both input and output to `System Default` in VCS.
2. In the OS sound settings (not VCS), change the system default input
   device to a different physical device.
   **Expected:** VCS's capture follows the new OS default without any
   change inside VCS itself and without restarting — this is the entire
   point of storing `System Default` as an unresolved empty ID (spec §8)
   rather than pinning to whatever was default at selection time.
3. Repeat for the output device.

### 6. Coexistence with Star Citizen — DoD (D6), the single most important item

**This is what the whole shared-mode (non-exclusive) design exists for.**
Run both launch orders; do not skip either.

1. Start Star Citizen first. Confirm its audio (engine, comms, UI sounds)
   works normally. Then start VCS, select the same input/output devices SC
   is using, and hold PTT / run TEST MIC.
   **Expected:** SC's own audio is completely unaffected — no dropouts, no
   glitching, no SC audio cutting out or reinitializing when VCS opens the
   devices. VCS's own capture/playback also works.
2. Quit both. Start VCS first, select devices, confirm capture/playback
   work. Then start SC.
   **Expected:** SC's audio comes up normally and is unaffected by VCS
   already holding the devices. VCS continues working unaffected by SC's
   device open.
3. With both running simultaneously for several minutes, engage in a
   transmission (PTT held) while SC is producing game audio (combat, engine
   noise, comms).
   **Expected:** neither app's audio glitches, drops, or is muted by the
   other. This is a shared/non-exclusive open on both sides (spec D6); any
   sign of exclusive-mode contention (one app's audio pausing while the
   other opens/uses the device) is a real regression to file, not an
   expected gap.

### 7. PTT from keyboard and joystick, including refcount — DoD 7, Phase 3.5 refcount now audible

1. Hold a keyboard-bound PTT trigger. Confirm the gate opens (audible if mic
   passthrough is on, or observable via VU/mic-muted state) with the
   configured start delay, and closes with the configured release delay on
   release.
2. Repeat with a joystick-bound PTT trigger (if hardware available).
3. **The refcount, now audible:** bind Global PTT to both a keyboard key and
   a joystick button. Hold the keyboard key, then also press the joystick
   button, then release **only the keyboard key**.
   **Expected:** transmission (or the passthrough/VU indication) CONTINUES —
   it must not cut while the joystick button is still held. This was
   verified structurally in Phase 3.5; this item is the first time it is
   verified as an actual audio behavior rather than an event-log line.
4. Release the joystick button. Confirm the gate closes (respecting the
   release delay).

### 8. PTT delays and VOX — DoD 7, 8

1. Set `ptt_start_delay_ms` and `ptt_release_delay_ms` to clearly different,
   perceptible values (e.g. 0 vs. 500 ms) and confirm the open/close timing
   feels correct against a stopwatch or by ear — not just "eventually
   happens."
2. Set them back to sensible defaults and confirm they feel imperceptible
   at small values (the DoD's "opens... honouring start and release delays"
   bar is about correctness, not just non-zero delay working).
3. Enable VOX, set a threshold, and speak in a room with realistic ambient
   noise (not dead silent — a fan, HVAC, or typical office/room tone).
   **Expected:** the gate opens on speech at the configured threshold and
   stays closed on background noise alone; it closes after the hang time
   once speech stops. Confirm the threshold is usable (not so sensitive it
   opens on room tone, not so insensitive normal speech doesn't trigger it).
4. Toggle `vox_noise_cancel` (pre/post-NS evaluation) and confirm the VOX
   behavior changes as expected — post-NS evaluation should make VOX less
   prone to opening on noise that NS would otherwise remove.

### 9. AGC and noise suppression audibly doing what their toggles claim — DoD 6

1. With NS off, speak in a room with steady background noise (fan, hum).
   Toggle NS on.
   **Expected:** audibly reduces the steady background noise, without a
   perceptible drop in speech intelligibility.
2. With AGC off, speak at clearly varying volumes (quiet, then loud).
   Toggle AGC on and repeat.
   **Expected:** AGC audibly levels the output — quiet speech is brought up,
   loud speech is not over-amplified — without obvious "pumping" (audible
   gain changes tracking transients rather than sustained level).
3. Confirm NS does not cause AGC to amplify residual noise during silence
   (spec §5 "NS before AGC" ordering) — listen for a hiss swell between
   sentences with both enabled; there should be none.

### 10. Four level knobs audibly independent — DoD 10

1. With mic passthrough on and an effect or SFX preview playing (once the
   sample pack lands) or a notification sound triggered, adjust master,
   voice, sfx, and notification levels one at a time.
   **Expected:** each knob audibly and independently changes only its own
   bus — e.g. changing `sfx` must not change the perceived level of the
   passthrough voice, and vice versa. Master should scale all of them
   together.
2. Confirm the knobs feel perceptually linear across their range (spec §7
   "perceptual taper") — the top third of travel should not be a dead zone
   where nothing audible changes.

### 11. Effect PREVIEW — DoD 11 (once the sample pack lands)

1. If `internal/audio/assets/` holds real WAV files: click PREVIEW on each
   of the seven sample rows (`tx_start`, `tx_end`, `rx_start`, `rx_end`,
   `intercom_start`, `intercom_end`, `encryption_beep`).
   **Expected:** each plays its configured sample through the selected
   output device, respecting the `sfx` and `master` levels.
2. If the pack has NOT landed: confirm PREVIEW on an empty slot does nothing
   audible, is logged once (not per click, not a crash/dialog), and the
   `<select>` for that slot lists only `None` plus whatever is actually
   present in the embedded FS (spec §12). This is the expected, accepted
   state per risk A3 — do not file it as a bug.
3. For the two preset rows (`voice_effect`, `clipping_effect`), select each
   built-in preset in turn and confirm mic passthrough audibly changes
   character (e.g. `comms_filter_mid` should sound bandpass-filtered;
   `saturated_overdrive` should sound clipped/distorted).

### 12. Latency and glitching under load — DoD 3, spec §5 rings

1. With the game (or another audio-heavy app) running and several other
   applications open, hold PTT with mic passthrough on and speak
   continuously for at least a minute.
   **Expected:** no audible crackling, popping, or dropouts; perceived
   round-trip latency (voice to passthrough output) stays low and constant,
   not increasing over time.
2. Check `audio:state`'s overrun/underrun counters (via a temporary debug
   log or devtools inspection of the event payload) before and after.
   **Expected:** both counters stay at **zero** under normal load. A
   non-zero and growing count under load that a real user's machine would
   plausibly hit is a real finding — the ring sizes (capture ≈200 ms,
   playback ≈100 ms, spec §5) are sized against typical scheduling jitter,
   not against every possible load.
3. Deliberately induce heavier load (e.g. a CPU-bound background task) and
   confirm behaviour degrades gracefully (occasional counter increments
   acceptable) rather than crashing or wedging capture/playback entirely.

### 13. Push-to-mute and mute-toggle — DoD 9

1. Bind and press `V` (push-to-mute, hold) while PTT/VOX would otherwise
   have the gate open.
   **Expected:** mic path goes silent immediately, `audio:mic_muted` fires
   `true`, and — per spec §6 — mute **overrides** PTT/VOX unconditionally;
   confirm by holding PTT while push-to-mute is also held and confirming
   silence wins.
2. Release `V`.
   **Expected:** unmutes (if nothing else is now muting), `audio:mic_muted`
   fires `false`.
3. Press `M` (mute-toggle, press-kind).
   **Expected:** toggles muted state on press only (not on release), and
   persists across a PTT press/release while muted-toggle is engaged.

### 14. Stop/restart with a device unplugged mid-reopen — concurrency bugs found and fixed this phase

**This item exists specifically because of bugs found during code review,
not because the spec calls it out.** Four related concurrency bugs were
found and fixed around abandoned device-reopen attempts racing a Stop/Start
cycle (`internal/audio/manager.go`); the worst of them left an OS microphone
stream running after the user had been told audio had stopped. The fixes are
covered by unit tests (`TestManagerStopAloneInvalidatesAnAbandonedReopen`,
`TestManagerStartAfterStopInvalidatesAnAbandonedReopen`,
`TestManagerAbandonedDSPLoopCannotTouchTheNextGenerationsRings`,
`TestManagerDiscardsLateReopenFromAStaleGeneration`), but every one of those
is a white-box or synthetic-timing test against a fake backend — none of
them exercises the real malgo backend or real device unplug timing. Exercise
it deliberately:

1. Unplug the selected input device. Immediately (while the bounded-backoff
   reopen attempt is plausibly in flight — repeat a few times if the timing
   is hard to hit) trigger a Stop of audio (however Stop is exposed in the
   running build — e.g. leaving the Comms/Settings context that owns the
   engine, or an explicit stop action) followed promptly by a Start/restart.
   **Expected:** no crash, no hang. The restarted engine's state is fully
   consistent with the device set that exists at restart time — no stale
   stream from the abandoned generation is silently still open.
2. **Confirm the mic is genuinely released after Stop()** — check the
   OS-level microphone-in-use indicator (macOS menu-bar orange dot, Windows
   Settings → Privacy → Microphone, or your Linux DE's equivalent), not just
   the app's own UI/state. This is the exact failure the worst of the four
   bugs produced: the app's own UI reported audio stopped while an OS mic
   stream was still actually running. If the OS indicator stays lit after
   Stop() with no restart pending, that is a real regression to file.
3. Repeat the unplug-during-reopen-then-stop-then-restart sequence a handful
   of times with slightly different timing to probabilistically cover the
   race window, since this class of bug is timing-dependent by nature.

### 15. First syllable after startup is not clipped — denoiser priming

`Start()` runs one throwaway all-zero frame through the RNNoise denoiser
before live audio reaches it, specifically to absorb RNNoise's ~60x
cold-start attenuation (a genuine, reproducible property of RNNoise itself,
confirmed during this phase's implementation) before any real speech does.

1. With NS enabled, start audio capture fresh (app launch, or Start after a
   Stop) and speak the very first syllable immediately upon starting
   capture — do not pause before speaking.
   **Expected:** that first syllable is at normal, un-attenuated volume —
   not quiet, clipped, or "swallowed" relative to the rest of the sentence.
   If it is noticeably quieter than what follows, the priming frame is not
   doing its job and that is a real regression to file.

---

## Known, already-documented gaps this checklist is not meant to "fail" on

These are pre-existing, understood limitations — running the checklist above
should confirm the *expected* degraded behavior, not surface them as new
bugs:

- **The SFX sample pack may not have landed** (risk A3): PREVIEW on empty
  slots is silent and logged once; this is the designed behavior, not a bug.
  Do not "fail" item 11 for this reason — confirm the fallback behavior
  instead.
- **DSP presets (`comms_filter_mid`, `saturated_overdrive`) are tuned
  against loopback only** (risk A5), not real received voice — a Phase 5/6
  re-tune is expected and already recorded as not settled.
- **Linux device-name churn on PulseAudio/PipeWire** (risk A4): a saved
  device selection surviving a rename is not guaranteed; falling back to
  system default with a health event on an unrecognized saved ID is the
  accepted behavior, not a bug.
- **This is the third consecutive phase accruing hardware-verification
  debt** (risk A6, named as a trend in the design doc): Phases 3 and 3.5
  both shipped with unexecuted manual checklists before this one. Phase 4's
  loopback design at least makes this phase self-verifiable by one person
  with a headset, unlike 3.5's HOTAS-and-Star-Citizen requirement.
- **No echo cancellation** is in scope this phase (spec §3 "Out") — headsets
  are the assumed hardware; running open speakers + mic and hearing an echo
  in passthrough is expected, not a bug.

## What to do with results

Record pass/fail per numbered item (not per DoD number) directly in this file
or a copy of it, including OS/platform, device make/model, and Bluetooth
stack where relevant. File any genuine failure (something behaving worse
than the documented gaps above predict, or any deviation from the coexistence
item's expectations) as a new task rather than patching it silently, per the
project's standard workflow.
