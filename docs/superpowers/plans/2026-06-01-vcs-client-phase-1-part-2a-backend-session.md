# VCS Client — Phase 1 Part 2A (Backend Session Core) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go backend that connects to a VCS server via the guest path, hydrates state, and streams live updates — with no UI changes (main window keeps its placeholder).

**Architecture:** A `session` orchestrator owns the gRPC connection and drives `InitAuth → GuestLogin → control dial → SyncClient → Ping + SubscribeToUpdates`. The `control` stream consumer routes `ServerUpdate`s into the existing `state.Store` and emits typed events. The frontend boundary uses hand-rolled DTOs (proto kept internal).

**Tech Stack:** Go 1.25, `google.golang.org/grpc` + generated `srspb`, `log/slog`, in-process `grpc.NewServer()` fakes for tests.

**Spec:** [`docs/superpowers/specs/2026-06-01-vcs-client-phase-1-part-2-auth-control-design.md`](../specs/2026-06-01-vcs-client-phase-1-part-2-auth-control-design.md) §3, §5, §6, §7.

**Environment notes (verified):**
- `CGO_ENABLED=0`, no C compiler locally → run `go test ./...` WITHOUT `-race` (CI runs race). 
- `srspb/` is generated + gitignored but present on disk; regenerate with `buf generate` if missing.
- A gitignored `frontend/dist/index.html` placeholder exists for `main.go`'s embed; leave it.

**Generated identifiers to use (verified in `srspb/`):** `srspb.NewAuthServiceClient`, `srspb.NewSRSServiceClient`; methods `InitAuth, GuestLogin, SyncClient, UpdateClientInfo, UpdateRadioInfo, GetServerSettings, Disconnect, Ping, SubscribeToUpdates`. Getters: `AuthInitResult.GetClientGuid()/GetHasGuestLogin()`, `GuestLoginResult.GetToken()/GetCoalition()`, `ServerSyncResult.GetClients()/GetRadios()/GetSettings()`, `ServerUpdate.GetType()/GetClientUpdate()/GetServerAction()/GetSettingsUpdate()`, `ClientUpdate.GetClientGuid()/GetClientInfo()/GetRadioInfo()`.

---

## File Structure (after 2A)

```
internal/
  grpctest/fakeserver.go      shared test harness: fake Auth+SRS servers + dial helper
  auth/auth.go                InitAuth + Client type
  auth/guest.go               GuestLogin
  auth/errors.go              typed errors (ErrGuestUnavailable, ErrLoginRejected)
  auth/auth_test.go
  control/client.go           dial + token creds + SyncClient/Update*/GetServerSettings/Disconnect
  control/ping.go             Ping ticker
  control/stream.go           SubscribeToUpdates consumer → state + events
  control/client_test.go
  control/stream_test.go
  events/events.go            (extend) RadioUpdate/SettingsUpdate/ServerAction/SessionChanged/ClientState
  events/events_test.go       (extend)
  app/dto.go                  binding DTOs + proto↔DTO mapping
  app/dto_test.go
  session/session.go          Connect/Disconnect/Reconnect orchestrator
  session/session_test.go
```

---

## Task 1: Shared fake gRPC server harness

**Files:**
- Create: `internal/grpctest/fakeserver.go`

- [ ] **Step 1: Write the harness (no test of its own; it is test support used by later tasks)**

