interface VUProps {
  /** Signal level, normalized 0–1 (the backend emits normalized levels).
   * Out-of-range values are clamped rather than allowed to overflow or
   * underflow the segment count. */
  level: number;
  /** Number of discrete segments to render. Defaults to 16, matching the
   * meter used in the Settings audio panel in the design prototype. */
  segs?: number;
  /** Renders bottom-up in a narrow vertical strip, matching the prototype's
   * vertical variant (used where horizontal space is tight). */
  vertical?: boolean;
  className?: string;
  /** Accessible name for the meter. Optional: in most places this component
   * is used, a visible text label ("VU · 50%") already sits next to it, so
   * forcing a redundant name here would over-annotate a passive display. */
  "aria-label"?: string;
}

/**
 * VU renders a segmented level meter ported from the design prototype's
 * `atoms.jsx` VU. Each segment lights when `level` reaches its threshold
 * `(i + 1) / segs`, and the top ~30%/~10% of segments carry `warn`/`peak`
 * classNames so the ported CSS colors them amber/red — matching the
 * prototype's markup and token usage unchanged.
 *
 * `data-vu-seg` and `data-lit` are added (not present in the prototype) so
 * tests can assert on segment count and lit state without depending on
 * brittle internal className strings.
 *
 * This is a passive, non-interactive display: it carries `role="meter"`
 * with numeric ARIA state so assistive tech can still read the level, but
 * no mandatory accessible name is forced on it -- callers that show this
 * meter without an adjacent visible label should pass `aria-label`.
 * Presentational only.
 */
export function VU({
  level,
  segs = 16,
  vertical = false,
  className,
  "aria-label": ariaLabel,
}: VUProps) {
  const clamped = Math.max(0, Math.min(1, level));

  return (
    <div
      className={`vu ${className ?? ""}`.trim()}
      style={
        vertical
          ? { flexDirection: "column-reverse", height: 60, width: 8 }
          : undefined
      }
      role="meter"
      aria-valuenow={Math.round(clamped * 100)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={ariaLabel}
    >
      {Array.from({ length: segs }).map((_, i) => {
        const threshold = (i + 1) / segs;
        const lit = clamped >= threshold;
        const warn = i >= segs * 0.7 && i < segs * 0.9;
        const peak = i >= segs * 0.9;
        return (
          <span
            key={i}
            data-vu-seg
            data-lit={lit}
            className={`seg ${lit ? "on" : ""} ${warn ? "warn" : ""} ${peak ? "peak" : ""}`.trim()}
          />
        );
      })}
    </div>
  );
}
