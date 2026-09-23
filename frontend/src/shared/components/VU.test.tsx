import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { VU } from "./VU";

describe("VU", () => {
  it("renders the requested number of segments", () => {
    const { container } = render(<VU level={0.5} segs={16} />);
    expect(container.querySelectorAll("[data-vu-seg]")).toHaveLength(16);
  });

  it("defaults to 16 segments when segs is omitted", () => {
    const { container } = render(<VU level={0.5} />);
    expect(container.querySelectorAll("[data-vu-seg]")).toHaveLength(16);
  });

  it("lights a proportion of segments matching the level", () => {
    const { container } = render(<VU level={0.5} segs={16} />);
    const lit = container.querySelectorAll("[data-vu-seg][data-lit='true']");
    expect(lit.length).toBeGreaterThan(6);
    expect(lit.length).toBeLessThan(10);
  });

  it("lights more segments as the level rises", () => {
    const low = render(<VU level={0.2} segs={16} />);
    const high = render(<VU level={0.8} segs={16} />);
    const litLow = low.container.querySelectorAll(
      "[data-vu-seg][data-lit='true']",
    ).length;
    const litHigh = high.container.querySelectorAll(
      "[data-vu-seg][data-lit='true']",
    ).length;
    expect(litHigh).toBeGreaterThan(litLow);
  });

  it("lights nothing at level 0", () => {
    const { container } = render(<VU level={0} segs={16} />);
    expect(
      container.querySelectorAll("[data-vu-seg][data-lit='true']"),
    ).toHaveLength(0);
  });

  it("lights every segment at the top of the range (boundary, not just overflow)", () => {
    const { container } = render(<VU level={1} segs={16} />);
    expect(
      container.querySelectorAll("[data-vu-seg][data-lit='true']"),
    ).toHaveLength(16);
  });

  it("clamps out-of-range levels instead of overflowing", () => {
    const { container } = render(<VU level={5} segs={16} />);
    expect(
      container.querySelectorAll("[data-vu-seg][data-lit='true']"),
    ).toHaveLength(16);
  });

  it("clamps negative levels instead of going negative", () => {
    const { container } = render(<VU level={-1} segs={16} />);
    expect(
      container.querySelectorAll("[data-vu-seg][data-lit='true']"),
    ).toHaveLength(0);
  });
});
