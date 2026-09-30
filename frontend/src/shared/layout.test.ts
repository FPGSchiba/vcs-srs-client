import { describe, expect, it } from "vitest";
import { place, GRID_PAD, GRID_GAP } from "./layout";
import { variantById } from "../windows/comms/variants";

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
    const { rects } = place([b(1, "dial-round")], { w: 540, h: 720 });
    const d = variantById("dial-round"); // resolves to the default
    expect(rects[0].w).toBe(d.w);
    expect(rects[0].w).toBeGreaterThan(0);
  });
});
