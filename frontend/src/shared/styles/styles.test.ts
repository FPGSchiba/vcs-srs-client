import { describe, it, expect } from "vitest";
import components from "./components.css?raw";
import global from "./global.css?raw";

// global.css is imported for the cases appended by the Tailwind removal (FE12).
void global;

describe("components.css", () => {
  it("defines .popout.native", () => {
    const rule = components.match(/\.popout\.native\s*\{[^}]*position:\s*static[^}]*\}/);
    expect(rule).not.toBeNull();
    for (const decl of [
      /position:\s*static/,
      /width:\s*100%/,
      /height:\s*100%/,
      /border:\s*none/,
      /border-radius:\s*0/,
      /box-shadow:\s*none/,
      /min-width:\s*0/,
      /min-height:\s*0/,
    ]) {
      expect(rule![0]).toMatch(decl);
    }
  });

  it("has no dead .client rule", () => {
    expect(components).not.toMatch(/(^|\n)\.client[\s.{]/);
  });
});
