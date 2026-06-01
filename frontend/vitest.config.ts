import { defineConfig } from "vitest/config";

// Vitest runs component tests in jsdom. JSX is transformed by esbuild using the
// React 18 automatic runtime (no Fast Refresh needed for tests), which avoids
// depending on the (older) @vitejs/plugin-react version used by the dev build.
export default defineConfig({
  test: {
    environment: "jsdom",
    globals: false,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
  esbuild: {
    jsx: "automatic",
  },
});