Create `internal/grpctest/fakeserver.go`:
```go
// Package grpctest provides an in-process fake VCS server (Auth + SRS) for tests.
package grpctest

import (
	"context"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// Fake is a configurable in-process Auth+SRS server.
type Fake struct {
	srspb.UnimplementedAuthServiceServer
	srspb.UnimplementedSRSServiceServer

	mu sync.Mutex

	// Auth behavior
	HasGuest      bool
	ClientGUID    string
	RejectLogin   bool
	GuestToken    string
	GuestCoalition string

	// SRS behavior
	SyncClients  map[string]*srspb.ClientInfo
	SyncRadios   map[string]*srspb.RadioInfo
	SyncSettings *srspb.ServerSettings
	LastRadio    *srspb.RadioInfo // records UpdateRadioInfo input

	updates chan *srspb.ServerUpdate // pushed to SubscribeToUpdates
}

// NewFake returns a Fake with sensible defaults (guest enabled).
func NewFake() *Fake {
	return &Fake{
		HasGuest:       true,
		ClientGUID:     "guid-test",
		GuestToken:     "tok-test",
		GuestCoalition: "VG",
		SyncClients:    map[string]*srspb.ClientInfo{},
		SyncRadios:     map[string]*srspb.RadioInfo{},
		SyncSettings:   &srspb.ServerSettings{},
		updates:        make(chan *srspb.ServerUpdate, 16),
	}
}

// PushUpdate enqueues a ServerUpdate to be delivered on the open stream.
func (f *Fake) PushUpdate(u *srspb.ServerUpdate) { f.updates <- u }

// CloseStream signals SubscribeToUpdates to return (simulates a drop).
func (f *Fake) CloseStream() { close(f.updates) }

func (f *Fake) InitAuth(_ context.Context, _ *srspb.AuthInitRequest) (*srspb.AuthInitResponse, error) {
	return &srspb.AuthInitResponse{
		Success: true,
		InitResult: &srspb.AuthInitResponse_Result{
			Result: &srspb.AuthInitResult{
				HasGuestLogin: f.HasGuest,
				ClientGuid:    f.ClientGUID,
			},
		},
	}, nil
}

func (f *Fake) GuestLogin(_ context.Context, _ *srspb.GuestLoginRequest) (*srspb.GuestLoginResponse, error) {
	if f.RejectLogin {
		return &srspb.GuestLoginResponse{
			Success:     false,
			LoginResult: &srspb.GuestLoginResponse_ErrorMessage{ErrorMessage: "bad password"},
		}, nil
	}
	return &srspb.GuestLoginResponse{
		Success: true,
		LoginResult: &srspb.GuestLoginResponse_Result{
			Result: &srspb.GuestLoginResult{Token: f.GuestToken, Coalition: f.GuestCoalition},
		},
	}, nil
}

func (f *Fake) SyncClient(_ context.Context, _ *srspb.Empty) (*srspb.SyncResponse, error) {
	return &srspb.SyncResponse{
		Success: true,
		Version: "test",
		SyncResult: &srspb.SyncResponse_Data{
			Data: &srspb.ServerSyncResult{
				Clients:  f.SyncClients,
				Radios:   f.SyncRadios,
				Settings: f.SyncSettings,
			},
		},
	}, nil
}

func (f *Fake) UpdateRadioInfo(_ context.Context, r *srspb.RadioInfo) (*srspb.ServerResponse, error) {
	f.mu.Lock()
	f.LastRadio = r
	f.mu.Unlock()
	return &srspb.ServerResponse{Success: true}, nil
}

func (f *Fake) UpdateClientInfo(_ context.Context, _ *srspb.ClientInfo) (*srspb.ServerResponse, error) {
	return &srspb.ServerResponse{Success: true}, nil
}

func (f *Fake) GetServerSettings(_ context.Context, _ *srspb.Empty) (*srspb.ServerSettings, error) {
	return f.SyncSettings, nil
}

func (f *Fake) Disconnect(_ context.Context, _ *srspb.Empty) (*srspb.ServerResponse, error) {
	return &srspb.ServerResponse{Success: true}, nil
}

func (f *Fake) Ping(_ context.Context, _ *srspb.PingRequest) (*srspb.PingResponse, error) {
	return &srspb.PingResponse{ServerTimeMs: 1}, nil
}

func (f *Fake) SubscribeToUpdates(_ *srspb.Empty, stream srspb.SRSService_SubscribeToUpdatesServer) error {
	for u := range f.updates {
		if err := stream.Send(u); err != nil {
			return err
		}
	}
	return nil // channel closed → stream ends (simulates drop)
}

// LastRadioInfo returns the most recent UpdateRadioInfo payload (race-safe).
func (f *Fake) LastRadioInfo() *srspb.RadioInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.LastRadio
}

// Start launches the fake on a bufconn and returns a dialer + cleanup.
func Start(t *testing.T, f *Fake) (dial func(context.Context) (*grpc.ClientConn, error), cleanup func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	srspb.RegisterAuthServiceServer(srv, f)
	srspb.RegisterSRSServiceServer(srv, f)
	go func() { _ = srv.Serve(lis) }()

	dial = func(ctx context.Context) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, "bufnet",
			grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) { return lis.DialContext(c) }),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
	cleanup = func() { srv.Stop(); _ = lis.Close() }
	return dial, cleanup
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/grpctest/...`
Expected: exits 0. (If a oneof wrapper name differs, e.g. `AuthInitResponse_Result`, confirm against `srspb/srs.pb.go` and adjust — these are the standard protoc-gen-go names.)

- [ ] **Step 3: Commit**

```powershell
git add internal/grpctest
git commit -m "test: add in-process fake Auth+SRS gRPC server harness"
```

---

## Task 2: `internal/auth` — guest login client

