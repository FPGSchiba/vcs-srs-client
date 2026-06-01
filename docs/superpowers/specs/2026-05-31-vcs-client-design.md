# VCS Client — Design

**Status:** Approved (brainstorm phase)
**Date:** 2026-05-31
**Author:** FPG Schiba (with Claude Code)
**Project:** `github.com/FPGSchiba/vcs-srs-client`
**Companion docs:** [`docs/ROADMAP.md`](../../ROADMAP.md), [`docs/PROTO_GAPS.md`](../../PROTO_GAPS.md)

---

## 1. Overview

The Vanguard Communications System (VCS) client is a desktop voice-comms application for the Star Citizen community, using the SRS-style radio paradigm. It is multi-window, distributed-voice-aware, and cross-platform (Windows / macOS / Linux). One Go process hosts the gRPC control client, custom UDP voice client, audio I/O, global hotkeys, and persistent state; it spawns N Wails v3 OS-level windows that share singleton Go services through typed bindings and events.

This document covers the cross-cutting architecture and **Phase 1** in implementable detail. Phases 2 – 10 are roadmapped here and tracked in `docs/ROADMAP.md`; each later phase gets its own spec when picked up.

## 2. Sources of truth

| Source | Path / URL | Authoritative for |
|---|---|---|
| Wire contract | `srs.proto` (repo root) | Auth + control gRPC surface |
| Design package | `design/vcs/` (in-repo) | UI screens, tokens, window geometries, naming |
| Design tokens block | `design/vcs/project/styles.css` lines 6 – 61 | Colors, type, sizing, layout vars |
| Voice protocol | (server-side Go reference — **awaiting from user**) | UDP framing, codec, multiplex, secret |

## 3. Decided technology stack

### Frontend
- **Wails v3** (already migrated by user; brief originally said v3)
- TypeScript, **React 18**, **Vite**, **Tailwind CSS** + CSS-variable tokens
- **shadcn/ui** primitives, restyled with VCS tokens
- **Zustand** for per-window UI state
- **TanStack Query** for one-shot gRPC calls only (cache-friendly)
- **TanStack Router** for in-window navigation in the main window
- Fonts: Space Grotesk + JetBrains Mono, bundled locally

### Backend (Go)
- Wails v3 scaffold, module `github.com/FPGSchiba/vcs-srs-client`
- `google.golang.org/grpc` with bindings generated from `srs.proto`
- `log/slog` (replaces zap in the scaffold) with `lumberjack` rotation
- `net/http` for the Operations REST endpoint (Phase 8)
- `github.com/gen2brain/malgo` for audio I/O (Phase 4)
- `golang.design/x/hotkey` for global hotkeys (Phase 7) — alt evaluated then
- Custom UDP voice client (Phase 5; protocol blocked on server reference)
- `minio/selfupdate` for GitHub Releases auto-update (Phase 10)

### Build & distribution
- GitHub Actions matrix: Windows, macOS, Linux
- Versioning from git tags
- Code-signing scripts staged but not enforced for v1
- Auto-update prompted on startup, non-blocking

### Open infrastructure choices (locked)
| Decision | Value |
|---|---|
| Go module name | `github.com/FPGSchiba/vcs-srs-client` |
| Logger | `log/slog` |
| Spec depth | Phase 1 detail + roadmap stubs |
| gRPC transport | **Insecure first**, TLS in a later phase. Insecure transport is allowed only when the host is literal `127.0.0.1` / `localhost`; remote hosts fail closed until TLS lands |
| Multi-window React | Per-window Vite entries (multiple bundles) |
| Guest login | Server resolves coalition from password; client form is `name + password + unit_id` |
| Generated dirs | Gitignored; CI re-generates on every build |
| Operations source (Phase 8) | REST API endpoint over HTTP |

---

## 4. Architecture

### 4.1 Process model

One Go process. Wails v3 `Application` owns N OS-level `Window` instances. Voice (UDP), audio (malgo), global hotkeys, gRPC streaming, and all persisted state live in the Go side. Each window is a thin React surface that calls into Go via Wails generated bindings and listens to typed Wails events.

