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

	lastDialer Dialer
	lastToken  string
}

// New constructs a Session.
func New(st *state.Store, em events.Emitter, dep Deps) *Session {
	return &Session{st: st, em: em, dep: dep}
}

// Connect runs the full guest sequence. Returns a typed error synchronously for
// any pre-connected failure; post-connected transitions flow through events.
func (s *Session) Connect(ctx context.Context, serverURL, name, password, unitID string) error {
	tagged := events.New(s.em)
	tagged.ConnectionState(events.ConnReconnecting) // transient "connecting" surfaced to UI

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

	s.mu.Lock()
	s.lastDialer = dialer
	s.lastToken = guest.Token
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

// Control returns the live control client (nil if not connected).
func (s *Session) Control() *control.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.control
}
