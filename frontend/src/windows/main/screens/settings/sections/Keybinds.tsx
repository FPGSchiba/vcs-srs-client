import { useEffect, useRef, useState } from "react";
import { Panel } from "../../../../../shared/components/Panel";
import { Button } from "../../../../../shared/components/Button";
import { KeyChip } from "../../../../../shared/components/KeyChip";
import type { Capture } from "../../../../../shared/components/KeyChip";
import { TriggerChip } from "../../../../../shared/components/TriggerChip";
import { api } from "../../../../../shared/api/client";
import { on, EV } from "../../../../../shared/api/events";
import { useSettings } from "../../../../../shared/store/settings";
import type { Keybind, Stolen } from "../../../../../shared/store/settings";

interface StolenInfo {
  actionId: string;
  triggerLabel: string;
  label: string;
}

interface GroupDef {
  category: string;
  title: string;
}

/** Categories rendered as flat rows (chips + UNBIND each). PerRadio is
 * handled separately below since it renders as a `.tbl` table instead. */
const SIMPLE_GROUPS: GroupDef[] = [
  { category: "global", title: "GLOBAL" },
  { category: "channel", title: "CHANNEL HOTKEYS" },
  { category: "status", title: "QUICK-STATUS HOTKEYS" },
];

/** Matches the action ids Go's `keybinds.PerRadioActions` emits:
 * "radio.<id>.ptt" / "radio.<id>.select". */
const PER_RADIO_RE = /^radio\.(\d+)\.(ptt|select)$/;

interface RadioGroup {
  id: string;
  ptt?: Keybind;
  select?: Keybind;
}

/** Pairs per-radio actions by their numeric radio id so the table can render
 * one row per radio with separate PTT/Select columns, matching the
 * prototype. A radio missing one half of the pair (shouldn't happen in
 * practice -- PerRadioActions always emits both -- but the fixture data used
 * in tests only supplies one) still gets a row, with the missing side shown
 * unbound and non-interactive. */
function groupPerRadio(rows: Keybind[]): RadioGroup[] {
  const groups = new Map<string, RadioGroup>();
  for (const kb of rows) {
    const m = PER_RADIO_RE.exec(kb.action_id);
    const key = m ? m[1] : kb.action_id;
    const group = groups.get(key) ?? { id: key };
    if (m?.[2] === "select") group.select = kb;
    else group.ptt = kb;
    groups.set(key, group);
  }
  return Array.from(groups.values());
}

