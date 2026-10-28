# Phase 6.9 CL1: Connect & Session Robustness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every connect and reconnect ends within a known time and can be cancelled. Every failure ends `disconnected`. A second connect cannot leak the first. Logout forgets the session. A session the server ended (SV1 C1-C12) takes the user back to Welcome with a message. A stream that fell behind (SV2 C13) resyncs. Welcome gets Enter-to-submit, a remembered address, CANCEL, the new FFID copy and an announced error. The TLS refusal flake is fixed. CI is re-pinned to the SV1 server.

**Architecture:**
- `internal/session` gets a lifecycle mutex, a cancellable per-attempt context, a 15 s post-dial bound and a status-code classifier (`classifyRPCError`).
- One `endSession` path handles "session ended". It stops voice through a hook, resets the store, clears the token, and emits `auth:session_ended` followed by `auth:session_changed=logged_out`.
- `internal/control` gains `Subscribe`/`Consume`, so `resync` can resubscribe before `SyncClient`.
- App adds two bindings (`CancelConnect`, `GetLastServerAddress`) and per-call timeouts.
- The frontend adds one hook (`useSessionEndedNotice`) and Welcome changes that keep the prototype's layout.

**Tech Stack:** Go 1.27.3, grpc-go v1.85.0 (`codes`, `status`, `bufconn`), BurntSushi/toml. React 19, TypeScript, Zustand, Vitest with Testing Library. Wails v3 bindings.

**Spec:** `docs/superpowers/specs/2026-10-27-phase-6-9-cl1-connect-session-robustness-design.md`. Until Task 13 commits it, the spec is the scratchpad `design-CL1.md`. Read the spec's **Decisions table (D1-D10)**, §3 and §5 before starting. Umbrella: `docs/superpowers/specs/2026-10-27-vcs-client-phase-6-9-cleanup-umbrella-design.md` §3 CL1. Server contract: `vcs-srs-server` `docs/superpowers/specs/2026-10-27-phase-6-9-sv1-session-liveness-design.md` §11 (C1-C12), plus SV2 C13.

**Worktree / branch:** a fresh worktree on `chore/cleanup-cl1`, cut from `D:\Projects\vcs-srs-client` `origin/main` (`54ab0fb` or later). Once, before Task 1, from the worktree root:

```bash
buf generate                                   # srspb/
mkdir -p frontend/dist && touch frontend/dist/index.html   # embed placeholder
cd frontend && npm ci && cd ..
```

Every command below runs from the worktree root unless it says `cd frontend`.

## Global Constraints

- **Timeouts (D1, user decision):**
  - `session.defaultPostDialTimeout = 15 * time.Second`;
  - the dial keeps `dialTimeout = 10 * time.Second` (unchanged);
  - `app.disconnectTimeout = 2 * time.Second`;
  - `app.radioPushTimeout = 5 * time.Second`;
  - `session.cancelDisconnectTimeout = 2 * time.Second`.
- **Session-ended copy (D2, exact):**
  - `kicked` → `You were removed from the server. Log in again.`
  - `banned` → `You are banned from this server.`
  - anything else → `Your session has ended. Log in again.`
- **Server-not-responding copy (proposed; confirm at plan approval):** `The server accepted the connection but did not answer within 15 s. Check the address, or try again later.`
- **FFID (D5):** label `FFID`, placeholder `e.g. VG12`. `unitIdError` copy unchanged.
- **Config key (D4):** `last_server_address` (`Config.LastServerAddress`). Write it only after a successful `Connect`. Never write the password. Never touch `server_url`.
- **Event names:** `auth:session_ended`, payload `{"reason": string}`. Existing `auth:session_changed` values stay `logged_in`/`logged_out`.
- **Branch on gRPC status codes, never on message text.** Message text is used only to extract the `session ended: <reason>` suffix for copy.
- **Lock order:** `lifeMu → s.mu` only. `CancelConnect`, `endSession`, `resync`, `markControlLost` never take `lifeMu`. Hooks (`OnSessionEnded`, `OnControlState`) are called with no Session lock held.
- **CL3 contract (D8):** `UpdateRadioInfo` keeps `reportAction(actRadioSave, …)` then `reportAction(actRadioSend, …)` in that order. `ResetLayout` is untouched. CL3's notify tests stay green unchanged.
- **CLAUDE.md Welcome rules:**
  - two stages (`welcome`, `manual`);
  - prototype classNames, inline styles and layout unchanged;
  - the SSO button stays `disabled`.

  The only structural change is that the manual stage's root `div` becomes a `form` with the identical `className`/`style`.
- **Bound surface:** exactly two new bound methods, `CancelConnect` and `GetLastServerAddress`. All other new Go identifiers on `*App` are unexported. Main-only wiring stays in package-level funcs.
- **No dependency change** (Go or npm). No proto change. `google.golang.org/grpc/codes` and `status` are in the already-required grpc module.
- **No Tailwind utility classes.** Use existing `components.css`/`global.css` classes or inline styles.
- **gopls is unreliable in this repo.** Trust only `go build`/`go vet`.
- **Go verification (full):**

  ```bash
  go build -tags purego ./...
  go vet -tags purego ./...
  go test -race -count=1 -tags purego ./...
  ```
- **Frontend verification (full).** Regenerate the bindings first whenever a bound Go method changed:

  ```bash
  wails3 generate bindings -ts -f "-tags purego" -clean=true
  cd frontend
  npx vitest run
  npx tsc --noEmit
  npm run build
  ```
- **Server verification (Task 12 only).** Run it in a detached worktree of `D:\Projects\vcs-srs-server` at `dcce3add91e8fe3e2d23f850899b36a4da880730`. Generate first, with the exact commands from `.github/actions/generate/action.yml`, then build/vet/test:

  ```bash
  mkdir -p srspb voicecontrolpb vcsauthpb
  protoc --go_out=./srspb --go_opt=paths=source_relative --go-grpc_out=./srspb --go-grpc_opt=paths=source_relative srs.proto
  protoc --go_out=./voicecontrolpb --go_opt=paths=source_relative --go-grpc_out=./voicecontrolpb --go-grpc_opt=paths=source_relative control.proto
  protoc --go_out=./vcsauthpb --go_opt=paths=source_relative --go-grpc_out=./vcsauthpb --go-grpc_opt=paths=source_relative vcs-auth-plugin.proto
  go run github.com/99designs/gqlgen generate
  go build -tags headless ./...
  go vet -tags headless ./...
  go test -race -count=1 -tags headless ./...
  ```

  This needs protoc 36.2, `protoc-gen-go` at the server's go.mod version and `protoc-gen-go-grpc@v1.6.2` on PATH.

