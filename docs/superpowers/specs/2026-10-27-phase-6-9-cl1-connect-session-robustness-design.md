**Status: draft for approval** (pass 2 of 2). Q1-Q4 are resolved by user decision (2026-10-27), plus the SV2 C13 decision. The plan is `docs/superpowers/plans/2026-10-27-phase-6-9-cl1-connect-session-robustness.md`.

# Phase 6.9 / CL1: Client connect & session robustness

**Repo:** `vcs-srs-client`. Branch `chore/cleanup-cl1`, cut from `origin/main`. That is `54ab0fb` at pass 2: `0fff657` plus #48, which touched only `.github/actions/linux-gui-deps`, `go-deps.yml`, `release.yml`, `test.yml` job/step timeouts (lines 227+, 284+, 304+) and `internal/buildcheck/ci_hardening_test.go`. No file CL1 edits outside `test.yml` moved. Every `file:line` below still holds, and `test.yml`'s pin (`:46`), comments (`:49-68`) and gqlgen line (`:173`) are unchanged.
**Umbrella:** `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md` §3 CL1, §5, §6, §8, §9, §10.
**Builds on the server (merged):** `vcs-srs-server` `origin/main` `dcce3add91e8fe3e2d23f850899b36a4da880730` (SV3 #226, SV5 #225, SV1 #229). CL1 codes against **SV1 design §11 (the SV1 → CL1 table C1-C12)**, plus **SV2's C13** (`ResourceExhausted`, by user decision). SV2 is not merged, so C13 is tested against the in-process fake only.

## Decisions

| # | Decision | Source |
|---|---|---|
| D1 | Post-dial sequence (InitAuth → bcrypt → GuestLogin → SyncClient; Reconnect: SyncClient) bounded at **15 s**. The dial keeps its own **10 s** cap. `App.Disconnect` bounded at **2 s**. Radio pushes (`App.UpdateRadioInfo`, `pushPersistedRadios`) bounded at **5 s**. | user decision Q1 (A) |
| D2 | Session ended → Welcome **manual** stage, address prefilled, message in the `role="alert"` error slot. Copy: kicked "You were removed from the server. Log in again."; banned "You are banned from this server."; any other reason "Your session has ended. Log in again." | user decision Q2 (A) |
| D3 | CANCEL aborts dial + post-dial at once, closes the conn, returns to the filled manual form with **no** error. If `GuestLogin` had already succeeded, a best-effort `Disconnect` bounded at 2 s is sent first. | user decision Q3 (A) |
| D4 | New top-level config key `last_server_address`, written **only** after a successful connect, read through a new `GetLastServerAddress` binding (added to `TestBoundSurfaceIsTheFrontendAPI`). The password is never stored. `server_url` is untouched. | user decision Q4 (A) |
| D5 | FFID label **"FFID"**, placeholder **"e.g. VG12"**. | user decision Q4 (A) |
| D6 | A stream ending `codes.ResourceExhausted` ("update stream fell behind: resubscribe and call SyncClient") is **not** a session loss. Resubscribe on the same connection, then `SyncClient`, replacing the roster (`ResetRoster`). | user decision (SV2 Q1 / C13) |
| D7 | CI re-pin to server `dcce3add91e8fe3e2d23f850899b36a4da880730` + gqlgen in-module invocation + comment rewording. **CI GATE**, approved at plan approval. | user decision |
| D8 | CL3's `reportAction` calls stay. The `TestTLS_UntrustedCertificateIsRefused` flake fix is in scope. CLAUDE.md's Welcome rules hold (two stages, prototype classNames/layout, disabled SSO). | user decision |
| D9 | Stream `Aborted` on the current generation (C6) → ordinary control loss (WARN, ConnBanner), never `logged_out`. | worker's call (pass 1), unchanged |
| D10 | Flake fix uses a non-blocking `DialUntilFailure` helper, not a longer timeout. | worker's call (pass 1), unchanged |
**Builds on the client (merged):** CL3's `App.reportAction` (`internal/app/notify_actions.go`), `useClientStateSync`, hydrate helpers; CL2's `persistCfg` helper and `TestBoundSurfaceIsTheFrontendAPI` (`internal/app/surface_test.go:43`).

## 0. Baseline (fresh detached worktree at `0fff657`, removed afterwards)

`buf generate` + placeholder `frontend/dist/index.html`; `go build -tags purego ./...` ok; `go vet` on `session`, `control`, `state`, `app`, `config` ok; `go test -race -count=1 -tags purego` on the same five packages: all `ok`. Frontend on `0fff657`, from the post-Wave-2 main run (`mains-after-wave2.log`): vitest 50 files / 580 tests passed. `TestTLS_*` × 40 with `-race -cpu 1` in isolation: all passed, so the flake did **not** reproduce in isolation (see §3.10).

## 1. Intent

Make the client's connect lifecycle bounded, cancellable, single-instance and honest about the server's session semantics. Every connect or reconnect ends within a known time. The user can cancel it. A failed connect says so. A second connect cannot leak the first one. Logout really forgets the session. A session the server has ended (SV1: removed, kicked, banned, restarted, token expired) takes the user back to Welcome with a message, instead of leaving a "connected" UI with dead voice. The Welcome screen gets the four approved small changes, and the prototype layout, the two-stage UX and the disabled SSO button stay as they are.

## 2. Re-verification (on `origin/main` `0fff657`; gopls not used)

| Finding | Status | Evidence (file:line on `0fff657`) |
|---|---|---|
| **GO-S5** | holds-with-correction | `internal/app/bindings.go:26` `ctx := context.Background()` → `a.sess.Connect`. `internal/session/session.go:240-272` dial → `InitAuth` → `GuestLogin` → `SyncClient`, all on that ctx. Reconnect: `bindings.go:63` and `session.go:332-338`. Only the dial is bounded (`dial.go:51` `dialTimeout = 10s`, `:148-166` `boundedDial`). Welcome `Welcome.tsx:135-146` has no cancel. **Correction:** "subscribe" is **not** part of the synchronous sequence. `startStreamLocked` (`session.go:105-112`) opens `SubscribeToUpdates` on a goroutine after Connect has returned. Its failures arrive through `handleStreamEnd`, so the bound covers dial + InitAuth + GuestLogin + SyncClient only, and the stream keeps its own uncancelled ctx. |
| **GO-03** | holds-with-correction | `bindings.go:52` `a.sess.Disconnect(context.Background())`, with `session.go:306-307` running `cc.Disconnect(ctx)` before `conn.Close()`. `bindings.go:107` `UpdateRadioInfo(context.Background(), …)`. **Correction (location moved):** `pushPersistedRadios` now lives at `internal/app/voice.go:994` (RPC at `:1017`). Its callers are `bindings.go:42` and `:67` (Background ctx from Connect/Reconnect) and `profiles.go:203` (`context.Background()`). The quit path stays bounded (`app.go:272` `shutdownDisconnectTimeout = 2s`, `:326`). |
| **GO-01** | holds | `session.go:229` publishes `ConnReconnecting`. None of the error returns at `:234-271` publishes again. Reconnect (`:327-341`) publishes `ConnDisconnected` on each failure. |
| **GO-02** | holds | `session.go:283-288` installs `s.conn, s.control, s.cancel` without cancelling or closing the previous ones. Reconnect does both (`:346-351`). No lifecycle mutex. |
| **GO-10** | holds | `Disconnect` (`session.go:296-317`) never clears `lastDialer`/`lastToken`. `Reconnect` reads them at `:324-326`. |
| **CON-07** | holds | `control/client.go:51-56` upserts only. `Disconnect` calls only `s.st.ClearSelf()` (`session.go:312`). Also found: the voice secret survives logout (`store.go:233-243`, nothing clears it). Folded in below. |
| **SRV-06 (client half)** | holds | `control/client.go:63` `st.SetVoiceCredentials(data.GetVoiceSecret(), …)` runs unconditionally, so an empty secret overwrites a good one. `Session.Reconnect` (`session.go:337-342`) treats any `SyncClient` error as an ordinary failure. Since SV1 (server `control/server.go` `sessionExists`), an unknown GUID gets `Unauthenticated` "session not found for …: log in again" (C1). |
| **SRV-07 (client half)** | holds | No client code inspects a gRPC status: `git grep "codes\.\|status\.Code\|status\.FromError" -- internal/*.go` on non-test files has no hits. An expired or removed session reaches the user as a raw string in ConnBanner's failure slot, while the main UI stays "connected". |
| **FE06** | holds | `Welcome.tsx:17` `UNIT_ID_PATTERN = /^[A-Z0-9]{2,4}$/`; `:183` `label="FFID (optional)"`. The prototype `design/vcs/project/screens/welcome.jsx:184` has the same label. Copy decided: D5. |
| **FE18** | holds | `Welcome.tsx:62` `useState("localhost:5002")`. No `<form>`: CONNECT is an `onClick` (`:207`), so Enter does nothing. `cfg.ServerURL` is never written (`bindings.go:30-38`, `main.go:153-159`). |
| **FE14 (Welcome half)** | holds | `Welcome.tsx:204-206`: the error `div` has no `role`/`aria-live`. The toast half was done in CL3. |
| **Welcome CANCEL** | holds (absent) | `Welcome.tsx:135-146`: the connecting panel has no action, and the form is hidden while `phase === "connecting"`. |
| **`TestTLS_UntrustedCertificateIsRefused` flake** | holds (mechanism identified) | `internal/session/tls_integration_test.go:30-35` `refusalCtx` = **500 ms**. `grpctest.StartWith` dials with `WithReturnConnectionError` (blocking, `fakeserver.go:194-202`). Recorded failure (CL4 run, `cl4-gotest.log:20-21`): `got: context deadline exceeded` after 3.44 s under the full `-race` suite. The first handshake had not completed inside 500 ms, so there was no "last connection error" to return. It did not reproduce in isolation (40/40). The same 500 ms pattern is in `TestTLS_HostnameMismatchIsRefused` (`:94`) and `TestTLS_ClientAgainstPlaintextServerFails` (`:115`), so both are latent flakes. |
| **CI server pin (CL5 hand-off)** | holds | Client `.github/workflows/test.yml` checks out `FPGSchiba/vcs-srs-server` at `ref: c3096ce6acb421d2e530e0238471dacdf1b1128e` (Go 1.25). Server `main` is now `dcce3add91e8fe3e2d23f850899b36a4da880730`. **Correction:** this is more than a SHA bump. See §3.11. |

Nothing is refuted or already fixed.

## 3. Design

### 3.1 Session lifecycle shape (GO-02, GO-S5, CANCEL)

- New `Session.lifeMu sync.Mutex` serialises `Connect`, `Reconnect` and `Disconnect` with each other. `s.mu` stays the short field lock it is today. Nothing takes `lifeMu` while holding `s.mu`.
- New `s.attemptCancel context.CancelFunc`, held under `s.mu`. Connect and Reconnect derive `attemptCtx, cancel := context.WithCancel(ctx)` and publish `cancel` there before they dial. **`Session.CancelConnect()`** takes only `s.mu` (never `lifeMu`, or it would wait behind the attempt it wants to cancel) and calls it.
- **GO-S5 bound:** the post-dial sequence `InitAuth → bcrypt → GuestLogin → SyncClient` (Reconnect: `SyncClient`) runs on `context.WithTimeout(attemptCtx, postDialTimeout)`. The dial keeps its own 10 s bound (`boundedDial`), derived from `attemptCtx`, so CANCEL also aborts a dial in progress. The update stream gets a fresh `context.WithCancel(context.Background())`, as today, so it never inherits the timeout.
- On a deadline: close the conn and return the sentinel **`ErrServerNotResponding`** (wrapping the RPC error), with an actionable message. Copy (proposed, confirmed at plan approval, see the plan's new question): "The server accepted the connection but did not answer within 15 s. Check the address, or try again later." On a cancel: close the conn and return **`ErrConnectCancelled`** (D3). If `GuestLogin` had already returned a token, a best-effort `control.Client.Disconnect` on a fresh `context.WithTimeout(context.Background(), cancelDisconnectTimeout)` runs before `conn.Close()`. The frontend shows no error for a cancel. It decides that from its own "CANCEL was clicked" ref, not from the error text.
- **GO-02:** under `lifeMu`, Connect first tears down any live connection exactly as Reconnect does today (bump `streamGen`, cancel the stream ctx, close the conn), and only then dials. `Disconnect` calls `CancelConnect()` **before** it takes `lifeMu`. A Logout during a connect therefore aborts the connect instead of waiting for it.
- **GO-01:** Connect gets a named `err` return and a deferred `if err != nil { s.setConnState(events.ConnDisconnected) }`. Every failure path, including cancel and timeout, ends in `disconnected`.

### 3.2 Per-call timeouts (GO-03)

Constants live in `internal/app` beside `shutdownDisconnectTimeout` (D1, user decision): `disconnectTimeout = 2 * time.Second`, `radioPushTimeout = 5 * time.Second`. In `internal/session`: `defaultPostDialTimeout = 15 * time.Second` (overridable through `Deps.PostDialTimeout` for tests) and `cancelDisconnectTimeout = 2 * time.Second` (D3).
- `App.Disconnect` → `context.WithTimeout(…, disconnectTimeout)`. `Session.Disconnect` already does its local teardown regardless of the RPC result. The order (cancel the stream ctx, then the `Disconnect` RPC, then `conn.Close()`) is kept, because C12 depends on the stream ctx being cancelled before the server ends the stream.
- `App.UpdateRadioInfo` → `radioPushTimeout`. **CL3's two deferred/inline `reportAction(actRadioSave, …)` / `reportAction(actRadioSend, …)` calls stay**, in the same order (CL3 R1).
- `pushPersistedRadios` derives its own `radioPushTimeout` ctx internally, so its three callers (`bindings.go:42,67`, `profiles.go:203`) need no change. **CL3's `ResetLayout` reportAction is untouched.**

### 3.3 Logout forgets the session (GO-10, CON-07)

- `Session.Disconnect`, and the session-ended path in §3.4, clear `lastDialer`/`lastToken`. `Reconnect` then returns "cannot reconnect: never connected" (test).
- New `Store.ResetSession()` (worker's call on the name) clears clients, radios **and the voice credentials**, then notifies the radio observers once. Self is still cleared by `ClearSelf`. It is called from Disconnect and from the session-ended path. **Voice is stopped before the credentials are cleared**: App's voice stop runs first (§3.5), so `handleVoiceRedirect` sees `live == false` and does not redial with an empty secret.
- New `Store.ResetRoster()` clears clients and radios only. `control.Client.SyncClient` calls it immediately before hydrating, so Reconnect and re-Connect drop ghosts. The frontend already replaces its roster on re-hydrate (`useClientStateSync` `hydrateDelta` on `connected`), so Go and the frontend now agree (CL3 §11).

### 3.4 Session ended (SRV-06 / SRV-07 client half, against SV1 §11)

One classifier, `classifyRPCError(err) sessionVerdict`, in `internal/session`. It branches on `status.Code` and never on message text:

| Code / situation | SV1 row | Verdict |
|---|---|---|
| unary `Unauthenticated` on SyncClient, Ping, UpdateRadioInfo, Disconnect | C1, C2 | **session ended** (reason `expired`) |
| unary `PermissionDenied` | C3 | **session ended** (umbrella §3 CL1: "map Unauthenticated/PermissionDenied"). Unreachable for a guest today, because every SRSService method is `GuestRole` (server `utils/auth.go`). |
| `Internal` | C4 | **not** a session loss: an ordinary failure, as today |
| stream ends `Unauthenticated` with the stream ctx **not** cancelled | C5, C7 | **session ended**. The reason is taken from the message suffix after `"session ended: "` (`kicked`, `banned`, `stale`, `stream lost`, `disconnected`), else `expired`. The suffix is used only for the copy; the branch is on the code. |
| stream ends `Aborted` | C6 | **not** a session loss and **not** "session expired". A stale generation is ignored, as today. On the *current* generation it can only mean another subscriber used our token. It is logged at WARN and treated as an ordinary control-link loss (`markControlLost` → ConnBanner RECONNECT), never `logged_out`. *(worker's call)* |
| stream ends `ResourceExhausted` with the stream ctx not cancelled, on the current generation | SV2 C13 (D6) | **not** a session loss. **Resync** (§3.12). |
| stream ends after the user's Disconnect | C12 | ignored, because `handleStreamEnd`'s existing `ctx.Err() != nil` guard fires first. Pinned by a test. |
| `SyncClient` returns success with `voice_secret == ""` on **Reconnect** | SRV-06 (umbrella) | **session ended** (`expired`). The SV1 server only does this when a removal races the RPC. |
| `SyncClient` with `voice_secret == ""` on any path | — | the stored secret is **never** overwritten with `""`. The same rule as `VOICE_ADDRESS_UPDATE` (`stream.go:72-83`): addresses are updated and the secret is kept. |

**`Session.endSession(reason)`** is the one place that reacts. Exactly once per generation (the same guard as `markControlLost`), it:
1. tears down the stream and conn;
2. clears `lastDialer`/`lastToken`;
3. calls the new `Deps.OnSessionEnded(reason)` hook, which App wires to `stopVoiceSession`;
4. calls `ResetSession` + `ClearSelf`;
5. publishes `ConnDisconnected`;
6. emits a new event **`auth:session_ended`** with `{reason}`, then the **existing `auth:session_changed` = `logged_out`**.

Because of step 6, MainApp's existing handler routes to Welcome, and `useClientStateSync` clears every window (CL3 §11 contract), with no change to either hook.

It is reached from:
- `handleStreamEnd` (C5/C7);
- `Reconnect`'s `SyncClient` (C1, empty secret), which returns sentinel `ErrSessionEnded` so ConnBanner gets an error and the window is already on Welcome;
- `PingOnce` (C1). It runs with no connhealth lock held (`connhealth/monitor.go:90-113`), so a direct call is safe. The probe then returns the error and the monitor's failure counter is moot;
- `UpdateRadioInfo` / `pushPersistedRadios` (C1).

For the CL3 notification: a session-ended error passed to `reportAction` **resolves** the key instead of raising it (a small `actionErr` filter inside `reportAction`, or at the call site; worker's call). The session-ended surface has already told the user, and a stale "not sent to the server" toast after re-login would be noise.

**Frontend:**
- `useSession` gains `sessionNotice: string | null` (`setSessionNotice`). A new hook `useSessionEndedNotice()` (mounted by MainApp) subscribes to `auth:session_ended` and sets the notice through `sessionEndedMessage(reason)` (D2, user decision):
  - `kicked` → "You were removed from the server. Log in again."
  - `banned` → "You are banned from this server."
  - anything else → "Your session has ended. Log in again."
- Welcome renders the notice in the same `role="alert"` error slot. While a notice is present, Welcome mounts directly in the **manual** stage, with the remembered address prefilled. The two stages and their layout are unchanged; only the initial stage differs (D2).
- The Go payload is `events.SessionEndedPayload{Reason string \`json:"reason"\`}`.
- The notice is cleared on the next CONNECT or BACK.

### 3.5 App wiring

`App.Connect` / `App.Reconnect` keep their shape and their CL3 calls:
- `App.Connect`: on success, persist the remembered address (§3.7), `health.SetServer`, `pushPersistedRadios`, `startVoiceSession`.
- **New binding `App.CancelConnect()`** → `a.sess.CancelConnect()`, added to `sessionAPI`.
- **New binding `App.GetLastServerAddress() string`** (D4).
- The session-ended hook is wired in `app.SetBackend`, the same optional-interface pattern as `SetGeometryObserver`: if `sess` implements `SetSessionEndedHook(func(reason string))`, SetBackend installs `func(string) { a.stopVoiceSession() }`. `main.go` is unchanged. The hook is called outside every `Session` lock.
- `TestBoundSurfaceIsTheFrontendAPI` gains `CancelConnect` and `GetLastServerAddress`. `frontend/src/shared/api/client.ts` gains `cancelConnect` / `getLastServerAddress`, and `EV.authSessionEnded = "auth:session_ended"` is added.

### 3.6 Welcome CANCEL

- A `<Button variant="ghost" size="sm">CANCEL</Button>` in the connecting panel, in the existing `.row` with the NEGOTIATING pill (`.row between acenter`, the BACK-button pattern from the manual stage). No new classNames.
- Click → set `cancelledRef.current = true`, then `api.cancelConnect()`. The in-flight `api.connect` then rejects with the cancelled sentinel. Welcome returns to the manual stage with the form intact and **no** error text (D3, user decision).

### 3.7 FE18: Enter submits; the server address is remembered

- The manual stage's root `<div className="col gap-5" style={{ padding: 28 }}>` becomes `<form className="col gap-5" style={{ padding: 28 }} onSubmit={…preventDefault; connect()} noValidate>`. CONNECT gets `type="submit"`. **BACK gets `type="button"`**, because inside a form the default is submit. Same classNames and style; `.col` is a class selector, and no CSS rule targets the element type (`components.css` checked).
- **Persisted in Go on a successful Connect only**, through CL2's `persistCfg`. The password is never written; a test asserts that the saved `config.toml` does not contain it. A persist failure is logged at WARN and never fails the connect. Read on Welcome mount through `GetLastServerAddress`; an empty answer or a rejected promise keeps the fallback `localhost:5002` (today's default). A value the user has already typed is never overwritten by the late answer. The key is `last_server_address` (`Config.LastServerAddress`, top level next to `server_url`, default `""`) (D4, user decision).

### 3.8 FE06: FFID label

The label text becomes **"FFID"** and the input gains `placeholder="e.g. VG12"` (D5, user decision). Field, validation and `unitIdError` copy are unchanged.

### 3.9 FE14 (Welcome half)

The error `div` gets `role="alert"` (implicit `aria-live="assertive"`, `aria-atomic`). The session notice from §3.4 renders in the same element.

### 3.10 `TestTLS_UntrustedCertificateIsRefused` flake (worker's call)

Do not lengthen the 500 ms to N seconds: with a blocking dial, each refusal test would then always take the full N. Instead, add a `grpctest` helper `DialUntilFailure(t, f, serverCreds, clientCreds) error`. It:
1. creates a non-blocking client (`grpc.NewClient` over the same bufconn dialer) and calls `conn.Connect()`;
2. waits with `WaitForStateChange` (bounded by a generous 10 s ctx) until `TransientFailure`;
3. issues one RPC **without** wait-for-ready. That RPC fails at once with `Unavailable` and a message that carries the last connection error ("…x509: certificate signed by unknown authority").

The helper returns as soon as the handshake fails, however loaded the machine is. All four `refusalCtx` users move to it (pass-2 recount: `TestTLS_PlaintextServerProducesTheExplainedError` at `:141` is the fourth; it feeds the helper's error into `explainDialError`, so the Unavailable message must still carry "first record does not look like a TLS handshake"). They and keep their exact substring assertions. `refusalCtx` goes away. RED/GREEN for the flake itself is not deterministic, so the plan pins it with `-race -count=50` of the three tests, alongside a parallel `go test ./...` load run. Production `boundedDial` is unchanged, because its 10 s bound is long enough.

### 3.11 CI: re-pin the server checkout (CL5 hand-off; CI GATE, D7)

- `test.yml`: `ref: c3096ce6acb421d2e530e0238471dacdf1b1128e` → **`ref: dcce3add91e8fe3e2d23f850899b36a4da880730`** (server `main` = merge of #229 "chore/cleanup-sv1", which contains SV3, SV5 and SV1).
- **Required alongside it (approval-gate item, beyond the plain SHA bump):** at the new SHA the server is Go 1.27.0 / toolchain go1.27.3, and gqlgen is a `tool` directive with `golang.org/x/tools v0.46.0`. The client step `go run github.com/99designs/gqlgen@$(go list -m …) generate` builds gqlgen against **gqlgen's own** go.mod (x/tools v0.42.0), which fails under Go 1.27 (server `.github/actions/generate/action.yml` comment). It must become **`go run github.com/99designs/gqlgen generate`** (in-module, no `@version`), exactly as the server's own composite action does. The server's composite action itself cannot be reused, because it is Linux-X64-only and our matrix includes macOS and Windows.
- The "TWO TOOLCHAINS" and "No toolchain assert here" comments are reworded: the server now has a `toolchain` line. The two-setup-go structure stays (no structural CI change). protoc 36.2 and protoc-gen-go-grpc v1.6.2 already match the server's pins.
- Verified only by CI on the PR, across all three OS legs. Locally: the server at `dcce3ad` builds with `-tags headless` and the client integration suite runs against it (SV1 Task 10 already did this against `b7c902c`: 25/25 ok).

### 3.12 Stream fell behind: resync (SV2 C13, D6 user decision)

`handleStreamEnd`, when the stream ctx is not cancelled, the generation is current and `status.Code(err) == codes.ResourceExhausted`, calls **`Session.resync(gen)`** instead of `markControlLost`:
1. Under `s.mu`, re-check that `gen == s.streamGen` and `s.conn != nil`; capture `cc` and the live stream ctx. If that fails, return.
2. **Resubscribe first.** `cc.Subscribe` (opens `SubscribeToUpdates` and returns the client stream), wait for the C17 ack, and start holding the stream (`openHeldStream`; see the Connect/Reconnect paragraph below). `streamGen` is not bumped yet.
3. **Then `SyncClient`** on `context.WithTimeout(streamCtx, postDialTimeout)`. `SyncClient` calls `ResetRoster` before it hydrates, so the snapshot **replaces** the roster.
4. On success, install the held stream under a new `streamGen`, release it (replaying what it held on top of the snapshot), and publish `ConnConnected` again. `useClientStateSync` rehydrates on every `connected`, the monitor and the SFX gate dedupe a repeated state, and there is no `reconnecting` flash.
5. On failure, `classifyRPCError` decides: session ended → `endSession`; otherwise → `markControlLost(currentGen)` (ConnBanner RECONNECT).

There is no rate cap. The server only sends C13 after more than its buffer size of events stalled behind a slow `Send`, so a resync cannot hot-loop on its own (SV2 R1 owns storm sizing). C13 is not produced by the server at `dcce3ad` (SV2 unmerged), so it is covered by fake-server tests only.

**Subscription ack (SV2 C17, user decision 2026-10-27, after CI exposed the gap).** grpc-go returns from a server-streaming call once the request is sent, so "resubscribe, then SyncClient" was only the client's call order: the server could serve `SyncClient` before registering the new subscriber, losing an event published in between (CI hit this on ubuntu-latest). The server now sends empty response headers (`stream.SendHeader`) right after it registers the subscriber (C17, server PR #230), and step 2 waits on `stream.Header()` (`Session.awaitSubscribed`) before `SyncClient`, which turns the order into a guarantee. The wait is bounded by `postDialTimeout`. A stream that ends without headers is a failed subscribe (step 5). Past the bound, a server that predates C17 (`dcce3ad`) gets a Warn and the pre-C17 behaviour (proceed to `SyncClient`). Such a server sends headers with its first update, which releases the wait early.

**Connect and Reconnect use the same order, and every new stream holds until its snapshot is applied (user decision 2026-10-27, "Fix in CL1 before merge").** Two gaps remained after the resync fix:
- *Gap 1.* Connect and Reconnect called `SyncClient` first and subscribed only once the session was installed, so an update published between the server taking the snapshot and registering the subscription was in neither. Both now subscribe right after `GuestLogin` (Connect) or the dial (Reconnect), wait for the C17 ack (`awaitSubscribed`, same refusal semantics: a stream that ends before any header is a failed subscribe), and only then call `SyncClient`. The ack wait runs inside the existing post-dial bound (`pctx`), so the D1 contract is unchanged: everything after the dial is still one 15 s bound. A refused subscribe fails the attempt the way a failed `SyncClient` did when `SyncClient` was the first call after `GuestLogin`: Connect returns the error (`subscribe to updates: …`, the status code intact; Welcome shows it verbatim, no `session_ended`, no `logged_in`); Reconnect classifies it (`Unauthenticated` ends the session, anything else is `reconnect subscribe: …`). A cancel or timeout maps through `attemptError` as before, so D3's best-effort Disconnect still applies.
- *Gap 2.* The stream reader routed updates as they arrived, so one received before the `SyncClient` answer was applied was wiped by its `ResetRoster`. Every new stream (Connect, Reconnect, resync) is now a `pendingStream`: its goroutine reads into a `control.Held`, which keeps updates in arrival order until the session has applied the snapshot and installed the stream under a new generation; `Held.Release` then replays them on top of the snapshot (on the installing goroutine, after `s.mu` is released, before `connected` is published) and switches to live routing. A stream that ends before Release keeps its error until Release and only then reports it through `handleStreamEnd`, so no stream end is handled before its session exists. An attempt that never installs the stream cancels it (`abandon`, or the connection context), and its goroutine exits without reporting. `streamGen` is bumped only at install, as before.
- *Bound.* The hold keeps at most `pendingUpdateCap` = 256 updates, the server's own per-subscription buffer (`updateStreamBuffer`, SV2). One more is a fell-behind: the reader stops, and after Release the stream ends with `control.ErrHeldOverflow` (`codes.ResourceExhausted`), which `handleStreamEnd` resyncs exactly like the server's C13. Each stream runs on its own child context, cancelled when its reader exits, so an overflowed stream is closed rather than left open beside its replacement.
- *Replay idempotency.* Every update type `route` applies is a full-value overwrite or a single-key delete (`UpdateClient`, `RemoveClient`, `SetRadios`, `SetSettings`, `SetVoiceCredentials`/`SetVoiceAddresses`; `SERVER_ACTION`, `DISTRIBUTION_UPDATE` and unknown types are ignored), so replaying an update the snapshot already reflects leaves the store unchanged, and an in-order replay converges on the server's state. This assumes the server publishes each event after the state change it describes, in order. One side effect is not idempotent: `SetVoiceCredentials`/`SetVoiceAddresses` always notify, and `App.handleVoiceRedirect` redials a live voice session on every notification without comparing values. A replayed `VOICE_ADDRESS_UPDATE` therefore redials once more. That is no new redial (the same update was applied before the snapshot until now; the hold only moves it after), and it only happens when voice is live, which during Connect it never is.

**Cost against a server without the ack (`dcce3ad`, the CI pin).** No update arrives on a fresh subscription on that server unless another client acts, so Connect waits in `awaitSubscribed` until the post-dial bound and then fails with "server not responding". Measured locally: 15.00 s and a failed Connect, twice in a row; the integration suite's `connectGuest` (10 s caller deadline) fails at ~10 s. Against the C17 server (`850d984`, server PR #230 merged) Connect takes 72-88 ms and the voice integration suite passes. **This branch therefore requires the C17 server; the CI re-pin past `dcce3ad` is a user decision and is not part of this change.**

## 4. Files

**Go:**
- `internal/session/session.go`: lifeMu, attemptCancel, CancelConnect, bounds, endSession, classifier use, GO-01/02/10.
- `internal/session/errors.go` (new): sentinels, `classifyRPCError`.
- `internal/control/client.go`: `ResetRoster` before hydrate; never write an empty secret; return a "secret missing" indicator.
- `internal/control/stream.go`: split `ConsumeUpdates` into `Subscribe` + `Consume` (§3.12); behaviour of `ConsumeUpdates` unchanged.
- `internal/state/store.go`: `ResetRoster`, `ResetSession`.
- `internal/events/events.go`: `EventAuthSessionEnded` + `Tagged.SessionEnded(reason)`.
- `internal/app/app.go`: `sessionAPI.CancelConnect`, timeout constants.
- `internal/app/bindings.go`: Connect persists the address, Disconnect/UpdateRadioInfo timeouts, `CancelConnect`, `GetLastServerAddress`.
- `internal/app/voice.go`: `pushPersistedRadios` timeout.
- `internal/app/notify_actions.go`: session-ended filter.
- `internal/config/config.go`: `LastServerAddress string \`toml:"last_server_address"\`` (D4).
- `internal/app/app.go` `SetBackend`: the session-ended hook (no `main.go` change).
- `internal/grpctest/fakeserver.go`: knobs listed in §5.

**Tests (Go):**
- `internal/session/{session,reconnect,liveness,tls_integration}_test.go`, new `sessionend_test.go`
- `internal/control/*_test.go`
- `internal/state/store_test.go`
- `internal/app/{bindings,surface,notify_actions}_test.go`
- `internal/config/*_test.go`

**Frontend:**
- `frontend/src/windows/main/screens/Welcome.tsx` (+ `Welcome.test.tsx`)
- `frontend/src/windows/main/MainApp.tsx`
- `frontend/src/shared/store/session.ts`
- `frontend/src/shared/api/{client,events}.ts`
- `frontend/src/shared/store/useSessionEndedNotice.ts` (+ test), mounted by MainApp

**CI:** `.github/workflows/test.yml` (pin + gqlgen line + comments).

**Docs (in-PR):** a CL1 section appended through the orchestrator to `docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`. CLAUDE.md's "post-dial auth … hangs the Welcome screen forever" sentence is now false and is handed to DOC, not edited here.

## 5. Tests first (each seen failing on `0fff657` first)

`grpctest.Fake` gains these knobs:
- `Block chan struct{}` per RPC (InitAuth / GuestLogin / SyncClient block until it is closed or the ctx is done);
- `SyncErr error`;
- `StreamEndErr error` (SubscribeToUpdates returns it once `updates` closes);
- `DisconnectBlock`;
- `PingErr`, `UpdateRadioErr`.

**Session:**
1. `Connect` against a blocking `InitAuth` returns `ErrServerNotResponding` within `postDialTimeout` + slack (the timeout is injected via `Deps` for test speed), and the conn is closed.
2. The same for a blocking `SyncClient` on `Reconnect`.
3. `CancelConnect` during a blocking `GuestLogin` returns `ErrConnectCancelled` promptly, and the final state is `disconnected`.
4. **GO-01:** for each failure (dial, InitAuth error, no guest, GuestLogin reject, SyncClient error, timeout, cancel), the final emitted `control:connection` is `disconnected`.
5. **GO-02:** a second Connect while live cancels and closes the first. The first stream's goroutine exits, checked with a goroutine-count or "old fake stream ctx done" assertion. Connect racing Reconnect leaves exactly one live conn (`-race`).
6. **GO-10:** after Disconnect, Reconnect returns "never connected" and dials nothing.
7. **CON-07:** a roster with clients A and B, then Reconnect whose `SyncClient` returns only A, leaves the store with A only. Disconnect empties clients, radios and the voice secret.
8. **SRV-06:** Reconnect with `SyncErr = status.Error(codes.Unauthenticated, "session not found for /srs.SRSService/SyncClient: log in again")` emits `auth:session_ended{expired}` + `logged_out`, returns `ErrSessionEnded`, and clears the token. With `Internal`, it is an ordinary failure: no `logged_out`, and the token is kept.
9. Reconnect with success but `voice_secret == ""` → session ended. Connect with `""` leaves the existing secret untouched.
10. The stream ends with `Unauthenticated "session ended: kicked"` → `session_ended{kicked}` + `logged_out`, exactly once, and the `OnSessionEnded` hook is called once.
11. The stream ends with `Aborted "superseded by a newer subscription"` on the current generation → `disconnected`, **never** `logged_out`. On a stale generation → nothing.
12. **C12:** user `Disconnect` while the fake ends the stream with `Unauthenticated "session ended: disconnected"` → exactly one `logged_out` and **no** `session_ended`.
13. `PingOnce` / `UpdateRadioInfo` returning `Unauthenticated` → session ended.
13a. **C13:** the stream ends `ResourceExhausted` → a second `SubscribeToUpdates` is opened **before** the second `SyncClient` (fake records call order), the roster equals the new snapshot (ghost removed), `connected` is published again, and there is no `disconnected` and no `logged_out`. A resync whose `SyncClient` returns `Unauthenticated` → session ended.

**Store / control:**
14. `ResetRoster`, `ResetSession` (radio observers notified once).
15. `SyncClient` calls `ResetRoster` before upserting.

**App:**
16. `Disconnect` returns within `disconnectTimeout` + slack against a fake whose `Disconnect` blocks. The local teardown and `logged_out` still happen.
17. `UpdateRadioInfo` is bounded. A session-ended error resolves (does not raise) `action:radio.send`. The CL3 tests stay green.
18. Connect persists the address on success, not on failure, and never writes the password (scan the saved file).
19. `CancelConnect` / `GetLastServerAddress` are in the bound surface.

**TLS:**
20. The refusal tests use `DialUntilFailure`, with `-count=50 -race`.

**Frontend (vitest):**
- Enter in any manual-stage field calls `api.connect` once. BACK does **not** submit.
- The error has `role="alert"`.
- CANCEL calls `api.cancelConnect`. A cancelled rejection returns to the manual stage with no error.
- The remembered address prefills the server field.
- An `auth:session_ended` → `logged_out` sequence lands on the manual stage showing the notice.
- The FFID label renders the approved copy.
- The SSO button is still `disabled`, and the two stages are still reachable (a regression pin for the CLAUDE.md rule).

## 6. Out of scope

- Token refresh (PROTO_GAPS #12, server side).
- Remembering player name / FFID across sessions or restarts (FE18 names only the server address).
- Bounding `App.ReconnectVoice` (voice dial has its own bounds).
- Closing the Comms popout on logout (CL3 §6 follow-up).
- MainApp Logout's `void api.disconnect()` failure surface. Disconnect can no longer fail visibly: it is bounded and the teardown is unconditional, so it stays fire-and-forget.
- C# peer behaviour.
- Changing `markControlLost`'s documented unreachable race.
- **Observation for the orchestrator (not fixed here):** `startVoiceSession` derives the voice host from `cfg.ServerURL` (`voice.go:718`, `voice/endpoint.go:67-75`) only when SyncClient/`[voice] host` give none. Because `server_url` is never written, a standalone server that returns `""` coalition addresses gets "no voice server address available" unless `[voice] host` is set. D4 deliberately leaves `server_url` untouched, so this stays as it is. It is handed to DOC/ROADMAP as a follow-up.

## 7. User / wire-visible behaviour changes

- Connect/Reconnect give up after `postDialTimeout` with "server did not respond"; before, they could hang forever.
- The CANCEL button.
- Every failed connect ends `disconnected`. The status-bar pill no longer sticks at "reconnecting" after a failed login.
- Logout is bounded (≤ `disconnectTimeout`).
- Logout forgets the token. A stale Reconnect button can no longer resurrect a session.
- The roster no longer shows ghosts after reconnect or re-login.
- **The session-ended path:** after a kick or ban, a server restart, more than 120 s offline, or an expired token, the user is taken to Welcome with a message. Voice is stopped. Before, the UI stayed "connected" with dead voice, or showed a raw RPC error string in the banner.
- Welcome: Enter submits, the server address is remembered, the FFID label changes, and the error is announced to screen readers.
- A stream that fell behind (SV2 C13) resyncs silently (roster replaced, `connected` re-published) instead of showing the RECONNECT banner.
- **Wire:** none. The same RPCs are sent. Only their deadlines are new (a deadline shows up server-side as a cancelled RPC), plus the D3 best-effort `Disconnect` on a cancel after `GuestLogin`. No proto change.
- **Webview-internal:** new event `auth:session_ended`; new bindings `CancelConnect` (and `GetLastServerAddress`).

## 8. Approval-gate items (CLAUDE.md)

- **CI GATE (D7):** `test.yml` server pin `c3096ce…` → `dcce3add91e8fe3e2d23f850899b36a4da880730`, **plus** the gqlgen in-module invocation and the comment rewording. Approved at plan approval; the plan marks its task "GATE — needs user approval before this task".
- User-visible UI copy: FFID label/placeholder (D5) and session-ended messages (D2) are user decisions. The **server-not-responding message** is still proposed copy and is confirmed at plan approval.
- **None of:** dependency change (`google.golang.org/grpc/codes` and `status` are in the already-required grpc module), proto change, file deletion, cross-phase work.

## 9. Risks

- **R1 CL3 rebase.** `UpdateRadioInfo` keeps both `reportAction` lines in order. `ResetLayout` is untouched. CL3's notify tests must stay green unchanged.
- **R2 Lock order.** `lifeMu → s.mu` only. `CancelConnect` and `endSession` take `s.mu` only. `endSession` runs from the stream goroutine, the ping ticker, and RPC callers. Its once-per-generation guard plus `-race` tests 5 and 10-13 cover it. The `OnSessionEnded` hook (voice stop) is called with no Session lock held.
- **R3 Voice redial on reset.** Clearing the credentials while voice is live would trigger `handleVoiceRedirect`. Mitigated by stopping voice first (§3.3), and tested.
- **R4 Status-code reliance.** The branch is on codes only. Server message text is used only for the reason suffix, with `expired` as the fallback, so a server rewording costs copy, not behaviour.
- **R5 CI pin.** The gqlgen in-module change is untested until CI runs on three OSes. Fallback: keep the SHA bump and investigate per leg. Client CI must never point at a server SHA that cannot generate.
- **R6 Bound surface.** UP regenerates bindings later. The surface test is the guard.
- **R7 Field.** None of this is hardware/real-server verified here. Manual checklist additions:
  - CANCEL and timeout against a server that accepts TLS and then stalls (umbrella §8);
  - kick/ban → Welcome with the right message;
  - server restart → RECONNECT → Welcome "session expired";
  - a blip under 120 s reconnects with the session kept;
  - Enter submits in the real Wails webview;
  - the remembered address survives a restart.
- **R9 Snapshot/stream ordering (C13 resync, Connect, Reconnect).** Closed by the SV2 C17 subscription ack plus the client hold (§3.12): every new stream is registered with the server before `SyncClient` is requested, and holds what it receives until the snapshot is applied, replaying it after (bounded at 256; past that, a resync). Residuals: (a) a pre-C17 server. On the resync the ack wait falls back after `postDialTimeout` to the old best-effort order, but on Connect and Reconnect the wait shares the single post-dial bound and the attempt **fails** with "server not responding" after 15 s unless an update arrives first (measured against `dcce3ad`). So the client now requires a C17 server, and CI must be re-pinned before this merges (user decision, open). (b) Replaying a `VOICE_ADDRESS_UPDATE` redials a live voice session once more (§3.12). (c) A C13 storm on a slow link is SV2's R1.
- **R8 CLAUDE.md Welcome rule.** The layout, classNames, two stages and disabled SSO are pinned by test. The only structural change is `div` → `form` on one element with an identical className and style. A before/after screenshot is a manual item.

## 10. Cross-track contracts

- **SV1 → CL1:** coded against the C1-C12 table, mapped in §3.4.
- **SV2 → CL1:** C13 → §3.12 (D6). C14 (roster carries `unit_id`/`role_id`) needs no client change: `SyncClient` already stores the full `ClientInfo`.
- **CL3 → CL1:**
  - `reportAction` lines are kept (R1);
  - session expiry reaches the windows as `auth:session_changed = logged_out` (kept, plus the new `auth:session_ended`), so `useClientStateSync` needs no change;
  - `ResetRoster` aligns Go with the hook;
  - a Connect failure surface via `reportAction` is **not** used for Welcome: Welcome shows no ToastHost, and the inline `role="alert"` error is the surface there.
- **CL5 → CL1:** the server pin bump (§3.11).
- **→ DOC:**
  - CLAUDE.md §"Control-plane TLS": remove "a server that completes TLS and then never answers still hangs the Welcome screen forever" and describe the bound and the CANCEL;
  - the umbrella's CL3×CL1 matrix cell should read "bindings, profiles, voice (pushPersistedRadios)";
  - the "session expired" flow;
  - the remembered-address key.
- **→ UP:** keep the `on()` envelope; the new bindings must survive regeneration.

## 11. Questions (resolved)

All four pass-1 open questions were answered by the user on 2026-10-27. The options they chose are recorded here as **user decision**. The options they did not choose are dropped.

- **Q1: timeout values. User decision: A.** Post-dial 15 s; the dial keeps its 10 s; Disconnect 2 s; radio push 5 s. Worst-case connect is 10 s + 15 s. → D1.
- **Q2: session-ended message and landing. User decision: A.** Per-reason copy:
  - kicked: "You were removed from the server. Log in again."
  - banned: "You are banned from this server."
  - any other reason: "Your session has ended. Log in again."

  Welcome opens on the manual stage with the address prefilled and the message in the `role="alert"` slot. → D2.
- **Q3: CANCEL semantics. User decision: A.** Abort dial + post-dial at once, close the conn, return to the filled form with no error. If login had already succeeded, send a bounded (2 s) best-effort `Disconnect`. → D3.
- **Q4: remembered-address key and FFID copy. User decision: A.** New key `last_server_address`, written only after a successful connect, read via the new `GetLastServerAddress` binding (added to `TestBoundSurfaceIsTheFrontendAPI`). The password is never stored; `server_url` is untouched. FFID label "FFID", placeholder "e.g. VG12". → D4, D5.
- **SV2 C13. User decision.** `codes.ResourceExhausted` "update stream fell behind: resubscribe and call SyncClient" is not a session loss: resubscribe, then `SyncClient`, replacing the roster. → D6, §3.12.

**Still open (confirmed at plan approval):** the copy of the server-not-responding message (§3.1).
