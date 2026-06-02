import { Icon } from "./Icon";
import { useSession } from "../store/session";
import type { Conn } from "../store/session";

type StatusBarProps = Record<string, never>;

/**
 * Maps the control-connection state to a design status color token.
 * connected → ok (green), reconnecting → warn, disconnected → alert.
 * The control-only status dot is colored inline (as in the design prototype)
 * because the `.d.ok/.warn/.alert` CSS classes are scoped to `.dual-pill`.
 */
function dotColor(conn: Conn): string {
  if (conn === "connected") return "var(--ac-ok)";
  if (conn === "reconnecting") return "var(--ac-warn)";
  return "var(--ac-alert)";
}

/**
 * StatusBar is the bottom application status bar, ported from the design
 * prototype's `shell.jsx` StatusBar in its control-only form (the dual
 * control/voice pill is deferred to a later phase). It reads the session store
 * for the server address and control-connection state. Live ping is not wired
 * to the frontend yet, so latency renders as a placeholder. The NETWORK / HELP
 * / ALERTS buttons render as no-op placeholders this phase. classNames are kept
 * byte-identical to the design so the ported CSS applies unchanged.
 */
export function StatusBar(_props: StatusBarProps) {
  const server = useSession((s) => s.server);
  const conn = useSession((s) => s.conn);
  return (
    <div className="statusbar">
      <span className="sb-item">v3.2.1-stable</span>
      <span className="sb-divider"></span>

      <span className="sb-item">
        <span
          style={{
            width: 5,
            height: 5,
            borderRadius: "50%",
            background: dotColor(conn),
            display: "inline-block",
          }}
        />
        {server || "standalone"} · — ms
      </span>

      <span className="sb-divider"></span>
      <span className="sb-item">Region —</span>
      <span className="sb-spacer"></span>

      <span className="sb-btn">
        <Icon name="server" size={11} /> NETWORK
      </span>
      <span className="sb-btn">
        <Icon name="help" size={11} /> HELP
      </span>
      <span className="sb-btn">
        <Icon name="bell" size={11} />
        ALERTS
      </span>
    </div>
  );
}