## Review Focus

1. **CANCEL clicked in the instant `Connect` succeeds.** The attempt has already installed the session, and the cancel arrives after.
   - *Expected:* the cancel is a no-op. The user lands on the console, not on a half-torn session.
   - *Test:* Task 4, `TestCancelConnect_AfterSuccessIsNoOp`.
2. **Logout pressed while a connect is still negotiating** (MainApp is not shown, but `Disconnect` can come from `ServiceShutdown`).
   - *Expected:* the connect aborts. `Disconnect` returns within its bound, and exactly one `logged_out` is emitted.
   - *Test:* Task 5, `TestDisconnect_DuringConnectAbortsIt`.
3. **The server kicks the client while the user is mid-Reconnect.** The stream-end and Reconnect's `SyncClient` both see the session gone.
   - *Expected:* exactly one `auth:session_ended` and one `logged_out`.
   - *Test:* Task 6, `TestSessionEnded_StreamAndReconnectRaceFiresOnce`.
4. **Late `GetLastServerAddress` answer after the user typed an address.**
   - *Expected:* the typed value is kept.
   - *Test:* Task 11, `"late remembered address does not overwrite typed input"`.
5. **A C13 resync whose new stream ends `ResourceExhausted` again at once.**
   - *Expected:* a second resync, still no `disconnected`, and the roster equals the latest snapshot.
   - *Test:* Task 7, `TestResync_RepeatedFellBehindResyncsAgain`.

---

### Task 1: TLS refusal flake: `grpctest.DialUntilFailure`

**Files:**
- Modify: `internal/grpctest/fakeserver.go`: add the helper; keep `StartWith`'s signature.
- Modify: `internal/session/tls_integration_test.go`: delete `refusalCtx`; move the four refusal tests (`:68`, `:94`, `:115`, `:141`) onto the helper.
- Modify: `internal/session/dial.go`: the `boundedDial` comment that cites `refusalCtx` (`:149-153`) now says the shorter-caller-deadline property is kept for production callers.

**Interfaces:**
- Produces: `func DialUntilFailure(t *testing.T, f *Fake, serverCreds, clientCreds credentials.TransportCredentials) error`. It starts its own bufconn server (stopped through `t.Cleanup`) and returns the first RPC error once the channel is in `TransientFailure`. `StartWith` is unchanged.

- [ ] **Step 1: Rewrite the four tests onto the helper first.** Keep every substring assertion exactly:
  - `"unknown authority"`;
  - `"x509"`;
  - `"first record does not look like a TLS handshake"`;
  - `explainDialError(err, "srs.example.org:5002", "")` containing `"not speaking TLS"`.
- [ ] **Step 2: Run** `go test -tags purego ./internal/session/ -run TestTLS_`. Expected: build failure (helper undefined).
- [ ] **Step 3: Implement the helper.** It must:
  - build `grpc.NewClient("passthrough:///bufnet", WithContextDialer(lis.DialContext), WithTransportCredentials(clientCreds))`;
  - call `conn.Connect()`;
  - loop `WaitForStateChange` under a 10 s ctx until `connectivity.TransientFailure` (`t.Fatal` on timeout, or on reaching `Ready`);
  - issue one `srspb.NewSRSServiceClient(conn).Ping` **without** wait-for-ready on a 10 s ctx;
  - return its error, then close the conn.
- [ ] **Step 4: Run** `go test -tags purego ./internal/session/ -run TestTLS_ -v`. Expected: all 5 `TestTLS_*` PASS.
- [ ] **Step 5: Stress.** Run `go test -race -count=50 -tags purego ./internal/session/ -run 'TestTLS_(Untrusted|Hostname|ClientAgainst|PlaintextServer)'` while `go test -race -count=1 -tags purego ./...` runs in a second shell. Expected: `ok`, with no `context deadline exceeded`.
- [ ] **Step 6: Commit** `test(session): make TLS refusal tests wait for the handshake failure, not a 500ms deadline`.

### Task 2: Store resets, control `SyncClient` replace semantics, `Subscribe`/`Consume` split

**Files:**
- Modify: `internal/state/store.go`
- Modify: `internal/control/client.go:41-65`
- Modify: `internal/control/stream.go:12-30`
- Test: `internal/state/store_test.go`, `internal/control/client_test.go`, `internal/control/stream_test.go`

**Interfaces:**
- Produces:
  - `func (s *Store) ResetRoster()`: clears clients and radios, then notifies radio observers once.
  - `func (s *Store) ResetSession()`: `ResetRoster` + clears voice secret/addrs (notifies voice-credential observers once). Does **not** clear self (that is `ClearSelf`) or settings.
  - `func (c *Client) SyncClient(ctx context.Context, st *state.Store) (hadSecret bool, err error)`: calls `st.ResetRoster()` immediately before hydrating, and only on a successful response. With `voice_secret == ""` it calls `st.SetVoiceAddresses(...)` and leaves the secret alone, returning `hadSecret=false`.
  - `func (c *Client) Subscribe(ctx context.Context) (srspb.SRSService_SubscribeToUpdatesClient, error)`
  - `func (c *Client) Consume(stream srspb.SRSService_SubscribeToUpdatesClient, st *state.Store, em events.Emitter) error`
  - `ConsumeUpdates` becomes `Subscribe` + `Consume`, with unchanged behaviour.

