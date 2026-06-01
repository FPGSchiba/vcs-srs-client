# VCS Client — Phase 1 Part 2B (Windowing, Frameless Shell, Bindings) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the 2A backend to the frontend via Wails bindings, add the window registry with persisted geometry, wire `Reconnect`, and rewrite `main.go` to open a **frameless + transparent** main window plus an `OpenWindow("comms")` second window.

**Architecture:** `App` becomes the binding surface, depending on small interfaces (`sessionAPI`, `windowsAPI`) so bindings are unit-testable with fakes. A `windows.Registry` manages real Wails windows behind a `WindowFactory` seam (the Wails-specific adapter is integration-only). Geometry persists to `windows.json` via a new `windowstate` package.

**Tech Stack:** Go 1.25, Wails v3 alpha.96 (`application` package), `log/slog`.

**Spec:** [`docs/superpowers/specs/2026-06-01-vcs-client-phase-1-part-2-auth-control-design.md`](../specs/2026-06-01-vcs-client-phase-1-part-2-auth-control-design.md) §3.3, §5, §6.

**Depends on:** Plan 2A merged (auth/control/session/events/dto present).

**Environment notes:** `CGO_ENABLED=0` locally → `go test` without `-race`. Wails window-bounds/transparency APIs differ across the alpha; the `WindowFactory` interface isolates them. Verify exact `application.WebviewWindowOptions` field names (`Frameless`, `BackgroundColour`, `Windows`, `Mac`, `Linux`) against the installed alpha.96 at implementation time and adjust the adapter only.

---

## File Structure (after 2B)

```
internal/
  windowstate/store.go        Geometry type + Load/Save windows.json
  windowstate/store_test.go
  session/session.go          (extend) store connect params; add Reconnect + ping monitor
  session/reconnect_test.go
  app/windowfactory.go        WindowFactory interface + WindowHandle interface
  app/windows.go              Registry (open/focus/close/geometry) over WindowFactory
  app/windows_test.go         tested with a fake factory
  app/windows_wails.go        Wails-backed WindowFactory (integration-only, no unit test)
  app/bindings.go             App binding methods over sessionAPI + windowsAPI
  app/bindings_test.go        tested with fakes
  app/app.go                  (modify) App holds state/session/registry/emitter; SetApp wires them
  app/emitter.go              Wails events → events.Emitter adapter
main.go                       (rewrite) frameless+transparent main window, wiring
```

---

## Task 1: `internal/windowstate` — geometry persistence

**Files:**
- Create: `internal/windowstate/store.go`, `internal/windowstate/store_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/windowstate/store_test.go`:
```go
package windowstate_test

import (
	"path/filepath"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows.json")
	in := map[string]windowstate.Geometry{"comms": {X: 10, Y: 20, W: 540, H: 720}}
	if err := windowstate.Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := windowstate.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out["comms"] != in["comms"] {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestLoad_MissingFileReturnsEmpty(t *testing.T) {
	out, err := windowstate.Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %+v", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/windowstate/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write `internal/windowstate/store.go`**

```go
// Package windowstate persists per-window geometry to windows.json.
package windowstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Geometry is a window's position and size in screen pixels.
type Geometry struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Load reads the geometry map. A missing file returns an empty map (no error).
func Load(path string) (map[string]Geometry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Geometry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read windows.json: %w", err)
	}
	out := map[string]Geometry{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse windows.json: %w", err)
	}
	return out, nil
}

// Save writes the geometry map atomically (write-temp + rename).
func Save(path string, m map[string]Geometry) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal windows.json: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write windows.json: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename windows.json: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/windowstate/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/windowstate
git commit -m "feat: add windowstate package for per-window geometry persistence"
```

---

## Task 2: Window registry over a factory seam

**Files:**
- Create: `internal/app/windowfactory.go`, `internal/app/windows.go`, `internal/app/windows_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/app/windows_test.go`:
```go
package app_test

