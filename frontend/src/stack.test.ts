import { describe, it, expect } from "vitest";
import raw from "../package.json?raw";

const pkg = JSON.parse(raw) as { dependencies?: Record<string, string>; devDependencies?: Record<string, string> };
const REMOVED = ["@tanstack/react-query", "@tanstack/react-router", "zod"];

describe("frontend stack", () => {
  it.each(REMOVED)("does not declare %s", (name) => {
    expect(pkg.dependencies ?? {}).not.toHaveProperty([name]);
    expect(pkg.devDependencies ?? {}).not.toHaveProperty([name]);
  });
});
