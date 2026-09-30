import type { ReactElement, ReactNode } from "react";
import type { RadioVariant } from "./variants";
import { RadioColumn } from "./RadioColumn";
import { RadioRow } from "./RadioRow";

/**
 * What a shell is handed. A shell ARRANGES pieces; it builds none of them, and
 * it reads only `orientation`, `shows` and the paddings those imply. That is
 * what keeps a new SIZE from touching anything outside variants.ts — the
 * pieces already size themselves from the descriptor.
 */
export interface RadioShellProps {
  variant: RadioVariant;
  rid: ReactNode;
  name: ReactNode;
  lcd: ReactNode;
  talker: ReactNode;
  ptt: ReactNode;
  chips: ReactNode;
}

/**
 * One shell per orientation. Adding a new SHAPE is a new shell plus a key
 * here; adding a new SIZE is neither.
 */
export const SHELLS: Record<RadioVariant["orientation"], (p: RadioShellProps) => ReactElement> = {
  column: RadioColumn,
  row: RadioRow,
};

export { RadioColumn, RadioRow };
