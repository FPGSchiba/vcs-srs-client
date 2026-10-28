// Postbuild guard: fails `npm run build` if dist/ is missing a page the
// packaged app needs.
//
// Wails v3's bundled asset server finds its root by looking for an index.html
// in the embedded frontend/dist. Without one it answers EVERY request -- the
// window pages included -- with "no `index.html` could be found in your Assets
// fs.FS". v0.1.0-rc.1 shipped exactly that: vite.config.ts built only the
// three window entries. The Go-side regression tests live in
// main_assets_test.go; this script is what checks the REAL build output in the
// CI Frontend job, where the bundle is actually produced.
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const dist = join(dirname(fileURLToPath(import.meta.url)), "..", "dist");
const required = ["index.html", "main.html", "comms.html", "notifications.html"];
const missing = required.filter((f) => !existsSync(join(dist, f)));

if (missing.length > 0) {
  console.error(
    `check-dist: dist/ is missing ${missing.join(", ")}. ` +
      "The Wails asset server refuses to serve anything without dist/index.html; " +
      "every page listed here must be a rollup input in vite.config.ts.",
  );
  process.exit(1);
}
console.log(`check-dist: dist/ contains ${required.join(", ")}`);
