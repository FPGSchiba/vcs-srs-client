import type { ReactNode } from "react";

interface PanelProps {
  title: string;
  children: ReactNode;
}

/**
 * Panel wraps content in the design's `.panel` container with a `.panel-h`
 * title strip and a `.panel-body`. classNames match the design so the ported
 * CSS applies unchanged. Presentational only.
 */
export function Panel({ title, children }: PanelProps) {
  return (
    <div className="panel">
      <div className="panel-h">
        <span className="cap">{title}</span>
      </div>
      <div className="panel-body">{children}</div>
    </div>
  );
}
