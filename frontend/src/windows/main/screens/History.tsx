import { useEffect, useMemo, useState } from "react";
import { api } from "../../../shared/api/client";
import type { HistoryEntry } from "../../../shared/api/client";
import { EV, on } from "../../../shared/api/events";
import { useHistory } from "../../../shared/store/history";
import { Icon } from "../../../shared/components/Icon";

const WINDOWS = [
  { value: "5m", label: "5M", ms: 5 * 60_000 },
  { value: "30m", label: "30M", ms: 30 * 60_000 },
  { value: "1h", label: "1H", ms: 60 * 60_000 },
  { value: "4h", label: "4H", ms: 4 * 60 * 60_000 },
  { value: "all", label: "ALL", ms: Number.POSITIVE_INFINITY },
];

/**
 * History is the Transmission Log nav screen, ported from the design
 * prototype's `ScreenHistory`.
 *
 * Two deliberate deviations from the prototype, both forced by what exists:
 * the Replay column is DROPPED (recording is PROTO_GAPS #9, so every cell
 * would be dead forever), and the channel filter is built from the rows
 * present rather than from the current radio list -- a channel you have
 * since retuned away from still has entries worth finding.
 *
 * The log is Go-owned: this hydrates once with App.GetHistory and applies
 * `history:appended` deltas after that. It is not a full-snapshot broadcast
 * like notifications; see the store's own note.
 */
export function History() {
  const entries = useHistory((s) => s.entries);
  const [channel, setChannel] = useState("all");
  const [timeWindow, setTimeWindow] = useState("1h");
  const [search, setSearch] = useState("");

  useEffect(() => {
    api
      .getHistory()
      .then((rows) => useHistory.getState().replaceAll(rows ?? []))
      .catch(() => {
        /* not wired yet -- ignore */
      });
    const offs = [
      on<HistoryEntry>(EV.historyAppended, (e) => useHistory.getState().append(e)),
      on<null>(EV.historyCleared, () => useHistory.getState().clear()),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  const channels = useMemo(() => {
    const seen = new Map<number, string>();
    for (const e of entries) {
      if (!seen.has(e.freq_khz)) seen.set(e.freq_khz, e.radio);
    }
    return [...seen.entries()].sort((a, b) => a[0] - b[0]);
  }, [entries]);

  const rows = useMemo(() => {
    const cutoff =
      WINDOWS.find((w) => w.value === timeWindow)?.ms ?? Number.POSITIVE_INFINITY;
    const since = Date.now() - cutoff;
    const q = search.trim().toLowerCase();
    return entries.filter((e) => {
      if (channel !== "all" && String(e.freq_khz) !== channel) return false;
      if (Number.isFinite(cutoff) && new Date(e.at).getTime() < since) return false;
      if (q && !`${e.sender} ${e.radio}`.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [entries, channel, timeWindow, search]);

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
      <div
        style={{
          padding: "10px 16px",
          borderBottom: "1px solid var(--bd-1)",
          background: "var(--bg-0)",
          display: "flex",
          alignItems: "center",
          gap: 12,
        }}
      >
        <span className="cap">FILTER</span>
        <label className="sr-only" htmlFor="hist-chan">
          Channel
        </label>
        <select
          id="hist-chan"
          className="input"
          value={channel}
          onChange={(e) => setChannel(e.target.value)}
          style={{ width: 220 }}
        >
          <option value="all">All channels</option>
          {channels.map(([khz, name]) => (
            <option key={khz} value={String(khz)}>
              {name ? `${name} · ` : ""}
              {(khz / 1000).toFixed(3)}
            </option>
          ))}
        </select>
        <span className="sep-v" style={{ height: 20 }} />
        <span className="cap">TIME</span>
        <div className="seg">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              type="button"
              className={`seg-btn ${timeWindow === w.value ? "active" : ""}`}
              onClick={() => setTimeWindow(w.value)}
            >
              {w.label}
            </button>
          ))}
        </div>
        <span className="sep-v" style={{ height: 20 }} />
        <input
          className="input flex"
          placeholder="Search sender or callsign…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <button type="button" className="btn" onClick={() => void api.exportHistoryCsv()}>
          <Icon name="download" size={11} /> EXPORT CSV
        </button>
      </div>

      <div style={{ flex: 1, overflow: "auto", minHeight: 0 }}>
        {rows.length === 0 ? (
          <div
            className="col acenter"
            style={{ justifyContent: "center", height: "100%", color: "var(--tx-3)", gap: 8 }}
          >
            No transmissions yet
          </div>
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 90 }}>Time</th>
                <th>Sender</th>
                <th>Channel</th>
                <th>Frequency</th>
                <th style={{ width: 80 }}>Duration</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((e, i) => (
                <tr key={`${e.at}-${e.freq_khz}-${i}`}>
                  <td className="mono" style={{ color: "var(--tx-2)" }}>
                    {new Date(e.at).toLocaleTimeString()}
                  </td>
                  <td>
                    <span className={e.own ? "own" : undefined}>{e.sender}</span>
                  </td>
                  <td>{e.radio}</td>
                  <td className="mono" style={{ color: "var(--ac-lcd)" }}>
                    {e.freq_mhz.toFixed(3)}
                  </td>
                  <td className="mono">{(e.dur_ms / 1000).toFixed(1)}s</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
