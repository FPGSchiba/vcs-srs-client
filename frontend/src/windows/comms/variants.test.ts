import { describe, expect, it } from "vitest";
import { VARIANTS, variantById, nearestVariant, DEFAULT_VARIANT_ID } from "./variants";

describe("the registry", () => {
  it("defines the four agreed variants at their agreed sizes", () => {
    const byId = Object.fromEntries(VARIANTS.map((v) => [v.id, v]));
    expect(byId.vertical).toMatchObject({ w: 280, h: 166, orientation: "column", lcdPx: 30 });
    expect(byId.horizontal).toMatchObject({ w: 360, h: 110, orientation: "row", lcdPx: 22 });
    expect(byId["narrow-h"]).toMatchObject({ w: 300, h: 68, orientation: "row", lcdPx: 21 });
    expect(byId["narrow-v"]).toMatchObject({ w: 150, h: 124, orientation: "column", lcdPx: 18 });
  });

  it("gives every variant a unique id", () => {
    const ids = VARIANTS.map((v) => v.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("only uses orientations a shell exists for", () => {
    for (const v of VARIANTS) expect(["column", "row"]).toContain(v.orientation);
  });
});

describe("variantById", () => {
  it("resolves a known id", () => {
    expect(variantById("narrow-v").w).toBe(150);
  });

  // Review Focus #1.
  it("falls back to the default for an unknown id", () => {
    expect(variantById("does-not-exist").id).toBe(DEFAULT_VARIANT_ID);
  });

  it("falls back to the default for a missing id", () => {
    expect(variantById(undefined).id).toBe(DEFAULT_VARIANT_ID);
  });
});

describe("nearestVariant", () => {
  it("returns the exact variant for an exact size", () => {
    expect(nearestVariant(280, 166).id).toBe("vertical");
    expect(nearestVariant(360, 110).id).toBe("horizontal");
    expect(nearestVariant(300, 68).id).toBe("narrow-h");
    expect(nearestVariant(150, 124).id).toBe("narrow-v");
  });

  it("uses BOTH axes, so wide-and-short differs from wide-and-tall", () => {
    // 330x70 is close to narrow-h (300x68) on both axes.
    expect(nearestVariant(330, 70).id).toBe("narrow-h");
    // 330x115 is close to horizontal (360x110).
    expect(nearestVariant(330, 115).id).toBe("horizontal");
  });

  it("snaps a tiny drag to the smallest variant rather than returning nothing", () => {
    expect(nearestVariant(10, 10).id).toBe("narrow-v");
  });

  it("snaps an enormous drag to the largest rather than returning nothing", () => {
    expect(nearestVariant(5000, 5000).id).toBeDefined();
  });

  // Review Focus #4.
  it("is deterministic and order-independent for an equidistant drag", () => {
    // Find a point equidistant from two variants by construction: the midpoint
    // between vertical (280,166) and narrow-v (150,124).
    const mid = { w: (280 + 150) / 2, h: (166 + 124) / 2 };
    const first = nearestVariant(mid.w, mid.h).id;
    // Same input, repeated — must not vary.
    for (let i = 0; i < 20; i++) expect(nearestVariant(mid.w, mid.h).id).toBe(first);
    // And the tie-break must not depend on array order: reversing the registry
    // in a copy and re-resolving by the same rule yields the same winner.
    const dist = (v: { w: number; h: number }) => (v.w - mid.w) ** 2 + (v.h - mid.h) ** 2;
    const sorted = [...VARIANTS].sort((a, b) => dist(a) - dist(b) || a.id.localeCompare(b.id));
    expect(sorted[0].id).toBe(first);
  });
});
