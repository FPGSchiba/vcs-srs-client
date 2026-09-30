# Phase 7.2 — Manual verification: the notification channel

**Status:** outstanding. Phase 7.2 is code-complete — automated suite green
(`go build` / `go vet` / `go test -race ./...` with `-tags purego`,
`vitest`, `tsc --noEmit`, frontend production build) — but **not
field-verified**, and this is the **seventh** manual checklist to sit unrun
in `docs/superpowers/plans/`, joining Phases 3, 3.5, 4, 5, 6 and 7.1.

Every item below rests on behaviour read out of source, not observed. See
the design spec's §7 (`docs/superpowers/specs/2026-09-28-vcs-client-phase-7-2-notification-channel-design.md`)
for the full accounting of what this phase could not verify itself.

## Before you start

- **The notification sound cannot be verified by anyone today — not
  "untested", *unverifiable*.** `notify_alert.wav` does not exist.
  `internal/audio/assets/README.md` records the ten-slot sample pack as a
  blocking dependency on the user that must not be substituted with a
  placeholder tone. Item 7 below can only be re-attempted once that file
  lands; until then, skip straight to confirming the item is silent and
  move on — that silence is the expected, correct state of this build, not
  a bug to chase.
- Record, for every item: **Date**, **Platform** (OS/arch), **Client
  build** (`git rev-parse HEAD`), **Observed result**, and any deviation
  from the expected text quoted below (copy the actual string verbatim).

---

## 1. The joystick flap

**Setup:** On Windows and separately on Linux, unplug and replug a joystick
repeatedly (aim for at least ten cycles, mixing fast and slow replugs).
Watch the Notifications popout and the status-bar bell throughout.

**Expected:** the notification list does not fill with duplicate
`joystick.global` entries — at most one visible item at a time — and the
final state shown after the flap stops matches the joystick's actual
physical state (plugged in and healthy, or genuinely absent/erroring).

**What is unmeasured:** the 2 s coalescing window (§4.2 of the spec) is
sized to a *read* of the 100 Hz poll loop in `internal/joystick/manager.go`,
never a measurement of a real flap. Record whether 2 s is too short (visible
duplicate entries survive it), too long (a real, settled failure takes
longer than 2 s to appear), or about right, and whether Windows and Linux
behave the same.

**Actual result:** _(record here)_

---

## 2. The audio error text

**Setup:** With a session running, physically unplug a USB microphone
mid-session. Repeat at least three times, ideally across more than one
device model if available.

**Expected:** a `Microphone unavailable` notification (key `audio.input`)
appears carrying malgo's error string as its body.

**What is unmeasured:** whether that string is one stable value across
repeated unplug/replug cycles, or varies per attempt (e.g. carries a
timestamp, an attempt count, or a different phrasing depending on which
retry in the bounded-backoff sequence catches it). A varying string defeats
the identity-dedupe fingerprint (§4.1) and means **one notification per
distinct string**, not one per outage. Record the exact string(s) observed,
verbatim, across all attempts.

**Actual result:** _(record here)_

---

## 3. Audio hot-unplug transitions

**Setup:** Same physical action as item 2, but watch the transition itself
rather than only the settled state — specifically whether an `Input
Substituted` warning (if the device was substituted) or the raw error
appears cleanly, or flickers through an intermediate error state first.

**Expected:** a clean, single transition to the correct resolved state
(`Substituted` or the error, whichever applies) with no visible flap.

**Actual result:** _(record here)_

---

## 4. Xrun churn — the single most important audio behaviour

**Setup:** Drive the audio engine into sustained xruns — heavy CPU load
(e.g. a stress test alongside VCS) and/or an artificially tiny buffer
size, for at least two minutes continuous.

**Expected:** **no** notification appears at any point, regardless of how
many overruns/underruns accumulate. `Overruns`/`Underruns` are excluded
from the notification fingerprint by design (§5.4) specifically so a
glitching engine emitting `audio:state` every 2 s does not become 1800
notifications an hour. If any notification appears during this test, that
is a real regression, not a false positive to explain away.

