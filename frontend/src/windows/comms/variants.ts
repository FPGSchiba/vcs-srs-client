/**
 * The radio-card variant registry.
 *
 * This file is the contract for the whole Comms card design. Adding a new SIZE
 * of an existing shape is an entry here and nothing else — it joins the snap
 * set automatically and the shells render it without change. Adding a new
 * SHAPE is one new shell plus an entry here.
 *
 * `h` is the FRAME's border-box height, so it must cover the content plus the
 * frame's 1px top and bottom border. design/vcs/radio-variants.md §2 built its
 * row tables without that allowance (and its vertical table sums to 167, not
 * 166); the heights here are the measured requirement in Chromium, rounded up:
 * vertical 167.6 -> 168, horizontal 114.6 -> 115, narrow-v 135.4 -> 136,
 * narrow-h needs 56.2 and keeps 68. fit.test.tsx pins this.
 *
 * If adding a size ever requires touching a file other than this one, the
 * abstraction has leaked. See docs/superpowers/specs/2026-09-30-vcs-client-comms-radio-redesign-design.md §3.2
 * and design/vcs/radio-variants.md for the agreed layouts.
 */

export interface RadioVariant {
  id: string;
  /** Shown in the resize affordance. */
  label: string;
  w: number;
  h: number;
  orientation: "column" | "row";
  /**
   * The layout-density decision shells branch on. Deliberately independent of
   * `lcdPx`: that sizes the pieces, this picks
   * the arrangement, so a new size never forces a shell edit.
   */
  density: "comfortable" | "compact";
  /** Frequency digit size in px. */
  lcdPx: number;
  /** "fill" means the PTT spans the card's content width. */
  ptt: { w: number | "fill"; h: number };
  shows: {
    /** The "MHZ" suffix after the digits. */
    unit: boolean;
    /** The border-top above the status row. */
    statusDivider: boolean;
    /** The variant has room for the PTT's text as well as its icon. */
    pttLabel: boolean;
  };
}

export const DEFAULT_VARIANT_ID = "vertical";

export const VARIANTS: readonly RadioVariant[] = [
  {
    id: "vertical",
    label: "Vertical",
    w: 280,
    h: 168,
    orientation: "column",
    density: "comfortable",
    lcdPx: 30,
    ptt: { w: 96, h: 28 },
    shows: { unit: true, statusDivider: true, pttLabel: true },
  },
  {
    id: "horizontal",
    label: "Horizontal",
    w: 360,
    h: 115,
    orientation: "row",
    density: "comfortable",
    lcdPx: 22,
    ptt: { w: 64, h: 64 },
    shows: { unit: true, statusDivider: false, pttLabel: false },
  },
  {
    id: "narrow-h",
    label: "Narrow (wide)",
    w: 300,
    h: 68,
    orientation: "row",
    density: "compact",
    lcdPx: 21,
    ptt: { w: 32, h: 30 },
    shows: { unit: false, statusDivider: false, pttLabel: false },
  },
  {
    id: "narrow-v",
    label: "Narrow (tall)",
    w: 150,
    h: 136,
    orientation: "column",
    density: "compact",
    lcdPx: 18,
    ptt: { w: "fill", h: 26 },
    shows: { unit: false, statusDivider: false, pttLabel: true },
  },
];

const DEFAULT = VARIANTS.find((v) => v.id === DEFAULT_VARIANT_ID) as RadioVariant;

/**
 * Resolves a stored variant id.
 *
 * An unknown or missing id falls back to the default rather than throwing: a
 * hand-edited profile, or one written before a descriptor was renamed, must
 * still render. This is a default for a missing field, not a migration —
 * the {w,h} block format never left local development.
 */
export function variantById(id: string | undefined): RadioVariant {
  if (!id) return DEFAULT;
  return VARIANTS.find((v) => v.id === id) ?? DEFAULT;
}

/**
 * Resolves a dragged size to the nearest variant by Euclidean distance in
 * (w, h), so both axes count: dragging wide-and-short lands on a narrow
 * strip while wide-and-tall lands on the horizontal card.
 *
 * Ties break on id, NOT on array order, so reordering the registry can never
 * silently change which variant a given drag produces.
 */
export function nearestVariant(w: number, h: number): RadioVariant {
  const dist = (v: RadioVariant) => (v.w - w) ** 2 + (v.h - h) ** 2;
  return [...VARIANTS].sort((a, b) => dist(a) - dist(b) || a.id.localeCompare(b.id))[0];
}
