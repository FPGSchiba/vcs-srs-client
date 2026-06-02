import { Window, Application } from "@wailsio/runtime";
import { Icon } from "./Icon";
import { api } from "../api/client";
import { useBuildInfo } from "../hooks/useBuildInfo";
import { useSession } from "../store/session";
import type { Conn } from "../store/session";

interface TopBarProps {
  view: string;
}

const VIEW_TITLES: Record<string, string> = {
  home: "Home",
  operations: "Operations",
  players: "Player List",
  server: "Server Details",
  admin: "Administration",
  profiles: "Radio Profiles",
  settings: "Settings",
  support: "Support",
  history: "Transmission Log",
  serverNetwork: "Server Network",
  operationDetail: "Operation Detail",
};

interface Launcher {
  key: string;
  label: string;
  icon: string;
}

const POPOUT_LAUNCHERS: Launcher[] = [
  { key: "comms", label: "Comms", icon: "comms" },
  { key: "fleet", label: "Fleet", icon: "fleet" },
  { key: "ship", label: "Ship", icon: "ship" },
  { key: "messages", label: "Msgs", icon: "chat" },
  { key: "notifications", label: "Notif", icon: "bell" },
];

/**
 * TopBar is the post-login application title bar, ported from the design
 * prototype's `shell.jsx` TopBar. The launcher strip currently wires only the
 * Comms pop-out (`api.openWindow("comms")`); the remaining launchers render but
 * are disabled placeholders for later phases. The user/callsign identity store
 * is not wired yet, so the user-menu trigger shows placeholder text. The
 * `.topbar` className carries the drag region from the ported CSS. classNames
 * are kept byte-identical to the design so the ported CSS applies unchanged.
 */
const CONN_PILL: Record<Conn, { cls: string; label: string }> = {
  connected: { cls: "conn-pill", label: "CONNECTED" },
  reconnecting: { cls: "conn-pill warn", label: "RECONNECTING" },
  disconnected: { cls: "conn-pill alert", label: "DISCONNECTED" },
};

export function TopBar({ view }: TopBarProps) {
  const build = useBuildInfo();
  const conn = useSession((s) => s.conn);
  const pill = CONN_PILL[conn];
  return (
    <div className="topbar">
      <div className="brand">
        <div className="brand-mark">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
            <circle cx="12" cy="12" r="9" stroke="var(--ac-primary)" strokeWidth="1" opacity="0.5" />
            <circle cx="12" cy="12" r="5" stroke="var(--ac-primary)" strokeWidth="1" />
            <path d="M7 14 L12 8 L17 14" stroke="var(--ac-primary)" strokeWidth="1.5" fill="none" strokeLinecap="square" />
            <circle cx="12" cy="12" r="1.5" fill="var(--ac-primary)" />
          </svg>
        </div>
        <div className="col" style={{ lineHeight: 1.1 }}>
          <span className="brand-name">VCS</span>
          <span className="brand-sub">Vanguard · v{build?.client_version ?? "—"}</span>
        </div>
      </div>

      <div className="topbar-crumbs">
        <span className="crumb-active">{VIEW_TITLES[view] || view}</span>
      </div>

      <div className="topbar-right">
        {/* Panel launcher strip */}
        <div className="launcher">
          {POPOUT_LAUNCHERS.map((l) => {
            const isComms = l.key === "comms";
            return (
              <span
                key={l.key}
                className="launcher-btn"
                onClick={isComms ? () => void api.openWindow("comms") : undefined}
                title={isComms ? `${l.label} · Closed — click to open` : "Arrives in a later phase"}
                aria-disabled={isComms ? undefined : true}
                style={isComms ? undefined : { opacity: 0.45, pointerEvents: "none" }}
              >
                <Icon name={l.icon} size={11} />
                {l.label}
                <span className="dot" />
              </span>
            );
          })}
        </div>

        {/* Connection status */}
        <span className={pill.cls}>
          <span className="dot" />
          {pill.label}
        </span>

        {/* User menu (identity store not wired yet — placeholders) */}
        <div className="user-menu-anchor">
          <div className="user-menu-trigger">
            <div className="avatar">—</div>
            <div className="col" style={{ lineHeight: 1.15 }}>
              <span className="um-name">GUEST</span>
              <span className="um-meta">—</span>
            </div>
            <Icon name="chevronD" size={10} style={{ color: "var(--tx-3)" }} />
          </div>
        </div>

        <div className="win-ctrl">
          <button title="Minimize" onClick={() => void Window.Minimise()}>
            <Icon name="minimize" size={12} />
          </button>
          <button className="close" title="Close · Quit" onClick={() => void Application.Quit()}>
            <Icon name="close" size={12} />
          </button>
        </div>
      </div>
    </div>
  );
}