- [ ] **Step 1: Write the failing tests.**
  - `TestResetRoster_ClearsClientsAndRadiosNotifiesOnce`: observer count == 1; `Client("a")` absent; settings kept.
  - `TestResetSession_ClearsVoiceCredentials`: `VoiceCredentials()` all `""`; self untouched.
  - `TestSyncClient_ReplacesRoster`: store pre-seeded with `ghost`; fake returns `{a}`; store has only `a`.
  - `TestSyncClient_EmptySecretKeepsStoredSecret`: store secret `s1`; fake returns `""` + addr `x:1`; secret still `s1`, coalition addr `x:1`, `hadSecret == false`.
  - `TestSyncClient_FailureLeavesRoster`: on a `SyncClient` error, the roster is untouched.
  - `TestSubscribeThenConsume`: routes a pushed update.
- [ ] **Step 2: Run** `go test -race -tags purego ./internal/state/ ./internal/control/`. Expected: compile failure / FAIL.
- [ ] **Step 3: Implement.** Update `internal/session/session.go`'s two `SyncClient` call sites to the new signature (ignore `hadSecret` for now) so the tree builds.
- [ ] **Step 4: Run** the Go verification (full). Expected: all `ok`.
- [ ] **Step 5: Commit** `fix(control,state): SyncClient replaces the roster and never blanks the voice secret (CON-07, SRV-06)`.

### Task 3: Session errors and the classifier

**Files:**
- Create: `internal/session/errors.go`
- Test: `internal/session/errors_test.go` (package `session`, internal)

**Interfaces:**
- Produces:
  - `var ErrConnectCancelled = errors.New("connect cancelled")`
  - `var ErrServerNotResponding = errors.New("server not responding")`
  - `var ErrSessionEnded = errors.New("session ended")`
  - `type serverNotRespondingError struct{ after time.Duration; cause error }`:
    - `Error()` returns the Global-Constraints copy, with `after` rendered in whole seconds;
    - `Is(target) bool` is true for `ErrServerNotResponding`;
    - `Unwrap()` returns `cause`.
  - `func sessionEndedError(reason string) error` → `fmt.Errorf("%w: %s", ErrSessionEnded, reason)`
  - `type verdict int` with `verdictOrdinary`, `verdictSessionEnded`, `verdictSuperseded`, `verdictResync`
  - `func classifyRPCError(err error) (v verdict, reason string)`

- [ ] **Step 1: Write table test `TestClassifyRPCError`.** Errors are wrapped with `fmt.Errorf("sync client: %w", …)` to prove `status.Code` unwraps.

  | input | verdict | reason |
  |---|---|---|
  | `status.Error(codes.Unauthenticated, "session not found for /srs.SRSService/SyncClient: log in again")` | SessionEnded | `expired` |
  | `status.Error(codes.Unauthenticated, "session ended: kicked")` | SessionEnded | `kicked` |
  | `status.Error(codes.Unauthenticated, "session ended: stream lost")` | SessionEnded | `stream lost` |
  | `status.Error(codes.PermissionDenied, "insufficient role for x")` | SessionEnded | `expired` |
  | `status.Error(codes.Internal, "token verification unavailable")` | Ordinary | `""` |
  | `status.Error(codes.Aborted, "superseded by a newer subscription")` | Superseded | `""` |
  | `status.Error(codes.ResourceExhausted, "update stream fell behind: resubscribe and call SyncClient")` | Resync | `""` |
  | `status.Error(codes.Unavailable, "x")` | Ordinary | `""` |
  | `errors.New("plain")` | Ordinary | `""` |
  | `nil` | Ordinary | `""` |

  Plus `TestServerNotRespondingError`: `errors.Is(e, ErrServerNotResponding)`, `errors.Is(e, context.DeadlineExceeded)` when that is the cause, and `e.Error()` equals the exact copy for `after = 15 * time.Second`.
- [ ] **Step 2: Run** `go test -tags purego ./internal/session/ -run 'TestClassify|TestServerNotResponding'`. Expected: build failure.
- [ ] **Step 3: Implement.** The reason is the text after the first `"session ended: "` in `status.Convert(err).Message()`, else `expired`.
- [ ] **Step 4: Run** the same command. Expected: PASS.
- [ ] **Step 5: Commit** `feat(session): status-code classifier and connect sentinels`.

### Task 4: Bounded, cancellable Connect/Reconnect (GO-S5, GO-01, CANCEL)

**Files:**
- Modify: `internal/session/session.go` (`Deps`, `Session` fields, `Connect`, `Reconnect`)
- Modify: `internal/grpctest/fakeserver.go`: knobs below
- Test: new `internal/session/bound_test.go`

**Interfaces:**
- Consumes: Task 3 sentinels.
- Produces:
  - `Deps.PostDialTimeout time.Duration` (0 → `defaultPostDialTimeout`).
  - `func (s *Session) CancelConnect()`: takes only `s.mu`; calls the current `attemptCancel` if any; no-op otherwise.
  - Fake knobs, all guarded by `f.mu`:
    - `func (f *Fake) Block(method string, ch chan struct{})`, for `"InitAuth" | "GuestLogin" | "SyncClient" | "Disconnect" | "UpdateRadioInfo"`. The handler waits on `ch` or the RPC ctx.
    - `func (f *Fake) Fail(method string, err error)`, for `"SyncClient" | "Ping" | "UpdateRadioInfo" | "Disconnect"`.
    - `func (f *Fake) Calls() []string`, an ordered log of every handled RPC name, including `"SubscribeToUpdates"`.
    - `func (f *Fake) SetSync(clients map[string]*srspb.ClientInfo, secret string)`.
    - `func (f *Fake) EndStream(err error)`: the current `SubscribeToUpdates` returns `err`. `SubscribeToUpdates` becomes a `select` loop over `updates` (closed → return nil, as `CloseStream` does today), a buffered `streamEnd chan error` (cap 4), and `stream.Context().Done()`. The last case matters: a stream the client already cancelled must not consume a later `EndStream`.

  Existing fields and `CloseStream` keep working.

