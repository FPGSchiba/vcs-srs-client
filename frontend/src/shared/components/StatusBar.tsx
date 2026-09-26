import { Icon } from "./Icon";
import { useBuildInfo } from "../hooks/useBuildInfo";
import {
  useConnection,
  controlDot,
  voiceDot,
  formatRTT,
  type ConnLink,
  type Dot,
} from "../store/connection";

interface StatusBarProps {
  /** Navigates the main window to a nav key. Optional because a consumer
   *  with no navigation of its own to offer need not supply it -- today
   *  `MainApp` is the only renderer of `StatusBar` and always passes it. */
  onNavigate?: (key: string) => void;
}

/** One half of the dual-pill. `kind` selects the design's `.seg.ctrl` /
 *  `.seg.voice` tinting; `dot` selects `.d.ok` / `.d.warn` / `.d.alert`, plus
 *  `.d.off` for a plane that is not running at all. */
function Segment({
  kind,
  label,
  link,
  dot,
  server,
}: {
  kind: "ctrl" | "voice";
  label: string;
  link: ConnLink;
  dot: Dot;
  server: string;
}) {
  return (
    <span className={`seg ${kind}`}>
      <span className={`d ${dot}`} />
      <span className="lbl">{label}</span>
      <span className="val">{server || "standalone"}</span>
      <span className="ping">{formatRTT(link.rtt_ms)}</span>
    </span>
  );
}

/**
 * StatusBar is the bottom application status bar, ported from the design
 * prototype's `shell.jsx` StatusBar.
 *
 * It renders the prototype's `.dual-pill` UNCONDITIONALLY, not only in
 * distributed mode. Standalone genuinely has two independently-failing
 * transports — a gRPC control plane and a UDP voice plane — and the
 * ConnBanner's `control-only` / `voice-only` variants are only legible if the
 * pill can show which half is down. The prototype's single-dot standalone
 * branch cannot express that. Phase 9 fills in real per-host names and adds
 * hosts; it does not restructure this widget.
 *
 * Both segments show the same server address today: `srs.proto` carries no
 * server display name (PROTO_GAPS #10), so `.val` is the host from
 * `server_url`. The prototype's region item is omitted rather than rendered
 * as a permanent em dash — there is no data source for it.
 *
 * classNames are byte-identical to the design so the ported CSS applies.
 */
export function StatusBar({ onNavigate }: StatusBarProps) {
  const conn = useConnection((s) => s.conn);
  const build = useBuildInfo();
  const goServer = () => onNavigate?.("server");

  return (
    <div className="statusbar">
      <span className="sb-item">v{build?.client_version ?? "—"}</span>
      <span className="sb-divider"></span>

      <div className="dual-pill" onClick={goServer} title="Open Server Details">
        <Segment
          kind="ctrl"
          label="CTRL"
          link={conn.control}
          dot={controlDot(conn.control)}
          server={conn.server}
        />
        <Segment
          kind="voice"
          label="VOICE"
          link={conn.voice}
          dot={voiceDot(conn.voice)}
          server={conn.server}
        />
      </div>

      <span className="sb-spacer"></span>

      <span className="sb-btn" onClick={goServer}>
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
