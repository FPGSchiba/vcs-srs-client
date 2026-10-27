// Package grpctest provides an in-process fake VCS server (Auth + SRS) for tests.
package grpctest

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// Fake is a configurable in-process Auth+SRS server.
type Fake struct {
	srspb.UnimplementedAuthServiceServer
	srspb.UnimplementedSRSServiceServer

	mu sync.Mutex

	// Auth behavior
	HasGuest       bool
	ClientGUID     string
	RejectLogin    bool
	GuestToken     string
	GuestCoalition string

	// SRS behavior
	SyncClients  map[string]*srspb.ClientInfo
	SyncRadios   map[string]*srspb.RadioInfo
	SyncSettings *srspb.ServerSettings
	LastRadio    *srspb.RadioInfo // records UpdateRadioInfo input

	// SyncCoalitionVoiceAddr, SyncGlobalVoiceAddr and SyncVoiceSecret are
	// returned on ServerSyncResult. Left "" by default, matching a
	// standalone server, which never populates the registry-backed
	// coalition/global addrs.
	SyncCoalitionVoiceAddr string
	SyncGlobalVoiceAddr    string
	SyncVoiceSecret        string

	updates chan *srspb.ServerUpdate // pushed to SubscribeToUpdates

	// Test knobs, all guarded by mu. See Block, Fail, Calls and EndStream.
	blocks    map[string]chan struct{}
	fails     map[string]error
	calls     []string
	streamEnd chan error
	active    int // SubscribeToUpdates handlers currently running

	// subDelay delays SubscribeToUpdates before it records and registers
	// the subscription; noSubHeader suppresses the subscription ack (the
	// pre-SV2-C17 server). See SetSubscribeDelay and SetSubscribeAck.
	subDelay    time.Duration
	noSubHeader bool
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
		streamEnd:      make(chan error, 4),
	}
}

// Block makes every later call to method ("InitAuth", "GuestLogin",
// "SyncClient", "Disconnect" or "UpdateRadioInfo") wait until ch is closed
// or the RPC's context is done. Passing nil removes the block.
func (f *Fake) Block(method string, ch chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blocks == nil {
		f.blocks = map[string]chan struct{}{}
	}
	if ch == nil {
		delete(f.blocks, method)
		return
	}
	f.blocks[method] = ch
}

// Fail makes every later call to method ("SyncClient", "Ping",
// "UpdateRadioInfo", "Disconnect" or "SubscribeToUpdates") return err. A
// failed SubscribeToUpdates is recorded but never acknowledged. Passing nil removes it.
func (f *Fake) Fail(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fails == nil {
		f.fails = map[string]error{}
	}
	if err == nil {
		delete(f.fails, method)
		return
	}
	f.fails[method] = err
}

// Calls returns the ordered names of every RPC handled so far, including
// "SubscribeToUpdates".
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// ActiveStreams returns how many SubscribeToUpdates handlers are running. A
// stream the client cancelled or closed stops counting once the server
// notices, so a leaked client stream shows up as a count above one.
func (f *Fake) ActiveStreams() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

// SetSync replaces the clients and voice secret the next SyncClient returns
// (race-safe, for use while a session is live).
func (f *Fake) SetSync(clients map[string]*srspb.ClientInfo, secret string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SyncClients = clients
	f.SyncVoiceSecret = secret
}

// SetSubscribeDelay makes every later SubscribeToUpdates wait d before it
// records and registers the subscription. It widens the window between the
// client's request leaving and the server registering the stream, which
// grpc-go's client does not wait for on its own.
func (f *Fake) SetSubscribeDelay(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subDelay = d
}

// SetSubscribeAck controls whether SubscribeToUpdates acknowledges the
// registered subscription by sending (empty) response headers -- SV2 C17,
// the server's stream.SendHeader once the subscriber is registered. On by
// default; false imitates a server from before C17 (e.g. dcce3ad), whose
// headers go out only with the first update.
func (f *Fake) SetSubscribeAck(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.noSubHeader = !on
}

// EndStream makes the current SubscribeToUpdates handler return err (a nil
// err ends it cleanly). A stream the client has already cancelled returns on
// its own context instead, so it cannot consume a later EndStream.
func (f *Fake) EndStream(err error) {
	f.mu.Lock()
	if f.streamEnd == nil {
		f.streamEnd = make(chan error, 4)
	}
	ch := f.streamEnd
	f.mu.Unlock()
	ch <- err
}