- [ ] **Step 1: Write the failing tests.** Use `PostDialTimeout: 200*time.Millisecond`. Assert elapsed < 2 s and the final recorded control state.
  - `TestConnect_BoundedWhenInitAuthStalls`: `Block("InitAuth", ch)`; `errors.Is(err, ErrServerNotResponding)`; final state `disconnected`; `s.Control() == nil`.
  - `TestReconnect_BoundedWhenSyncStalls`: Connect OK, then `Block("SyncClient", ch)`; Reconnect → `ErrServerNotResponding`; final `disconnected`.
  - `TestCancelConnect_DuringGuestLogin`: `Block("GuestLogin")`; `CancelConnect()` from another goroutine after 50 ms; `errors.Is(err, ErrConnectCancelled)` within 1 s; final `disconnected`; `Calls()` contains no `"Disconnect"` (no token yet).
  - `TestCancelConnect_AfterGuestLoginSendsDisconnect`: `Block("SyncClient")`; cancel; `Calls()` ends with `"Disconnect"`.
  - `TestCancelConnect_DuringDial`: a `Deps.Dialer` that blocks on ctx; cancel → `ErrConnectCancelled`.
  - `TestCancelConnect_AfterSuccessIsNoOp`: after Connect returns nil, `CancelConnect()`; `s.Control() != nil`; no extra `disconnected`.
  - `TestConnect_EveryFailureEndsDisconnected` (GO-01), table over:
    - dial error;
    - `HasGuest=false`;
    - `RejectLogin`;
    - `Fail("SyncClient", status.Error(codes.Internal,"x"))`;
    - timeout;
    - cancel.

    For each, the last recorded state is `disconnected`.
- [ ] **Step 2: Run** `go test -race -tags purego ./internal/session/ -run 'TestConnect_|TestReconnect_Bounded|TestCancelConnect'`. Expected: FAIL / build failure.
- [ ] **Step 3: Implement** per spec §3.1:
  - `lifeMu` around Connect/Reconnect;
  - `attemptCtx, cancel := context.WithCancel(ctx)` published as `s.attemptCancel` under `s.mu` before the dial, and cleared (under `s.mu`) when the attempt ends;
  - the dial on `attemptCtx`;
  - post-dial on `context.WithTimeout(attemptCtx, postDialTimeout)`;
  - error mapping, in this order: `attemptCtx.Err() == context.Canceled` → `ErrConnectCancelled`; post-dial ctx `DeadlineExceeded` → `serverNotRespondingError`; otherwise the original error;
  - the D3 best-effort `control.New(conn, token).Disconnect` on `context.WithTimeout(context.Background(), cancelDisconnectTimeout)` when a cancel/timeout lands after `GuestLogin` returned a token;
  - a named `err` return with a deferred `setConnState(ConnDisconnected)` on error.

  Reconnect keeps emitting `disconnected` on every failure (one emission, not two).
- [ ] **Step 4: Run** the Go verification (full). Expected: all `ok`.
- [ ] **Step 5: Commit** `fix(session): bound and cancel the post-dial sequence; every failed connect ends disconnected (GO-S5, GO-01)`.

### Task 5: Single live session and logout forgets it (GO-02, GO-10, CON-07)

**Files:**
- Modify: `internal/session/session.go` (`Connect`, `Disconnect`)
- Test: `internal/session/lifecycle_test.go` (new), `internal/session/reconnect_test.go`

**Interfaces:**
- Consumes: `Store.ResetSession` (Task 2), `CancelConnect`/`lifeMu` (Task 4), fake knobs (Task 4).
- Produces:
  - `Disconnect` calls `CancelConnect()` **before** taking `lifeMu`;
  - it then tears down (cancel the stream ctx → `cc.Disconnect(ctx)` → `conn.Close()`; order kept for C12);
  - it clears `lastDialer`/`lastToken`;
  - it calls `st.ResetSession()` + `st.ClearSelf()`;
  - it emits `disconnected` + `logged_out` exactly as today.

- [ ] **Step 1: Write the failing tests.**
  - `TestConnect_SecondConnectReplacesFirst`: Connect twice. The first stream's goroutine has exited: `Calls()` shows two `SubscribeToUpdates`, and after `f.EndStream(status.Error(codes.Unavailable,"x"))` targets the old stream, no `disconnected` is recorded. Exactly one live conn (`s.Control()` is the second).
  - `TestConnectRacesReconnect_OneLiveConn` (`-race`): run the two concurrently 20×. No race report, and `s.Control() != nil`.
  - `TestReconnect_AfterDisconnectNeverConnected`: Disconnect, then Reconnect → error containing `never connected`; `Calls()` has no new `SyncClient`.
  - `TestReconnect_DropsGhosts`: `SetSync({a,b})`, Connect, `SetSync({a})`, Reconnect → store has `a` only.
  - `TestDisconnect_ClearsRosterAndSecret`: after Disconnect, `Snapshot()` has no clients or radios, and `VoiceCredentials()` secret is `""`.
  - `TestDisconnect_BoundedByCallerCtx`: `Block("Disconnect")`; `Disconnect` with a 100 ms ctx returns < 1 s and still emits `logged_out`.
  - `TestDisconnect_DuringConnectAbortsIt`: `Block("GuestLogin")`; Connect in a goroutine; `Disconnect(ctx 2s)`. Connect returns `ErrConnectCancelled`, Disconnect returns < 1 s, and exactly one `auth:session_changed`, which is `logged_out`.
- [ ] **Step 2: Run** `go test -race -tags purego ./internal/session/`. Expected: the new tests FAIL.
- [ ] **Step 3: Implement** per spec §3.1 (GO-02) and §3.3.
- [ ] **Step 4: Run** the Go verification (full). Expected: all `ok`.
- [ ] **Step 5: Commit** `fix(session): one live connection; logout forgets token, roster and voice secret (GO-02, GO-10, CON-07)`.

### Task 6: Session ended (SV1 C1-C12, SRV-06/07 client half)

**Files:**
- Modify: `internal/events/events.go`: `EventAuthSessionEnded = "auth:session_ended"`, `type SessionEndedPayload struct{ Reason string \`json:"reason"\` }`, `func (t *Tagged) SessionEnded(reason string)`
- Modify: `internal/session/session.go` (`handleStreamEnd`, `Reconnect`, `PingOnce`, `UpdateRadioInfo`, new `endSession`, `SetSessionEndedHook`)
- Test: new `internal/session/sessionend_test.go`, `internal/events/events_test.go`

