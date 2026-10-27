import { useState } from "react";
import { Icon } from "./Icon";
import { useSession } from "../store/session";
import { activatable } from "./activatable";

interface NavRailProps {
  activeKey: string;
  onSelect: (key: string) => void;
  onLogout?: () => void;
}

interface NavItem {
  key: string;
  label: string;
  icon: string;
}

const NAV_ITEMS: NavItem[] = [
  { key: "home", label: "Home", icon: "grid" },
  { key: "operations", label: "Operations", icon: "fleet" },
  { key: "players", label: "Player List", icon: "users" },
  { key: "history", label: "Transmission Log", icon: "history" },
  { key: "profiles", label: "Radio Profiles", icon: "layout" },
  { key: "server", label: "Server Details", icon: "server" },
  { key: "admin", label: "Administration", icon: "admin" },
  { key: "settings", label: "Settings", icon: "settings" },
  { key: "support", label: "Support", icon: "help" },
];

/**
 * NavRail is the left console navigation rail, ported from the design
 * prototype's `shell.jsx` NavRail. It is decoupled from the router: selection
 * is reported via `onSelect` and the active item is driven by `activeKey`. The
 * collapse toggle keeps its local UI state. The footer user-chip shows the
 * local client's callsign/FFID from the session store. classNames are kept
 * byte-identical to the design so the ported CSS applies unchanged.
 *
 * Keyboard (Phase 6.9 CL3, FE04): the items and Logout keep the design's
 * `div` markup but spread `activatable`, so they are tabbable buttons that
 * Enter and Space activate. The active item carries `aria-current="page"`,
 * and every item an `aria-label`, so the rail stays named when collapsed to
 * icons.
 */
export function NavRail({ activeKey, onSelect, onLogout }: NavRailProps) {
  const [collapsed, setCollapsed] = useState(false);
  const self = useSession((s) => s.self);
  const initials = self?.callsign ? self.callsign.slice(0, 2).toUpperCase() : "—";
  return (
    <nav className={`nav ${collapsed ? "collapsed" : ""}`}>
      <div className="nav-header">
        <span className="cap">Console</span>
        <button
          type="button"
          className="nav-toggle"
          onClick={() => setCollapsed((c) => !c)}
          title={collapsed ? "Expand" : "Collapse"}
          aria-label={collapsed ? "Expand" : "Collapse"}
        >
          <Icon name={collapsed ? "chevron" : "chevronD"} size={12} />
        </button>
      </div>
      <div style={{ flex: 1, overflow: "auto" }}>
        <div className="nav-list" style={{ padding: "8px 6px" }}>
          {NAV_ITEMS.map((it) => (
            <div
              key={it.key}
              className={`nav-item ${it.key === activeKey ? "active" : ""}`}
              {...activatable(() => onSelect(it.key))}
              aria-current={it.key === activeKey ? "page" : undefined}
              aria-label={it.label}
              title={collapsed ? it.label : undefined}
            >
              <span className="ic"><Icon name={it.icon} size={16} /></span>
              <span className="label">{it.label}</span>
            </div>
          ))}
        </div>
      </div>
      {onLogout && (
        <div style={{ borderTop: "1px solid var(--bd-1)", padding: "6px" }}>
          <div
            className="nav-item"
            {...activatable(onLogout)}
            title={collapsed ? "Logout" : undefined}
            style={{ color: "var(--ac-alert)" }}
          >
            <span className="ic"><Icon name="unlock" size={16} /></span>
            <span className="label">Logout</span>
          </div>
        </div>
      )}
      <div className="nav-footer">
        <div className="user-chip">
          <div className="avatar">{initials}</div>
          <div className="user-meta">
            <div className="user-name">{self?.callsign || "—"}</div>
            <div className="user-ffid">FFID {self?.ffid || "—"}</div>
          </div>
        </div>
      </div>
    </nav>
  );
}