**Files:**
- Create: `internal/auth/errors.go`, `internal/auth/auth.go`, `internal/auth/guest.go`, `internal/auth/auth_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/auth/auth_test.go`:
```go
package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/auth"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
)

func TestInitAuth_ReturnsGUIDAndGuestFlag(t *testing.T) {
	f := grpctest.NewFake()
	f.ClientGUID = "g-1"
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	conn, err := dial(context.Background())
	if err != nil { t.Fatal(err) }
	c := auth.New(conn)

	res, err := c.InitAuth(context.Background(), "1.0.0")
	if err != nil { t.Fatalf("InitAuth: %v", err) }
	if res.ClientGUID != "g-1" || !res.HasGuest {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestGuestLogin_Success(t *testing.T) {
	f := grpctest.NewFake()
	f.GuestToken, f.GuestCoalition = "tok", "VG"
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())
	c := auth.New(conn)

	out, err := c.GuestLogin(context.Background(), "Name", "pw", "unit", "g-1")
	if err != nil { t.Fatalf("GuestLogin: %v", err) }
	if out.Token != "tok" || out.Coalition != "VG" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestGuestLogin_Rejected(t *testing.T) {
	f := grpctest.NewFake()
	f.RejectLogin = true
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())
	c := auth.New(conn)

	_, err := c.GuestLogin(context.Background(), "n", "bad", "u", "g")
	if !errors.Is(err, auth.ErrLoginRejected) {
		t.Fatalf("expected ErrLoginRejected, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/auth/...`
Expected: FAIL — package `auth` does not exist.

- [ ] **Step 3: Write `internal/auth/errors.go`**

```go
package auth

import "errors"

// ErrGuestUnavailable means the server reports no guest login support.
var ErrGuestUnavailable = errors.New("guest login not available on this server")

// ErrLoginRejected means the server rejected the guest credentials.
var ErrLoginRejected = errors.New("login rejected")
```

- [ ] **Step 4: Write `internal/auth/auth.go`**

```go
// Package auth wraps the AuthService gRPC client (guest path for Phase 1).
package auth

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// Client wraps srspb.AuthServiceClient.
type Client struct {
	rpc srspb.AuthServiceClient
}

// New builds a Client over an existing gRPC connection.
func New(conn grpc.ClientConnInterface) *Client {
	return &Client{rpc: srspb.NewAuthServiceClient(conn)}
}

// InitResult is the relevant slice of AuthInitResult.
type InitResult struct {
	ClientGUID string
	HasGuest   bool
}

// InitAuth performs the capability handshake.
func (c *Client) InitAuth(ctx context.Context, version string) (InitResult, error) {
	resp, err := c.rpc.InitAuth(ctx, &srspb.AuthInitRequest{
		Capabilities: &srspb.ClientCapabilities{
			Version:                    version,
			SupportedDistributionModes: []srspb.DistributionMode{srspb.DistributionMode_STANDALONE},
		},
	})
	if err != nil {
		return InitResult{}, fmt.Errorf("init auth: %w", err)
	}
	if !resp.GetSuccess() {
		return InitResult{}, fmt.Errorf("init auth: %s", resp.GetErrorMessage())
	}
	r := resp.GetResult()
	return InitResult{ClientGUID: r.GetClientGuid(), HasGuest: r.GetHasGuestLogin()}, nil
}
```

- [ ] **Step 5: Write `internal/auth/guest.go`**

```go
package auth

import (
	"context"
	"fmt"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// GuestResult is the outcome of a successful guest login.
type GuestResult struct {
	Token     string
	Coalition string
}

// GuestLogin authenticates via the guest path. A server-side rejection maps to
// ErrLoginRejected.
func (c *Client) GuestLogin(ctx context.Context, name, password, unitID, clientGUID string) (GuestResult, error) {
	resp, err := c.rpc.GuestLogin(ctx, &srspb.GuestLoginRequest{
		Name:       name,
		Password:   password,
		UnitId:     unitID,
		ClientGuid: clientGUID,
	})
	if err != nil {
		return GuestResult{}, fmt.Errorf("guest login: %w", err)
	}
	if !resp.GetSuccess() {
		return GuestResult{}, fmt.Errorf("%w: %s", ErrLoginRejected, resp.GetErrorMessage())
	}
	r := resp.GetResult()
	return GuestResult{Token: r.GetToken(), Coalition: r.GetCoalition()}, nil
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/auth/...`
Expected: PASS (3 tests).

- [ ] **Step 7: Commit**

```powershell
git add internal/auth
git commit -m "feat: add internal/auth guest login client"
```

---

## Task 3: Extend `internal/events`

**Files:**
- Modify: `internal/events/events.go`
- Modify: `internal/events/events_test.go`

- [ ] **Step 1: Add failing tests** (append to `internal/events/events_test.go`)

