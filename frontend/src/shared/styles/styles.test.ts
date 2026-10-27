import { describe, it, expect } from "vitest";
import components from "./components.css?raw";
import global from "./global.css?raw";

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

  it("defines .sr-only", () => {
    expect(components).toMatch(/\.sr-only\s*\{[^}]*position:\s*absolute[^}]*clip-path:\s*inset\(50%\)[^}]*\}/);
  });

  // Tailwind's own `@keyframes pulse` (50% -> 0.5) used to override this one;
  // with Tailwind gone the design's 0.4 applies (user decision D1). This
  // guards against a second keyframe of the same name creeping back in.
  it("keeps the design's pulse keyframe (D1)", () => {
    expect(components.match(/@keyframes pulse/g)).toHaveLength(1);
    const block = components.slice(components.indexOf("@keyframes pulse"));
    expect(block).toMatch(/50%\s*\{\s*opacity:\s*0\.4;?\s*\}/);
    expect(global).not.toMatch(/@keyframes pulse/);
  });
});

describe("global.css", () => {
  it("has no Tailwind directives", () => {
    expect(global).not.toMatch(/@import\s+["']tailwindcss["']/);
    expect(global).not.toMatch(/@config/);
  });

  it("ports the relied-on preflight inside @layer base", () => {
    expect(global).toContain("@layer base");
    const base = global.slice(global.indexOf("@layer base"));
    for (const re of [
      /::backdrop[^{]*\{[^}]*box-sizing:\s*border-box/,
      /svg[^{]*\{[^}]*display:\s*block/,
      /button,\s*input,\s*select[^{]*\{[^}]*font:\s*inherit/,
      /::placeholder\s*\{[^}]*opacity:\s*1/,
      /table\s*\{[^}]*border-collapse:\s*collapse/,
      /ul,\s*menu\s*\{[^}]*list-style:\s*none|ol,\s*ul,\s*menu\s*\{[^}]*list-style:\s*none/,
      /b,\s*strong\s*\{[^}]*font-weight:\s*bolder/,
      /-webkit-inner-spin-button[^{]*\{[^}]*height:\s*auto/,
    ]) {
      expect(base).toMatch(re);
    }
  });
});
