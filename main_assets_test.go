package main

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Regression guard for v0.1.0-rc.1, which shipped with every window showing
// only this text instead of the UI.
//
// Wails v3's bundled asset server (internal/assetserver.newAssetFileServerFS)
// locates the asset root by searching the embedded fs.FS for an index.html.
// If there is none, EVERY request -- /main.html included -- answers with this
// error, even though main.html, comms.html and notifications.html are sitting
// right there in the bundle. The Vite config built only the three window
// entries, so frontend/dist never contained an index.html.
const noIndexHTMLError = "no `index.html` could be found"

// windowPages are the URLs the app's windows open: main.go creates the main
// window at /main.html, internal/app/windowfactory.go maps the popouts to
// /comms.html and /notifications.html. TestWindowPagesMatchSources keeps this
// list honest.
var windowPages = []string{"/main.html", "/comms.html", "/notifications.html"}

func TestWindowPagesMatchSources(t *testing.T) {
	var src strings.Builder
	for _, f := range []string{"main.go", "internal/app/windowfactory.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src.Write(b)
	}
	for _, p := range windowPages {
		if !strings.Contains(src.String(), `"`+p+`"`) {
			t.Errorf("window page %s no longer appears in main.go or windowfactory.go -- update windowPages", p)
		}
	}
}

// assertServesWindowPages serves vfs through the same public constructor
// main.go uses and requires every window page to come back as real HTML.
func assertServesWindowPages(t *testing.T, vfs fs.FS) {
	t.Helper()
	srv := application.BundledAssetFileServer(vfs)
	for _, p := range windowPages {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		body, _ := io.ReadAll(rec.Result().Body)
		if strings.Contains(string(body), noIndexHTMLError) {
			t.Errorf("GET %s: asset server rejected the whole bundle: %s", p, strings.TrimSpace(string(body)))
			continue
		}
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200 (body %q)", p, rec.Code, body)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s: Content-Type %q, want text/html", p, ct)
		}
	}
}

// viteInputRe matches each `resolve(__dirname, "x.html")` rollup input in
// frontend/vite.config.ts.
var viteInputRe = regexp.MustCompile(`resolve\(\s*__dirname\s*,\s*["']([^"']+\.html)["']\s*\)`)

// TestViteBundleShapeIsServable is the half of this guard that is meaningful
// in the CI Backend job. That job embeds a one-file placeholder dist (the real
// bundle is built by the Frontend job), so the embedded assets cannot show the
// production shape there. Instead this test derives the shape from the Vite
// config itself -- one dist/<name>.html per rollup input, under the same
// frontend/dist prefix main.go embeds -- and serves it through Wails.
func TestViteBundleShapeIsServable(t *testing.T) {
	cfg, err := os.ReadFile("frontend/vite.config.ts")
	if err != nil {
		t.Fatalf("read vite config: %v", err)
	}
	matches := viteInputRe.FindAllStringSubmatch(string(cfg), -1)
	if len(matches) == 0 {
		t.Fatal("found no `resolve(__dirname, \"*.html\")` rollup inputs in frontend/vite.config.ts -- " +
			"this test's parser needs updating to match the config")
	}
	bundle := fstest.MapFS{
		// Mirrors the hashed chunks Vite emits next to the pages.
		"frontend/dist/assets/main-0000.js": {Data: []byte("export {}")},
	}
	for _, m := range matches {
		bundle["frontend/dist/"+m[1]] = &fstest.MapFile{
			Data: []byte("<!doctype html><html><head><title>" + m[1] + "</title></head><body></body></html>"),
		}
	}
	assertServesWindowPages(t, bundle)
}

// TestEmbeddedAssetsServeWindowPages runs the same check against the REAL
// embedded bundle (the `assets` var from main.go), with the embed path
// main.go uses. After `npm run build` this is the full end-to-end regression
// test. In the CI Backend job frontend/dist is a placeholder holding only
// index.html; there it still proves the embed is not rejected wholesale, and
// the production shape is covered by TestViteBundleShapeIsServable plus the
// Frontend job's postbuild check (frontend/scripts/check-dist.mjs).
func TestEmbeddedAssetsServeWindowPages(t *testing.T) {
	if _, err := fs.Stat(assets, "frontend/dist/main.html"); err != nil {
		t.Log("frontend/dist is a placeholder (no main.html) -- checking only that the embed is not rejected")
		srv := application.BundledAssetFileServer(assets)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/main.html", nil))
		if body := rec.Body.String(); strings.Contains(body, noIndexHTMLError) {
			t.Fatalf("asset server rejected the embedded bundle: %s", strings.TrimSpace(body))
		}
		return
	}
	assertServesWindowPages(t, assets)
}
