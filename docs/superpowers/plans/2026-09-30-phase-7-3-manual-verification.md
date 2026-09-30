# Phase 7.3 — Manual Verification Checklist

**Status: NOT RUN.** This is the **eighth** unrun checklist in this directory, joining Phases 3, 3.5, 4, 5, 6, 7.1 and 7.2. The automated suite is green; nothing below has been exercised by a human on real hardware.

## The phase's single most important unknown

**Item 1 (the 500 ms RX idle threshold) is the most important open question in Phase 7.3.** The value was chosen by reasoning about speech cadence, never measured. If it is too low, one sentence becomes several Transmission Log rows; if too high, two separate exchanges merge into one. Everything else below is conventional verification. Run item 1 first.

---

## 1. Measure the RX idle threshold

Setup: two client instances on one server (second on another machine, or a second OS user), both on the same frequency. Client B talks in natural sentences, with normal pauses between words and clauses. Client A is the observer with the Transmission Log open.

1. Read a paragraph of roughly 5 sentences, at a normal pace, as one continuous PTT hold.
2. Then make two distinct exchanges: a short call, a clear 3-second gap, a short reply.
3. Repeat for each `[voice] history_idle_ms` value **200, 350, 500, 800, 1200** in `config.toml` (restart client A between values). Record rows produced per paragraph and per two-exchange test.

| history_idle_ms | rows for one paragraph | rows for two exchanges |
|---|---|---|
| 200 | | |
| 350 | | |
| 500 | | |
| 800 | | |
| 1200 | | |

**Expected:** a value exists where one paragraph is exactly one row and two exchanges are exactly two rows. **Report that value**, and change the default if it is not 500.

Clamp check: set `history_idle_ms = 9000`. Expected: behaves as 4000 ms (values above 4000 are clamped so the threshold stays below the 5 s stream reap).

## 2. Native dialogs (repeat on Windows, macOS, Linux)

For each of BROWSE (folder picker), IMPORT, EXPORT and OPEN (reveal in file manager): open the dialog and **cancel** it.

**Expected:** no notification is raised, no error toast, nothing changes. Then complete each once: BROWSE sets the folder, IMPORT adds the profile, EXPORT writes the file, OPEN reveals the right folder.

## 3. Builtin profile seeding

1. Delete the profiles directory. Launch. **Expected:** the shipped builtins are written.
2. Edit one builtin (change a radio, SAVE). Quit, relaunch. **Expected:** the edit survives byte-identically (compare the file before and after).
3. Delete one builtin. Relaunch twice. **Expected:** it stays deleted across both restarts.

## 4. Layout persistence

1. Resize and reorder several Comms blocks. Quit normally. Relaunch. **Expected:** layout restored.
2. Repeat, but kill the process instead of quitting (Task Manager / `kill -9`). **Expected:** layout changes made since the last config write are lost. This is by design (layout is written at shutdown or alongside other config writes).

## 5. Comms window resize reaches the profile (decision D3)

1. Load a profile. **Expected:** the dirty dot is off and the Comms window takes the profile's size.
2. Drag the Comms **window** edge to a new size. **Expected:** the dirty dot lights.
3. SAVE, close the Comms window, reopen, reload the profile. **Expected:** the new size is applied.
4. Quit and relaunch. **Expected:** the new size is in effect.

## 6. Profile whose radios differ from the live set

Connect, then load a profile with a different radio set than the one currently live. **Expected:** PTT still transmits (the selected radio is re-validated against the new set).

## 7. Dirty-state rules

With a profile loaded: click a different radio. **Expected:** the dirty dot does **not** light. Resize a block. **Expected:** it **does** light.

## 8. RX rows from a second client

A second client transmits. **Expected:** RX rows appear on client A with the sender's callsign resolved (not a GUID), on the right frequency, with a plausible duration.

## 9. CSV export

Give a sender a callsign containing a comma (e.g. `Smith, J`). Export the log to CSV and open it in Excel/LibreOffice/Numbers. **Expected:** the sender stays in one cell, columns align, no mangled characters.

## 10. Delete-profile confirmation

1. Click DELETE once. **Expected:** the button shows CONFIRM.
2. Fast double-click DELETE. **Expected:** the profile is **not** deleted.
3. Click DELETE once and wait ~3 s. **Expected:** the arm clears back to DELETE.
4. Click DELETE, then CONFIRM after a deliberate pause. **Expected:** deleted.

## 11. Touch/pen block drag (known gap)

Drag a Comms block with touch or pen. Interrupt it (e.g. palm rejection, pen leaves range). **Expected:** the drag ends cleanly, no stuck ghost block. There is **no `pointercancel` handler**; if the drag sticks, record it as a bug.
