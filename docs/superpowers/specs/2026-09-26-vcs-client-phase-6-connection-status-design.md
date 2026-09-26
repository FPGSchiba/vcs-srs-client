# Phase 6 — Connection-status surface — design

**Date:** 2026-09-26
**Status:** approved design, ready for implementation planning
**Builds on:** [`2026-05-31-vcs-client-design.md`](./2026-05-31-vcs-client-design.md) §5, §6 (DoD 9); [`2026-09-24-vcs-client-phase-5-udp-voice-design.md`](./2026-09-24-vcs-client-phase-5-udp-voice-design.md) §7 (the voice lifecycle this phase surfaces)
**Roadmap row:** [`docs/ROADMAP.md`](../../ROADMAP.md) — Phase 6

---

## 1. Goal

Tell the user the truth about their connection.

The client has two independent transports — a gRPC control plane and a UDP voice plane — that fail independently and, today, mostly invisibly. This phase makes both planes' health observable, renders it in the two places the design prototype reserves for it (the `conn-banner` and the status bar's `dual-pill`), and gives the user a working way to act on a failure.

It is not primarily a UI phase. The surface already exists in skeleton form; what is missing is anything for it to display.

### 1.1 Starting state, verified rather than assumed

Every claim below was read from source on the revisions named, not inherited from a prior phase's summary.

