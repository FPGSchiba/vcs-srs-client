interface PlaceholderProps {
  title?: string;
}

/**
 * Placeholder is the in-window fallback screen rendered for nav targets whose
 * real implementation lands in a later phase. It centers a `.state-card` with a
 * mono title so the navigation flow is exercisable end-to-end while individual
 * screens are still stubs. (Distinct from shared/components/Placeholder.tsx,
 * which is the per-window bundle splash.) classNames match the ported design.
 */
export function Placeholder({ title = "Arrives in a later phase" }: PlaceholderProps) {
  return (
    <div style={{ padding: 14, height: "100%", display: "grid", placeItems: "center" }}>
      <div className="state-card">
        <div className="state-title">{title}</div>
      </div>
    </div>
  );
}
