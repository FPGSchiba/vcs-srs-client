import { useState } from "react";
import { Icon } from "./Icon";
import { api } from "../api/client";
import { useConnection, bannerVariant, type BannerVariant } from "../store/connection";

interface VariantConfig {
  kind: "warn" | "alert";
  title: string;
  msg: string;
  action: string;
  /** Which reconnect the action button drives. */
  target: "control" | "voice";
}

/** Copy taken verbatim from the design prototype's `shell.jsx` ConnBanner.
 *  `control-only` means "control is fine, voice is not", matching the
 *  prototype's own naming. */
const VARIANTS: Record<Exclude<BannerVariant, "none">, VariantConfig> = {
  "control-only": {
    kind: "warn",
    title: "VOICE DEGRADED",
    msg: "Connection to voice server lost — control is healthy. You cannot transmit or hear traffic until reconnected.",
    action: "RECONNECT VOICE",
    target: "voice",
  },
  "voice-only": {
    kind: "warn",
    title: "CONTROL DEGRADED",
    msg: "Lost link to control server — voice continues but state is frozen. Profile changes won't persist.",
    action: "RECONNECT CONTROL",
    target: "control",
  },
  disconnected: {
    kind: "alert",
    title: "DISCONNECTED",
    msg: "All servers unreachable. Audio is muted. Verify network and retry.",
    action: "FULL RECONNECT",
    target: "control",
  },
};

/**
 * ConnBanner is the connection-degraded banner, ported from the design
 * prototype's `shell.jsx` ConnBanner.
 *
 * All three of the prototype's variants are reachable as of Phase 6. Before
 * it, only `disconnected` was ported, and it could essentially never fire:
 * both `ConsumeUpdates` goroutines discarded the stream's terminating error,
 * so nothing ever reported a control link that died on its own.
 *
 * A voice plane that is merely UNAVAILABLE — no secret yet, no session, or a
 * build whose codec is the stub — drops out of the banner's input entirely.
 * It is not a failure the user can act on, and on every CGO-less Windows
 * release build it is the normal state.
 *
 * classNames are byte-identical to the design so the ported CSS applies.
 */
export function ConnBanner() {
  const conn = useConnection((s) => s.conn);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  const variant = bannerVariant(conn);
  if (variant === "none") return null;
  const conf = VARIANTS[variant];

  const onAction = () => {
    // Guarded rather than merely disabled: App.Reconnect re-dials and
    // re-pushes the persisted radios, and two in flight race each other's
    // stream generation — the loser's stream is cancelled under a connection
    // the user believes is live.
    if (busy) return;
    setBusy(true);
    setFailure(null);
    const call = conf.target === "voice" ? api.reconnectVoice() : api.reconnect();
    void Promise.resolve(call)
      .catch((err: unknown) => {
        // Surfaced, not discarded. The banner used to call
        // `void api.reconnect()` and drop the result, so a failed reconnect
        // was invisible and the button appeared to do nothing at all.
        setFailure(err instanceof Error ? err.message : String(err));
      })
      .finally(() => setBusy(false));
  };

  return (
    <div className={`conn-banner ${conf.kind}`}>
      <span className="blink" />
      <span style={{ fontWeight: 600, letterSpacing: "0.18em" }}>{conf.title}</span>
      <span
        style={{
          color: "var(--tx-2)",
          letterSpacing: 0,
          textTransform: "none",
          fontFamily: "var(--ff-sans)",
        }}
      >
        {failure ?? conf.msg}
      </span>
      <span style={{ flex: 1 }} />
      <button className="btn btn-sm" onClick={onAction} disabled={busy}>
        <Icon name="refresh" size={10} /> {busy ? "RECONNECTING…" : conf.action}
      </button>
    </div>
  );
}