```
                         +--------------------------+
                         |   Wails v3 Application   |
                         +------------+-------------+
              +--------------+--------+--------+--------------+
              v              v        v        v              v
        Main Window      Comms     Ship    Messages     Notifications  Fleet
        1440x900        540x720  1120x760  980x700       460x600     1300x880
              |              |        |        |              |
              +-- share --+-- singleton Go services --+----- events
                          |
        +-----------------+-----------------------+
        |  state, auth, control(gRPC),            |
        |  voice(UDP), audio, hotkeys, profiles,  |
        |  config, events, updater, logger(slog)  |
        +-----------------+-----------------------+
                          |
              +-----------+-----------+
              v                       v
       SRS Control server       Voice host(s)
       (gRPC over TCP)          (custom UDP - Phase 5)
```

### 4.2 State sync

Go is the single source of truth. Each window's React app has its own Zustand store; the store hydrates by calling a Go binding (`api.GetClientState`) on mount and then subscribes to scoped Wails events. Frontend never broadcasts to other windows — it writes through Go.

- **TanStack Query** wraps one-shot gRPC unary calls (`SyncClient`, `GetServerSettings`, etc.) with stale-while-revalidate semantics.
- **Streaming** (`SubscribeToUpdates`) and per-frame events bypass Query and feed Zustand directly via Wails events. This keeps streaming traffic out of Query's cache and avoids fighting it.

### 4.3 Persistence

| Kind | Location | Format |
|---|---|---|
| App config (server URL, audio defaults, last login) | `%APPDATA%/VCS/config.toml` (+ OS equivalents) | TOML |
| Keybinds | `%APPDATA%/VCS/keybinds.toml` | TOML |
| Ship & role catalog | `%APPDATA%/VCS/ships.toml`, `roles.toml` | TOML |
| Radio profiles | user-chosen dir; default `%APPDATA%/VCS/profiles/` | JSON |
| Window geometry per window | `%APPDATA%/VCS/windows.json` | JSON |
| Token from last successful login | `%APPDATA%/VCS/session.json` (chmod 0600 on Unix) | JSON |
| Ephemeral runtime state | in-memory only | n/a |

Token storage is not OS-keychain-protected in v1; this is a known security gap (R4) tracked in Risks.

### 4.4 Frontend build

Per-window Vite entries, one bundle each:

```
frontend/
  index.html               Wails dev shim only; never user-facing
  src/
    main.tsx               built to dist/main.html
    comms.tsx              built to dist/comms.html
    ship.tsx               built to dist/ship.html
    fleet.tsx              built to dist/fleet.html
    messages.tsx           built to dist/messages.html
    notifications.tsx      built to dist/notifications.html
    shared/                tokens, components, stores, api
```

`vite.config.ts` uses `build.rollupOptions.input = { main, comms, ship, ... }`. Go's `Application.NewWindow({ URL: "/comms.html", ... })` loads the correct bundle per window.

### 4.5 Design system

Design tokens are ported from `design/vcs/project/styles.css` (`:root { … }` block) into `frontend/src/shared/styles/tokens.css`. Tailwind aliases the CSS variables so utility classes use the palette directly:

```ts
// tailwind.config.ts (sketch)
theme: {
  extend: {
    colors: {
      'ac-primary': 'var(--ac-primary)',
      'ac-warn':    'var(--ac-warn)',
      'ac-alert':   'var(--ac-alert)',
      'ac-ok':      'var(--ac-ok)',
      'bg-0':       'var(--bg-0)',
      // ... full token set
    },
    fontFamily: {
      sans: ['Space Grotesk', 'system-ui', 'sans-serif'],
      mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
    },
  },
}
```

shadcn primitives (Dialog, Popover, Dropdown, Tooltip, Slider, Switch, Select) are restyled against these tokens. Fonts are bundled locally to avoid the runtime Google Fonts dependency.

---

## 5. Backend service layout

