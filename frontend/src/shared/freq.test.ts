import { describe, expect, it } from "vitest";
import { MAX_KHZ, clampKhz, digitsOf, khzToMhz, mhzToKhz, stepDigit } from "./freq";

const render = (khz: number) => digitsOf(khz).map((c) => c.char).join("");

describe("digitsOf", () => {
  it("formats a typical frequency with a three-digit MHz part", () => {
    expect(render(118_500)).toBe("118.500");
  });

  it("pads the MHz part so digit positions never shift", () => {
    expect(render(5_000)).toBe("005.000");
    expect(render(0)).toBe("000.000");
  });

  it("grows past three MHz digits rather than truncating", () => {
    expect(render(MAX_KHZ)).toBe("16777.215");
  });

  it("tags each digit with its kHz decade and the separator with null", () => {
    const cells = digitsOf(118_500);
    expect(cells.map((c) => c.place)).toEqual([5, 4, 3, null, 2, 1, 0]);
  });
});

describe("stepDigit", () => {
  it("changes only the targeted decade", () => {
    expect(stepDigit(118_500, 2, 1)).toBe(118_600); // 100 kHz up
    expect(stepDigit(118_500, 3, -1)).toBe(117_500); // 1 MHz down
    expect(stepDigit(118_500, 0, 1)).toBe(118_501); // 1 kHz up
  });

  it("carries into the decade to its left, as the design specifies", () => {
    expect(render(stepDigit(118_509, 0, 1))).toBe("118.510");
    expect(render(stepDigit(118_900, 2, 1))).toBe("119.000");
  });

  it("borrows downward the same way", () => {
    expect(render(stepDigit(119_000, 2, -1))).toBe("118.900");
  });

  // Review Focus #2: the ceiling must not silently rewrite untouched digits.
  it("refuses a step past the 24-bit ceiling instead of clamping", () => {
    expect(stepDigit(MAX_KHZ, 0, 1)).toBe(MAX_KHZ);
    // A step that would exceed (leave) the range is refused, not clamped.
    // Test with a value that truly exceeds when stepped by 100 MHz.
    expect(stepDigit(16_777_215, 5, 1)).toBe(MAX_KHZ);  // MAX + 100 MHz stays MAX (refused)
  });

  it("refuses a step below zero", () => {
    expect(stepDigit(0, 0, -1)).toBe(0);
    expect(stepDigit(500, 3, -1)).toBe(500);
  });

  it("allows a step that lands exactly on a bound", () => {
    expect(stepDigit(MAX_KHZ - 1, 0, 1)).toBe(MAX_KHZ);
    expect(stepDigit(1, 0, -1)).toBe(0);
  });

  it("allows a place-5 (100 MHz) step to land exactly on the ceiling", () => {
    // place 5 means 10^5 = 100_000 kHz = 100 MHz
    // 16_677_215 + 100_000 = 16_777_215 (MAX_KHZ)
    // This proves large steps (place > 3) may reach MAX_KHZ, so the
    // refusal logic cannot mistakenly forbid landing on a bound.
    expect(stepDigit(16_677_215, 5, 1)).toBe(MAX_KHZ);
  });

  it("refuses a step that would exceed the 24-bit ceiling, keeping lower digits untouched", () => {
    // Review Focus #2: distinguish refuse from clamp.
    // place 5 = 100 MHz = 100_000 kHz
    // 16_700_000 + 100_000 = 16_800_000, which exceeds MAX_KHZ (16_777_215).
    // Refuse returns the original value with lower digits intact (00_000).
    // Clamp would rewrite those digits to 77_215 (from the clamped MAX value).
    // This test fails if stepDigit clamps instead of refusing.
    expect(stepDigit(16_700_000, 5, 1)).toBe(16_700_000);
  });
});

describe("clampKhz", () => {
  it("bounds both ends", () => {
    expect(clampKhz(-5)).toBe(0);
    expect(clampKhz(MAX_KHZ + 5)).toBe(MAX_KHZ);
    expect(clampKhz(118_500)).toBe(118_500);
  });
});

describe("the MHz edge conversions", () => {
  it("khzToMhz mirrors the Go KHz.MHz32 expression exactly", () => {
    // internal/voice/freq.go: float32(uint32(k)) / 1000.0
    expect(khzToMhz(118_500)).toBe(Math.fround(118_500 / 1000));
  });

  it("round-trips the frequencies real radios use", () => {
    for (const k of [0, 1, 118_500, 122_750, 251_000, 999_999, 1_000_000]) {
      expect(mhzToKhz(khzToMhz(k))).toBe(k);
    }
  });
});
