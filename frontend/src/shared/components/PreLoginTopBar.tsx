import { Window } from "@wailsio/runtime";
import { Icon } from "./Icon";

interface PreLoginTopBarProps {
  className?: string;
}

/**
 * PreLoginTopBar is the minimal title bar shown before the user signs in,
 * ported from the design prototype's `shell.jsx` PreLoginTopBar. The `.topbar`
 * className carries `-webkit-app-region: drag` from the ported CSS so the bar
 * is draggable; `.win-ctrl` buttons are marked `no-drag` by that same CSS. The
 * minimize/close buttons drive the current Wails window directly. classNames
 * are kept byte-identical to the design so the ported CSS applies unchanged.
 */
export function PreLoginTopBar({ className }: PreLoginTopBarProps) {
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
          <span className="brand-sub">Vanguard · v3.2.1</span>
        </div>
      </div>
      <div className="topbar-crumbs">
        <span className="crumb-active">Sign In</span>
        <span className="crumb-sep">·</span>
        <span className="cap" style={{ color: "var(--tx-3)" }}>DRAG TITLE BAR TO MOVE WINDOW</span>
      </div>
      <div className="win-ctrl">
        <button title="Minimize" onClick={() => void Window.Minimise()}>
          <Icon name="minimize" size={12} />
        </button>
        <button className="close" title="Close" onClick={() => void Window.Close()}>
          <Icon name="close" size={12} />
        </button>
      </div>
    </div>
  );
}