```go
func TestEmitter_RadioUpdate(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.RadioUpdate("g", events.RadioInfoPayload{Muted: true})
	if f.last().name != events.EventRadioUpdate {
		t.Fatalf("unexpected: %q", f.last().name)
	}
}

func TestEmitter_SessionChanged(t *testing.T) {
	f := &fakeEmitter{}
	e := events.New(f)
	e.SessionChanged("logged_in")
	if f.last().name != events.EventAuthSession {
		t.Fatalf("unexpected: %q", f.last().name)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/events/...`
Expected: FAIL — `RadioUpdate`, `RadioInfoPayload`, `SessionChanged` undefined.

- [ ] **Step 3: Append to `internal/events/events.go`** (after the existing `ConnectionState` method)

```go
// RadioPayload mirrors srspb.Radio for the binding/event surface.
type RadioPayload struct {
	ID         uint32  `json:"id"`
	Name       string  `json:"name"`
	Frequency  float32 `json:"frequency"`
	Enabled    bool    `json:"enabled"`
	IsIntercom bool    `json:"is_intercom"`
}

// RadioInfoPayload mirrors srspb.RadioInfo.
type RadioInfoPayload struct {
	Radios []RadioPayload `json:"radios"`
	Muted  bool           `json:"muted"`
}

// RadioUpdateEnvelope is the EventRadioUpdate payload.
type RadioUpdateEnvelope struct {
	Guid  string           `json:"guid"`
	Radio RadioInfoPayload `json:"radio"`
}

// RadioUpdate emits EventRadioUpdate.
func (t *Tagged) RadioUpdate(guid string, info RadioInfoPayload) {
	t.em.Emit(EventRadioUpdate, RadioUpdateEnvelope{Guid: guid, Radio: info})
}

// SettingsUpdate emits EventSettingsUpdate with an opaque payload.
func (t *Tagged) SettingsUpdate(payload any) { t.em.Emit(EventSettingsUpdate, payload) }

// ServerAction emits EventServerAction with an opaque payload.
func (t *Tagged) ServerAction(payload any) { t.em.Emit(EventServerAction, payload) }

// SessionChanged emits EventAuthSession ("logged_in" | "logged_out").
func (t *Tagged) SessionChanged(state string) { t.em.Emit(EventAuthSession, state) }

// ClientState emits EventClientState with a full snapshot payload.
func (t *Tagged) ClientState(snapshot any) { t.em.Emit(EventClientState, snapshot) }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/events/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/events
git commit -m "feat: extend events emitter with radio/settings/session/state methods"
```

---

## Task 4: `internal/control` — client (dial, calls, token creds)

**Files:**
- Create: `internal/control/client.go`, `internal/control/ping.go`, `internal/control/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/control/client_test.go`:
```go
package control_test

import (
	"context"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/control"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

func TestSyncClient_HydratesStore(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncClients = map[string]*srspb.ClientInfo{"g1": {Name: "Alice"}}
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	c := control.New(conn, "tok")
	if err := c.SyncClient(context.Background(), st); err != nil {
		t.Fatalf("SyncClient: %v", err)
	}
	if _, ok := st.Client("g1"); !ok {
		t.Fatal("expected store hydrated with g1")
	}
}

func TestUpdateRadioInfo_SendsPayload(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())
	c := control.New(conn, "tok")

	err := c.UpdateRadioInfo(context.Background(), &srspb.RadioInfo{
		Radios: []*srspb.Radio{{Id: 1, Name: "Fleet", Frequency: 118.5, Enabled: true}},
	})
	if err != nil { t.Fatalf("UpdateRadioInfo: %v", err) }
	got := f.LastRadioInfo()
	if got == nil || len(got.GetRadios()) != 1 || got.GetRadios()[0].GetName() != "Fleet" {
		t.Fatalf("server did not receive radio: %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/control/...`
Expected: FAIL — package `control` does not exist.

- [ ] **Step 3: Write `internal/control/client.go`**