**Actual result:** _(record here)_

---

## 5. macOS Accessibility denial

**Setup:** On macOS, revoke (or never grant) the Accessibility permission
VCS needs for global hotkeys, then launch/use the app.

**Expected:**
- The notification carries `PERMISSION: denied` in its context row.
- `PermissionCard` renders in Settings → Keybinds with a **GRANT ACCESS**
  button (this replaced the old inline banner — see `Keybinds.tsx`'s
  extraction to `sections/keybinds/PermissionCard.tsx`).
- Granting the permission resolves the notification.
- If Accessibility is granted but hotkeys are still unregistered (the
  one-shot macOS prompt already spent), the restart hint appears.

**What is unmeasured:** the permission-transition sequencing and
`promptSpent`'s "the one-shot prompt is spent" inference are read from
source, never exercised on real hardware (Phase 3's checklist, still
unrun, covers the same gap).

**Actual result:** _(record here)_

---

## 6. macOS joystick unsupported

**Setup:** Run on macOS with any joystick/gamepad attached.

**Expected:** an informational `joystick.global` item appears in the
Notifications popout stating joystick input is unsupported on this
platform — and, throughout, the badge, the bell, and the toast stack all
stay silent and uncoloured. Info-severity items are raised already-read
(§5.3) specifically so this case never nags.

**Actual result:** _(record here)_

---

## 7. The notification sound — cannot be verified until `notify_alert.wav` exists

**Status today:** the ask on the user is now **ten** outstanding WAV files
(nine SFX slots from Phase 4, plus `notify_alert` from this phase).
`internal/audio/assets/README.md` records the pack as a blocking
dependency, not something to substitute with a placeholder tone. Confirm
today only that the item is **silent** — that is correct, not a bug.

**Once `notify_alert.wav` is delivered, re-run this item and confirm:**
- It is audible when an error-severity notification is raised.
- It is correctly levelled against voice via the notification bus slider
  (Settings → Audio).
- It respects `play_notification_sounds` (default true) — toggling it off
  silences the sound without affecting the visible notification.
- It is not startling when it fires during a received transmission.

**Actual result:** _(record here — expect "silent, as designed" until the
sample exists)_

---

## 8. Cross-window agreement

**Setup:** Open both the main window and the Notifications popout at the
same time. Raise at least one notification (any of the cases above, or
trigger a per-binding hotkey failure). Mark it read, then mark-all-read,
then clear-all, performing each action from alternating windows.

**Expected:** badge count (TopBar), bell count (StatusBar), and the popout's
list all agree at every step, and an action taken in one window (mark-all
read / clear-all) is reflected immediately in the other via the
`notifications:changed` full-snapshot event.

**Actual result:** _(record here)_

---

## 9. Geometry persistence

**Setup:** Open the Notifications popout, move it, resize it, close it,
then reopen it.

**Expected:** it reopens at the same position and size, through the
existing window Registry (the same mechanism Comms already uses).

**Actual result:** _(record here)_

---

## 10. Dismissal

**Setup:** Force a live hotkey failure (e.g. an unregisterable chord).
Dismiss the resulting notification while the condition is still true.
Then edit an unrelated keybind (which re-runs `applyHotkeys()`/emits
`hotkeys:state` again). Finally, clear the failing condition and cause it
to recur.

**Expected:**
- The dismissed notification does **not** reappear from the unrelated
  keybind edit (dismiss-then-identical-`Raise` stays suppressed, §4.1).
- It **does** reappear once the condition genuinely clears and then
  recurs (a `Resolve` clears the suppression, so the next `Raise` is a
  fresh occurrence).

**Actual result:** _(record here)_

---

## 11. Found during implementation, not yet in spec §7: leading-edge deferral via audio's constant no-op window

