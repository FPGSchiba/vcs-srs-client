# VCS Client — Phase 1 Part 2 (Auth + Control + Windowing) Design

**Status:** Approved (brainstorm phase)
**Date:** 2026-06-01
**Author:** FPG Schiba (with Claude Code)
**Branch:** `feat/phase-1-part-2-auth-control`
**Builds on:** Part 1 foundation (`internal/{app,config,events,state}`, `pkg/logger`, `srspb/`, multi-entry frontend)
**Parent spec:** [`2026-05-31-vcs-client-design.md`](./2026-05-31-vcs-client-design.md) · Roadmap: [`../../ROADMAP.md`](../../ROADMAP.md)

---

## 1. Overview

Part 2 completes Roadmap **Phase 1**: a user can launch the client, connect via the **guest** login path, see other connected clients live, edit radios with a server round-trip, and open the Communications pop-out as a real second OS window. The shell is rendered as a **frameless, transparent** window with React drawing the entire chrome (header + footer) to match the design's tactical look.

Plugin SSO (`DiscoverAuthenticationFlows`/`StartAuth`/`ContinueAuth`) and the coalition/unit/role pickers are **Phase 2**, not here. Voice/audio, settings/keybinds, profiles, the other pop-outs, operations, and distributed voice are later phases.

## 2. Decisions locked in brainstorming

| Decision | Value |
|---|---|
| Connect orchestration | **Backend-orchestrated single `Connect()`**; Welcome advances on events |
| UI fidelity | **Pixel-faithful** port of the design (port CSS + JSX, not re-derive in Tailwind) |
| Comms pop-out | **Real OS window** + persisted geometry (`windows.json`) |
| Window styling | **Frameless + transparent**; React draws header (`TopBar`) + footer (`StatusBar`); drag via `-webkit-app-region`; custom min/close controls |
| Test strategy | **In-process fake gRPC server** for Go; Vitest for frontend; real-server smoke is informal/non-gating |
| gRPC transport | Insecure permitted only for `127.0.0.1`/`localhost`; remote hosts fail closed (TLS is a later phase) |
| Binding surface | Hand-rolled DTOs (proto kept internal), Wails-generated TS types |

## 3. Backend packages & the `Connect` orchestration

```
internal/
  auth/        AuthService gRPC client (guest path only this phase)
    auth.go      InitAuth(caps) → AuthInitResult{plugins, hasGuest, distMode, clientGUID}
    guest.go     GuestLogin(name, password, unitID, clientGUID) → {token, coalition}
  control/     SRSService gRPC client + lifecycle
    client.go    dial, SyncClient, UpdateClientInfo, UpdateRadioInfo, GetServerSettings,
                 Disconnect; token attached as gRPC metadata on every call
    ping.go      Ping ticker (interval from config.PingIntervalSeconds), records RTT
    stream.go    SubscribeToUpdates consumer → routes ServerUpdate into state + events
  session/
    session.go   orchestrator: owns *grpc.ClientConn, auth.Client, control.Client, lifecycle
  windowstate/   JSON persistence of per-window geometry (windows.json)
  app/
    bindings.go  FE-facing methods (Connect, Disconnect, Reconnect, UpdateRadioInfo, …)
    windows.go   window registry + geometry wiring
    dto.go       hand-rolled binding DTOs + proto↔DTO mapping
```

### 3.1 `Connect` flow (in `session`)

```go
func (s *Session) Connect(ctx, serverURL, name, password, unitID string) error
// 1. validate + dial serverURL (insecure only if host is 127.0.0.1/localhost, else fail-closed)
// 2. auth.InitAuth(capabilities{version, [STANDALONE]}) → clientGUID, hasGuest
// 3. if !hasGuest → return ErrGuestUnavailable
// 4. auth.GuestLogin(name, password, unitID, clientGUID) → token, coalition
// 5. open control client with token; SyncClient → hydrate state.Store
// 6. start Ping ticker + SubscribeToUpdates stream (goroutines; cancel on Disconnect)
// 7. emit auth:session_changed=logged_in, control:connection=connected
```

