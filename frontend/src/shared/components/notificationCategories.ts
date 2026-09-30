/**
 * The notification categories and their accent colours, ported from the
 * design prototype (`design/vcs/project/screens/misc.jsx:404-412`).
 *
 * The full set is fixed HERE, once, rather than grown per phase. Phase 7.2
 * only ever emits `system`; 7.3 and 7.4 will emit the rest. The popout's
 * filter populates from the categories actually PRESENT, so it is never a
 * dropdown of six empty options -- but nothing later renegotiates this
 * table.
 */
export interface NotifyCategory {
  key: string;
  label: string;
  color: string;
}

export const CATEGORIES: NotifyCategory[] = [
  { key: "distress", label: "Distress", color: "#ef4f4f" },
  { key: "fleet", label: "Fleet alert", color: "#f5a524" },
  { key: "sync", label: "Sync", color: "#f5a524" },
  { key: "comms", label: "Comms", color: "#60a5fa" },
  { key: "profile", label: "Profile", color: "#4ade80" },
  { key: "system", label: "System", color: "#a78bfa" },
  { key: "operation", label: "Operation", color: "#60a5fa" },
];

const FALLBACK: NotifyCategory = { key: "system", label: "System", color: "#a78bfa" };

/** Resolves a category key to its entry, falling back to System rather than
 *  rendering an uncoloured row for a category a later phase added without
 *  updating this table. */
export function categoryFor(key: string): NotifyCategory {
  return CATEGORIES.find((c) => c.key === key) ?? FALLBACK;
}