/**
 * Keybinds renders the four keybind groups from the design prototype's
 * SettingsKeybinds (`design/vcs/project/screens/settings.jsx`), backed by
 * `useSettings().keybinds` / `.hotkeys` / `.joystick`.
 *
 * Five correctness properties this file is built around:
 *
 * 1. `handleCapture` always ends the capture in a `finally`. The backend
 *    suspends every OS hotkey registration for the duration of a capture;
 *    if `addTrigger` throws and the capture is never ended, every global
 *    hotkey stays dead until the backend's own timeout.
 *
 * 2. Only one chip may listen at a time, by construction. `capturingId` is a
 *    single string, and each row's capture affordance is a straight type
 *    swap on it: `capturingId === kb.action_id` renders that row's KeyChip,
 *    anything else renders its `+` button. Clicking a different row's `+`
 *    therefore does not just start a new capture; it also flips the
 *    PREVIOUS row's element from KeyChip back to a button, a type change
 *    React unmounts regardless of any `key`. KeyChip already treats
 *    "unmount while listening" as a cancel path (see its own doc-comment),
 *    so that existing safety net closes the superseded capture without this
 *    file having to orchestrate it directly -- without it, two mounted
 *    chips would both hold a window keydown listener and a single keypress
 *    would fire `addTrigger` twice.
 *
 *    That forced unmount arrives AFTER React commits, so the superseded
 *    chip's cancel reaches the backend after the new chip's
 *    `beginCapture` -- the calls are inverted and this layer cannot
 *    reorder them. Correctness therefore does not live here: every
 *    `beginCapture` returns a capture token, every end hands its own row's
 *    token back, and the backend only re-arms for the token that is still
 *    current. A superseded row's end is a no-op there, so the new capture
 *    keeps its hotkeys suspended. The same inversion happens when the user
 *    clicks a new chip while the previous row's `addTrigger` is still in
 *    flight, and the token covers that too.
 *
 *    (An earlier revision of this file forced that unmount itself, via a
 *    per-row `epoch` counter bumped into a KeyChip `key`. That machinery is
 *    gone: once idle-vs-capturing became a type swap instead of one KeyChip
 *    always mounted per row, the swap itself already unmounts the old
 *    instance, and the extra remount trigger had nothing left to do.)
 *
 * 3. Per-row failures: `internal/chord` accepts keys the OS layer can't
 *    register (Numpad, F21-F24, punctuation, navigation), so a chord can
 *    save successfully and still never fire. Every row whose action_id
 *    appears in `hotkeys.failed` shows that reason inline -- but ONLY while
 *    `hotkeys.registered` is true. `registered === false` means nothing
 *    registered at all, which implies `failed` names every bound action, so
 *    the per-row text would just reprint the banner once per row.
 *
 * 4. The banner is permission-aware. `hotkeys.permission` is a state, not an
 *    error string, so this branches on it directly: "denied" (macOS only,
 *    meaning the process is not trusted for Accessibility -- the grant
 *    `x/hotkey`'s CGEventTap actually requires) earns an explanation and
 *    GRANT ACCESS / OPEN SETTINGS / RE-CHECK;
 *    "not_applicable" (Windows, Linux/X11) earns none of it, because there
 *    is nothing to grant. `requestHotkeyPermission()`'s `prompted` result is
 *    never treated as a grant -- macOS resolves the prompt asynchronously
 *    and the answer only ever arrives via `hotkeys:state`.
 *
 * 5. One capture affordance, either input. Rather than making the user
 *    choose "keyboard or joystick" first, a single capture accepts
 *    whichever arrives first: a DOM keydown through KeyChip, or a joystick
 *    binding the backend captured and bound itself. The joystick half
 *    completes in Go (the manager already holds the binding, so bouncing it
 *    through here would add a race for nothing) and arrives as
 *    `keybinds:joy_captured`, which this component uses only to close the
 *    listening chip. KeyChip is told to start listening the instant it
 *    mounts (`autoListen`), rather than requiring a second click to arm it,
 *    so the keyboard half is live from the same click that starts the
 *    joystick half -- one click, one capture, either input.
 */