**Background:** `NotifyAudioState` calls `RaiseWindowed`/`ResolveWindowed`
for all four audio keys (`audio.input`, `audio.output`,
`audio.input.substituted`, `audio.output.substituted`) on **every**
`audio:state` emission, including the common case where all four are
no-op resolves (nothing wrong). Because a no-op resolve still opens the
10 s coalescing window for that key (§4.2), a genuine microphone failure
arriving just after a run of healthy no-op resolves can be held inside an
already-open window for up to 10 s before the user is told — not because
the fault itself flapped, but because the window mechanism does not
distinguish "this key's window is open because of real flapping" from
"this key's window is open because we called Raise/Resolve on a
schedule". This is a real UX consequence of a constant (10 s) that was
designed from a source read of the poll interval, not measured against a
real failure's timing.

**Setup:** With a live session and normal (fault-free) audio running for
at least 30 s so the four keys have cycled through their no-op resolves
at least once, physically fail the microphone (unplug) and time from the
physical action to the notification appearing.

**Expected (per design intent):** near-immediate ( <2 s, one poll tick )
notification.

**What to check:** whether the observed delay ever approaches the full
10 s window, and if so, whether that is acceptable for a voice-comms
client where a dead microphone is the single most urgent fault this
channel carries. This is not a soundness bug — no notification is lost —
but is worth deciding whether the constant needs tuning or the no-op path
needs to skip opening a window entirely.

**Actual result:** _(record here)_

---

## 12. CI observation — one unreproduced `internal/app` test failure (ruling R26)

**This is not a manual step for a human at a machine; it is a thing to watch
for in CI.** It is recorded here because the working notes that hold the
evidence are git-ignored and will not survive this phase.

During whole-branch review 6, `go test -tags purego -race -count=10
./internal/app` failed **once** in roughly 180 executions. The output was
piped through `tail`, which discarded the `--- FAIL:` line, so **the failing
test was never identified** — and it may not have been an assertion failure
at all.

It has not reproduced since, across four independent agents:

| Campaign | Executions | Failures |
|---|---|---|
| Review 6 | ~180 | **1** (identity lost) |
| Fix wave 6, pre-fix | 320 | 0 |
| Fix wave 6, post-fix | 50 | 0 |
| Review 7 | 110 | 0 |
| Fix wave 7 + review 8 | ~40 | 0 |
| **Total** | **~700** | **1** |

The prime suspect — two wall-clock `time.Sleep` waits in this phase's own
tests, sized at only 4x an injected 30 ms coalescing window — was removed in
fix wave 6 and replaced with `waitForNotify`, a bounded state poll with a 1 s
deadline that prints the snapshot on timeout. That change also turned the
failure mode from a **panic** (`index out of range [0] with length 0`) into a
clean, diagnosable failure. So the class is closed even though the specific
sighting never was.

**What to do:** if CI reddens on `internal/app`, capture the full
`--- FAIL:` line before anything else — that single line is the one piece of
evidence nobody has managed to obtain, and it would settle this immediately.
Do not assume it is this phase's code: the suspect tests were rewritten, and
`internal/app` also carries Phase 3's keybind and permission suites, which
contain their own timing-sensitive waits.


## Summary table (fill in after running all items)

| # | Item | Result (PASS/FAIL/BLOCKED/UNVERIFIABLE) | Deviation from expected | Tester | Date |
|---|---|---|---|---|---|
| 1 | Joystick flap |  |  |  |  |
| 2 | Audio error text stability |  |  |  |  |
| 3 | Audio hot-unplug transition |  |  |  |  |
| 4 | Xrun churn produces no notification |  |  |  |  |
| 5 | macOS Accessibility denial |  |  |  |  |
| 6 | macOS joystick unsupported |  |  |  |  |
| 7 | Notification sound | UNVERIFIABLE until `notify_alert.wav` exists |  |  |  |
| 8 | Cross-window agreement |  |  |  |  |
| 9 | Geometry persistence |  |  |  |  |
| 10 | Dismissal / re-raise suppression |  |  |  |  |
| 11 | Audio window leading-edge deferral |  |  |  |  |
| 12 | CI: unreproduced internal/app flake | WATCH IN CI — capture `--- FAIL:` if it reddens |  |  |  |