```go
// Package control wraps the SRSService gRPC client and its update stream.
package control

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// tokenCreds attaches the guest token as gRPC metadata on every call.
type tokenCreds struct{ token string }

func (t tokenCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": t.token}, nil
}
func (tokenCreds) RequireTransportSecurity() bool { return false } // insecure phase

// Client wraps srspb.SRSServiceClient with the per-call token.
type Client struct {
	rpc   srspb.SRSServiceClient
	token string
}

// New builds a control client bound to a token.
func New(conn grpc.ClientConnInterface, token string) *Client {
	return &Client{rpc: srspb.NewSRSServiceClient(conn), token: token}
}

func (c *Client) callOpts() []grpc.CallOption {
	return []grpc.CallOption{grpc.PerRPCCredentials(tokenCreds{c.token})}
}

// SyncClient fetches the initial snapshot and hydrates the store.
func (c *Client) SyncClient(ctx context.Context, st *state.Store) error {
	resp, err := c.rpc.SyncClient(ctx, &srspb.Empty{}, c.callOpts()...)
	if err != nil {
		return fmt.Errorf("sync client: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("sync client: %s", resp.GetErrorMessage())
	}
	data := resp.GetData()
	for guid, ci := range data.GetClients() {
		st.UpdateClient(guid, ci)
	}
	for guid, ri := range data.GetRadios() {
		st.SetRadios(guid, ri)
	}
	if data.GetSettings() != nil {
		st.SetSettings(data.GetSettings())
	}
	return nil
}

// UpdateRadioInfo pushes the client's radio config to the server.
func (c *Client) UpdateRadioInfo(ctx context.Context, info *srspb.RadioInfo) error {
	resp, err := c.rpc.UpdateRadioInfo(ctx, info, c.callOpts()...)
	if err != nil {
		return fmt.Errorf("update radio info: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("update radio info: %s", resp.GetErrorMessage())
	}
	return nil
}

// UpdateClientInfo pushes the client's metadata to the server.
func (c *Client) UpdateClientInfo(ctx context.Context, info *srspb.ClientInfo) error {
	resp, err := c.rpc.UpdateClientInfo(ctx, info, c.callOpts()...)
	if err != nil {
		return fmt.Errorf("update client info: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("update client info: %s", resp.GetErrorMessage())
	}
	return nil
}

// GetServerSettings fetches current server settings.
func (c *Client) GetServerSettings(ctx context.Context) (*srspb.ServerSettings, error) {
	return c.rpc.GetServerSettings(ctx, &srspb.Empty{}, c.callOpts()...)
}

// Disconnect notifies the server the client is leaving.
func (c *Client) Disconnect(ctx context.Context) error {
	_, err := c.rpc.Disconnect(ctx, &srspb.Empty{}, c.callOpts()...)
	return err
}
```

- [ ] **Step 4: Write `internal/control/ping.go`**

```go
package control

import (
	"context"
	"time"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// PingOnce sends one Ping carrying the previously measured RTT and returns the
// RTT of this round-trip in milliseconds.
func (c *Client) PingOnce(ctx context.Context, lastRTTms int64) (int64, error) {
	start := time.Now()
	_, err := c.rpc.Ping(ctx, &srspb.PingRequest{LastRttMs: lastRTTms}, c.callOpts()...)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/control/...`
Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```powershell
git add internal/control
git commit -m "feat: add internal/control SRS client with token creds and ping"
```

---

## Task 5: `internal/control` — stream consumer

**Files:**
- Create: `internal/control/stream.go`, `internal/control/stream_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/control/stream_test.go`:
```go
package control_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/control"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

type capEmitter struct {
	mu     sync.Mutex
	names  []string
}

func (c *capEmitter) Emit(name string, _ any) {
	c.mu.Lock(); defer c.mu.Unlock()
	c.names = append(c.names, name)
}
func (c *capEmitter) saw(name string) bool {
	c.mu.Lock(); defer c.mu.Unlock()
	for _, n := range c.names { if n == name { return true } }
	return false
}

func TestStream_RoutesClientJoined(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	em := &capEmitter{}
	c := control.New(conn, "tok")

	guid := "g9"
	f.PushUpdate(&srspb.ServerUpdate{
		Type: srspb.ServerUpdate_CLIENT_JOINED,
		Update: &srspb.ServerUpdate_ClientUpdate{ClientUpdate: &srspb.ClientUpdate{
			ClientGuid: &guid,
			ClientInfo: &srspb.ClientInfo{Name: "Zoe"},
		}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.ConsumeUpdates(ctx, st, em)

	deadline := time.After(2 * time.Second)
	for {
		if _, ok := st.Client(guid); ok && em.saw("state:client_update") { break }
		select {
		case <-deadline:
			t.Fatal("client_update not observed in time")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/control/ -run TestStream`
Expected: FAIL — `ConsumeUpdates` undefined.

- [ ] **Step 3: Write `internal/control/stream.go`**

