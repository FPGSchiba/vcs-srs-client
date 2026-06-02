import { Window, Application } from "@wailsio/runtime";
import { Icon } from "./Icon";
import { useBuildInfo } from "../hooks/useBuildInfo";

interface PreLoginTopBarProps {
  className?: string;
}

/**
 * PreLoginTopBar is the minimal title bar shown before the user signs in,
 * ported from the design prototype's `shell.jsx` PreLoginTopBar. The `.topbar`
 * className carries `--wails-draggable: drag` (set in components.css) so the bar
 * drags the window in Wails v3; `.win-ctrl` buttons opt out with `no-drag`. The
 * minimize/close buttons drive the current Wails window directly.
 */
export function PreLoginTopBar({ className }: PreLoginTopBarProps) {
  const build = useBuildInfo();
  return (
    <div
      className={className ? `topbar ${className}` : "topbar"}
      style={{ gridTemplateColumns: "auto 1fr auto" }}
    >
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
        <span className="crumb-active">Sign In</span>
        <span className="crumb-sep">·</span>
        <span className="cap" style={{ color: "var(--tx-3)" }}>SRS PROTOCOL {build?.protocol_version ?? "—"}</span>
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
  );
}