### 5.1 Go module layout

```
github.com/FPGSchiba/vcs-srs-client/
  main.go                       Wails3 Application bootstrap, wires services & windows
  go.mod
  srs.proto                     single source of truth
  srspb/                        GENERATED  (protoc-gen-go, -go-grpc); gitignored
    srs.pb.go
    srs_grpc.pb.go
  internal/
    app/                        top-level App struct, lifecycle, window orchestration
      app.go
      windows.go                window registry: open/close/focus/persist
      bindings.go               Wails-exposed methods (the FE-facing API surface)
    auth/                       AuthService gRPC client
      auth.go                   InitAuth -> flow discovery -> start/continue -> UnitSelect
      guest.go                  GuestLogin path (name + password + unit_id)
      session.go                token persistence (session.json)
    control/                    SRSService gRPC client + SubscribeToUpdates stream
      client.go                 Sync/UpdateClientInfo/UpdateRadioInfo/Disconnect/GetServerSettings
      ping.go                   latency probe ticker
      stream.go                 stream consumer -> events
    voice/                      Phase 5 — interface stub only in Phase 1
      voice.go                  interface Voice { Start/Stop/SetFrequencies/PushFrame }
    audio/                      Phase 4 — interface stub only in Phase 1
      audio.go
    hotkeys/                    Phase 7 — interface stub only in Phase 1
      hotkeys.go
    operations/                 Phase 8 — REST client
      operations.go
    state/                      single in-memory state hub
      store.go                  thread-safe state (clients, radios, settings, servers)
      snapshot.go               serializable snapshots for FE GetClientState
    events/                     typed Wails event emitter
      events.go                 event names + payload types (one place)
    profiles/                   Phase 7 — interface stub only in Phase 1
      profiles.go
    config/                     TOML config read/write
      config.go
      paths.go                  OS-specific app-data paths
    updater/                    Phase 10 — no-op stub in Phase 1
      updater.go
  pkg/logger/                   slog setup, file rotation via lumberjack
  frontend/                     multi-entry Vite (see 4.4)
  build/                        Wails build outputs; gitignored
  docs/
    superpowers/specs/          design docs per phase
    ROADMAP.md                  cross-phase roadmap
    PROTO_GAPS.md               proto extension candidates for server team
  design/                       in-repo copy of the design package
```

### 5.2 Singleton services + per-window pattern

`internal/app/App` holds singletons:

```go
type App struct {
    cfg      *config.Config
    log      *slog.Logger
    state    *state.Store
    auth     *auth.Client
    control  *control.Client
    events   *events.Emitter  // wraps wails Application.Events
    profiles *profiles.Manager
    windows  *windows.Registry
}
```

The frontend never calls `auth` or `control` directly. It calls `App.*` methods (the binding surface). `App` is the only thing Wails knows about; everything else is internal.

### 5.3 Frontend-facing API (Phase 1 surface)

```go
// internal/app/bindings.go (sketch — exact signatures finalized in implementation)
func (a *App) Login(req LoginRequest) (LoginResponse, error)
func (a *App) ContinueLogin(req ContinueLoginRequest) (LoginResponse, error)
func (a *App) GuestLogin(req GuestLoginRequest) (LoginResponse, error)
func (a *App) SelectUnit(req SelectUnitRequest) (UnitSelectResponse, error)
func (a *App) Logout() error

func (a *App) GetClientState() (ClientStateSnapshot, error)
func (a *App) GetServerSettings() (ServerSettings, error)

func (a *App) UpdateClientInfo(info ClientInfo) error
func (a *App) UpdateRadioInfo(info RadioInfo) error

func (a *App) OpenWindow(id WindowID) error
func (a *App) CloseWindow(id WindowID) error
func (a *App) GetWindowGeometry(id WindowID) (Geometry, error)
func (a *App) SetWindowGeometry(id WindowID, g Geometry) error
```

Each binding mutates Go state and re-emits the corresponding event. Wails generates TS for both bindings and event payload shapes so FE has end-to-end types.