// enter records the call and applies any Block/Fail knob for method.
func (f *Fake) enter(ctx context.Context, method string) error {
	f.mu.Lock()
	f.calls = append(f.calls, method)
	block := f.blocks[method]
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fails[method]
}

// PushUpdate enqueues a ServerUpdate to be delivered on the open stream.
func (f *Fake) PushUpdate(u *srspb.ServerUpdate) { f.updates <- u }

// CloseStream signals SubscribeToUpdates to return (simulates a drop).
func (f *Fake) CloseStream() { close(f.updates) }

func (f *Fake) InitAuth(ctx context.Context, _ *srspb.AuthInitRequest) (*srspb.AuthInitResponse, error) {
	if err := f.enter(ctx, "InitAuth"); err != nil {
		return nil, err
	}
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

func (f *Fake) GuestLogin(ctx context.Context, _ *srspb.GuestLoginRequest) (*srspb.GuestLoginResponse, error) {
	if err := f.enter(ctx, "GuestLogin"); err != nil {
		return nil, err
	}
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

func (f *Fake) SyncClient(ctx context.Context, _ *srspb.Empty) (*srspb.SyncResponse, error) {
	if err := f.enter(ctx, "SyncClient"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return &srspb.SyncResponse{
		Success: true,
		Version: "test",
		SyncResult: &srspb.SyncResponse_Data{
			Data: &srspb.ServerSyncResult{
				Clients:            f.SyncClients,
				Radios:             f.SyncRadios,
				Settings:           f.SyncSettings,
				CoalitionVoiceAddr: f.SyncCoalitionVoiceAddr,
				GlobalVoiceAddr:    f.SyncGlobalVoiceAddr,
				VoiceSecret:        f.SyncVoiceSecret,
			},
		},
	}, nil
}

func (f *Fake) UpdateRadioInfo(ctx context.Context, r *srspb.RadioInfo) (*srspb.ServerResponse, error) {
	if err := f.enter(ctx, "UpdateRadioInfo"); err != nil {
		return nil, err
	}
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

func (f *Fake) Disconnect(ctx context.Context, _ *srspb.Empty) (*srspb.ServerResponse, error) {
	if err := f.enter(ctx, "Disconnect"); err != nil {
		return nil, err
	}
	return &srspb.ServerResponse{Success: true}, nil
}

func (f *Fake) Ping(ctx context.Context, _ *srspb.PingRequest) (*srspb.PingResponse, error) {
	if err := f.enter(ctx, "Ping"); err != nil {
		return nil, err
	}
	return &srspb.PingResponse{ServerTimeMs: 1}, nil
}

// SubscribeToUpdates behaves like the server after SV2 C17: it records and
// registers the subscriber, and only then acknowledges it with
// stream.SendHeader, so a client that waits on Header() knows an update
// published from here on reaches this stream.
func (f *Fake) SubscribeToUpdates(_ *srspb.Empty, stream srspb.SRSService_SubscribeToUpdatesServer) error {
	f.mu.Lock()
	delay := f.subDelay
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, "SubscribeToUpdates")
	if err := f.fails["SubscribeToUpdates"]; err != nil {
		f.mu.Unlock()
		return err
	}
	f.active++
	end := f.streamEnd
	ack := !f.noSubHeader
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	if ack {
		if err := stream.SendHeader(metadata.MD{}); err != nil {
			return err
		}
	}
	for {
		select {
		case u, ok := <-f.updates:
			if !ok {
				return nil // channel closed → stream ends (simulates drop)
			}
			if err := stream.Send(u); err != nil {
				return err
			}
		case err := <-end:
			return err
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// LastRadioInfo returns the most recent UpdateRadioInfo payload (race-safe).
func (f *Fake) LastRadioInfo() *srspb.RadioInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.LastRadio
}

// Start launches the fake on a plaintext bufconn and returns a dialer +
// cleanup. Preserved for the tests written before TLS existed.
//
// Its dialer is now BLOCKING where it was previously lazy: StartWith's
// grpc.WithReturnConnectionError() implies WithBlock(), so the returned
// dial func waits for the connection attempt to resolve instead of handing
// back an unconnected *grpc.ClientConn immediately. Every existing caller
// already passes a context with a deadline or awaits the result, so this is
// unobserved today -- but a future caller dialing with a cancelled or very
// short-lived context would now see that context's error instead of a
// nil-error, not-yet-connected ClientConn.
func Start(t *testing.T, f *Fake) (dial func(context.Context) (*grpc.ClientConn, error), cleanup func()) {
	t.Helper()
	dialWith, cleanup := StartWith(t, f, nil)
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		return dialWith(ctx, insecure.NewCredentials())
	}, cleanup
}

// StartWith launches the fake on a bufconn with the given SERVER transport
// credentials (nil means plaintext) and returns a dialer that lets each test
// choose its own CLIENT credentials.
//
// Splitting the two sides is what makes the mismatch cases testable at all:
// a TLS client against a plaintext server, and a client pinning the wrong
// issuer. And running TLS over bufconn rather than a real socket is
// deliberate -- TLS is just a byte stream over the conn, so the genuine
// handshake runs with no call to bind(2), which the development sandbox
// blocks.
func StartWith(t *testing.T, f *Fake, serverCreds credentials.TransportCredentials) (dialWith func(context.Context, credentials.TransportCredentials) (*grpc.ClientConn, error), cleanup func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)

	var opts []grpc.ServerOption
	if serverCreds != nil {
		opts = append(opts, grpc.Creds(serverCreds))
	}
	srv := grpc.NewServer(opts...)
	srspb.RegisterAuthServiceServer(srv, f)
	srspb.RegisterSRSServiceServer(srv, f)
	go func() { _ = srv.Serve(lis) }()

	dialWith = func(ctx context.Context, clientCreds credentials.TransportCredentials) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, "bufnet",
			grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) { return lis.DialContext(c) }),
			grpc.WithTransportCredentials(clientCreds),
			// Without this a failed handshake surfaces as a bare context
			// deadline and the real cause is lost, which would make the
			// refusal tests below unable to tell a rejection from a timeout.
			grpc.WithReturnConnectionError(),
		)
	}
	cleanup = func() { srv.Stop(); _ = lis.Close() }
	return dialWith, cleanup
}

