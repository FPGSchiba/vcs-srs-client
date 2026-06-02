// Extends Vitest's `expect` with jest-dom matchers (e.g. toBeInTheDocument).
// The `/vitest` entry registers against Vitest's scoped expect, so no globals.
import "@testing-library/jest-dom/vitest";

// With `globals: false`, Testing Library cannot auto-register its afterEach
// cleanup (it looks for a global `afterEach`). Register it explicitly so each
// test's rendered DOM is unmounted between tests and queries stay isolated.
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

afterEach(() => {
  cleanup();
});
