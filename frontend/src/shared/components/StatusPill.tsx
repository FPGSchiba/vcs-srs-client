interface StatusPillProps {
  kind: "available" | "combat" | "discipline" | "afk";
  mini?: boolean;
}

/**
 * StatusPill renders a colored status indicator pill with a leading dot and an
 * uppercase label, ported from the design prototype's `atoms.jsx` StatusPill.
 * classNames (`pill`, `pill-${kind}`, `dot`) are kept identical to the design so
 * the ported CSS applies unchanged. Presentational only.
 */
export function StatusPill({ kind, mini = false }: StatusPillProps) {
  return (
    <span className={`pill pill-${kind}`}>
      <span className="dot" />
      {!mini && kind.toUpperCase()}
    </span>
  );
}
