import { Window, Application } from "@wailsio/runtime";
import { Icon } from "./Icon";
import { activatable } from "./activatable";
import { api } from "../api/client";
import { useBuildInfo } from "../hooks/useBuildInfo";
import { useSession } from "../store/session";
import type { Conn } from "../store/session";
import { useWindows } from "../store/windows";
import { useNotifications } from "../store/notifications";

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
 * prototype's `shell.jsx` TopBar. The launcher strip wires `comms` and
 * `notifications` through `api.toggleWindow`; `fleet`, `ship` and `messages`
 * remain disabled placeholders for later phases. The `notifications`
 * launcher additionally carries an unread badge. The user/callsign identity
 * store is not wired yet, so the user-menu trigger shows placeholder text.
 * The `.topbar` className carries the drag region from the ported CSS.
 * classNames are kept byte-identical to the design so the ported CSS
 * applies unchanged.
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
  const self = useSession((s) => s.self);
  const openWindows = useWindows((s) => s.open);
  const unread = useNotifications((s) => s.snap.unread);
  const callsign = self?.callsign || "GUEST";
  const initials = self?.callsign ? self.callsign.slice(0, 2).toUpperCase() : "—";
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
            const wired = l.key === "comms" || l.key === "notifications";
            const isOpen = openWindows.includes(l.key);
            const badge = l.key === "notifications" ? unread : 0;
            return (
              <span
                key={l.key}
                data-launcher={l.key}
                className={`launcher-btn${isOpen ? " open" : ""}`}
                // The prototype's CSS keys off this className, so the
                // element stays a span and gains button semantics instead.
                // Touching this line makes it new code for SonarCloud's
                // gate (typescript:S1082), so the keyboard path is not
                // optional -- and adding it here also fixes a pre-existing
                // gap for the Comms launcher.
                {...(wired ? activatable(() => void api.toggleWindow(l.key)) : {})}
                title={
                  wired
                    ? `${l.label} · ${isOpen ? "Open — click to close" : "Closed — click to open"}`
                    : "Arrives in a later phase"
                }
                aria-disabled={wired ? undefined : true}
                aria-pressed={wired ? isOpen : undefined}
                style={wired ? undefined : { opacity: 0.45, pointerEvents: "none" }}
              >
                <Icon name={l.icon} size={11} />
                {l.label}
                <span className="dot" />
                {badge > 0 && <span className="badge">{badge}</span>}
              </span>
            );
          })}
        </div>

        {/* Connection status */}
        <span className={pill.cls}>
          <span className="dot" />
          {pill.label}
        </span>

        {/* Local user identity */}
        <div className="user-menu-anchor">
          <div className="user-menu-trigger">
            <div className="avatar">{initials}</div>
            <div className="col" style={{ lineHeight: 1.15 }}>
              <span className="um-name">{callsign}</span>
              <span className="um-meta">{self?.coalition || "—"}</span>
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