**Interfaces:**
- Consumes: `classifyRPCError`, `sessionEndedError`, `ErrSessionEnded` (Task 3); `SyncClient` `hadSecret` (Task 2).
- Produces:
  - `func (s *Session) SetSessionEndedHook(fn func(reason string))`, with the same late-binding discipline as `SetControlStateObserver`. Task 8 wires it.
  - `func (s *Session) endSession(reason string) error`. Its once-guard is "under `s.mu`, `lastToken == ""` and `conn == nil` → return `sessionEndedError(reason)` with no side effects". Otherwise it:
    1. bumps `streamGen`, takes and clears conn/control/cancel/lastDialer/lastToken;
    2. after unlocking: cancels the stream, closes the conn;
    3. calls the hook;
    4. calls `ResetSession` + `ClearSelf`;
    5. calls `setConnState(disconnected)`;
    6. emits `SessionEnded(reason)` then `SessionChanged("logged_out")`.

    It returns `sessionEndedError(reason)`.
  - Routing:
    - `handleStreamEnd` (after the ctx guard, current gen only): SessionEnded → `endSession`; Superseded → WARN log + `markControlLost` (D9); Resync → `markControlLost` for now (Task 7 replaces it); Ordinary → `markControlLost`.
    - `Reconnect`: a `SyncClient` error classified SessionEnded, or `hadSecret == false`, → `endSession` (reason from the classifier, `expired` for the empty secret); return its error.
    - `PingOnce` / `UpdateRadioInfo`: a SessionEnded verdict → `endSession`; return its error.

- [ ] **Step 1: Write the failing tests** (a capturing emitter that records name **and** payload, in order).
  - `TestReconnect_UnauthenticatedEndsSession` (C1): `Fail("SyncClient", status.Error(codes.Unauthenticated,"session not found for /srs.SRSService/SyncClient: log in again"))`. Events end with `auth:session_ended{expired}`, `auth:session_changed logged_out`; `errors.Is(err, ErrSessionEnded)`; a later Reconnect says `never connected`.
  - `TestReconnect_InternalIsOrdinary` (C4): `codes.Internal` → no `logged_out`; a later Reconnect dials again.
  - `TestReconnect_EmptySecretEndsSession`: `SetSync(clients, "")` → `session_ended{expired}`.
  - `TestConnect_EmptySecretKeepsStored`: a store secret set beforehand is still set after Connect with `""`, and Connect succeeds.
  - `TestStreamEnd_KickedEndsSessionOnce` (C5): `EndStream(status.Error(codes.Unauthenticated,"session ended: kicked"))` → exactly one `session_ended{kicked}`, one `logged_out`; the hook is called once with `kicked`.
  - `TestStreamEnd_UnknownSessionEnds` (C7): `"session not found: log in again"` → `session_ended{expired}`.
  - `TestStreamEnd_AbortedIsControlLoss` (C6): → `disconnected`, no `logged_out`, token kept (a later Reconnect dials).
  - `TestStreamEnd_AfterUserDisconnectIgnored` (C12): `Disconnect`, then `EndStream(Unauthenticated "session ended: disconnected")` → one `logged_out`, zero `session_ended`.
  - `TestPingOnce_UnauthenticatedEndsSession`, `TestUpdateRadioInfo_UnauthenticatedEndsSession`.
  - `TestSessionEnded_StreamAndReconnectRaceFiresOnce`: `Fail("SyncClient", Unauthenticated)`; run `EndStream(Unauthenticated "session ended: kicked")` and `Reconnect` concurrently → exactly one `session_ended`, one `logged_out`.
  - `TestTaggedSessionEnded` (events package): emits `auth:session_ended` with `SessionEndedPayload{Reason:"banned"}`.
- [ ] **Step 2: Run** `go test -race -tags purego ./internal/session/ ./internal/events/`. Expected: FAIL.
- [ ] **Step 3: Implement** per spec §3.4.
- [ ] **Step 4: Run** the Go verification (full). Expected: all `ok`, including the unchanged `liveness_test.go`.
- [ ] **Step 5: Commit** `feat(session): route server-ended sessions to logout with a reason (SV1 C1-C12)`.

### Task 7: Stream fell behind → resync (SV2 C13, D6)

**Files:**
- Modify: `internal/session/session.go` (`handleStreamEnd`, new `resync`, `startStreamLocked` takes an open stream)
- Test: new `internal/session/resync_test.go`

**Interfaces:**
- Consumes: `Client.Subscribe`/`Consume` (Task 2), `endSession` (Task 6).
- Produces: `func (s *Session) resync(gen uint64)`, per spec §3.12 steps 1-5. The `SyncClient` is bounded by `postDialTimeout` derived from the stream ctx.

- [ ] **Step 1: Write the failing tests.**
  - `TestResync_FellBehindResubscribesThenSyncs`:
    - setup: `SetSync({a,b})`, Connect, `SetSync({a})`, then `EndStream(status.Error(codes.ResourceExhausted,"update stream fell behind: resubscribe and call SyncClient"))`;
    - wait until `Calls()` has a second `SubscribeToUpdates` followed (later index) by a second `SyncClient`;
    - the store has `a` only;
    - recorded states end with `connected`, contain no `disconnected`, and there is no `logged_out`.
  - `TestResync_RepeatedFellBehindResyncsAgain`: two `EndStream(ResourceExhausted)` in sequence → three `SubscribeToUpdates` in total, still no `disconnected`.
  - `TestResync_SyncUnauthenticatedEndsSession`: before the end, `Fail("SyncClient", Unauthenticated)` → `session_ended{expired}`.
  - `TestResync_SyncInternalIsControlLoss`: `Fail("SyncClient", Internal)` → `disconnected`, no `logged_out`.
  - `TestResync_StaleGenerationIgnored`: Reconnect first, then end the **old** stream with `ResourceExhausted` → no extra `SubscribeToUpdates`.