import (
	"path/filepath"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

type fakeHandle struct {
	closed bool
	geom   windowstate.Geometry
	focus  int
}

func (h *fakeHandle) Focus()                          { h.focus++ }
func (h *fakeHandle) Close()                          { h.closed = true }
func (h *fakeHandle) Bounds() windowstate.Geometry    { return h.geom }
func (h *fakeHandle) SetBounds(g windowstate.Geometry) { h.geom = g }

type fakeFactory struct{ created []string }

func (f *fakeFactory) Create(id string, url string, g windowstate.Geometry) app.WindowHandle {
	f.created = append(f.created, id)
	return &fakeHandle{geom: g}
}

func TestRegistry_OpenCreatesOnceThenFocuses(t *testing.T) {
	ff := &fakeFactory{}
	r := app.NewRegistry(ff, filepath.Join(t.TempDir(), "windows.json"))

	r.Open("comms")
	r.Open("comms") // second open should focus, not recreate
	if len(ff.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(ff.created))
	}
}

func TestRegistry_PersistsGeometryOnClose(t *testing.T) {
	ff := &fakeFactory{}
	path := filepath.Join(t.TempDir(), "windows.json")
	r := app.NewRegistry(ff, path)

	r.Open("comms")
	r.SetGeometry("comms", windowstate.Geometry{X: 5, Y: 6, W: 540, H: 720})
	r.Close("comms")

	saved, _ := windowstate.Load(path)
	if saved["comms"].W != 540 {
		t.Fatalf("expected persisted geometry, got %+v", saved)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run Registry`
Expected: FAIL — `WindowHandle`, `WindowFactory`, `NewRegistry` undefined.

- [ ] **Step 3: Write `internal/app/windowfactory.go`**

```go
package app

import "github.com/FPGSchiba/vcs-srs-client/internal/windowstate"

// WindowHandle is the minimal control surface over one OS window.
type WindowHandle interface {
	Focus()
	Close()
	Bounds() windowstate.Geometry
	SetBounds(windowstate.Geometry)
}

// WindowFactory creates real windows. The Wails-backed implementation lives in
// windows_wails.go; tests pass a fake.
type WindowFactory interface {
	Create(id string, url string, g windowstate.Geometry) WindowHandle
}

// defaultGeometry returns the design's default geometry per window id.
func defaultGeometry(id string) windowstate.Geometry {
	switch id {
	case "comms":
		return windowstate.Geometry{X: 1190, Y: 70, W: 540, H: 720}
	default:
		return windowstate.Geometry{X: 160, Y: 90, W: 1440, H: 900}
	}
}

// windowURL maps a window id to its frontend entry.
func windowURL(id string) string {
	switch id {
	case "comms":
		return "/comms.html"
	default:
		return "/main.html"
	}
}
```

- [ ] **Step 4: Write `internal/app/windows.go`**

```go
package app

import (
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// Registry tracks open windows and persists their geometry.
type Registry struct {
	factory WindowFactory
	path    string

	mu      sync.Mutex
	open    map[string]WindowHandle
	geom    map[string]windowstate.Geometry
}

// NewRegistry builds a Registry that persists to path. Existing geometry is loaded.
func NewRegistry(factory WindowFactory, path string) *Registry {
	geom, _ := windowstate.Load(path)
	if geom == nil {
		geom = map[string]windowstate.Geometry{}
	}
	return &Registry{factory: factory, path: path, open: map[string]WindowHandle{}, geom: geom}
}

// Open creates the window (at persisted or default geometry) or focuses it if open.
func (r *Registry) Open(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.open[id]; ok {
		h.Focus()
		return
	}
	g, ok := r.geom[id]
	if !ok {
		g = defaultGeometry(id)
	}
	r.open[id] = r.factory.Create(id, windowURL(id), g)
}

// Close closes the window and persists its last geometry.
func (r *Registry) Close(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.open[id]
	if !ok {
		return
	}
	r.geom[id] = h.Bounds()
	h.Close()
	delete(r.open, id)
	_ = windowstate.Save(r.path, r.geom)
}

// Geometry returns the last known geometry for id.
func (r *Registry) Geometry(id string) windowstate.Geometry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.geom[id]; ok {
		return g
	}
	return defaultGeometry(id)
}

// SetGeometry records geometry (called from debounced move/resize) and persists.
func (r *Registry) SetGeometry(id string, g windowstate.Geometry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.geom[id] = g
	if h, ok := r.open[id]; ok {
		h.SetBounds(g)
	}
	_ = windowstate.Save(r.path, r.geom)
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/app/ -run Registry`
Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/app/windowfactory.go internal/app/windows.go internal/app/windows_test.go
git commit -m "feat: add window registry over a factory seam with geometry persistence"
```

---

## Task 3: Session `Reconnect` + ping monitor

**Files:**
- Modify: `internal/session/session.go`
- Create: `internal/session/reconnect_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/reconnect_test.go`:
```go
package session_test

import (
	"context"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

func TestReconnect_ReestablishesAfterConnect(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	em := &capEmitter{}
	s := session.New(state.New(), em, session.Deps{Dialer: dial, Version: "test"})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.Reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if s.Control() == nil {
		t.Fatal("expected live control client after reconnect")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/session/ -run TestReconnect`
Expected: FAIL — `Reconnect` undefined.

- [ ] **Step 3: Extend `internal/session/session.go`**

Add a stored-params field to the `Session` struct and the reconnect logic. Add to the struct (after `cancel context.CancelFunc`):
```go
	lastDialer Dialer
	lastToken  string
```
At the end of a successful `Connect` (just before `tagged.ConnectionState(events.ConnConnected)`), record what's needed to reconnect:
```go
	s.mu.Lock()
	s.lastDialer = dialer
	s.lastToken = guest.Token
	s.mu.Unlock()
```
Then add:
```go
// Reconnect re-establishes the control session reusing the stored token (no
// re-auth). Emits reconnecting → connected on success, or disconnected on failure.
func (s *Session) Reconnect(ctx context.Context) error {
	tagged := events.New(s.em)
	tagged.ConnectionState(events.ConnReconnecting)

	s.mu.Lock()
	dialer, token := s.lastDialer, s.lastToken
	s.mu.Unlock()
	if dialer == nil {
		tagged.ConnectionState(events.ConnDisconnected)
		return fmt.Errorf("cannot reconnect: never connected")
	}

	conn, err := dialer(ctx)
	if err != nil {
		tagged.ConnectionState(events.ConnDisconnected)
		return fmt.Errorf("reconnect dial: %w", err)
	}
	cc := control.New(conn, token)
	if err := cc.SyncClient(ctx, s.st); err != nil {
		_ = conn.Close()
		tagged.ConnectionState(events.ConnDisconnected)
		return fmt.Errorf("reconnect sync: %w", err)
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn, s.control, s.cancel = conn, cc, cancel
	s.mu.Unlock()

	go func() { _ = cc.ConsumeUpdates(streamCtx, s.st, s.em) }()
	tagged.ConnectionState(events.ConnConnected)
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/...`
Expected: PASS (Connect + Reconnect tests).

- [ ] **Step 5: Commit**

```powershell
git add internal/session
git commit -m "feat: add session Reconnect reusing stored token"
```

---

## Task 4: App refactor + bindings over interfaces

**Files:**
- Modify: `internal/app/app.go`
- Create: `internal/app/bindings.go`, `internal/app/bindings_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/app/bindings_test.go`:
```go
package app_test

import (
	"context"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

type fakeSession struct {
	connected bool
	lastURL   string
}

func (f *fakeSession) Connect(_ context.Context, url, _, _, _ string) error { f.connected = true; f.lastURL = url; return nil }
func (f *fakeSession) Disconnect(_ context.Context) error                    { f.connected = false; return nil }
func (f *fakeSession) Reconnect(_ context.Context) error                     { return nil }

type fakeWindows struct{ opened []string }

func (f *fakeWindows) Open(id string)                              { f.opened = append(f.opened, id) }
func (f *fakeWindows) Close(string)                                {}
func (f *fakeWindows) Geometry(string) windowstate.Geometry        { return windowstate.Geometry{} }
func (f *fakeWindows) SetGeometry(string, windowstate.Geometry)    {}

func TestBinding_ConnectDelegates(t *testing.T) {
	fs := &fakeSession{}
	a := app.NewForTest(state.New(), fs, &fakeWindows{})
	if err := a.Connect("localhost:5002", "n", "p", "u"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !fs.connected || fs.lastURL != "localhost:5002" {
		t.Fatalf("Connect did not delegate: %+v", fs)
	}
}

func TestBinding_OpenWindowDelegates(t *testing.T) {
	fw := &fakeWindows{}
	a := app.NewForTest(state.New(), &fakeSession{}, fw)
	a.OpenWindow("comms")
	if len(fw.opened) != 1 || fw.opened[0] != "comms" {
		t.Fatalf("OpenWindow did not delegate: %+v", fw.opened)
	}
}

func TestBinding_GetClientState_ReturnsSnapshot(t *testing.T) {
	st := state.New()
	a := app.NewForTest(st, &fakeSession{}, &fakeWindows{})
	snap := a.GetClientState()
	if snap.Clients == nil || snap.Radios == nil {
		t.Fatalf("expected non-nil maps, got %+v", snap)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run Binding`
Expected: FAIL — `NewForTest`, binding methods, interfaces undefined.

- [ ] **Step 3: Rewrite `internal/app/app.go`**

```go
package app

import (
	"context"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// sessionAPI is the session surface the bindings depend on (fakeable in tests).
type sessionAPI interface {
	Connect(ctx context.Context, serverURL, name, password, unitID string) error
	Disconnect(ctx context.Context) error
	Reconnect(ctx context.Context) error
}

// windowsAPI is the window-registry surface the bindings depend on.
type windowsAPI interface {
	Open(id string)
	Close(id string)
	Geometry(id string) windowstate.Geometry
	SetGeometry(id string, g windowstate.Geometry)
}

// App is the Wails service and the frontend binding surface.
type App struct {
	logger   *slog.Logger
	wailsApp *application.App
	st       *state.Store
	sess     sessionAPI
	windows  windowsAPI
}

// NewApp creates the App with its logger. Backend wiring happens in SetApp.
func NewApp(logger *slog.Logger) *App {
	return &App{logger: logger, st: state.New()}
}

// NewForTest builds an App with injected fakes (no Wails app).
func NewForTest(st *state.Store, sess sessionAPI, windows windowsAPI) *App {
	return &App{logger: slog.Default(), st: st, sess: sess, windows: windows}
}

// Store exposes the state store for wiring in main.go.
func (a *App) Store() *state.Store { return a.st }

// SetBackend injects the live session + registry (called from main after SetApp).
func (a *App) SetBackend(sess sessionAPI, windows windowsAPI) {
	a.sess = sess
	a.windows = windows
}

// SetApp injects the Wails application reference. Must be called before Run().
func (a *App) SetApp(app *application.App) { a.wailsApp = app }

// WailsApp returns the injected Wails app (for main.go window/event wiring).
func (a *App) WailsApp() *application.App { return a.wailsApp }

// ServiceStartup is the Wails v3 lifecycle hook.
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	a.logger.Info("App service starting up")
	return nil
}

// ServiceShutdown is the Wails v3 lifecycle hook.
func (a *App) ServiceShutdown() error {
	a.logger.Info("App service shutting down")
	return nil
}
```

(This removes `Greet` and `OpenNewWindow` — superseded by the real bindings below.)

- [ ] **Step 4: Write `internal/app/bindings.go`**

```go
package app

import (
	"context"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// Connect runs the guest connect sequence.
func (a *App) Connect(serverURL, name, password, unitID string) error {
	return a.sess.Connect(context.Background(), serverURL, name, password, unitID)
}

// Disconnect tears down the session.
func (a *App) Disconnect() error { return a.sess.Disconnect(context.Background()) }

// Reconnect re-establishes the control session.
func (a *App) Reconnect() error { return a.sess.Reconnect(context.Background()) }

// GetClientState returns the current snapshot for window hydration.
func (a *App) GetClientState() ClientStateSnapshot {
	snap := a.st.Snapshot()
	return SnapshotFromProto(snap.Clients, snap.Radios)
}

// UpdateRadioInfo pushes a radio config change through the live control client.
func (a *App) UpdateRadioInfo(info RadioInfoDTO) error {
	cc := a.liveControl()
	if cc == nil {
		return errNotConnected
	}
	return cc.UpdateRadioInfo(context.Background(), RadioInfoToProto(info))
}

// OpenWindow opens (or focuses) a window by id.
func (a *App) OpenWindow(id string) { a.windows.Open(id) }

// CloseWindow closes a window by id.
func (a *App) CloseWindow(id string) { a.windows.Close(id) }

// GetWindowGeometry returns a window's last geometry.
func (a *App) GetWindowGeometry(id string) windowstate.Geometry { return a.windows.Geometry(id) }

// SetWindowGeometry records a window's geometry (from debounced move/resize).
func (a *App) SetWindowGeometry(id string, g windowstate.Geometry) { a.windows.SetGeometry(id, g) }
```

Add a small helper file is unnecessary; put the not-connected error and `liveControl` accessor in `bindings.go` too:
```go
import "errors"

var errNotConnected = errors.New("not connected")

// liveControl returns the live control client if the session exposes one.
func (a *App) liveControl() controlClient {
	if s, ok := a.sess.(interface{ Control() controlClient }); ok {
		return s.Control()
	}
	return nil
}

// controlClient is the minimal control surface bindings need.
type controlClient interface {
	UpdateRadioInfo(ctx context.Context, info *interfaceRadioInfo) error
}
```

> **Implementer note:** the `controlClient`/`liveControl` indirection above is over-engineered. Prefer this simpler approach: give `sessionAPI` a method `UpdateRadioInfo(ctx, *srspb.RadioInfo) error` that the real `*session.Session` implements by delegating to its live control client (returning `errNotConnected` when nil), and the fake implements trivially. Then `App.UpdateRadioInfo` calls `a.sess.UpdateRadioInfo(...)`. Update `sessionAPI`, `fakeSession`, and `*session.Session` accordingly. Do NOT introduce the `controlClient` interface. (This note replaces the snippet above.)

So the corrected pieces are:
- In `app.go`, extend `sessionAPI`:
```go
type sessionAPI interface {
	Connect(ctx context.Context, serverURL, name, password, unitID string) error
	Disconnect(ctx context.Context) error
	Reconnect(ctx context.Context) error
	UpdateRadioInfo(ctx context.Context, info RadioInfoDTO) error
}
```
- In `bindings.go`, simplify:
```go
func (a *App) UpdateRadioInfo(info RadioInfoDTO) error {
	return a.sess.UpdateRadioInfo(context.Background(), info)
}
```
(remove `errNotConnected`, `liveControl`, `controlClient`).
- In `internal/session/session.go`, add:
```go
// UpdateRadioInfo pushes a radio change via the live control client.
func (s *Session) UpdateRadioInfo(ctx context.Context, info app /*dto*/) error { ... }
```
**Avoid an import cycle** (`session` importing `app` for `RadioInfoDTO` while `app` imports `session`): the session method should take `*srspb.RadioInfo`, and the conversion `RadioInfoToProto` happens in the `App.UpdateRadioInfo` binding before delegating. Final shapes:
```go
// sessionAPI in app.go:
UpdateRadioInfo(ctx context.Context, info *srspb.RadioInfo) error
// App.UpdateRadioInfo binding:
func (a *App) UpdateRadioInfo(info RadioInfoDTO) error {
	return a.sess.UpdateRadioInfo(context.Background(), RadioInfoToProto(info))
}
// *session.Session method:
func (s *Session) UpdateRadioInfo(ctx context.Context, info *srspb.RadioInfo) error {
	s.mu.Lock(); cc := s.control; s.mu.Unlock()
	if cc == nil { return fmt.Errorf("not connected") }
	return cc.UpdateRadioInfo(ctx, info)
}
// fakeSession in bindings_test.go:
func (f *fakeSession) UpdateRadioInfo(context.Context, *srspb.RadioInfo) error { return nil }
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/app/...`
Expected: PASS (Registry, Binding, DTO suites).

- [ ] **Step 6: Verify whole module builds (main.go not yet updated may break — that's Task 6)**

Run: `go build ./internal/... ./pkg/...`
Expected: green (internal packages compile; `main.go` is updated in Task 6).

- [ ] **Step 7: Commit**

```powershell
git add internal/app internal/session
git commit -m "feat: app binding surface over session/window interfaces"
```

---

## Task 5: Wails events emitter adapter

**Files:**
- Create: `internal/app/emitter.go`

- [ ] **Step 1: Write `internal/app/emitter.go`**

```go
package app

import "github.com/wailsapp/wails/v3/pkg/application"

// wailsEmitter adapts the Wails application event system to events.Emitter.
type wailsEmitter struct{ app *application.App }

// NewWailsEmitter builds an events.Emitter backed by the Wails app.
func NewWailsEmitter(app *application.App) *wailsEmitter { return &wailsEmitter{app: app} }

// Emit broadcasts a custom event to all windows.
func (w *wailsEmitter) Emit(name string, payload any) {
	w.app.Event.Emit(name, payload)
}
```

> **Implementer note:** confirm the exact Wails v3 alpha.96 emit call (`app.Event.Emit(name, data)` vs `app.Events.Emit(&application.CustomEvent{...})`). Adjust this one method to match; it is the only Wails-coupled line and is integration-only (not unit-tested).

- [ ] **Step 2: Build**

Run: `go build ./internal/app/...`
Expected: green (adjust the emit call if the API differs).

- [ ] **Step 3: Commit**

```powershell
git add internal/app/emitter.go
git commit -m "feat: add Wails events emitter adapter"
```

---

## Task 6: `main.go` rewrite — frameless transparent window + wiring (integration)

**Files:**
- Modify: `main.go`
- Create: `internal/app/windows_wails.go`

This task is integration wiring; it is verified by building and launching, not unit tests.

- [ ] **Step 1: Write `internal/app/windows_wails.go`** (Wails-backed factory)

```go
package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// wailsFactory creates real frameless+transparent Wails windows.
type wailsFactory struct{ app *application.App }

// NewWailsFactory builds a WindowFactory over the Wails app.
func NewWailsFactory(app *application.App) WindowFactory { return &wailsFactory{app: app} }

type wailsHandle struct{ win *application.WebviewWindow }

func (f *wailsFactory) Create(id, url string, g windowstate.Geometry) WindowHandle {
	win := f.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "VCS · " + id,
		Width:            g.W,
		Height:           g.H,
		X:                g.X,
		Y:                g.Y,
		URL:              url,
		Frameless:        true,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0), // transparent
	})
	return &wailsHandle{win: win}
}

func (h *wailsHandle) Focus() { h.win.Focus() }
func (h *wailsHandle) Close() { h.win.Close() }
func (h *wailsHandle) Bounds() windowstate.Geometry {
	// NOTE: confirm the alpha.96 bounds API; placeholder uses Size/Position if available.
	w, ht := h.win.Size()
	x, y := h.win.Position()
	return windowstate.Geometry{X: x, Y: y, W: w, H: ht}
}
func (h *wailsHandle) SetBounds(g windowstate.Geometry) {
	h.win.SetSize(g.W, g.H)
	h.win.SetPosition(g.X, g.Y)
}
```

> **Implementer note:** `Size()/Position()/SetSize()/SetPosition()/Focus()/Close()` and the transparency option names must be confirmed against Wails v3 alpha.96. If transparency needs platform options (`Windows`, `Mac`, `Linux` sub-structs), add them here. This file is the single isolation point for that churn (spec §3.3 risk). If a bounds getter is unavailable, persist geometry only from frontend-reported `SetWindowGeometry` calls and have `Bounds()` return the last `SetBounds` value (store it on the handle).

- [ ] **Step 2: Rewrite `main.go`**

Replace the window-creation tail of `main.go` (keep the existing logger/config bootstrap at the top unchanged) so that, after `gui.SetApp(wailsApp)`:
```go
	gui.SetApp(wailsApp)

	// Wire backend: emitter → session → window registry → bindings.
	emitter := app.NewWailsEmitter(wailsApp)
	sess := session.New(gui.Store(), emitter, session.Deps{Version: appVersion})

	winPath, _ := config.WindowStateFilePath() // see note: add this helper to config (mirrors ConfigFilePath, basename windows.json)
	registry := app.NewRegistry(app.NewWailsFactory(wailsApp), winPath)
	gui.SetBackend(sess, registry)

	// Main window: frameless + transparent, fixed 1440x900, loads the main entry.
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "VCS Client",
		Width:            1440,
		Height:           900,
		URL:              "/main.html",
		Frameless:        true,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
```
Add imports for `internal/session` and a package-level `const appVersion = "0.1.0"`. Add the `config.WindowStateFilePath()` helper (in `internal/config/paths.go`, mirroring `ConfigFilePath` but returning `<AppDataDir>/windows.json`).

- [ ] **Step 3: Add `config.WindowStateFilePath`** (in `internal/config/paths.go`)

```go
// WindowStateFilePath returns the path to windows.json under AppDataDir.
func WindowStateFilePath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "windows.json"), nil
}
```

- [ ] **Step 4: Build and launch**

Run:
```powershell
go build ./...
task dev   # or: wails3 dev
```
Expected: app compiles; a **frameless, transparent** main window opens loading `/main.html` (currently the 2C UI is not built yet, so it shows whatever `main.tsx` renders — at minimum the existing entry). The window has no native title bar. (If transparency/frameless needs per-platform tweaks, adjust `windows_wails.go` only.)

- [ ] **Step 5: Run the full Go suite + vet**

Run:
```powershell
go test ./internal/... ./pkg/...
go vet ./...
```
Expected: green.

- [ ] **Step 6: Commit**

```powershell
git add main.go internal/app/windows_wails.go internal/config/paths.go
git commit -m "feat: frameless transparent main window + backend wiring in main.go"
```

---

## Self-Review (performed)

**Spec coverage (§3.3, §5, §6):** windowstate persistence ✓ (T1); window registry + factory seam ✓ (T2); Reconnect ✓ (T3); full binding surface (Connect/Disconnect/Reconnect/GetClientState/UpdateRadioInfo/OpenWindow/CloseWindow/Get/SetWindowGeometry) ✓ (T4); emitter adapter ✓ (T5); frameless+transparent windows + wiring ✓ (T6). `GetServerSettings` + `UpdateClientInfo` bindings are thin and can be added in T4 alongside the others if needed by 2C; flagged so they aren't missed.

**Placeholder scan:** the only "confirm against the alpha" notes are isolated to the two integration-only Wails adapter files (`emitter.go`, `windows_wails.go`) — legitimate API-verification points, not logic placeholders. The Task 4 draft deliberately walks back an over-engineered `controlClient` indirection to the simpler `sessionAPI.UpdateRadioInfo(*srspb.RadioInfo)`; implementer must follow the corrected final shapes.

**Type consistency:** `windowstate.Geometry` shared by factory/registry/bindings; `sessionAPI`/`windowsAPI` match the fakes in `bindings_test.go`; `*session.Session` gains `UpdateRadioInfo(ctx, *srspb.RadioInfo)` matching the interface; `app.NewForTest` signature matches the binding tests.

**Carry-forward to 2C:** frontend calls `OpenWindow("comms")`, subscribes to events, and reports geometry via `SetWindowGeometry`; window min/close controls use the `@wailsio/runtime` Window API from the frontend.