```go
package control

import (
	"context"
	"io"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// ConsumeUpdates opens SubscribeToUpdates and routes each ServerUpdate into the
// store and the typed emitter. Returns when the stream ends or ctx is cancelled.
func (c *Client) ConsumeUpdates(ctx context.Context, st *state.Store, em events.Emitter) error {
	stream, err := c.rpc.SubscribeToUpdates(ctx, &srspb.Empty{}, c.callOpts()...)
	if err != nil {
		return err
	}
	tagged := events.New(em)
	for {
		upd, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		route(upd, st, tagged)
	}
}

func route(upd *srspb.ServerUpdate, st *state.Store, tagged *events.Tagged) {
	switch upd.GetType() {
	case srspb.ServerUpdate_CLIENT_JOINED, srspb.ServerUpdate_CLIENT_INFO_UPDATE:
		cu := upd.GetClientUpdate()
		if cu == nil || cu.GetClientInfo() == nil {
			return
		}
		guid := cu.GetClientGuid()
		st.UpdateClient(guid, cu.GetClientInfo())
		tagged.ClientUpdate(guid, events.ClientUpdatePayload{
			Name:      cu.GetClientInfo().GetName(),
			Coalition: cu.GetClientInfo().GetCoalition(),
			UnitId:    cu.GetClientInfo().GetUnitId(),
			RoleId:    cu.GetClientInfo().GetRoleId(),
		})
	case srspb.ServerUpdate_CLIENT_LEFT:
		cu := upd.GetClientUpdate()
		if cu == nil {
			return
		}
		st.RemoveClient(cu.GetClientGuid())
		tagged.ClientLeft(cu.GetClientGuid())
	case srspb.ServerUpdate_CLIENT_RADIO_UPDATE:
		cu := upd.GetClientUpdate()
		if cu == nil || cu.GetRadioInfo() == nil {
			return
		}
		st.SetRadios(cu.GetClientGuid(), cu.GetRadioInfo())
		tagged.RadioUpdate(cu.GetClientGuid(), radioInfoToPayload(cu.GetRadioInfo()))
	case srspb.ServerUpdate_SERVER_SETTINGS_CHANGED:
		if s := upd.GetSettingsUpdate(); s != nil {
			st.SetSettings(s)
			tagged.SettingsUpdate(struct{}{})
		}
	case srspb.ServerUpdate_SERVER_ACTION:
		tagged.ServerAction(struct{}{})
	default:
		// DISTRIBUTION_UPDATE / VOICE_ADDRESS_UPDATE / UNKNOWN — ignored this phase.
	}
}

func radioInfoToPayload(ri *srspb.RadioInfo) events.RadioInfoPayload {
	out := events.RadioInfoPayload{Muted: ri.GetMuted()}
	for _, r := range ri.GetRadios() {
		out.Radios = append(out.Radios, events.RadioPayload{
			ID:         r.GetId(),
			Name:       r.GetName(),
			Frequency:  r.GetFrequency(),
			Enabled:    r.GetEnabled(),
			IsIntercom: r.GetIsIntercom(),
		})
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/control/...`
Expected: PASS (all control tests).

- [ ] **Step 5: Commit**

```powershell
git add internal/control
git commit -m "feat: add control stream consumer routing updates to state+events"
```

---

## Task 6: `internal/app/dto.go` — binding DTOs + mapping

**Files:**
- Create: `internal/app/dto.go`, `internal/app/dto_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/app/dto_test.go`:
```go
package app_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

func TestRadioInfoFromDTO_RoundTrip(t *testing.T) {
	dto := app.RadioInfoDTO{
		Muted:  true,
		Radios: []app.RadioDTO{{ID: 1, Name: "Fleet", Frequency: 118.5, Enabled: true, IsIntercom: false}},
	}
	pb := app.RadioInfoToProto(dto)
	if pb.GetMuted() != true || len(pb.GetRadios()) != 1 || pb.GetRadios()[0].GetName() != "Fleet" {
		t.Fatalf("to-proto mismatch: %+v", pb)
	}
	back := app.RadioInfoFromProto(pb)
	if back.Radios[0].Frequency != 118.5 || !back.Muted {
		t.Fatalf("from-proto mismatch: %+v", back)
	}
}

func TestSnapshotFromState_MapsClients(t *testing.T) {
	pb := map[string]*srspb.ClientInfo{"g1": {Name: "Al", Coalition: "VG"}}
	snap := app.SnapshotFromProto(pb, nil)
	if snap.Clients["g1"].Name != "Al" {
		t.Fatalf("unexpected: %+v", snap)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/app/ -run "DTO|Snapshot"`
Expected: FAIL — types/functions undefined.

- [ ] **Step 3: Write `internal/app/dto.go`**