### 5.4 Event channel (Phase 1 set)

```
state:client_state        full snapshot (rarely; on (re)hydrate)
state:client_update       {client_guid, ClientInfo}
state:client_left         {client_guid}
state:radio_update        {client_guid, RadioInfo}
state:settings_update     ServerSettings
state:server_action       ServerAction
auth:flow_step            NextStepRequired (mid-login push)
auth:session_changed      "logged_in" | "logged_out"
control:connection        "connected" | "reconnecting" | "disconnected"
window:geometry_changed   {window_id, x, y, w, h}
log:line                  optional, for an in-app log viewer later
```

Later phases add: `voice:rx_active:{radioId}`, `voice:address_update`, `hotkey:ptt_pressed`, `audio:device_changed`, etc.

### 5.5 State model in Go

```go
type Store struct {
    mu       sync.RWMutex
    self     *ClientInfo
    clients  map[string]*ClientInfo   // by client_guid
    radios   map[string]*RadioInfo    // by client_guid
    settings *ServerSettings
    servers  ServerStatus             // control/voice ping + state
    session  *Session                 // token, secret, coalition, unit, role
}
```

Mutations go through `Store` methods (`UpdateClient`, `RemoveClient`, `SetRadios`, …); each method emits the corresponding event. The frontend never directly mutates; it calls a binding that calls `Store`.

---

## 6. Phase 1 — Definition of Done

A user can:

1. `git clone && make proto && cd frontend && npm ci && cd .. && wails build` produces a working binary on Windows. (macOS / Linux smoke-tested via CI but not gating.)
2. Running the binary on a fresh machine pops the welcome window. The single guest form (`server URL`, `name`, `password`, `unit_id`) succeeds against a running `vcs-srs-server`, lands on the Home view. SSO multi-step deferred to Phase 2.
3. Main window's chrome renders with design tokens — top bar, nav rail, status bar — matching the prototype's visual language (no voice, no popouts other than Comms shell).
4. Opening the **Communications** popout creates a real OS window at the design's default geometry (540×720). Closing it persists last position/size; reopening restores.
5. Editing a radio's frequency/name/enabled/intercom in Comms → pushes `UpdateRadioInfo` over gRPC → server fans out via `SubscribeToUpdates` → main window's Player List reflects the change within ≤ 200 ms.
6. Other connected clients' join/leave/info changes from `SubscribeToUpdates` show up live in the Player List.
7. Drag main window via the custom title bar.
8. Logout returns to the welcome screen.
9. Ping ticker runs in background; status bar shows latency value updating.
10. `go test -race ./...` is green; `go vet ./...` is clean.
11. CI workflow runs on PR and produces build artifacts for all three platforms.

**Out of scope for Phase 1:** UDP voice, audio I/O, global hotkeys, profile load/save, all popouts except Comms, distributed mode UI, Ship/Fleet/Messages/Notifications, settings, admin, operations, code signing, auto-update prompts (stub only).

### 6.1 Phase 1 file inventory (new or modified)

