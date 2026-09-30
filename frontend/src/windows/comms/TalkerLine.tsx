import { VU } from "../../shared/components/VU";

interface Props {
  /** Who is being received. Null/undefined means nothing is coming in. */
  talker?: string | null;
  /** This is our own transmission. Takes precedence over `talker`. */
  self?: boolean;
  /** Normalised 0-1 level for the meter. */
  level?: number;
  disabled: boolean;
}

/**
 * The card's status line: state dot, who is talking, and the meter — ONE
 * group, with the flexible spacer AFTER the meter so the three read as a unit.
 * Putting `flex: 1` on the name instead pushes the meter to the far edge,
 * where it looks like an unrelated widget.
 *
 * There is no live per-radio RX feed yet (design/vcs/radio-variants.md §9), so
 * with no talker this honestly says "no traffic" at zero level. Nothing here
 * simulates activity.
 */
export function TalkerLine({ talker, self, level = 0, disabled }: Props) {
  if (disabled) {
    return (
      <div className="row acenter gap-2" style={{ minWidth: 0 }}>
        <span className="cap mono" style={{ color: "var(--tx-4)", letterSpacing: "0.16em" }}>
          OFF
        </span>
      </div>
    );
  }

  const active = self || Boolean(talker);
  const accent = self ? "var(--ac-primary)" : talker ? "var(--ac-ok)" : "var(--tx-4)";

  return (
    <div className="row acenter gap-2" style={{ minWidth: 0 }}>
      <span
        aria-hidden="true"
        style={{
          width: 6,
          height: 6,
          borderRadius: "50%",
          flexShrink: 0,
          background: accent,
          boxShadow: active ? `0 0 6px ${accent}` : undefined,
        }}
      />
      <span data-testid="talker-marker" aria-hidden="true" style={{ color: accent, fontSize: 9 }}>
        {self ? "▲" : "▶"}
      </span>
      <span
        className="mono"
        style={{
          color: accent,
          fontSize: 11,
          minWidth: 0,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
      >
        {self ? "you" : (talker ?? "no traffic")}
      </span>
      <VU level={active ? level : 0} segs={6} aria-label="signal" />
      <span data-testid="talker-spacer" style={{ flex: 1 }} />
    </div>
  );
}