```go
package app

import srspb "github.com/FPGSchiba/vcs-srs-client/srspb"

// RadioDTO is the binding-facing radio shape.
type RadioDTO struct {
	ID         uint32  `json:"id"`
	Name       string  `json:"name"`
	Frequency  float32 `json:"frequency"`
	Enabled    bool    `json:"enabled"`
	IsIntercom bool    `json:"is_intercom"`
}

// RadioInfoDTO is the binding-facing radio-set shape.
type RadioInfoDTO struct {
	Radios []RadioDTO `json:"radios"`
	Muted  bool       `json:"muted"`
}

// ClientInfoDTO is the binding-facing client shape.
type ClientInfoDTO struct {
	Name      string `json:"name"`
	Coalition string `json:"coalition"`
	UnitID    string `json:"unit_id"`
	RoleID    uint32 `json:"role_id"`
}

// ClientStateSnapshot is returned by GetClientState.
type ClientStateSnapshot struct {
	Clients map[string]ClientInfoDTO `json:"clients"`
	Radios  map[string]RadioInfoDTO  `json:"radios"`
}

// RadioInfoToProto maps a DTO to the proto message.
func RadioInfoToProto(d RadioInfoDTO) *srspb.RadioInfo {
	out := &srspb.RadioInfo{Muted: d.Muted}
	for _, r := range d.Radios {
		out.Radios = append(out.Radios, &srspb.Radio{
			Id: r.ID, Name: r.Name, Frequency: r.Frequency,
			Enabled: r.Enabled, IsIntercom: r.IsIntercom,
		})
	}
	return out
}

// RadioInfoFromProto maps a proto message to a DTO.
func RadioInfoFromProto(p *srspb.RadioInfo) RadioInfoDTO {
	out := RadioInfoDTO{Muted: p.GetMuted()}
	for _, r := range p.GetRadios() {
		out.Radios = append(out.Radios, RadioDTO{
			ID: r.GetId(), Name: r.GetName(), Frequency: r.GetFrequency(),
			Enabled: r.GetEnabled(), IsIntercom: r.GetIsIntercom(),
		})
	}
	return out
}

func clientInfoFromProto(p *srspb.ClientInfo) ClientInfoDTO {
	return ClientInfoDTO{
		Name: p.GetName(), Coalition: p.GetCoalition(),
		UnitID: p.GetUnitId(), RoleID: p.GetRoleId(),
	}
}

// SnapshotFromProto builds a snapshot DTO from proto maps.
func SnapshotFromProto(clients map[string]*srspb.ClientInfo, radios map[string]*srspb.RadioInfo) ClientStateSnapshot {
	snap := ClientStateSnapshot{
		Clients: map[string]ClientInfoDTO{},
		Radios:  map[string]RadioInfoDTO{},
	}
	for g, c := range clients {
		snap.Clients[g] = clientInfoFromProto(c)
	}
	for g, r := range radios {
		snap.Radios[g] = RadioInfoFromProto(r)
	}
	return snap
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/app/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/app/dto.go internal/app/dto_test.go
git commit -m "feat: add app binding DTOs and proto mapping"
```

---

## Task 7: `internal/session` — Connect orchestrator

**Files:**
- Create: `internal/session/session.go`, `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/session_test.go`:
```go
package session_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/auth"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

type capEmitter struct {
	mu    sync.Mutex
	names []string
}
func (c *capEmitter) Emit(n string, _ any) { c.mu.Lock(); c.names = append(c.names, n); c.mu.Unlock() }
func (c *capEmitter) saw(n string) bool {
	c.mu.Lock(); defer c.mu.Unlock()
	for _, x := range c.names { if x == n { return true } }
	return false
}

func TestConnect_HappyPath(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	st := state.New()
	em := &capEmitter{}
	s := session.New(st, em, session.Deps{Dialer: dial, Version: "test"})

	if err := s.Connect(context.Background(), "localhost:0", "Name", "pw", "unit"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !em.saw("control:connection") || !em.saw("auth:session_changed") {
		t.Fatalf("expected connect events, got %v", em.names)
	}
}

func TestConnect_GuestUnavailable(t *testing.T) {
	f := grpctest.NewFake()
	f.HasGuest = false
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	s := session.New(state.New(), &capEmitter{}, session.Deps{Dialer: dial, Version: "test"})
	err := s.Connect(context.Background(), "localhost:0", "n", "p", "u")
	if !errors.Is(err, auth.ErrGuestUnavailable) {
		t.Fatalf("expected ErrGuestUnavailable, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/session/...`
Expected: FAIL — package `session` does not exist.

- [ ] **Step 3: Write `internal/session/session.go`**

