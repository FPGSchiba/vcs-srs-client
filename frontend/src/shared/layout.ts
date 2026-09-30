import type { LayoutBlock, LayoutWindow } from "./api/client";
import { variantById } from "../windows/comms/variants";

// The Comms flow container's padding and inter-block gap, in window pixels.
// CommsApp renders with exactly these values and `place` assumes them, so the
// Profiles preview and the real grid cannot drift apart.
export const GRID_PAD = 12;
export const GRID_GAP = 8;

export interface Placed {
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * Flows blocks left to right inside the window's content width, wrapping when
 * the next block would overflow. This is the rule the real Comms grid (a CSS
 * flex-wrap container with GRID_PAD padding, GRID_GAP gap, flex-shrink 0 items
 * and align-content flex-start) produces, and the Profiles preview draws with
 * the same function. Pure, so the geometry is testable without rendering.
 *
 * Each block's size comes from its VARIANT, not from stored pixels — see
 * windows/comms/variants.ts. An unknown id resolves to the default rather than
 * producing a zero-size rect.
 */
export function place(
  blocks: LayoutBlock[],
  win: LayoutWindow,
): { rects: Placed[]; w: number; h: number } {
  const sized = blocks.map((b) => {
    const v = variantById(b.variant);
    return { w: v.w, h: v.h };
  });
  const inner = Math.max(win.w - 2 * GRID_PAD, ...sized.map((s) => s.w), 1);
  const rects: Placed[] = [];
  let x = 0;
  let y = 0;
  let rowH = 0;
  for (const s of sized) {
    if (x > 0 && x + s.w > inner) {
      x = 0;
      y += rowH + GRID_GAP;
      rowH = 0;
    }
    rects.push({ x, y, w: s.w, h: s.h });
    x += s.w + GRID_GAP;
    rowH = Math.max(rowH, s.h);
  }
  return { rects, w: inner, h: y + rowH };
}