// DialUntilFailure starts the fake on its own bufconn with serverCreds (nil
// means plaintext), connects a NON-blocking client with clientCreds, waits
// until that channel reports TransientFailure, and returns the error of one
// RPC issued without wait-for-ready -- which carries the last connection
// error, e.g. "x509: certificate signed by unknown authority".
//
// It exists for the TLS refusal tests. They used to dial blocking with
// WithReturnConnectionError under a 500ms deadline, and that only surfaces
// the handshake error if the first handshake has completed inside the
// deadline; under a loaded -race run it sometimes had not, and the test saw a
// bare "context deadline exceeded". Waiting for the channel state instead
// returns as soon as the handshake fails, however slow the machine is.
//
// The server is stopped through t.Cleanup. It fails the test if the channel
// reaches Ready (the handshake was expected to fail) or if no failure is seen
// within 10s.
func DialUntilFailure(t *testing.T, f *Fake, serverCreds, clientCreds credentials.TransportCredentials) error {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	var opts []grpc.ServerOption
	if serverCreds != nil {
		opts = append(opts, grpc.Creds(serverCreds))
	}
	srv := grpc.NewServer(opts...)
	srspb.RegisterAuthServiceServer(srv, f)
	srspb.RegisterSRSServiceServer(srv, f)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop(); _ = lis.Close() })

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) { return lis.DialContext(c) }),
		grpc.WithTransportCredentials(clientCreds),
	)
	if err != nil {
		t.Fatalf("DialUntilFailure: NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()
	conn.Connect()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		st := conn.GetState()
		if st == connectivity.TransientFailure {
			break
		}
		if st == connectivity.Ready {
			t.Fatal("DialUntilFailure: the channel became Ready; expected the handshake to fail")
		}
		if !conn.WaitForStateChange(ctx, st) {
			t.Fatalf("DialUntilFailure: no TransientFailure within 10s (last state %v)", st)
		}
	}

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer rpcCancel()
	_, err = srspb.NewSRSServiceClient(conn).Ping(rpcCtx, &srspb.PingRequest{})
	return err
}
