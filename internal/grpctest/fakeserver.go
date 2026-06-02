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
