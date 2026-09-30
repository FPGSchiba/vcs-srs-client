import type { LayoutBlock, LayoutWindow } from "./api/client";

// Mirror profile.MinBlockW / profile.MinBlockH in internal/profile/profile.go.
// The Go side clamps too (SetCommsLayout), but the UI must not send garbage:
// a zero-size block renders as nothing and cannot be grabbed to resize it back.
export const MIN_BLOCK_W = 240;
export const MIN_BLOCK_H = 96;

// Mirror profile.DefaultBlockW / profile.DefaultBlockH. Used only for a radio
// that has no stored block yet.
export const DEFAULT_BLOCK_W = 516;
export const DEFAULT_BLOCK_H = 180;

// The Comms flow container's padding and inter-block gap, in window pixels.
// CommsApp renders with exactly these values, and `place` assumes them, so the
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
 * Flows blocks left to right inside the window's content width, wrapping to a
 * new line when the next block would overflow. This is the rule the real Comms
 * grid (a CSS flex-wrap container with GRID_PAD padding, GRID_GAP gap,
 * flex-shrink 0 items and align-content flex-start) produces, and the Profiles
 * preview draws with the same function. Pure so the geometry is testable
 * without rendering.
 */
export function place(
  blocks: LayoutBlock[],
  win: LayoutWindow,
): { rects: Placed[]; w: number; h: number } {
  const inner = Math.max(win.w - 2 * GRID_PAD, ...blocks.map((b) => b.w), 1);
  const rects: Placed[] = [];
  let x = 0;
  let y = 0;
  let rowH = 0;
  for (const b of blocks) {
    if (x > 0 && x + b.w > inner) {
      x = 0;
      y += rowH + GRID_GAP;
      rowH = 0;
    }
    rects.push({ x, y, w: b.w, h: b.h });
    x += b.w + GRID_GAP;
    rowH = Math.max(rowH, b.h);
  }
  return { rects, w: inner, h: y + rowH };
}

export function clampBlock(w: number, h: number): { w: number; h: number } {
  return { w: Math.max(MIN_BLOCK_W, Math.round(w)), h: Math.max(MIN_BLOCK_H, Math.round(h)) };
}
