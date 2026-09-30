# Phase 7.3 — Manual Verification Checklist

**Status: NOT RUN.** This is the **eighth** unrun checklist in this directory, joining Phases 3, 3.5, 4, 5, 6, 7.1 and 7.2. The automated suite is green; nothing below has been exercised by a human on real hardware.

## The phase's single most important unknown

**Item 1 (the 500 ms RX idle threshold) is the most important open question in Phase 7.3.** The value was chosen by reasoning about speech cadence, never measured. If it is too low, one sentence becomes several Transmission Log rows; if too high, two separate exchanges merge into one. Everything else below is conventional verification. Run item 1 first.

## Reference: where things are

| Thing | Location |
|---|---|
| App-data dir (`<AppData>`) | Windows `%APPDATA%\VCS`; macOS `~/Library/Application Support/VCS`; Linux `${XDG_CONFIG_HOME:-~/.config}/VCS` |
| `config.toml` | `<AppData>/config.toml` (edit with the client **quit**; it rewrites the file on exit) |
| Transmission history | `<AppData>/history.json` |
| Profiles | `<AppData>/profiles/*.vcs.json` unless `profiles_dir` is set |
| Client log | `<AppData>/log/vcs-client.log` (JSON lines) |
| `windows.json` | in `<AppData>` |
| Transmission Log screen | main window, left nav rail, **Transmission Log** (history icon); one row per transmission; **EXPORT CSV** button on that screen |
| Radio Profiles screen | main window, left nav rail, **Radio Profiles** (BROWSE, OPEN, IMPORT FROM FILE, SAVE, SAVE CURRENT AS NEW; each profile row has Export and Delete buttons) |
| Comms window | opened from the main window's Comms launcher; title bar reads "Communications" |
| **Dirty dot** | in the Comms title bar, a filled `●` in the accent colour next to the profile name, tooltip "Unsaved layout changes". **Clean = no dot and no REVERT button.** Dirty = dot **and** a REVERT button. |

To set `history_idle_ms`, add to `config.toml`:

```toml
[voice]
history_idle_ms = 500
```

---

## 1. Measure the RX idle threshold

Setup: two client instances on one server (second on another machine, or a second OS user), both on the same frequency. Client B talks in natural sentences, with normal pauses between words and clauses. Client A is the observer with the Transmission Log open.

Definitions. **A paragraph** = about 5 sentences (roughly 40 to 60 words) read aloud at a normal pace in ONE continuous PTT hold, with ordinary pauses between sentences (0.3 to 1 s) and no deliberate gap. **Two exchanges** = a short call ("Alpha, radio check"), release PTT, a deliberate 3-second silence, press PTT again and give a short reply; the two presses are separate transmissions. **Counting rows**: open Transmission Log, clear it (or note the row count), run the test, wait 5 s after releasing, then count the new **RX** rows from client B's callsign.

1. Read one paragraph as defined above.
2. Then do the two-exchange test as defined above.
3. Repeat for each `[voice] history_idle_ms` value **200, 350, 500, 800, 1200** in `config.toml` (restart client A between values). Record rows produced per paragraph and per two-exchange test.

| history_idle_ms | rows for one paragraph | rows for two exchanges |
|---|---|---|
| 200 | | |
| 350 | | |
| 500 | | |
| 800 | | |
| 1200 | | |

**Expected:** a value exists where one paragraph is exactly one row and two exchanges are exactly two rows. **Report that value**, and change the default if it is not 500.

Clamp check: set `history_idle_ms = 9000`, restart, connect and receive one short transmission. **Expected:** (a) `<AppData>/log/vcs-client.log` contains `voice: history_idle_ms is not below the stream reap timeout; clamping` with `requested_ms` 9000 and `effective_ms` 4000; (b) the row appears about 4 s after the talker releases PTT, not 9 s and never missing. Also confirm `history_idle_ms = 4000` exactly logs no clamp warning.

## 2. Native dialogs (repeat on Windows, macOS, Linux)

Where each appears: BROWSE (Radio Profiles) is a native folder picker (Windows folder dialog; macOS open panel, folders only; Linux GTK/portal folder chooser). IMPORT FROM FILE is a native open-file dialog for `.vcs.json`. EXPORT (a profile row's Export button, and EXPORT CSV on the Transmission Log) is a native save-file dialog. OPEN has no dialog: it reveals the profiles folder in Explorer / Finder / the default file manager. For BROWSE, IMPORT and both EXPORTs, open the dialog and **cancel** it. For OPEN, confirm the file manager opens on the right folder and nothing is notified.

**Expected:** no notification is raised (TopBar bell badge and status-bar bell unchanged, no toast), nothing changes. Then complete each once: BROWSE sets the folder, IMPORT adds the profile, EXPORT writes the file, OPEN reveals the right folder.

## 3. Builtin profile seeding

1. Delete the profiles directory. Launch. **Expected:** the shipped builtins are written.
2. Edit one builtin (change a radio, SAVE). Quit, relaunch. **Expected:** the edit survives byte-identically (compare the file before and after).
3. Delete one builtin. Relaunch twice. **Expected:** it stays deleted across both restarts.

## 4. Layout persistence

1. Resize and reorder several Comms blocks. Quit normally. Relaunch. **Expected:** layout restored.
2. Repeat, but kill the process instead of quitting (Task Manager / `kill -9`). **Expected:** layout changes made since the last config write are lost. This is by design (layout is written at shutdown or alongside other config writes).

## 5. Comms window resize reaches the profile (decision D3)

Prerequisite: this depends on the native resize hook (`WindowDidResize`/`WindowDidMove`, debounced 300 ms in `Registry`), which has never been seen firing on a real OS window. If the dot never lights, that is the bug to report.

1. Load a profile whose saved window size differs from the current one. **Expected:** the Comms window takes the profile's size and the dirty dot stays **off** (a size that comes back within 2 px of what was set is deliberately ignored). If the dot lights right after a load, the echo tolerance is too small for this OS/DPI: record the pixel difference.
2. Drag the Comms **window** edge by at least 20 px, release, wait half a second. **Expected:** the dirty dot lights and REVERT appears.
3. SAVE, close the Comms window, reopen, reload the profile. **Expected:** the new size is applied.
4. Quit and relaunch. **Expected:** the new size is in effect. Also confirm `windows.json` is written once after the drag settles, not continuously during it.

## 6. Profile whose radios differ from the live set

Connect, then load a profile with a different radio set than the one currently live. **Expected:** PTT still transmits (the selected radio is re-validated against the new set).

## 7. Dirty-state rules

With a profile loaded: click a different radio. **Expected:** the dirty dot does **not** light. Resize a block. **Expected:** it **does** light.

## 8. RX rows from a second client

Also once: quit client A while a transmission is still arriving (talker holding PTT), relaunch A. **Expected:** that partial transmission is in the log (the final history flush runs after the voice session closes).

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
