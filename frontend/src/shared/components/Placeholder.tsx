interface PlaceholderProps {
  label: string;
}

/**
 * Placeholder is the temporary per-window splash used during Phase 1 before the
 * real screens land. It centers a single mono, accent-colored label on the dark
 * token background so each window proves its bundle + design tokens load.
 */
export function Placeholder({ label }: PlaceholderProps) {
  return (
    <div className="h-full grid place-items-center text-tx-0">
      <div className="font-mono text-ac-primary tracking-widest">{label}</div>
    </div>
  );
}