- [ ] **Step 2: Run** `go test -race -tags purego ./internal/session/ -run TestResync`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the Go verification (full). Expected: all `ok`.
- [ ] **Step 5: Commit** `feat(session): resubscribe and resync when the update stream fell behind (SV2 C13)`.

### Task 8: App wiring: timeouts, CancelConnect, session-ended hook, reportAction

**Files:**
- Modify: `internal/app/app.go`:
  - `sessionAPI` gains `CancelConnect()`;
  - constants `disconnectTimeout`, `radioPushTimeout` beside `shutdownDisconnectTimeout`;
  - `SetBackend` installs the hook if `sess` implements `interface{ SetSessionEndedHook(func(string)) }`.
- Modify: `internal/app/bindings.go`:
  - `Disconnect` uses `context.WithTimeout(context.Background(), disconnectTimeout)`;
  - `UpdateRadioInfo`'s RPC uses `radioPushTimeout`, with both `reportAction` lines kept in order;
  - new `func (a *App) CancelConnect()`.
- Modify: `internal/app/voice.go:994` `pushPersistedRadios`: derive `context.WithTimeout(ctx, radioPushTimeout)` internally. Callers (`bindings.go:42,67`, `profiles.go:203`) are unchanged.
- Modify: `internal/app/notify_actions.go` `reportAction`: `errors.Is(err, session.ErrSessionEnded)` → `Resolve`, not `Raise`.
- Modify: `internal/app/surface_test.go`: add `"CancelConnect"` to `boundSurface`.
- Modify: the test fakes implementing `sessionAPI` (`voice_test.go` `fakeControlSession`, `bindings_test.go` `fakeSession`): add `CancelConnect`, and record the ctx each call received.
- Test: `internal/app/bindings_test.go`, `internal/app/notify_actions_test.go`

**Interfaces:**
- Consumes: `session.ErrSessionEnded` (Task 3), `Session.SetSessionEndedHook`, `Session.CancelConnect` (Tasks 4, 6).
- Produces: bound `App.CancelConnect()`.

- [ ] **Step 1: Write the failing tests.**
  - `TestDisconnect_HasDeadline`: the fake records `ctx.Deadline()`; it exists and is ≤ `2*time.Second` from the call.
  - `TestUpdateRadioInfo_HasDeadline`: same at ≤ `5*time.Second`; the existing CL3 `UpdateRadioInfo` notify tests are unchanged and green.
  - `TestPushPersistedRadios_HasDeadline`, via `Connect` on the fake.
  - `TestCancelConnect_Forwards`: the fake's counter == 1.
  - `TestReportAction_SessionEndedResolves`: raise `action:radio.send` with a plain error, then `reportAction(actRadioSend, fmt.Errorf("x: %w", session.ErrSessionEnded))` → the item is resolved and nothing new is raised.
  - `TestSetBackend_InstallsSessionEndedHook`: a fake with `SetSessionEndedHook` captures fn; with a live fake voice session, `fn("kicked")` → `a.voiceSession() == nil`.
  - `TestBoundSurfaceIsTheFrontendAPI` (updated list).
- [ ] **Step 2: Run** `go test -race -tags purego ./internal/app/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the Go verification (full). Expected: all `ok`, and `main_wiring_test.go` still passes (no `main.go` change).
- [ ] **Step 5: Commit** `fix(app): bound Disconnect and radio pushes; add CancelConnect; stop voice when the server ends the session (GO-03)`.

### Task 9: Remembered server address (FE18 Go half, D4)

**Files:**
- Modify: `internal/config/config.go`: `LastServerAddress string \`toml:"last_server_address"\`` directly after `ServerURL`; `Default()` sets `""` explicitly.
- Modify: `internal/app/bindings.go`:
  - `Connect` calls `a.rememberServerAddress(serverURL)` after a successful `a.sess.Connect`, before `health.SetServer`;
  - new bound `func (a *App) GetLastServerAddress() string`.
- Modify: `internal/app/surface_test.go`: add `"GetLastServerAddress"`.
- Test: `internal/config/config_test.go`, `internal/app/bindings_test.go`

**Interfaces:**
- Produces:
  - `func (a *App) rememberServerAddress(addr string)` (unexported). It uses `a.settings.persistCfg(cfgTxn{mutate: func(c *config.Config){ c.LastServerAddress = addr }})`, is a no-op when `a.settings == nil`, and logs a failure at WARN without returning it.
  - `GetLastServerAddress` returns `""` when `a.settings == nil`, else `sb.cfg.LastServerAddress` read under `sb.mu`.

- [ ] **Step 1: Write the failing tests.**
  - `TestConfig_LastServerAddressRoundTrips`: Save/Load keep `vcs.example:443`; an old file without the key loads `""`.
  - `TestConnect_RemembersAddressOnSuccess`: settings backend on a temp `cfgPath` (the `persistcfg_test.go` harness); `Connect("srv.example:5002","n","hunter2","VG12")`. Then:
    - `GetLastServerAddress() == "srv.example:5002"`;
    - the saved file contains `last_server_address = "srv.example:5002"`;
    - it does **not** contain `hunter2`;
    - `server_url` is unchanged.
  - `TestConnect_FailureDoesNotRemember`: the fake session's `Connect` returns an error → the key is unchanged.
  - `TestConnect_PersistFailureDoesNotFailConnect`: the `sb.save` seam returns an error → `Connect` returns nil.
  - `TestGetLastServerAddress_NoSettings` → `""`.
