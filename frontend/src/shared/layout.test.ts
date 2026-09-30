import { describe, expect, it } from "vitest";
import { clampBlock, GRID_GAP, GRID_PAD, MIN_BLOCK_H, MIN_BLOCK_W, place } from "./layout";

describe("place", () => {
  it("flows blocks left to right with the grid gap", () => {
    const { rects } = place(
      [
        { radio_id: 1, w: 200, h: 100 },
        { radio_id: 2, w: 200, h: 80 },
      ],
      { w: 2 * GRID_PAD + 500, h: 700 },
    );
    expect(rects[0]).toEqual({ x: 0, y: 0, w: 200, h: 100 });
    expect(rects[1]).toEqual({ x: 200 + GRID_GAP, y: 0, w: 200, h: 80 });
  });

  it("wraps to a new row below the tallest block of the previous row", () => {
    const { rects, h } = place(
      [
        { radio_id: 1, w: 300, h: 100 },
        { radio_id: 2, w: 300, h: 140 },
        { radio_id: 3, w: 300, h: 90 },
      ],
      { w: 2 * GRID_PAD + 620, h: 700 },
    );
    expect(rects[2]).toEqual({ x: 0, y: 140 + GRID_GAP, w: 300, h: 90 });
    expect(h).toBe(140 + GRID_GAP + 90);
  });

  it("wraps exactly when the row plus gap no longer fits", () => {
    // 300 + gap + 300 == 608 fits a 608 row; one pixel narrower must wrap.
    const blocks = [
      { radio_id: 1, w: 300, h: 50 },
      { radio_id: 2, w: 300, h: 50 },
    ];
    expect(place(blocks, { w: 2 * GRID_PAD + 600 + GRID_GAP, h: 0 }).rects[1].y).toBe(0);
    expect(place(blocks, { w: 2 * GRID_PAD + 600 + GRID_GAP - 1, h: 0 }).rects[1].y).toBe(
      50 + GRID_GAP,
    );
  });
});

describe("clampBlock", () => {
  it("enforces the minimums independently and passes larger sizes through", () => {
    expect(clampBlock(10, 500)).toEqual({ w: MIN_BLOCK_W, h: 500 });
    expect(clampBlock(500, 10)).toEqual({ w: 500, h: MIN_BLOCK_H });
    expect(MIN_BLOCK_W).toBe(240);
    expect(MIN_BLOCK_H).toBe(96);
  });
});