`Connect` returns a typed error **synchronously** for everything in the pre-`connected` sequence (so the Welcome form renders it directly). Post-`connected` transitions (`reconnecting`/`disconnected`) flow through **events**, so the UI reacts rather than blocking.

### 3.2 Token handling

The guest `token` is attached as gRPC metadata (`authorization`) through a `credentials.PerRPCCredentials` on the control channel. It is persisted to `session.json` (chmod 0600 on Unix) and loaded on next launch as a Welcome **pre-fill** — no auto-reconnect this phase. (OS-keychain migration remains a Phase 7 follow-up per the parent spec, R4.)

### 3.3 Window registry & frameless/transparent styling

- Both windows are created **`Frameless: true`** with a transparent background (alpha 0). React renders the design's `.client` rounded panel (border-radius, border, drop-shadow) on the transparent surface — no native title bar.
- React draws the **header** (`TopBar`/`PreLoginTopBar`) and **footer** (`StatusBar`) as the window's chrome (design's header/body/footer column). The header is the drag region via `-webkit-app-region: drag` (with `no-drag` on its buttons); header window-controls (minimize/close) call Wails window methods.
- The prototype's `.viewport`/`.desktop` scale-to-fit wrapper is dropped — each panel is a real OS window.
- Registry (`internal/app/windows.go`) maps `WindowID` (`"main"`, `"comms"`) → `*application.WebviewWindow`.
  - Main: fixed 1440×900, frameless/transparent, position restored.
  - `OpenWindow("comms")`: focus if open; else create a frameless/transparent, OS-resizable window loading `/comms.html` at persisted geometry, falling back to the design default (540×720 at 1190,70). It draws the design's `.popout-chrome` header; OS edge-resize replaces the prototype's simulated resize handle.
- Move/resize → debounced (~300 ms) → `windowstate.Save` + emit `window:geometry_changed`; persisted again on close.
- `internal/windowstate` (JSON at `%APPDATA%/VCS/windows.json`) gets its own load/save + tests, mirroring the `config` package pattern.

**Risk:** the exact Wails v3 alpha.96 API for reading/observing window bounds and for per-platform transparency differs (Windows backdrop vs macOS vs GTK/Linux). The registry isolates that surface; Linux transparency is the most likely follow-up.

## 4. Frontend structure & screens (pixel-faithful)

**Porting strategy:** copy the relevant component CSS from `design/vcs/project/styles.css` into `frontend/src/shared/styles/components.css` (imported by `global.css`), and port the design JSX (`shell.jsx`, `atoms.jsx`, `radio.jsx`, `welcome.jsx`) into typed React components using those exact classNames. Tailwind/tokens remain for incidental layout.

```
frontend/src/
  main.tsx                  → <MainApp/>      (replaces Placeholder)
  comms.tsx                 → <CommsApp/>     (replaces Placeholder)
  shared/
    api/client.ts           typed wrappers over generated Wails bindings
    api/events.ts           typed Wails event subscribe helpers
    store/session.ts        Zustand: connection state, self, coalition, serverURL
    store/clients.ts        Zustand: client/player list
    store/radios.ts         Zustand: radios + in-flight edits
    components/             Icon, StatusPill, Button, Field, Toggle, Pill, LcdFreq,
                            TopBar, PreLoginTopBar, NavRail, StatusBar, ConnBanner
    styles/components.css   ported design CSS
  windows/
    main/MainApp.tsx        gates Welcome vs Shell on session state
    main/router.tsx         TanStack Router (home, players, + placeholder routes)
    main/screens/Welcome.tsx   manual-guest form → connecting → success
    main/screens/Home.tsx      launcher tiles (pixel-faithful)
    main/screens/Players.tsx   live player list from clients store
    main/screens/Placeholder.tsx  "arrives in a later phase" for not-yet-built nav items
    comms/CommsApp.tsx      radio strip
    comms/RadioCard.tsx     editable frequency / name / enabled / intercom
```