- [ ] **Step 2: Run** `go test -race -tags purego ./internal/config/ ./internal/app/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the Go verification (full). Expected: all `ok`.
- [ ] **Step 5: Commit** `feat(app): remember the last connected server address (FE18)`.

### Task 10: Frontend API, events, session notice

**Files:**
- Modify: `frontend/src/shared/api/client.ts`: `cancelConnect: (): Promise<void> => App.CancelConnect()`, `getLastServerAddress: (): Promise<string> => App.GetLastServerAddress()`
- Modify: `frontend/src/shared/api/events.ts`: `authSessionEnded: "auth:session_ended"`; `export interface SessionEndedPayload { reason: string }`
- Modify: `frontend/src/shared/store/session.ts`: `sessionNotice: string | null` (initial `null`), `setSessionNotice(n: string | null)`
- Create: `frontend/src/shared/store/useSessionEndedNotice.ts` (+ `.test.tsx`)
- Modify: `frontend/src/windows/main/MainApp.tsx`: call `useSessionEndedNotice()` beside `useClientStateSync()`

**Interfaces:**
- Consumes: the regenerated bindings (Tasks 8-9).
- Produces:
  - `export function sessionEndedMessage(reason: string): string`, with the exact D2 copy;
  - `export function useSessionEndedNotice(): void`, which subscribes to `EV.authSessionEnded` and calls `setSessionNotice(sessionEndedMessage(p.reason))`.

- [ ] **Step 1: Regenerate bindings:** `wails3 generate bindings -ts -f "-tags purego" -clean=true`. Expected: `App.CancelConnect` and `App.GetLastServerAddress` are present in `frontend/bindings/.../internal/app/app.ts`.
- [ ] **Step 2: Write the failing tests.** Use the `useClientStateSync.test.tsx` event-mock pattern.
  - `sessionEndedMessage("kicked" | "banned" | "expired" | "stale" | "")` → the three exact strings.
  - Emitting `auth:session_ended {reason:"banned"}` sets `useSession.getState().sessionNotice` to `"You are banned from this server."`.
  - Unmount unsubscribes.
- [ ] **Step 3: Run** `cd frontend && npx vitest run src/shared/store/useSessionEndedNotice.test.tsx`. Expected: FAIL.
- [ ] **Step 4: Implement**, and mount the hook in MainApp.
- [ ] **Step 5: Run** the frontend verification (full). Expected: all pass; `tsc` prints nothing.
- [ ] **Step 6: Commit** `feat(frontend): session-ended notice and new connect bindings`.

### Task 11: Welcome: form, CANCEL, remembered address, notice landing, FFID, alert (FE18, FE06, FE14, CANCEL)

**Files:**
- Modify: `frontend/src/windows/main/screens/Welcome.tsx`
- Test: `frontend/src/windows/main/screens/Welcome.test.tsx`. Add `cancelConnect` and `getLastServerAddress` (default `""`) to the `api` mock. The existing four tests stay unchanged; the label regex `/FFID/i` still matches `FFID`.

**Interfaces:**
- Consumes: `api.cancelConnect`, `api.getLastServerAddress`, `useSession` `sessionNotice`/`setSessionNotice` (Task 10).

- [ ] **Step 1: Write the failing tests.**
  - `"Enter in the password field submits once"`: fill the fields, `fireEvent.submit` on the form via Enter (`fireEvent.keyDown` is not enough in jsdom; use `fireEvent.submit(screen.getByLabelText(/Server Password/i).closest("form")!)`) → `api.connect` called once.
  - `"BACK does not submit"`: click BACK → stage `welcome`; `api.connect` not called; BACK has `type="button"`.
  - `"error slot is an alert"`: a connect rejection renders inside `role="alert"`.
  - `"CANCEL aborts and returns to the filled form with no error"`: `api.connect` returns a promise rejected after `cancelConnect` is called. Click CONNECT, then click CANCEL. Then:
    - `api.cancelConnect` was called once;
    - the manual form is shown with the server/name values kept;
    - `queryByRole("alert")` is null.
  - `"remembered address prefills the server field"`: `getLastServerAddress` resolves `"srv.example:5002"` → the input value matches after `waitFor`.
  - `"late remembered address does not overwrite typed input"`: a deferred promise; the user types `"typed:1"`; then resolve → the value stays `"typed:1"`.
  - `"empty or failed remembered address keeps localhost:5002"`.
  - `"session notice lands on the manual stage"`: `useSession.setState({sessionNotice:"You were removed from the server. Log in again."})` before render → the server field is visible without clicking JOIN, and the alert shows the copy. Clicking BACK or CONNECT clears `sessionNotice`.
  - `"FFID label and placeholder"`: `getByLabelText("FFID")` has `placeholder="e.g. VG12"`.
  - `"welcome rules pin"`: the SSO button is `disabled`; JOIN AS GUEST reaches the manual stage; the manual root `form` has `className="col gap-5"`.
- [ ] **Step 2: Run** `cd frontend && npx vitest run src/windows/main/screens/Welcome.test.tsx`. Expected: the new tests FAIL.
- [ ] **Step 3: Implement** per spec §3.4 (frontend), §3.6-§3.9:
  - initial stage: `useState<Stage>(() => useSession.getState().sessionNotice ? "manual" : "welcome")`;
  - a `serverTouched` ref guards the prefill;
  - `cancelledRef` is set on CANCEL and reset at the start of `connect()`;
  - `connect()` and BACK call `setSessionNotice(null)`;
  - the alert shows `error ?? sessionNotice`;
  - CANCEL is `<Button variant="ghost" size="sm" type="button">CANCEL</Button>` inside a `row between acenter` wrapping the existing NEGOTIATING pill;
  - CONNECT gets `type="submit"` and no `onClick`.
- [ ] **Step 4: Run** the frontend verification (full). Expected: all pass.
- [ ] **Step 5: Commit** `feat(welcome): Enter submits, remembered address, CANCEL, session notice, FFID copy, alert role (FE18, FE06, FE14)`.

### Task 12: CI re-pin to the SV1 server: GATE — needs user approval before this task

**Approval:** D7 is a CI/CD edit that goes beyond a SHA bump. Do not start without the user's recorded approval at plan approval.

**Files:**
- Modify: `.github/workflows/test.yml`:
  - `:46` `ref: c3096ce6acb421d2e530e0238471dacdf1b1128e` → `ref: dcce3add91e8fe3e2d23f850899b36a4da880730`.
  - `:43-45` comment: drop "(Phase 6.9 track CL1 bumps it after SV1/SV3)" and state the pin is server `main` after #229 (SV3, SV5, SV1).
  - `:49-68` comments: reword "TWO TOOLCHAINS" and "No toolchain assert here". The server's go.mod now has `go 1.27.0` / `toolchain go1.27.3`. The two-setup-go structure is kept on purpose: the server is still built with its own go.mod's toolchain, independent of the client's. No structural change, and no added assert step.
  - `:173` → `go run github.com/99designs/gqlgen generate` (in-module, no `@version`). Add a comment citing the server's `.github/actions/generate/action.yml` (`go run pkg@version` uses gqlgen's own go.mod with x/tools v0.42.0, which fails under Go 1.27).

**Interfaces:** none.

- [ ] **Step 1: Run** `go test -tags purego ./internal/buildcheck/`. Expected: `ok` before the edit (baseline).
- [ ] **Step 2: Apply the edits.** Keep the protoc 36.2 and `protoc-gen-go-grpc@v1.6.2` steps byte-identical.
- [ ] **Step 3: Run** `go test -tags purego ./internal/buildcheck/`. Expected: `ok` (the setup-go count and timeouts are unchanged).
- [ ] **Step 4: Local server check.** Run the Server verification (Global Constraints) in a detached `vcs-srs-server` worktree at `dcce3ad`. Expected:
  - generate succeeds;
  - build/vet print nothing;
  - all packages `ok`.

  Then `go build -tags headless -o <scratch>/vcs-server.exe .` and, in the client worktree, run `VCS_SERVER_BIN=<scratch>/vcs-server.exe go test -race -count=1 -tags purego ./internal/voice/ ./internal/session/`. Expected: `ok`, with the integration tests running (not skipped: check `-v` for `--- PASS: TestIntegration`). Remove the server worktree afterwards.
- [ ] **Step 5: Commit** `ci: pin the server checkout to dcce3ad (SV1) and run gqlgen in-module`. Final verification happens on the PR's CI across ubuntu/macOS/windows (Task 13).

### Task 13: Full verification, docs, PR

**Files:**
- Create: `docs/superpowers/specs/2026-10-27-phase-6-9-cl1-connect-session-robustness-design.md`, copied from the scratchpad `design-CL1.md`
- Create: `docs/superpowers/plans/2026-10-27-phase-6-9-cl1-connect-session-robustness.md`, this plan

- [ ] **Step 1: Run the Go verification (full).** Expected:
  - build/vet print nothing;
  - every package `ok`;
  - `-run TestBoundSurfaceIsTheFrontendAPI -v` PASS.
- [ ] **Step 2: Run the frontend verification (full)** after regenerating bindings. Expected:
  - vitest passes with ≥ 580 + the new tests;
  - `tsc` prints nothing;
  - `vite build` succeeds.
- [ ] **Step 3: Re-run the TLS stress** (Task 1 Step 5). Expected: `ok`.
- [ ] **Step 4:** Invoke `superpowers:verification-before-completion`, then `superpowers:requesting-code-review` on the branch diff. Fix the findings, re-run Steps 1-2, and commit.
- [ ] **Step 5: Commit the docs**

```bash
git add docs/superpowers/specs/2026-10-27-phase-6-9-cl1-connect-session-robustness-design.md docs/superpowers/plans/2026-10-27-phase-6-9-cl1-connect-session-robustness.md
git commit -m "docs(phase-6.9): CL1 connect & session robustness design and plan"
```

- [ ] **Step 6: Push and open the PR**

```bash
git push -u origin chore/cleanup-cl1
gh pr create --base main --title "Phase 6.9 CL1: connect & session robustness" --body-file <scratchpad>/cl1-pr-body.md
```

  The PR body must include:
  - the finding summary (GO-S5, GO-01, GO-02, GO-03, GO-10, CON-07, SRV-06/07 client half, FE06, FE14 Welcome half, FE18, CANCEL, the TLS flake, the CI pin);
  - the Decisions table D1-D10;
  - the behaviour changes (spec §7);
  - the CI gate note;
  - the verification output, including the CI result on all three OS legs;
  - the DOC hand-offs (spec §10: the CLAUDE.md "hangs the Welcome screen forever" sentence, the session-expired flow, `last_server_address`, the `server_url` voice-host observation);
  - the manual-verification items below;
  - the attribution line `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

  Do not merge. The orchestrator merges.

