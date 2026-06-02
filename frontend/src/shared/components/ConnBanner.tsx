import { Icon } from "./Icon";
import { api } from "../api/client";
import { useSession } from "../store/session";

type ConnBannerProps = Record<string, never>;

/**
 * ConnBanner is the connection-degraded banner, ported from the design
 * prototype's `shell.jsx` ConnBanner. Only the `disconnected` (alert) variant
 * is rendered this phase; the control-degraded / voice-degraded variants are
 * deferred until distributed voice/control health is wired. It reads the
 * session store for the control-connection state and renders nothing unless the
 * client is disconnected. The reconnect button calls `api.reconnect()`.
 * classNames are kept byte-identical to the design so the ported CSS applies.
 */
export function ConnBanner(_props: ConnBannerProps) {
  const conn = useSession((s) => s.conn);
  if (conn !== "disconnected") return null;
  return (
    <div className="conn-banner alert">
      <span className="blink" />
      <span style={{ fontWeight: 600, letterSpacing: "0.18em" }}>DISCONNECTED</span>
      <span
        style={{
          color: "var(--tx-2)",
          letterSpacing: 0,
          textTransform: "none",
          fontFamily: "var(--ff-sans)",
        }}
      >
        All servers unreachable. Audio is muted. Verify network and retry.
      </span>
      <span style={{ flex: 1 }} />
      <button className="btn btn-sm" onClick={() => void api.reconnect()}>
        <Icon name="refresh" size={10} /> FULL RECONNECT
      </button>
    </div>
  );
}