```go
// Package session orchestrates the guest connect sequence and owns the gRPC
// connection lifecycle.
package session

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc"

	"github.com/FPGSchiba/vcs-srs-client/internal/auth"
	"github.com/FPGSchiba/vcs-srs-client/internal/control"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// Dialer opens a gRPC connection to the resolved target.
type Dialer func(ctx context.Context) (*grpc.ClientConn, error)

// Deps are injected dependencies (the Dialer is overridable in tests).
type Deps struct {
	Dialer  Dialer // if nil, Connect builds an insecure-localhost dialer from serverURL
	Version string
}

// Session orchestrates connect/disconnect and owns the live connection.
type Session struct {
	st  *state.Store
	em  events.Emitter
	dep Deps

	mu      sync.Mutex
	conn    *grpc.ClientConn
	control *control.Client
	cancel  context.CancelFunc
}

// New constructs a Session.
func New(st *state.Store, em events.Emitter, dep Deps) *Session {
	return &Session{st: st, em: em, dep: dep}
}

// Connect runs the full guest sequence. Returns a typed error synchronously for
// any pre-connected failure; post-connected transitions flow through events.
func (s *Session) Connect(ctx context.Context, serverURL, name, password, unitID string) error {
	tagged := events.New(s.em)
	tagged.ConnectionState(events.ConnReconnecting) // "connecting" surfaced as a transient; see note

	dialer := s.dep.Dialer
	if dialer == nil {
		d, err := insecureDialer(serverURL)
		if err != nil {
			return err
		}
		dialer = d
	}

	conn, err := dialer(ctx)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	ac := auth.New(conn)
	init, err := ac.InitAuth(ctx, s.dep.Version)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if !init.HasGuest {
		_ = conn.Close()
		return auth.ErrGuestUnavailable
	}
	guest, err := ac.GuestLogin(ctx, name, password, unitID, init.ClientGUID)
	if err != nil {
		_ = conn.Close()
		return err
	}

	cc := control.New(conn, guest.Token)
	if err := cc.SyncClient(ctx, s.st); err != nil {
		_ = conn.Close()
		return err
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.conn, s.control, s.cancel = conn, cc, cancel
	s.mu.Unlock()

	go func() { _ = cc.ConsumeUpdates(streamCtx, s.st, s.em) }()

	tagged.ConnectionState(events.ConnConnected)
	tagged.SessionChanged("logged_in")
	return nil
}

// Disconnect tears down the stream and connection.
func (s *Session) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	conn, cc, cancel := s.conn, s.control, s.cancel
	s.conn, s.control, s.cancel = nil, nil, nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cc != nil {
		_ = cc.Disconnect(ctx)
	}
	if conn != nil {
		_ = conn.Close()
	}
	tagged := events.New(s.em)
	tagged.ConnectionState(events.ConnDisconnected)
	tagged.SessionChanged("logged_out")
	return nil
}

// Control returns the live control client (nil if not connected).
func (s *Session) Control() *control.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.control
}
```

- [ ] **Step 4: Write `internal/session/dial.go`**

```go
package session

import (
	"context"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// insecureDialer returns a Dialer for serverURL. Insecure transport is only
// permitted for localhost/127.0.0.1; remote hosts fail closed until TLS lands.
func insecureDialer(serverURL string) (Dialer, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server address is empty")
	}
	host, _, err := net.SplitHostPort(serverURL)
	if err != nil {
		return nil, fmt.Errorf("server address must be host:port: %w", err)
	}
	if !isLocal(host) {
		return nil, fmt.Errorf("refusing insecure connection to non-local host %q (TLS not yet supported)", host)
	}
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, serverURL,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
	}, nil
}

func isLocal(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasPrefix(host, "127.")
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/session/...`
Expected: PASS (2 tests). Note: the happy-path test injects `Deps.Dialer`, so `localhost:0` is never really dialed.

- [ ] **Step 6: Run the full backend suite + vet**

Run:
```powershell
go test ./internal/... ./pkg/...
go vet ./internal/... ./pkg/...
go build ./...
```
Expected: all green.

- [ ] **Step 7: Commit**

```powershell
git add internal/session
git commit -m "feat: add session orchestrator for guest connect/disconnect"
```

---

## Self-Review (performed)

**Spec coverage (§3, §5, §7):** auth guest client ✓ (T2), control client+ping ✓ (T4), stream routing ✓ (T5), events extensions ✓ (T3), DTO mapping ✓ (T6), session Connect/Disconnect ✓ (T7), fake server harness ✓ (T1). `Reconnect()` is intentionally deferred to the start of 2B (it needs the binding + ping-failure wiring) — noted here so it isn't lost.

**Placeholders:** none — every step has runnable code/commands. "Port from design file" does not appear in 2A.

**Type consistency:** `events.RadioInfoPayload`/`RadioPayload` reused by control stream (T5) and defined in T3; `state.Store` methods (`UpdateClient`/`SetRadios`/`SetSettings`/`RemoveClient`/`Client`) match Part 1; `srspb` getters verified against generated code; `session.Deps.Dialer` matches `grpctest.Start`'s returned `dial` signature `func(context.Context)(*grpc.ClientConn,error)`.

**Carry-forward to 2B:** `Reconnect()` binding + ping-driven failure detection (bounded backoff emitting `reconnecting`/`disconnected`).