## Manual verification (for `docs/superpowers/plans/2026-10-27-phase-6-9-manual-verification.md`; DOC owns that file)

Run these in a real Wails build (`wails3 build`) against a real server (SV1 or later; SV2 for item 7). None of them can be automated here.

1. **Stalled server.** Point the client at a server that completes TLS and then never answers (a TCP proxy that forwards the handshake and then stalls).
   - Welcome shows the server-not-responding message after about 15 s (plus up to 10 s of dial).
   - CANCEL during that wait returns at once to the filled form with no error.
2. **CANCEL after login.** CANCEL in the window between GuestLogin and SyncClient (throttle the link). The server log shows a `Disconnect` for that client, not a 120 s reap.
3. **Kick / ban.** Kick from the server admin UI → Welcome manual stage, address prefilled, "You were removed from the server. Log in again." Ban → "You are banned from this server." Voice stops in both cases, and the Comms popout shows no radios.
4. **Server restart.** Restart the server, then press RECONNECT in the banner → Welcome with "Your session has ended. Log in again."
5. **Short blip.** A network drop under 120 s, then RECONNECT → the session is kept (same callsign, voice resumes). No Welcome.
6. **Logout and Reconnect.** Logout, log in again → the roster has no ghosts from the previous session. The old RECONNECT path cannot resurrect a logged-out session.
7. **Slow link (SV2 server).** Throttle the client until the server ends its stream with "fell behind". The UI shows no RECONNECT banner, and the roster matches the server after the resync.
8. **Welcome in the real webview.**
   - Enter submits from every manual field.
   - BACK does not submit.
   - The address is remembered across an app restart, and `config.toml` has `last_server_address` and no password.
   - The FFID label reads "FFID" with "e.g. VG12".
   - A screen reader (NVDA / VoiceOver) announces a connect error and the session-ended notice.
   - A before/after screenshot shows the manual stage visually unchanged apart from the label and placeholder.
9. **Status bar.** After a failed login, the control pill reads disconnected, not reconnecting.
10. **CI legs.** The PR's `test.yml` run is green on ubuntu, macOS and Windows with the server at `dcce3ad`.
