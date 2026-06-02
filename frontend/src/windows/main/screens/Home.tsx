import { api } from "../../../shared/api/client";
import { Icon } from "../../../shared/components/Icon";
import { useWindows } from "../../../shared/store/windows";

interface LauncherTile {
  key: string;
  name: string;
  desc: string;
  icon: string;
  meta: string[];
}

const TILES: LauncherTile[] = [
  { key: "comms", name: "Communications", desc: "Tune, transmit, monitor", icon: "comms", meta: ["6 RADIOS", "1 KEYED"] },
  { key: "fleet", name: "Fleet Mode", desc: "C2 dashboard · roster", icon: "fleet", meta: ["6 SHIPS", "OP STARWALK"] },
  { key: "ship", name: "Ship Mode", desc: "Engineering & damage", icon: "ship", meta: ["16 COMPONENTS", "2 DEGRADED"] },
  { key: "messages", name: "Messages", desc: "Text channels per freq", icon: "chat", meta: ["6 CHANNELS"] },
  { key: "notifications", name: "Notifications", desc: "Alerts, broadcasts, sync", icon: "bell", meta: ["7 CATEGORIES"] },
];

/**
 * Home is the main-window landing screen, ported from the design prototype's
 * home.jsx launcher-tile grid. The Comms tile toggles the comms pop-out window
 * via `api.toggleWindow("comms")` and shows an `is-open` state; the remaining
 * tiles render with the same `.launcher-tile` styling but are inert this phase
 * (their windows arrive later). classNames match the ported design CSS.
 */
export function Home() {
  const openWindows = useWindows((s) => s.open);
  const toggleComms = () => void api.toggleWindow("comms");

  return (
    <div style={{ padding: 14, height: "100%", overflow: "auto" }}>
      <div className="row between acenter" style={{ marginBottom: 6 }}>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>PANELS</span>
        <span className="cap-dim">Click a tile to open its pop-out</span>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(5, 1fr)", gap: 10 }}>
        {TILES.map((t) => {
          const isComms = t.key === "comms";
          const isOpen = isComms && openWindows.includes("comms");
          return (
            <div
              key={t.key}
              className={`launcher-tile${isOpen ? " is-open" : ""}`}
              onClick={isComms ? toggleComms : undefined}
              title={
                isComms
                  ? `Comms · ${isOpen ? "click to close" : "click to open"}`
                  : "Arrives in a later phase"
              }
              aria-disabled={isComms ? undefined : true}
              aria-pressed={isComms ? isOpen : undefined}
              style={isComms ? undefined : { opacity: 0.45, pointerEvents: "none" }}
            >
              <span className="lt-bg"><Icon name={t.icon} size={92} /></span>
              <div className="lt-head">
                <Icon name={t.icon} size={18} style={{ color: "var(--ac-primary)" }} />
                <span className="lt-name">{t.name}</span>
              </div>
              <div className="lt-desc">{t.desc}</div>
              <div className="lt-meta">
                {t.meta.map((m) => (
                  <span key={m}>
                    <b>{m.split(" ")[0]}</b> {m.split(" ").slice(1).join(" ")}
                  </span>
                ))}
              </div>
              <span className="lt-state">
                <span className="dot" />
                {isComms ? (isOpen ? "OPEN · click to close" : "CLOSED · click to open") : "ARRIVES LATER"}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