```
go.mod                          rename to github.com/FPGSchiba/vcs-srs-client
main.go                         Wails v3 Application bootstrap, multi-window aware
srspb/                          GENERATED
internal/app/{app,bindings,windows}.go
internal/auth/{auth,guest,session}.go
internal/control/{client,ping,stream}.go
internal/state/{store,snapshot}.go
internal/events/events.go
internal/config/{config,paths}.go
internal/voice/voice.go          interface stub only
internal/audio/audio.go          interface stub only
internal/profiles/profiles.go    interface stub only
internal/hotkeys/hotkeys.go      interface stub only
internal/operations/operations.go interface stub only (REST client lands in Phase 8)
internal/updater/updater.go      no-op stub
pkg/logger/logger.go             slog + lumberjack

frontend/
  vite.config.ts                 multi-entry: main, comms only for Phase 1
  tailwind.config.ts
  postcss.config.cjs
  index.html                     Wails dev shim
  main.html, comms.html
  src/
    main.tsx, comms.tsx
    shared/
      styles/tokens.css          copied from design/vcs/project/styles.css :root block
      styles/global.css          chrome, popout, panel, etc. ported from styles.css
      api/client.ts              typed wrapper over Wails bindings
      api/events.ts              typed event subscriber
      store/main.ts, store/comms.ts (Zustand)
      components/...             TopBar, NavRail, StatusBar, ConnBanner ported
    screens/welcome/...          guest login + unit-select
    screens/home/...             placeholder Home grid
    screens/players/...          live Player List from state
    popouts/comms/...            radio strip (basic edit fields only, no audio)
  package.json                   add: tailwindcss, postcss, autoprefixer, zustand,
                                 @tanstack/react-query, @tanstack/react-router,
                                 shadcn deps, zod

Tooling:
  Makefile                       proto, gen, dev, build targets
  buf.yaml + buf.gen.yaml        OR simple Makefile target invoking protoc
  .github/workflows/build.yml    matrix build (see CI section)
  .gitignore                     entries for generated dirs (see below)
```

### 6.2 Phase 1 test plan

- **Go unit tests** for `state.Store` mutators + event emission (mock emitter).
- **Go integration test** for the guest auth flow using a fake `AuthService` server (spin up `grpc.NewServer()` in-process).
- **Go integration test** for `SubscribeToUpdates` reconnect logic against a fake server.
- **Manual E2E checklist** for the 11-step "Phase 1 done" list above; tracked as `docs/superpowers/specs/2026-05-31-vcs-client-phase1-test-plan.md` (sibling spec, created during implementation).
- Playwright deferred; Wails v3 + Vite multi-entry + Playwright integration is a separate effort, slotted for Phase 3+.

---

## 7. Phase roadmap (post-Phase 1)

Full table in [`docs/ROADMAP.md`](../../ROADMAP.md). Summary:

| Phase | Theme | Blocking deps |
|---|---|---|
| 1  | Scaffold + windowing + auth (guest) + control gRPC + Comms popout shell | none |
| 2  | Plugin SSO multi-step auth | none |
| 3  | Settings + keybinds | none |
| 4  | Audio I/O (malgo) | malgo on each OS |
| 5  | UDP voice | **server-side Go protocol reference** |
| 6  | Connection-status surface (standalone path) | Phase 5 |
| 7  | Profiles + remaining popouts (Ship, Messages, Notifications, Fleet) | Phase 4 |
| 8  | Operations REST API integration | Operations REST endpoint spec from server team |
| 9  | Distributed voice (Server Network panel, multi-host pings, voice-host rebalance) | server distributed-mode reference |
| 10 | Polish — auto-update, code signing, crash reporter, in-app log viewer | code-signing certs |

Settings/keybinds (Phase 3) is intentionally pulled forward of Audio (Phase 4) because keybinds become essential the moment PTT exists.

Each phase gets its own brainstorm + design doc, derived from this one.

---

## 8. Proto extension hooks — gaps to flag

Tracked in [`docs/PROTO_GAPS.md`](../../PROTO_GAPS.md). Summary:

| Design feature | Proto field today | Client-side handling in v1 | Future proto change candidate |
|---|---|---|---|
| Per-radio encryption + channel | none | local field on `Radio`; not synced | `bool encrypted = 8; uint32 enc_channel = 9;` on `Radio` |
| Per-client presence (available/combat/discipline/afk) | none | local field; visible only to self | `Presence presence = 6;` on `ClientInfo` |
| Status presence enum | none | local | `enum Presence { … }` |
| Ship-mode component registry | none | local TOML + JSON | future `ShipModeService` |
| Ships catalog | none | TOML | future `GetShipCatalog` RPC |
| Roles catalog | partial (`RoleSelection` in auth) | TOML mirror | reuse `RoleSelection` everywhere |
| Text channels per frequency | none | local in-memory ring buffer | future `MessagingService` |
| Transmission history | none | local; JSON ring buffer file | future server-pushed history |
| Notifications | none | local + future server channel | future `NotificationsService` stream |
| Recording (local Opus files) | none | client-only, no wire change | n/a |

