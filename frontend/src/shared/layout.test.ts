import { describe, expect, it } from "vitest";
import { place, GRID_PAD, GRID_GAP } from "./layout";
import { variantById, DEFAULT_VARIANT_ID } from "../windows/comms/variants";

const b = (radio_id: number, variant: string) => ({ radio_id, variant });

describe("place", () => {
  it("sizes each rect from its variant", () => {
    const { rects } = place([b(1, "vertical")], { w: 540, h: 720 });
    const v = variantById("vertical");
    expect(rects[0]).toMatchObject({ x: 0, y: 0, w: v.w, h: v.h });
  });

  it("flows left to right with GRID_GAP between", () => {
    const { rects } = place([b(1, "narrow-v"), b(2, "narrow-v")], { w: 540, h: 720 });
    const v = variantById("narrow-v");
    expect(rects[0].x).toBe(0);
    expect(rects[1].x).toBe(v.w + GRID_GAP);
    expect(rects[1].y).toBe(0);
  });

  it("wraps when the next block would overflow the content width", () => {
    // Inner width at 540 is 540 - 2*12 = 516. Two verticals (280 each) plus the
    // gap is 568, so the second must wrap.
    const { rects } = place([b(1, "vertical"), b(2, "vertical")], { w: 540, h: 720 });
    expect(rects[1].x).toBe(0);
    expect(rects[1].y).toBe(variantById("vertical").h + GRID_GAP);
  });

  it("reports the content width and total height", () => {
    const out = place([b(1, "vertical")], { w: 540, h: 720 });
    expect(out.w).toBe(540 - 2 * GRID_PAD);
    expect(out.h).toBe(variantById("vertical").h);
  });

  // Review Focus #5.
  it("handles an empty block list without NaN geometry", () => {
    const out = place([], { w: 540, h: 720 });
    expect(out.rects).toEqual([]);
    expect(Number.isFinite(out.w)).toBe(true);
    expect(Number.isFinite(out.h)).toBe(true);
    expect(out.h).toBe(0);
  });

  it("falls back to the default variant for an unknown id rather than a zero-size rect", () => {
    const { rects } = place([b(1, "bogus-id-that-does-not-exist")], { w: 540, h: 720 });
    const d = variantById(DEFAULT_VARIANT_ID);
    expect(rects[0].w).toBe(d.w);
    expect(rects[0].w).toBeGreaterThan(0);
  });

  // Finding 1: Mixed-height row test. horizontal (360×110) and narrow-h (300×68)
  // fit together on row 1 at window width 692 (inner 668). The wrap of block 3
  // must use the taller block (horizontal, h=110), not the last block (narrow-h, h=68).
  it("uses the tallest block height when wrapping a mixed-height row", () => {
    const h_variant = variantById("horizontal"); // 360×110
    const nh_variant = variantById("narrow-h"); // 300×68
    const windowW = h_variant.w + GRID_GAP + nh_variant.w + 2 * GRID_PAD; // 692
    const { rects } = place(
      [b(1, "horizontal"), b(2, "narrow-h"), b(3, "vertical")],
      { w: windowW, h: 720 },
    );
    // Block 3 should wrap below the taller block (horizontal, h=110), not narrow-h (h=68)
    expect(rects[2].y).toBe(h_variant.h + GRID_GAP);
  });

  // Finding 1: Total height after wrap test. Two rows with different heights.
  // Row 1: narrow-v (150×124)
  // Row 2: horizontal (360×110)
  // Total should be 124 + GRID_GAP + 110
  it("computes total height as sum of row heights plus gaps", () => {
    const nv_variant = variantById("narrow-v"); // 150×124
    const h_variant = variantById("horizontal"); // 360×110
    const out = place(
      [b(1, "narrow-v"), b(2, "horizontal")],
      { w: 540, h: 720 },
    );
    const expectedH = nv_variant.h + GRID_GAP + h_variant.h;
    expect(out.h).toBe(expectedH);
  });

  // Finding 1: Exact-fit boundary test. Two narrow-v blocks (150×124 each) with
  // a gap should fit exactly on row 1 when inner = 308. Choose window width
  // such that inner = 150 + 8 + 150 = 308, so win.w = 308 + 2*12 = 332.
  // A window 1 pixel narrower should cause wrap.
  it("does not wrap when a block's right edge lands exactly on inner", () => {
    const nv_variant = variantById("narrow-v"); // 150×124
    const exactInner = nv_variant.w + GRID_GAP + nv_variant.w;
    const windowWExact = exactInner + 2 * GRID_PAD;

    const outExact = place([b(1, "narrow-v"), b(2, "narrow-v")], { w: windowWExact, h: 720 });
    expect(outExact.rects[1].y).toBe(0); // should NOT wrap

    const outTooNarrow = place(
      [b(1, "narrow-v"), b(2, "narrow-v")],
      { w: windowWExact - 1, h: 720 },
    );
    expect(outTooNarrow.rects[1].y).toBeGreaterThan(0); // should wrap
  });

  // Finding 2: Math.max(..., 1) floor test. An empty block list with a narrow
  // window (w: 0) should still return w >= 1 due to the floor.
  it("enforces a minimum inner width of 1 even with a zero-width window", () => {
    const out = place([], { w: 0, h: 0 });
    expect(out.w).toBeGreaterThanOrEqual(1);
  });
});
