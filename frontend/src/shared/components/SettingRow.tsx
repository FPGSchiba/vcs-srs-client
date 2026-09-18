import type { ReactNode } from "react";

interface SettingRowProps {
  label: string;
  desc?: string;
  control: ReactNode;
}

/**
 * SettingRow lays out a single labelled setting with an optional description
 * and a trailing control (Toggle, KeyChip, etc). Ports the prototype's
 * `screens/settings.jsx` markup: existing `row`/`between`/`acenter`/`col`/
 * `gap-6`/`cap-dim` utility classes handle layout and color; the row's own
 * padding/border and the label/description type sizing have no dedicated
 * classes in components.css, so they're inlined here exactly as the
 * prototype inlines them. Presentational only.
 */
export function SettingRow({ label, desc, control }: SettingRowProps) {
  return (
    <div
      className="row between acenter gap-6"
      style={{ padding: "12px 0", borderBottom: "1px solid var(--bd-1)" }}
    >
      <div className="col" style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontSize: 13, color: "var(--tx-0)" }}>{label}</span>
        {desc && (
          <span
            className="cap-dim"
            style={{
              fontSize: 10,
              marginTop: 2,
              textTransform: "none",
              letterSpacing: "0.04em",
            }}
          >
            {desc}
          </span>
        )}
      </div>
      {control}
    </div>
  );
}