(Operations is **not** a proto gap — it is a REST endpoint owned by another system.)

---

## 9. Risks & unknowns

| # | Risk | Likelihood | Mitigation |
|---|---|---|---|
| R1 | Wails v3 still pre-stable; API churn breaks builds mid-development | M | Pin v3 version in `go.mod`; keep windowing/binding adapter (`internal/app/windows.go`) thin; run `wails doctor` in CI |
| R2 | UDP voice protocol unknown — Phase 5 blocked | H | Phase 1 – 4 don't touch voice; `internal/voice` ships as interface-only; spec defers Phase 5 until user provides server-side reference |
| R3 | malgo native deps complicate cross-platform builds | M | Add a CI job that compiles the audio package per platform in Phase 3 (one phase before audio lands) to surface toolchain issues early |
| R4 | Session token persisted to disk without OS-keychain protection in v1 | M | Document trade-off; `os.Chmod(0600)` on Unix; ticket a follow-up to use a keychain library in Phase 7 |
| R5 | gRPC insecure transport during dev → secrets in cleartext | M | Insecure permitted only for literal `127.0.0.1` / `localhost`; fail-closed on remote hosts; switch to TLS in Phase 7 |
| R6 | TanStack Query + gRPC streaming awkward fit | L | Query only wraps unary calls; streams feed Zustand via events |
| R7 | Multi-window state divergence | M | Every binding mutates Go state and re-emits; FE stores re-hydrate via `GetClientState` on window open; no optimistic FE-only updates |
| R8 | Hot-reload across multiple Vite entries during `wails dev` | M | If broken, fall back to single-bundle in dev / multi-bundle in prod |
| R9 | Design tokens drift between `design/vcs/project/styles.css` and ported `tokens.css` | L | CI check that diffs the `:root { … }` block byte-for-byte between source and frontend copy |
| R10 | Multiple FE bindings to same `App` can race control state | M | Bindings always read/write via `state.Store` mutex; events serialize via channel |

---

## 10. CI pipeline

```
.github/workflows/build.yml
  matrix: { windows-latest, macos-latest, ubuntu-latest }
  steps:
    setup-go (1.23)
    setup-node (20)
    install buf + protoc-gen-go + protoc-gen-go-grpc
    make proto                       -> srspb/                  (generated; gitignored)
    wails3 generate bindings         -> frontend/bindings/      (generated; gitignored)
    npm ci --prefix frontend
    npm run build --prefix frontend  -> frontend/dist/          (generated; gitignored)
    go vet ./... && go test -race ./...
    wails3 build -platform <os>
    upload-artifact: build/bin/*
  release job (on tag v*):
    gh release create with all platform artifacts
```

A `Makefile` mirrors the CI commands so devs run the same thing locally.

## 11. .gitignore additions

```
# generated
srspb/
frontend/wailsjs/
frontend/bindings/
frontend/dist/
frontend/node_modules/
build/bin/

# runtime artefacts
log/
*.log

# local-only configs that should never be committed
session.json
config.toml.local

# IDE
.idea/
.vscode/
```

---

## 12. Blocking dependencies on the user

1. **Server-side Go reference for the UDP voice protocol** — required before Phase 5 (packet structure, framing, codec, sample rate / frame size, per-frequency multiplex on one UDP socket, `VoiceHostDetails.secret` authentication, `coalition_voice_addr` vs `global_voice_addr` interaction).
2. **Operations REST API endpoint specification** — required before Phase 8 (URL, auth, response shape).
3. **Server-side distributed-mode reference** — required before Phase 9 (how `DistributionUpdate` payloads are produced, how voice-host rebalance is signalled in practice).
4. **Any non-obvious semantics of coalitions / units / roles** not captured in the proto.

## 13. Spec self-review

Performed inline before commit (see brainstorming skill checklist). No placeholders, no contradictions found. Scope: focused (Phase 1 detail + roadmap stubs, as agreed).
