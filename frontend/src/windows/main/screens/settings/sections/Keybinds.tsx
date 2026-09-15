import { useState } from "react";
import { Panel } from "../../../../../shared/components/Panel";
import { Button } from "../../../../../shared/components/Button";
import { KeyChip } from "../../../../../shared/components/KeyChip";
import type { Capture } from "../../../../../shared/components/KeyChip";
import { api } from "../../../../../shared/api/client";
import { useSettings } from "../../../../../shared/store/settings";
import type { Keybind } from "../../../../../shared/store/settings";

interface StolenInfo {
  actionId: string;
  chord: string;
  label: string;
}

interface GroupDef {
  category: string;
  title: string;
}

/** Categories rendered as flat rows (one KeyChip + UNBIND each). PerRadio is
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
 * `useSettings().keybinds` / `.hotkeys`.
 *
 * Three correctness properties this file is built around:
 *
 * 1. `handleCapture` always calls `api.endCapture()` in a `finally`. The
 *    backend suspends every OS hotkey registration for the duration of a
 *    capture; if `setKeybind` throws and `endCapture` is skipped, every
 *    global hotkey stays dead until the backend's own timeout.
 *
 * 2. Only one chip may listen at a time. `capturingId` tracks which
 *    action_id is currently allowed to listen; clicking a different chip
 *    bumps that other chip's `epoch`, which changes its KeyChip `key` and
 *    forces React to unmount the old (listening) instance. KeyChip already
 *    treats "unmount while listening" as a cancel path (see its own
 *    doc-comment), so the forced remount routes through that existing
 *    safety net instead of duplicating it here -- without this, two
 *    mounted chips would both hold a window keydown listener and a single
 *    keypress would fire `setKeybind` twice.
 *
 * 3. Per-row failures: `internal/chord` accepts keys the OS layer can't
 *    register (Numpad, F21-F24, punctuation, navigation), so a chord can
 *    save successfully and still never fire. Every row whose action_id
 *    appears in `hotkeys.failed` shows that reason inline.
 */
export function Keybinds() {
  const keybinds = useSettings((s) => s.keybinds);
  const hotkeys = useSettings((s) => s.hotkeys);

  const [capturingId, setCapturingId] = useState<string | null>(null);
  const [epoch, setEpoch] = useState<Record<string, number>>({});
  const [stolen, setStolen] = useState<StolenInfo | null>(null);

  const bumpEpoch = (actionId: string) =>
    setEpoch((e) => ({ ...e, [actionId]: (e[actionId] ?? 0) + 1 }));

  // Wired via onClickCapture on a wrapper around each KeyChip, so it runs
  // before KeyChip's own onClick (bubble phase) -- "beginCapture first,
  // then let KeyChip listen".
  const handleChipClick = (actionId: string) => {
    if (capturingId === actionId) {
      // Re-clicking the chip that's already listening is itself a cancel;
      // KeyChip's own click handler resolves that through onCancel.
      return;
    }
    setStolen(null);
    if (capturingId) bumpEpoch(capturingId);
    setCapturingId(actionId);
    void api.beginCapture();
  };

  const handleCapture = (actionId: string) => async (cap: Capture) => {
    try {
      const res = await api.setKeybind(actionId, cap);
      setStolen(
        res.stolen ? { actionId, chord: res.stolen.chord, label: res.stolen.label } : null,
      );
    } catch {
      // Swallowed deliberately: KeyChip invokes onCapture without awaiting
      // or catching its result, so a rethrow here would only surface as an
      // unhandled rejection. The backend is the source of truth for the
      // bound chord (SettingsScreen's keybinds:changed subscription), so a
      // failed write simply leaves the row showing its previous chord.
    } finally {
      // Runs even when setKeybind throws, so the backend always re-arms its
      // OS hotkey registrations instead of staying suspended until its own
      // timeout.
      await api.endCapture();
      setCapturingId((cur) => (cur === actionId ? null : cur));
    }
  };

  const handleCancel = (actionId: string) => () => {
    void api.endCapture();
    setCapturingId((cur) => (cur === actionId ? null : cur));
  };

  const handleUnbind = (actionIds: string[]) => () => {
    actionIds.forEach((id) => void api.clearKeybind(id));
  };

  const chipKey = (actionId: string) => `${actionId}:${epoch[actionId] ?? 0}`;

  const renderChip = (kb: Keybind) => {
    const failedReason = hotkeys.failed[kb.action_id];
    return (
      <div className="col" style={{ gap: 2, alignItems: "flex-end" }}>
        <span onClickCapture={() => handleChipClick(kb.action_id)}>
          <KeyChip
            key={chipKey(kb.action_id)}
            binding={kb.chord}
            onCapture={handleCapture(kb.action_id)}
            onCancel={handleCancel(kb.action_id)}
          />
        </span>
        {failedReason && (
          <span className="cap" style={{ fontSize: 9, color: "var(--ac-warn)" }}>
            {failedReason}
          </span>
        )}
        {stolen?.actionId === kb.action_id && (
          <span className="cap" style={{ fontSize: 9, color: "var(--ac-warn)" }}>
            ⚠ {stolen.chord} taken from {stolen.label}
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

  const radioGroups = groupPerRadio(keybinds.filter((k) => k.category === "per_radio"));

  return (
    <div className="col gap-5">
      {!hotkeys.registered && (
        <div className="cap" style={{ color: "var(--ac-alert)", padding: "8px 12px" }}>
          Global hotkeys unavailable — {hotkeys.error}
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
