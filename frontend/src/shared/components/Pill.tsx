import type { ReactNode } from "react";

interface PillProps {
  children: ReactNode;
  className?: string;
}

/**
 * Pill is a generic inline label chip using the design's `pill` className, with
 * an optional extra `className` for variants (e.g. `pill-disabled`).
 * Presentational only.
 */
export function Pill({ children, className }: PillProps) {
  return <span className={`pill ${className ?? ""}`.trim()}>{children}</span>;
}
