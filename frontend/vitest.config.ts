import { defineConfig } from "vitest/config";

// Vitest runs component tests in jsdom. JSX is transformed by Vitest's bundled
// transformer (oxc) using the React automatic runtime, picked up from the
// tsconfig "jsx": "react-jsx" setting — no extra plugin needed for tests.
export default defineConfig({
  test: {
    environment: "jsdom",
    globals: false,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