export function Keybinds() {
  const keybinds = useSettings((s) => s.keybinds);
  const hotkeys = useSettings((s) => s.hotkeys);
  const joystick = useSettings((s) => s.joystick);

  const [capturingId, setCapturingId] = useState<string | null>(null);
  const [stolen, setStolen] = useState<StolenInfo | null>(null);

  // Permission-banner state. `requested` unlocks RE-CHECK; `promptSpent`
  // swaps GRANT ACCESS for OPEN SETTINGS. Both are local rather than derived
  // from the store because neither is observable from the backend: macOS
  // gives no way to ask "has this app's one-shot prompt been used", so the
  // only evidence is a request that came back without prompting.
  const [requested, setRequested] = useState(false);
  const [promptSpent, setPromptSpent] = useState(false);

  // Pending capture token per action_id. A ref, not state: it must be
  // readable by the cancel that runs during the very commit that started
  // another row's capture, and it must never trigger a re-render.
  const tokens = useRef<Map<string, Promise<number>>>(new Map());

  const handleChipClick = (actionId: string) => {
    if (capturingId === actionId) {
      // Re-clicking the chip that's already listening is itself a cancel;
      // KeyChip's own click handler resolves that through onCancel.
      return;
    }
    setStolen(null);
    setCapturingId(actionId);
    tokens.current.set(
      actionId,
      api.beginCapture(actionId).catch((err) => {
        console.error("beginCapture failed", err);
        // 0 is never a live generation, so the matching endCapture below is
        // a no-op -- which is right: if the capture never began, the backend
        // never suspended anything and must not be told to re-arm.
        return 0;
      }),
    );
  };

  // Ends the capture this row started, handing back the token it was issued.
  // Passing another row's token (or none) is what the backend's staleness
  // check exists to reject, so this side just has to be honest about which
  // capture it is ending.
  const endCapture = async (actionId: string) => {
    const pending = tokens.current.get(actionId);
    tokens.current.delete(actionId);
    if (pending === undefined) return; // this row never began a capture
    try {
      await api.endCapture(await pending);
    } catch (err) {
      // Logged, not rethrown: callers are cancel paths with nowhere to
      // surface it, and the backend's 10s auto-resume is the real safety net.
      console.error("endCapture failed", err);
    }
  };

  // Ends this row's pending capture and stops it from listening. Shared by
  // every "capture is over" path that isn't a successful keyboard capture:
  // Escape/blur/re-click (via KeyChip's onCancel) and a joystick capture
  // completing server-side (via keybinds:joy_captured below).
  const closeCapture = (actionId: string) => {
    void endCapture(actionId);
    setCapturingId((cur) => (cur === actionId ? null : cur));
  };

  const handleCapture = (actionId: string) => async (cap: Capture) => {
    try {
      const res = await api.addTrigger(actionId, cap);
      setStolen(
        res.stolen
          ? { actionId, triggerLabel: res.stolen.trigger.label, label: res.stolen.label }
          : null,
      );
    } catch {
      // Swallowed deliberately: KeyChip invokes onCapture without awaiting
      // or catching its result, so a rethrow here would only surface as an
      // unhandled rejection. The backend is the source of truth for bound
      // triggers (SettingsScreen's keybinds:changed subscription), so a
      // failed write simply leaves the row showing its previous triggers.
    } finally {
      // Runs even when addTrigger throws, so the backend always re-arms its
      // OS hotkey registrations instead of staying suspended until its own
      // timeout.
      await endCapture(actionId);
      setCapturingId((cur) => (cur === actionId ? null : cur));
    }
  };

  const handleCancel = (actionId: string) => () => closeCapture(actionId);

  const handleRemove = async (actionId: string, index: number) => {
    try {
      await api.removeTrigger(actionId, index);
    } catch (err) {
      // The backend is the source of truth (keybinds:changed), so a failed
      // removal simply leaves the row as it was.
      console.error("removeTrigger failed", err);
    }
  };

  const handleUnbind = (actionIds: string[]) => () => {
    actionIds.forEach((id) => void api.clearKeybind(id));
  };

  // Closes the listening chip when a joystick capture completes and binds
  // itself server-side (see the doc-comment's property 5), and reports any
  // steal it caused. Guarded on the action id still matching so a late/stale
  // event for a row the user has already moved away from cannot reopen or
  // re-end a capture that finished through some other path.
  //
  // The steal has to arrive HERE rather than from a call's return value: a
  // joystick capture completes in the backend and returns to no caller, so
  // without the event the losing row's chip would simply vanish on the next
  // `keybinds:changed` with no warning -- while the identical steal performed
  // with a key shows one. Same banner, same shape, either input.
  useEffect(() => {
    return on<{ action_id: string; stolen?: Stolen | null }>(
      EV.joystickCaptured,
      ({ action_id, stolen: taken }) => {
        if (capturingId !== action_id) return;
        setStolen(
          taken ? { actionId: action_id, triggerLabel: taken.trigger.label, label: taken.label } : null,
        );
        closeCapture(action_id);
      },
    );
  }, [capturingId]);

  // Unsupported (macOS) is not denied -- there is nothing the user can grant
  // -- so the joystick half of the prompt, and any joystick affordance,
  // disappears entirely rather than showing a dead end.
  const capturePrompt = joystick.supported
    ? "Press a key or joystick button — hold a second button first for a modifier."
    : "Press a key.";

  const renderTriggers = (kb: Keybind) => (
    <span className="trigger-row">
      {kb.triggers.map((t, i) => (
        <TriggerChip
          key={`${t.kind}-${t.label}-${i}`}
          trigger={t}
          onRemove={() => void handleRemove(kb.action_id, i)}
        />
      ))}
      {capturingId === kb.action_id ? (
        <KeyChip
          binding=""
          autoListen
          onCapture={handleCapture(kb.action_id)}
          onCancel={handleCancel(kb.action_id)}
        />
      ) : (
        <button
          type="button"
          className="kbd add-binding"
          aria-label={`Add binding for ${kb.label}`}
          onClick={() => handleChipClick(kb.action_id)}
        >
          +
        </button>
      )}
    </span>
  );

  const renderChip = (kb: Keybind) => {
    // Suppressed while the banner is up. `registered === false` means NOTHING
    // registered (see hotkeys.Manager.Registered), which implies `failed`
    // names every bound action -- so rendering per-row reasons there repeats
    // the banner's single message on every single row. A PARTIAL failure
    // (one unregisterable Numpad7 among nineteen working binds) keeps
    // `registered === true`, and those rows still get their own reason,
    // which is the only case where the per-row text says something the
    // banner does not.
    const failedReason = hotkeys.registered ? hotkeys.failed[kb.action_id] : undefined;
    const isCapturing = capturingId === kb.action_id;
    return (
      <div className="col" style={{ gap: 2, alignItems: "flex-end" }}>
        {renderTriggers(kb)}
        {isCapturing && (
          <span
            className="cap-dim"
            style={{ fontSize: 9, textTransform: "none", letterSpacing: "0.04em" }}
          >
            {capturePrompt}
          </span>
        )}
        {failedReason && (
          <span className="cap" style={{ fontSize: 9, color: "var(--ac-warn)" }}>
            {failedReason}
          </span>
        )}
        {stolen?.actionId === kb.action_id && (
          <span className="cap" style={{ fontSize: 9, color: "var(--ac-warn)" }}>
            ⚠ {stolen.triggerLabel} taken from {stolen.label}
          </span>
        )}
      </div>
    );
  };

  const renderRow = (kb: Keybind) => (
    <div
      key={kb.action_id}
      data-row={kb.action_id}
      className="row between acenter gap-6"
      style={{ padding: "10px 0", borderBottom: "1px solid var(--bd-1)" }}
    >
      <div className="col" style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontSize: 13, color: "var(--tx-0)" }}>{kb.label}</span>
        {kb.desc && (
          <span
            className="cap-dim"
            style={{ fontSize: 10, marginTop: 2, textTransform: "none", letterSpacing: "0.04em" }}
          >
            {kb.desc}
          </span>
        )}
      </div>
      {renderChip(kb)}
      <Button variant="ghost" size="sm" style={{ marginLeft: 8 }} onClick={handleUnbind([kb.action_id])}>
        UNBIND
      </Button>
    </div>
  );

  const handleGrantPermission = async () => {
    setRequested(true);
    try {
      const res = await api.requestHotkeyPermission();
      // `prompted` is NOT the user's answer (the OS resolves the prompt
      // asynchronously and never reports the decision back through this
      // call). It is only evidence about whether a prompt could still be
      // shown: no prompt AND still not granted means the one-shot is spent,
      // and System Settings is the only remaining route.
      setPromptSpent(!res.prompted && res.permission !== "granted");
    } catch (err) {
      // Logged, not surfaced: the banner already says hotkeys are
      // unavailable, and RE-CHECK stays available as the retry.
      console.error("requestHotkeyPermission failed", err);
    }
  };

  const handleOpenPermissionSettings = () => {
    void api.openHotkeyPermissionSettings().catch((err) => {
      console.error("openHotkeyPermissionSettings failed", err);
    });
  };

  const handleRecheckPermission = () => {
    void api.recheckHotkeyPermission().catch((err) => {
      console.error("recheckHotkeyPermission failed", err);
    });
  };

  // Only macOS ever reports "denied". "not_applicable" (Windows, Linux/X11)
  // and "unknown" must render no permission affordance at all -- there is
  // nothing for the user to grant, so a button would be a dead end.
  const permissionDenied = hotkeys.permission === "denied";
  // Access is in place and hotkeys STILL will not register. The backend
  // already re-applied on the grant (which recreates the event tap and
  // usually suffices), so if this shows, a restart is the remaining step.
  // Text only, deliberately: a programmatic relaunch is platform-specific,
  // easy to get wrong, and mostly unnecessary.
  // The !registered half is redundant inside the banner (which only renders
  // when registration failed) but kept so the name cannot drift from the
  // condition if this ever moves.
  const grantedButUnregistered = hotkeys.permission === "granted" && !hotkeys.registered;

  const radioGroups = groupPerRadio(keybinds.filter((k) => k.category === "per_radio"));

  return (
    <div className="col gap-5">
      {!hotkeys.registered && (
        <div className="col gap-3" style={{ padding: "8px 12px" }}>
          <span className="cap" style={{ color: "var(--ac-alert)" }}>
            Global hotkeys unavailable — {hotkeys.error}
          </span>

          {permissionDenied && (
            <>
              <span
                className="cap-dim"
                style={{ fontSize: 10, textTransform: "none", letterSpacing: "0.04em" }}
              >
                macOS requires Accessibility permission for global hotkeys (System
                Settings → Privacy &amp; Security → Accessibility). Until it is granted,
                the system delivers no keypress to VCS while another application is
                focused.
              </span>
              <div className="row gap-3">
                {promptSpent ? (
                  <Button size="sm" onClick={handleOpenPermissionSettings}>
                    OPEN SETTINGS
                  </Button>
                ) : (
                  <Button size="sm" variant="primary" onClick={handleGrantPermission}>
                    GRANT ACCESS
                  </Button>
                )}
                {requested && (
                  <Button size="sm" variant="ghost" onClick={handleRecheckPermission}>
                    RE-CHECK
                  </Button>
                )}
              </div>
            </>
          )}

          {grantedButUnregistered && (
            <span
              className="cap-dim"
              style={{ fontSize: 10, textTransform: "none", letterSpacing: "0.04em" }}
            >
              Accessibility is granted — restart VCS for hotkeys to take effect.
            </span>
          )}
        </div>
      )}

      {/* Unsupported (macOS today) is informational, not an error -- there is
          nothing to grant -- so this never renders any affordance, only the
          fixed error text a supported-but-broken subsystem reports. Its own
          copy, deliberately not the Accessibility banner's: a Linux
          `input`-group permission problem and a macOS Accessibility grant
          are different causes with different fixes. */}
      {joystick.supported && joystick.error && (
        <div className="col gap-3" style={{ padding: "8px 12px" }}>
          <span className="cap" style={{ color: "var(--ac-warn)" }}>
            Joystick unavailable — {joystick.error}
          </span>
        </div>
      )}

      {SIMPLE_GROUPS.map(({ category, title }) => {
        const rows = keybinds.filter((k) => k.category === category);
        if (rows.length === 0) return null;
        return (
          <Panel key={category} title={title}>
            {rows.map(renderRow)}
          </Panel>
        );
      })}

      {radioGroups.length > 0 && (
        <Panel title="PER-RADIO BINDINGS">
          <table className="tbl">
            <thead>
              <tr>
                <th>Radio</th>
                <th>PTT</th>
                <th>Select</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {radioGroups.map((group) => {
                const label = group.ptt?.label ?? group.select?.label ?? group.id;
                const unbindIds = [group.ptt?.action_id, group.select?.action_id].filter(
                  (id): id is string => Boolean(id),
                );
                return (
                  <tr key={group.id}>
                    <td>
                      <span style={{ color: "var(--tx-0)" }}>{label}</span>
                    </td>
                    <td>{group.ptt ? renderChip(group.ptt) : <span className="kbd unbound">—</span>}</td>
                    <td>
                      {group.select ? renderChip(group.select) : <span className="kbd unbound">—</span>}
                    </td>
                    <td>
                      <Button variant="ghost" size="sm" onClick={handleUnbind(unbindIds)}>
                        UNBIND
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Panel>
      )}
    </div>
  );
}