| Component | Revision read |
|---|---|
| This repo | `origin/main` @ `69f8502` (post-#30) |
| Go server | `vngd-srs-server` @ `origin/main` `70bb9e2`, plus branch `fix/client-version-check` `063f6c5` |
| Wire contract | `srs.proto` at this repo's root |

Four gaps were found that the ROADMAP does not record. They are the actual content of this phase.

**G1 — `ConnBanner` is live code that can essentially never fire.** `internal/session/session.go:116` (in `Connect`) and `:183` (in `Reconnect`) are both:

```go
go func() { _ = cc.ConsumeUpdates(streamCtx, s.st, s.em) }()
```

The stream's terminating error is discarded, at both sites — so a stream that dies after a *reconnect* is as silent as one that dies after the first connect. When the control stream dies for any reason other than an explicit `Disconnect` — a server restart, a network drop, or the server's `SubscribeToUpdates` refusing a duplicate subscription — no event is emitted and no state changes. The UI reports `connected` indefinitely. There is **no control-plane liveness detection of any kind** in the client: nothing watches `grpc.ClientConn` state, and the dialer sets no keepalive parameters.

This was written deliberately in the Phase 1 plan (`2026-06-01-vcs-client-phase-1-part-2a-backend-session.md:1190`) and never revisited. `ConnBanner.tsx` and its `disconnected` variant are correct code sitting behind a signal that does not arrive.

**G2 — the Phase 1 ping ticker was never wired.** `internal/control/ping.go`'s `PingOnce` is fully implemented and has **zero callers**. `config.Config.PingIntervalSeconds` (default `5`) has **zero readers**. Master spec §6 DoD item 9 — *"Ping ticker runs in background; status bar shows latency value updating"* — is undelivered; `StatusBar.tsx` renders a hardcoded `— ms`. Server-side, `Ping` is the sole writer of `client.LatencyToControlMs` (`srs/srs_service.go:180–190`), so the server's latency map holds zero for every VCS client.

**G3 — voice state is emitted into the void.** Phase 5's I2 fix wired `voice.Session`'s `OnState` through to `events.EventVoiceState`. But `voice:state` and `voice:address_update` are **absent from `frontend/src/shared/api/events.ts`'s `EV` map** — nothing subscribes, in any window. `voice.Session.RTT()` is exposed nowhere at all: no DTO field, no event, no binding. `App.VoiceState()` is polled once on `CommsApp` mount and never refreshes.

**G4 — the prototype renders two fields that are not on the wire.** `design/vcs/project/lib/shell.jsx`'s `StatusBar` shows `vanguard-prime · {latency}ms` and `{app.server.region}`. `srs.proto` carries neither a server display name nor a region; `SyncResponse.version` is the only server-identifying string. See §8.

Two further findings that shape the design without being fixed here:

- `general.play_connection_sounds` is persisted, exposed in Settings, and **read by nothing**. `internal/audio/assets/manifest.toml` has no connect/disconnect slots. See §7.
- Server-side, a dead control stream removes the client from `s.streams` but **not** from `serverState.Clients` (`srs/srs_service.go:391–395`). Ghost clients persist until an explicit `Disconnect`. See §9.

---

## 2. Decisions

Settled during brainstorming. Not open for re-litigation during implementation.

| # | Decision | Rationale |
|---|---|---|
| D1 | **Detect loss; keep reconnect manual** | The ROADMAP scopes this phase's reconnect deliverable as *"manual reconnect button on banner"*. Detection is mandatory regardless — without it the banner is unreachable (G1). Automatic reconnect is a retry-policy design this phase did not scope, and it collides with a live server race (§9) |
| D2 | **`internal/connhealth` owns the derived dual-plane model** | One owner, one snapshot, one event. The alternative — each of the banner and the pill deriving health from two partially-overlapping event streams — is the frontend/backend divergence class the codebase already rejects for `keybinds:changed` and `joystick:state` |
| D3 | **Application-level `Ping` is the fast detector; gRPC keepalive is a backstop** | The server permits client keepalive pings no more often than every 60s (§3.3). `Ping` at 5s × 3 detects in ~15s, produces the RTT the status bar needs, and feeds the server's latency map. Keepalive is added too, but only as a cheap idle-case backstop — the surface depends on `Ping` alone |
| D4 | **Three control link states stay; health is a separate boolean** | `control:connection`'s `connected`/`reconnecting`/`disconnected` already gates login phase in `MainApp`. Adding a fourth state churns that contract. A probe that has begun failing while the stream is still alive is a *health* fact, not a *link* fact |
| D5 | **The dual-pill renders unconditionally, including standalone** | Standalone genuinely has two independently-failing transports. The banner variants this phase owns — `control-only`, `voice-only` — are only legible if the pill can show which half is down; a single dot cannot express that. Phase 9 then fills in real per-host names rather than restructuring the widget |
| D6 | **Voice gets a distinct `unavailable` state, not `alert`** | Voice that never started is not voice that broke. Exact precedent: `JoystickState.Supported` exists so macOS's "unsupported here" never renders as an error. Without it every Windows release build boots showing a red VOICE alert and advice to reconnect something that cannot connect (§6.3) |
| D7 | **`connection:state` is the surface's single source; `control:connection` and `voice:state` keep their existing roles** | They answer different questions — lifecycle versus health. They cannot disagree, because `connhealth` is fed from the *same call sites* that emit the lifecycle events |
| D8 | **Connection SFX wired asset-agnostically, shipping silent** | The engine is asset-agnostic by Phase 4's D11: a missing sample logs once and plays silence. Wiring it now closes a Settings toggle that currently lies, and goes audible the moment the samples land. It is silent on arrival — see §7 |
| D9 | **Server Network is a `Placeholder`** | The pill and the NETWORK button route there per the prototype; the screen is Phase 9's. `Placeholder` is how NavRail already handles unbuilt screens |

---

## 3. Control-plane liveness

This is the part that does not exist today. Two independent detectors, because they catch different failures.

### 3.1 Stream termination

`ConsumeUpdates` stops being called into a discarded return value. The goroutine reports its termination back into `Session`, which emits `disconnected` — **unless** the termination was our own doing.

Two guards, both necessary:

1. **Intent.** If `streamCtx.Err() != nil`, the stream ended because `Disconnect` or `Reconnect` cancelled it. That must not emit `disconnected` over a teardown already in progress, nor over a reconnect already underway.
2. **Generation.** A monotonic counter incremented on every stream launch, captured by the goroutine, compared before emitting. Without it, a stale goroutine from a previous connection can emit `disconnected` on top of a healthy new one. This is exactly the `nextVoiceGeneration` / `voiceSessionSwap` pattern in `internal/app/voice.go`, applied to the control half; that file's doc comments explain at length why capture-before-scheduling is the only ordering that works, and the same reasoning holds here.

Stream termination is a *definite* signal: it means the transport is gone. It emits `disconnected` immediately, with no threshold.

### 3.2 Ping ticker

Closes master-spec §6 DoD 9 (G2). `PingOnce` fires every `config.PingIntervalSeconds` (default 5) for the life of a connection, cancelled with `streamCtx`.

Each call is bounded by `context.WithTimeout` at one interval. `PingOnce` takes a `ctx` but no current caller bounds it, and an unbounded unary call on a half-open connection blocks forever — which is precisely the case this detector exists for.

Outcomes:

| Result | Effect |
|---|---|
| Success | `rtt_ms` updated; `healthy = true`; `last_rtt_ms` echoed to the server on the next `Ping`, feeding `LatencyToControlMs` |
| 1–2 consecutive failures | `healthy = false`. Link state unchanged. Pill dot goes warn; no banner |
| 3 consecutive failures | `disconnected`. Banner appears |

**Why 5s × 3.** It mirrors the voice plane's own liveness rule exactly — `defaultKeepalive = 5 * time.Second`, `bindingLossThreshold = 3` in `internal/voice/session.go` — so the two planes declare loss on the same budget and a user watching both sees them agree. The client's persisted default already is 5s, so no config change is needed.

**Why this is the fast detector.** `Recv()` on a half-open stream blocks until the transport notices, which is bounded only by the server's keepalive (§3.3) at roughly 70s. A `Ping` with a deadline answers in 15s.

### 3.3 gRPC keepalive — a backstop, at the server's pace

The client dialer sets no keepalive parameters. The server does, on the client-facing gRPC server that serves `SRSService` and `AuthService` (`vngd-srs-server/control/server.go:97–108`):

```go
grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
    MinTime:             60 * time.Second,
    PermitWithoutStream: true,
}),
grpc.KeepaliveParams(keepalive.ServerParameters{
    Time:    60 * time.Second,
    Timeout: 10 * time.Second,
}),
```

Two consequences, both load-bearing:

- The **server already pings us** every 60s when idle and drops the connection after 10s unacked. So a half-open connection *is* eventually detected without any client change — at roughly 70s. That is the floor this phase improves on, not a gap it fills.
- A client keepalive is **permitted, but not faster than every 60s**. `MinTime: 60s` means a client pinging more often earns a `GOAWAY` with `too_many_pings` and loses the connection — an availability feature turned into an outage. gRPC keepalive therefore cannot be the fast detector.

The client adds `grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 75s, Timeout: 10s, PermitWithoutStream: true})` in `internal/session/dial.go`. 75s rather than 60s leaves margin against the server's floor, since a ping arriving marginally early counts against the enforcement policy. `PermitWithoutStream: true` matches the server's policy.

This is cheap and needs no server change. It is a backstop for the idle case, not the mechanism the surface depends on.

> **Note for the server team, not a dependency of this phase:** the comment on `ServerParameters.Time` reads `// server sends pings every 30s if idle` while the value is `60 * time.Second`. The comment is stale; the value is what runs.

---

## 4. `internal/connhealth`

A new package owning the derived model. It depends only on small injected interfaces, so it is unit-testable under an injected clock with no sockets — the seam `voice.Session` already established and the sandbox requires (§10.1).

```go
// Link is one transport plane's health.
type Link struct {
    State     string // control: "connected" | "reconnecting" | "disconnected"
                     // voice:   voice.State.String(), or "unavailable"
    RTTMs     int64  // -1 when unknown (no successful probe yet)
    Healthy   bool   // the probe is answering
    Available bool   // voice only: a session is possible at all
    Error     string // last transition error, "" when none
}

type Snapshot struct {
    Server  string // host:port from server_url
    Control Link
    Voice   Link
}
```

**Inputs.** `SetControlState(events.ConnectionState)` and `SetVoiceState(voice.State, error)`, called from the same sites that emit the existing lifecycle events; plus one ticker at `PingIntervalSeconds` that probes control RTT through an injected `Pinger` and reads voice RTT through an injected `RTTSource`.

**Output.** An `OnChange(Snapshot)` hook, which `internal/app` turns into the `connection:state` event.

**Emission cadence.** Every tick, plus immediately on any state transition. Deliberately *not* suppressed-when-unchanged the way `audio:vu` is: RTT differs on nearly every tick, so suppression would save almost nothing, and at 0.2 Hz across a handful of windows the traffic is negligible. Transitions emit immediately so a drop is not hidden behind up to five seconds of tick latency.

**Hydration.** A window opened after a transition has missed it. `App.GetConnectionState()` returns the current `Snapshot` for mount-time hydration — the precedent set by `GetHotkeyState`, `GetJoystickState` and `GetAudioState`.

### 4.1 Why not extend `control:connection`

Enriching `control:connection`'s payload from a bare string to a struct would give one event instead of two. It is rejected because that event's consumers gate the **login phase** (`MainApp` sets `phase = "connected"` off it), and health updates every five seconds. Coupling a 0.2 Hz health feed to the login state machine invites exactly the kind of accidental re-render and re-hydration the phase gating is supposed to be insulated from.

`control:connection` and `voice:state` therefore keep their current shapes and roles. `connection:state` is additive.

---

## 5. Voice plane

Three gaps to close (G3); the state machine itself is already complete and is not touched.

1. **Subscribe.** Add `voice:state` to the frontend `EV` map. The backend has emitted it since Phase 5 and nothing has ever listened.
2. **Surface RTT.** `voice.Session.RTT()` is read on the `connhealth` tick rather than through a second ticker. `RTT()` already returns zero while rebinding — it is reset by `resetBindingLocked` on every new binding, specifically so the UI never shows a healthy ping for a broken session — so `0` maps to `RTTMs: -1` (unknown), not to a real zero-millisecond measurement.
3. **Availability.** `Available: false` when no voice secret has arrived, no session exists, or the build cannot do voice. See §6.3.

---

## 6. The surface

`design/vcs/` is canonical. Every class name below already exists in `design/vcs/project/styles.css`; nothing new is invented.

### 6.1 Status bar — `dual-pill`, always (D5)

```
┌──────────────────────┬────────────────────────┐
│ ● CTRL  127.0.0.1 8ms│ ● VOICE 127.0.0.1 12ms │
└──────────────────────┴────────────────────────┘
```

`.seg.ctrl` / `.seg.voice`, `.lbl` / `.val` / `.ping`, and `.d.ok` / `.d.warn` / `.d.alert` are used exactly as `styles.css:342–371` defines them. `.val` renders the host from `server_url` (G4 — there is no display name on the wire).

Dot derivation:

| Plane | Condition | Dot |
|---|---|---|
| Control | `disconnected` | `alert` |
| Control | `reconnecting` | `warn` |
| Control | `connected` && `!healthy` | `warn` |
| Control | `connected` && `healthy` | `ok` |
| Voice | `!available` | dim (§6.3) |
| Voice | `connected` | `ok` |
| Voice | `resolving` \| `handshaking` \| `rebinding` \| `retrying` | `warn` |
| Voice | `idle` \| `closed` | `alert` |

The `region` item is **omitted**, not rendered as a permanent em-dash. There is no data source for it (G4).

### 6.2 `ConnBanner` — all three prototype variants, now reachable

The prototype defines `control-only`, `voice-only` and `disconnected`. Until now only the third existed in the port, and it could not fire.

| Control | Voice | Variant | Title | Action |
|---|---|---|---|---|
| alert | alert | `disconnected` | DISCONNECTED | FULL RECONNECT |
| alert | ok/warn | `voice-only` | CONTROL DEGRADED | RECONNECT CONTROL |
| ok/warn | alert | `control-only` | VOICE DEGRADED | RECONNECT VOICE |
| ok/warn | ok/warn | — | no banner | — |

When voice is `unavailable` it drops out of the banner's input entirely and the banner reflects control alone — so an unavailable-voice build with a healthy control link shows **no banner**, and with a dead control link shows `DISCONNECTED`.

### 6.3 The `unavailable` state, and what it means on Windows today

`Available: false` is set when there is no voice secret yet (the window between `Connect` returning and `SyncClient` populating the store), when no session exists, or when the build's Opus codec is the stub.

**This will be the normal state on every Windows release build.** `build/windows/Taskfile.yml` defaults `CGO_ENABLED=0`, which selects the stub codec and the stub audio backend — Phase 5's open issue #1, on the only platform Star Citizen runs on. Until that is fixed, a Windows release user will see a dimmed VOICE segment and no banner. That is the correct and honest rendering of a build that cannot do voice, and it is exactly why D6 chose a distinct state over `alert`: the `alert` rendering would tell that user to reconnect something that was never going to connect.

### 6.4 Reconnect UX

Today `ConnBanner.tsx` calls `void api.reconnect()` and discards the result. `App.Reconnect()` returns an error; a failed reconnect is therefore **completely invisible** — the button appears to do nothing.

Changes:

- The button takes an in-flight disabled state while the call is outstanding, so it cannot be re-entered.
- A failure's reason is rendered in the banner's existing message slot (the `.conn-banner` message span the prototype already lays out). No new visual.
- `RECONNECT VOICE` needs a binding that does not exist: `App.ReconnectVoice()`, which tears down and re-dials the voice session via the existing `startVoiceSession` path. Control already has `App.Reconnect()`.

Note that voice also recovers on its own — `voice.Session`'s ladder re-HELLOs and, failing that, re-dials on a fresh socket every 15s indefinitely. `ReconnectVoice` exists so the user can force the attempt now rather than waiting out the current rung.

---

## 7. Connection SFX (D8)

Two new slots in `internal/audio/assets/manifest.toml`:

```toml
[connect]
label = "Connect"
file = "connect.wav"
order = 8

[disconnect]
label = "Disconnect"
file = "disconnect.wav"
order = 9
```

Played on control link transitions into `connected` and into `disconnected`, gated on `general.play_connection_sounds` — which is persisted, exposed in Settings, and read by nothing today.

**It ships silent.** The two WAV files do not exist, adding to the seven already outstanding from Phase 4. Per Phase 4's D11 the engine is asset-agnostic: a missing sample is logged once and plays silence, never a crash. The wiring is correct and testable now and goes audible the moment the samples land.

---

## 8. Proto gap

A new `docs/PROTO_GAPS.md` entry, #10 — **server identity and region**.

The prototype's status bar renders a server display name (`vanguard-prime`) and a region. Neither exists in `srs.proto`. The client renders `host:port` from `server_url` for the former and omits the latter.

Suggested shape, for the server team's queue:

```proto
message ServerSyncResult {
  // ...
  string server_name = 7;   // NEW — human-readable display name
  string server_region = 8; // NEW — e.g. "eu-central"
}
```

Not a blocking dependency: the pill is fully functional with a host address, and Phase 9 needs per-voice-host names anyway, so this is best negotiated once alongside `DistributionUpdate`.

---

## 9. Server-side behaviour this phase makes visible

Two behaviours were read out of the server and are **not** fixed here. Both become observable for the first time because G1 stops being swallowed.

**A fast reconnect can race into a silent no-updates state.** `SubscribeToUpdates` rejects a second subscription for the same `clientID` with an error (`srs/srs_service.go:383–389`), and the previous stream's cleanup is deferred on its context being done (`:391–395`). `Session.Reconnect` cancels the old stream and immediately opens a new one, so the window is real. Today that error vanishes into `_ =`; after this phase it surfaces as `disconnected`, which is honest but means a user could see a reconnect bounce. **Whether it actually fires needs a real server** — it goes on the manual checklist (§10.3).

This is also the strongest argument for D1's manual reconnect. An automatic reconnect loop hitting this race would retry into it repeatedly.

**Ghost clients are plausible after a real drop.** The server removes a dead stream's client from `s.streams` but not from `serverState.Clients` (`:391–395`), so the roster may retain a client whose control link is gone. `Reconnect` reuses the same token and client GUID, so the entry should be overwritten rather than duplicated — but that is reasoning from the code, not an observation. Also on the manual checklist.

---

## 10. Testing

### 10.1 Go

`internal/connhealth` under an injected clock, with fake `Pinger` and `RTTSource`: the 3-failure threshold, the `healthy` boolean's independence from link state, `RTT() == 0` mapping to unknown rather than zero, and `Available` transitions.

`internal/session`: that a stream terminating unexpectedly emits `disconnected`; that a stream terminating because *we* cancelled it does **not**; that a stale generation's goroutine cannot emit over a fresh connection; that `PingOnce` failures accumulate and reset correctly.

`internal/grpctest/fakeserver.go` already implements `Ping` and a `CloseStream()` documented as "simulates a drop" — the exact seam both suites need, with no new fixture.

Toolchain constraints, all verified:

- Every Go invocation carries `-tags purego`.
- The sandbox blocks `bind(2)`, so anything standing up a real listener needs `dangerouslyDisableSandbox: true`.
- The default `GOCACHE` is blocked; use `GOCACHE=$TMPDIR/vcs-gocache`.
- `gopls` is unreliable in this repo — trust `go build` / `go vet`.

### 10.2 Frontend

New: `ConnBanner.test.tsx`, `StatusBar.test.tsx`, a `useConnection` store suite, and a `MainApp`-level test covering banner appearance on a `connection:state` event.

**StrictMode is mandatory for the two components.** Both subscribe to Wails events inside an effect whose cleanup calls the unsubscribe function — a side-effecting cleanup, which is precisely the shape that broke keybind capture through two phases and was only fixed in #30. Both window roots render inside `React.StrictMode`, so a bare-rendered test does not exercise the mode the app runs in.

Each of the two components therefore gets:

- a test rendering **inside `StrictMode`** proving events still arrive after the simulated unmount/re-mount, and
- a **control** proving a real unmount unsubscribes exactly once.

Typecheck with `(cd frontend && npx tsc --noEmit)`. `npx --prefix frontend tsc --noEmit` prints a help banner and exits 0 without checking anything. `frontend/bindings/` is gitignored and goes stale across branch switches; missing methods on the App bindings are staleness, not code errors — regenerate with `wails3 generate bindings -ts -f "-tags purego" -clean=true`.

### 10.3 Manual — this phase's honest gap

**Nobody has ever watched this client lose a control connection to a real server.** Phases 3, 3.5, 4 and 5 are all code-complete and never field-verified, and Phase 5 has never been heard by a human at all. This phase inherits that and adds to it: the 5s × 3 threshold, the half-open detection claim, and the ~70s transport floor in §3.3 are **designed from the server's source, not measured**. Voice RTT has never been read by a human.

A manual checklist lands with the implementation at `docs/superpowers/plans/2026-09-26-phase-6-manual-verification.md`, covering at minimum:

- Pull the network cable with a live session — does the banner appear within ~15s, and does the pill show control alert before voice, or the reverse?
- Restart the server under a live client — banner, then successful manual reconnect.
- The `SubscribeToUpdates` "already subscribed" race (§9): reconnect fast, repeatedly, and watch for a bounce.
- Ghost clients in the roster after a real drop (§9).
- Half-open: suspend the machine or drop the link with a stateful firewall, and confirm the ping detector fires at ~15s rather than the transport's ~70s.
- Real control RTT and real voice RTT rendering in the pill, against a real server, read by a human.
- A Windows build confirming the `unavailable` voice rendering (§6.3).

Until that checklist has been run, Phase 6 is code-complete, not field-verified.

---

## 11. Scope

**In:**

- `internal/connhealth`: the derived dual-plane model, its ticker, and its change hook
- `internal/session`: stream-termination reporting with intent and generation guards; the ping ticker; `grpc.WithKeepaliveParams` in `dial.go`
- `internal/app`: `connection:state` emission, `GetConnectionState()`, `ReconnectVoice()`
- `internal/events`: the `connection:state` name and payload types
- `internal/audio`: two manifest slots, played on control transitions, gated on the existing setting
- Frontend: `useConnection` store; `voice:state` added to `EV`; `StatusBar` dual-pill; `ConnBanner`'s three variants and its reconnect UX; Server Network as `Placeholder`
- `docs/PROTO_GAPS.md` entry #10

**Out:**

- **Automatic reconnect** (D1) — the ROADMAP scopes manual for this phase
- **Real distributed-mode UI** — Phase 9 owns `DistributionUpdate`, multi-host pings and the Server Network screen. This phase renders the dual-pill in its standalone form and routes its click to a placeholder
- **Routing hotkey-registration failures to notifications** — Phase 7 owns that; the Phase 3 inline banner stays where it is
- **TLS** — Phase 7 (spec risk R5). `dial.go` still fails closed on non-localhost hosts, so this phase is only exercisable against a local server
- **Fixing the two server behaviours in §9** — cross-repo, and neither is confirmed to fire
- **Supplying the connection SFX samples** — an asset dependency, not code
- **The Phase 10 SonarCloud backlog** — the PR gate evaluates only new code

---

## 12. Risks

| # | Risk | Severity | Mitigation |
|---|---|---|---|
| R1 | The 5s × 3 threshold is guessed, not measured. Too tight on a lossy link means spurious `disconnected` banners; too loose means a slow surface | M | It is the interval already persisted as this client's default and the exact budget the voice plane uses, so the two planes agree. `PingIntervalSeconds` is user-configurable, so a bad default is tunable rather than baked in. §10.3 puts it on the manual checklist |
| R2 | Surfacing G1 makes the §9 `SubscribeToUpdates` race visible as a reconnect bounce that users will report as a new bug | M | It is pre-existing and currently silent; making it visible is the point. Documented here and on the checklist so a report is recognised, not re-diagnosed. D1's manual reconnect keeps it from looping |
| R3 | The generation guard on the stream goroutine is the same class of concurrency bug that took several iterations to get right on the voice side | M | Follow `internal/app/voice.go`'s established pattern rather than inventing one, capture-before-scheduling included. `go test -race` is already mandatory in CI |
| R4 | `Available: false` renders as the normal state on Windows release builds, and could be mistaken for this phase breaking voice | M | §6.3 states it explicitly. The real fix is Phase 5's open issue #1 and is the user's call, not this phase's |
| R5 | `connection:state` at 0.2 Hz × N windows adds event-bus traffic, and every window re-renders its status bar on each tick | L | 0.2 Hz across a handful of windows is negligible next to `audio:vu` at 20 Hz. If it proves otherwise, the `audio:vu` suppress-when-unchanged precedent applies directly |
| R6 | The client keepalive at 75s is close enough to the server's `MinTime: 60s` floor that clock skew or scheduling delay could still trip `too_many_pings` | L | 25% margin, and gRPC's enforcement tolerates a small number of early pings before a `GOAWAY`. The app-level `Ping` is the detector regardless, so losing the backstop degrades nothing the surface depends on |

---

## 13. Dependencies

**Blocking:** none. Phase 5 is merged (`#28`), and every input this phase needs is already in the tree or in the server as read.

**Not blocking, but relevant:**

- The connection SFX samples (§7) — the phase ships silent without them
- Phase 5 issue #1, `CGO_ENABLED=0` on Windows release builds (§6.3)
- PROTO_GAPS #10, server name and region (§8) — the pill works without it

---

## 14. Definition of Done

1. An unexpected control-stream termination emits `disconnected`; a termination we caused does not; a stale generation cannot emit over a fresh connection. Proven by test.
2. The ping ticker runs for the life of a connection, renders a live RTT in the status bar, and feeds `last_rtt_ms` to the server. **Master spec §6 DoD 9 is closed.**
3. Three consecutive ping failures declare `disconnected`; one or two set `healthy = false` without changing link state.
4. `dial.go` sets client keepalive parameters compatible with the server's `MinTime: 60s` enforcement policy.
5. `internal/connhealth` produces a correct `Snapshot` under an injected clock, with no sockets, under `-race`.
6. `connection:state` is emitted on every tick and immediately on every transition; `App.GetConnectionState()` hydrates a late-opening window.
7. `voice:state` is subscribed in the frontend; voice RTT and the `unavailable` state render.
8. The dual-pill renders both planes with correct dot derivation per §6.1, against a real local server.
9. All three `ConnBanner` variants render for their respective states; the reconnect button shows in-flight state and surfaces a failure's reason; `ReconnectVoice` works.
10. Connection SFX slots exist, are gated on `play_connection_sounds`, and log-once-play-silence with no sample present.
11. `ConnBanner` and `StatusBar` each have a StrictMode test **and** a real-unmount control, per §10.2.
12. `go build`, `go vet`, `go test -race ./...` (all with `-tags purego`), frontend `vitest`, `tsc --noEmit` and production build are all green.
13. `docs/PROTO_GAPS.md` #10 and the ROADMAP Phase 6 row are updated; the manual checklist is written to `docs/superpowers/plans/2026-09-26-phase-6-manual-verification.md`.
14. The phase is reported as **code-complete, not field-verified**, with §10.3's checklist outstanding.
