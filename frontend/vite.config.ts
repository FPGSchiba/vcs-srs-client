import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from "node:path";

export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      input: {
        // index.html is not a window -- it only redirects to /main.html. It
        // must still be built: Wails' bundled asset server locates the asset
        // root by finding an index.html in the embedded frontend/dist, and
        // without one it answers EVERY request (window pages included) with
        // "no `index.html` could be found". v0.1.0-rc.1 shipped that way.
        // Guarded by main_assets_test.go and scripts/check-dist.mjs.
        index: resolve(__dirname, "index.html"),
        main: resolve(__dirname, "main.html"),
        comms: resolve(__dirname, "comms.html"),
        notifications: resolve(__dirname, "notifications.html"),
      },
    },
  },
});