**Behaviors:**
- **MainApp** subscribes to `auth:session_changed` + `control:connection`; renders `Welcome` until connected, then the shell (header + router body + footer).
- **Welcome** guest form (Server Address, Server Password, Player Name, FFID-optional → `unitID`) calls `api.Connect(...)`; advances through *connecting*/*success* on `control:connection` events; inline error on synchronous `Connect` rejection.
- **Nav rail** ported in full; destinations beyond Home/Players/Comms render the placeholder screen.
- **Radio edits**: `RadioCard` holds transient local input, commits via `api.UpdateRadioInfo(...)`; the store updates only when the `state:radio_update` echo returns (write-through, no optimistic mutation — proves the round-trip).
- **Comms launcher** in `TopBar` calls `api.OpenWindow("comms")`.
- Every window calls `api.GetClientState()` on mount to hydrate, then subscribes to deltas (lets the later-opened Comms window load current radios).

## 5. State sync & events

Stream consumer routing (extends the Part 1 `events` package — name constants already exist):

| `ServerUpdate` type | `state.Store` mutation | Event |
|---|---|---|
| `CLIENT_JOINED` / `CLIENT_INFO_UPDATE` | `UpdateClient` | `state:client_update` |
| `CLIENT_LEFT` | `RemoveClient` | `state:client_left` |
| `CLIENT_RADIO_UPDATE` | `SetRadios` | `state:radio_update` |
| `SERVER_SETTINGS_CHANGED` | `SetSettings` | `state:settings_update` |
| `SERVER_ACTION` | — | `state:server_action` (toast; full handling later) |
| `DISTRIBUTION_UPDATE` / `VOICE_ADDRESS_UPDATE` | — | logged + ignored (voice is Phase 5/9) |

Section adds the missing emit methods to `events.Tagged` (`RadioUpdate`, `SettingsUpdate`, `ServerAction`, `SessionChanged`, `ClientState`).

**Binding DTOs:** hand-rolled `ClientDTO`, `RadioDTO`, `RadioInfoDTO`, `ClientInfoDTO`, `ServerSettingsDTO`, `ClientStateSnapshot`, with proto↔DTO mapping in `app/dto.go`. Keeps `srspb` out of the generated TS. Binding surface:

```
Connect(serverURL, name, password, unitID) error
Disconnect() error
Reconnect() error
GetClientState() ClientStateSnapshot
GetServerSettings() ServerSettingsDTO        // TanStack Query (unary, cacheable)
UpdateClientInfo(ClientInfoDTO) error
UpdateRadioInfo(RadioInfoDTO) error
OpenWindow(id) / CloseWindow(id) / GetWindowGeometry(id) / SetWindowGeometry(id, Geometry)
```

## 6. Connection status & error handling

**States (control-only this phase):** `control:connection` = `connecting` → `connected`; mid-session failure → `reconnecting` → `disconnected`. The design's dual pill renders in its control-only form (control node + live ping); `ConnBanner` shows only `disconnected` (voice-degraded variants wait for Phase 5/6).

**Reconnect:** the Ping ticker is the health probe. On N consecutive failures or stream `EOF`/error, `session` emits `reconnecting`, tears down the stream, and retries the control dial with bounded backoff; success re-runs `SyncClient` and re-emits `connected`; exhaustion emits `disconnected`. The `ConnBanner` button calls `Reconnect()`. Auto-reconnect reuses the in-memory token (no re-auth unless rejected).

**Error taxonomy (typed Go errors → friendly UI):**
| Failure | Where | UI surface |
|---|---|---|
| Bad/empty URL, non-localhost without TLS | `Connect` (sync) | inline on Welcome |
| Server unreachable / dial timeout | `Connect` (sync) | inline on Welcome |
| No guest login on server | `Connect` (sync) | inline: "guest login not available" |
| `GuestLogin` rejected | `Connect` (sync) | inline: "login failed — check server password" |
| Mid-session drop | stream/ping | `ConnBanner` (disconnected) + status pill |

Go logs full context via `slog`; the UI receives the friendly message.

## 7. Testing

**Go — shared fake-server harness:** registers fake `AuthService` + `SRSService` on an in-process `grpc.NewServer()`; behaviors configurable (reject login, no-guest, queue `ServerUpdate`s, force stream drop). Reused across suites.

| Package | Coverage |
|---|---|
| `auth` | InitAuth guid/hasGuest; GuestLogin success → token+coalition; rejected → typed error; no-guest → `ErrGuestUnavailable` |
| `control` | SyncClient hydrates store; UpdateRadioInfo payload; stream maps each update type → mutation + event (fake emitter); Ping records RTT |
| `control` reconnect | stream drop → `reconnecting` → bounded retry (short injected intervals) → `connected`; exhaustion → `disconnected` |
| `session` | full `Connect()` happy path emits `connected` + hydrates; each failure path returns the right typed error |
| `windowstate` | JSON round-trip; missing-file → defaults |
| `app` | proto↔DTO mapping; bindings against a fake `session` interface; window registry behind a window-factory interface |

**Frontend (Vitest + Testing Library), `api/client.ts` mocked:**
- Welcome renders fields, submit calls `api.Connect`, inline error on rejection.
- Players renders rows from store, updates on `state:client_update`.
- RadioCard edits call `api.UpdateRadioInfo`, reflect `state:radio_update` echo.
- StatusBar/ConnBanner render correct variant per session-store state.

**Isolation:** Wails runtime mocked in FE; gRPC faked in Go; no real server/window manager in unit tests. Coverage target 80%+. `main.go` wiring + the Wails window-creation adapter are integration-only and explicitly not unit-covered. Real-server smoke via `wails3 dev` is informal/non-gating.

## 8. Phasing within Part 2

Three coherent, independently-testable slices → likely three implementation plans (incremental landing):

- **2A — Backend session core (no UI change).** `auth` (guest), `control` (client/ping/stream), `session` (`Connect`/`Disconnect`/`Reconnect`), `events` extensions, `app/dto.go`. Gate: Go suites green vs fake server.
- **2B — Windowing & frameless/transparent shell.** `windowstate`, `app/windows.go`, `main.go` rewrite (frameless + transparent main loading `/main.html`, `OpenWindow("comms")`), drag + window-control wiring, geometry persistence. Gate: app launches transparent/frameless; Comms opens as a second window; geometry survives restart.
- **2C — Pixel-faithful frontend + round-trips.** Port CSS/atoms/chrome, Welcome/Home/Players/Comms; Zustand stores + `api` client + event subscriptions + TanStack Router; wire Connect + radio-edit round-trips; Vitest tests. Gate: full DoD.

## 9. Definition of Done (Phase 1 Part 2)

1. Launch → frameless, transparent main window with ported header/footer and the rounded tactical-panel look.
2. Welcome guest form (server, password, name, optional FFID) → CONNECT → guest auth → Home.
3. Player List shows other clients live (join/leave/info from the stream).
4. Comms opens as a separate frameless/transparent OS window; move/resize persists across restart.
5. Edit a radio in Comms → `UpdateRadioInfo` → server fan-out → reflected back ≤200 ms.
6. Status bar shows control node + live ping; server drop → `ConnBanner` + bounded auto-reconnect; Reconnect button works.
7. Drag main window via custom title bar; minimize/close via custom controls.
8. Disconnect/Logout → back to Welcome; relaunch pre-fills last server (token persisted, no auto-reconnect).
9. `go test -race ./...` (CI), Vitest, `go vet`, and both CI jobs green.

On completion, flip `docs/ROADMAP.md` Phase 1 → `[x]` (Part 1 + Part 2 = Phase 1 done).

## 10. Out of scope (later phases)

Plugin SSO + coalition/unit/role pickers (Phase 2); voice/audio (Phase 4/5); settings/keybinds (Phase 3); profiles + remaining pop-outs (Phase 7); operations (Phase 8); distributed voice + the dual control/voice connection variants (Phase 9); TLS + OS-keychain token storage (Phase 7).
