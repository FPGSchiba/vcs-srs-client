// Package session orchestrates the guest connect sequence and owns the gRPC
// connection lifecycle.
package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"

	"github.com/FPGSchiba/vcs-srs-client/internal/auth"
	"github.com/FPGSchiba/vcs-srs-client/internal/control"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// Dialer opens a gRPC connection to the resolved target.
type Dialer func(ctx context.Context) (*grpc.ClientConn, error)

// Deps are injected dependencies (the Dialer is overridable in tests).
type Deps struct {
	Dialer  Dialer // if nil, Connect builds an insecure-localhost dialer from serverURL
	Version string

	// OnControlState observes every control-link transition, alongside the
	// events.EventControlConnection emission. It exists so the connection-
	// health model is fed from the SAME call sites that emit the lifecycle
	// event -- which is what makes the two incapable of disagreeing about
	// the link. Optional; nil is a no-op.
	//
	// Called synchronously on the transitioning goroutine, so it must not
	// block. connhealth.Monitor.SetControlState satisfies that.
	OnControlState func(events.ConnectionState)
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

	// streamGen identifies the current update stream. A goroutine captures
	// it at launch and compares before reporting termination, so a stale
	// stream from a previous connection cannot report disconnected over a
	// healthy new one.
	//
	// This is the same pattern internal/app/voice.go uses for voice session
	// generations, and for the same reason: the generation must be captured
	// BEFORE the goroutine is scheduled, not inside it.
	streamGen uint64
}

// New constructs a Session.
func New(st *state.Store, em events.Emitter, dep Deps) *Session {
	return &Session{st: st, em: em, dep: dep}
}

// setConnState is the one place a control transition is published. Both the
// lifecycle event and the health observer are driven from here so no call
// site can ever feed one without the other.
func (s *Session) setConnState(st events.ConnectionState) {
	events.New(s.em).ConnectionState(st)
	s.mu.Lock()
	fn := s.dep.OnControlState
	s.mu.Unlock()
	if fn != nil {
		fn(st)
	}
}

// SetControlStateObserver installs the control-transition observer after
// construction. It exists because the session and the health monitor each
// need the other -- the monitor probes through Session.PingOnce, the session
// reports through the monitor's SetControlState -- so one of the two links
// must be late-bound, and this is the one with somewhere to put it.
//
// Must be called before Connect. Setting it twice replaces the first.
func (s *Session) SetControlStateObserver(fn func(events.ConnectionState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dep.OnControlState = fn
}

// startStreamLocked launches the update-stream consumer and returns the
// generation it was launched under. Caller holds s.mu.
func (s *Session) startStreamLocked(ctx context.Context, cc *control.Client) {
	s.streamGen++
	gen := s.streamGen
	go func() {
		err := cc.ConsumeUpdates(ctx, s.st, s.em)
		s.handleStreamEnd(ctx, gen, err)
	}()
}

// handleStreamEnd reports an update stream's termination as a control-link
// loss -- unless we caused it, or it belongs to a connection that has since
// been replaced.
//
// Before this existed the error went into `_ =` at both call sites, so a
// server restart, a network drop, or the server's SubscribeToUpdates
// refusing a duplicate subscription all left the UI reporting connected
// indefinitely, with ConnBanner's disconnected variant sitting behind a
// signal that never arrived.
func (s *Session) handleStreamEnd(ctx context.Context, gen uint64, err error) {
	// Intent guard. A cancelled context means Disconnect or Reconnect ended
	// this stream on purpose; that path publishes its own state, and
	// emitting here would stack a spurious disconnected on top of a
	// teardown or a reconnect already under way.
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		s.log("update stream ended", err)
	}
	s.markControlLost(gen)
}

// log reports a stream-level problem. Session has no logger of its own, and
// adding one is out of this phase's scope; slog.Default is routed to the
// rotating file by main.go.
func (s *Session) log(msg string, err error) {
	slog.Default().Warn("session: "+msg, "err", err)
}

// MarkControlLost declares the control link dead from outside the stream
// goroutine -- the probe detector's entry point, called when consecutive
// pings have failed past the threshold.
//
// It is idempotent against the stream-termination path: both detectors fire
// on a real cable pull, and the user must see one disconnected, not two.
func (s *Session) MarkControlLost() {
	s.mu.Lock()
	gen := s.streamGen
	s.mu.Unlock()
	s.markControlLost(gen)
}

// markControlLost tears the dead connection down and publishes the loss,
// exactly once per generation.
//
// F6 correction (Phase 6 whole-branch review): gen is read fresh, immediately
// before this call, by MarkControlLost or handleStreamEnd -- it is whatever
// generation is CURRENT at call time, not the generation whichever detector
// originally measured the failure against. That is enough to order this
// entry point against a concurrent STREAM DEATH: if the other detector has
// already fired, it has already bumped streamGen and cleared s.conn, so the
// freshly-read gen already reflects that and the guard below is a no-op --
// whichever detector arrives first wins, the other's fresh read already
// disagrees with itself.
//
// It is NOT ordered against a concurrent SUCCESSFUL Reconnect. Nothing stops
// a Reconnect from dialing, syncing and installing a brand-new connection
// (which also bumps streamGen exactly once) inside the gap between the probe
// detector deciding to fire OnLoss and this call re-reading streamGen a few
// function calls later -- and if that gap is hit, the freshly-read gen
// matches the NEW generation, and this call tears the new, healthy
// connection down instead of a no-op. Closing that window would need a full
// dial + SyncClient to complete inside a handful of lock-free function calls
// with no I/O of their own, so it is considered unreachable in practice, not
// actually closed by this code.
func (s *Session) markControlLost(gen uint64) {
	s.mu.Lock()
	if gen != s.streamGen || s.conn == nil {
		// Already handled by the other detector, or belongs to a connection
		// that has since been replaced.
		s.mu.Unlock()
		return
	}
	// Bump so the losing detector's captured generation goes stale.
	s.streamGen++
	cancel := s.cancel
	conn := s.conn
	s.conn, s.control, s.cancel = nil, nil, nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	s.setConnState(events.ConnDisconnected)
}

// PingOnce probes the control plane through the live control client,
// returning the round trip in milliseconds and echoing lastRTTMs to the
// server.
//
// The echo is not decoration: srs_service.go's Ping handler writes
// client.LatencyToControlMs = req.LastRttMs and nothing else ever does, so
// dropping it leaves the server's per-client latency map at zero for every
// VCS client.
//
// Returns an error when no control client is live, which is what a probe
// racing a teardown sees.
func (s *Session) PingOnce(ctx context.Context, lastRTTMs int64) (int64, error) {
	s.mu.Lock()
	cc := s.control
	s.mu.Unlock()
	if cc == nil {
		return 0, fmt.Errorf("ping: not connected")
	}
	return cc.PingOnce(ctx, lastRTTMs)
}

// Connect runs the full guest sequence. Returns a typed error synchronously for
// any pre-connected failure; post-connected transitions flow through events.
func (s *Session) Connect(ctx context.Context, serverURL, name, password, unitID string) error {
	tagged := events.New(s.em)
	s.setConnState(events.ConnReconnecting) // transient "connecting" surfaced to UI

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
	// The server matches the coalition by bcrypt-comparing the client-supplied
	// hash against its stored coalition passwords, so we never send plaintext.
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("hash password: %w", err)
	}
	guest, err := ac.GuestLogin(ctx, name, string(hashed), unitID, init.ClientGUID)
	if err != nil {
		_ = conn.Close()
		return err
	}

	cc := control.New(conn, guest.Token)
	if err := cc.SyncClient(ctx, s.st); err != nil {
		_ = conn.Close()
		return err
	}

	// Record the local client identity for the UI (callsign/FFID from the form,
	// coalition resolved by the server from the password).
	s.st.SetSelf(init.ClientGUID, &srspb.ClientInfo{
		Name:      name,
		Coalition: guest.Coalition,
		UnitId:    unitID,
	})

	streamCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.conn, s.control, s.cancel = conn, cc, cancel
	s.lastDialer = dialer
	s.lastToken = guest.Token
	s.startStreamLocked(streamCtx, cc)
	s.mu.Unlock()

	s.setConnState(events.ConnConnected)
	tagged.SessionChanged("logged_in")
	return nil
}

// Disconnect tears down the stream and connection.
func (s *Session) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	conn, cc, cancel := s.conn, s.control, s.cancel
	s.conn, s.control, s.cancel = nil, nil, nil
	s.streamGen++ // retire this stream's generation
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
	s.st.ClearSelf()
	tagged := events.New(s.em)
	s.setConnState(events.ConnDisconnected)
	tagged.SessionChanged("logged_out")
	return nil
}

// Reconnect re-establishes the control session reusing the stored token (no
// re-auth). Emits reconnecting → connected on success, or disconnected on failure.
func (s *Session) Reconnect(ctx context.Context) error {
	s.setConnState(events.ConnReconnecting)

	s.mu.Lock()
	dialer, token := s.lastDialer, s.lastToken
	s.mu.Unlock()
	if dialer == nil {
		s.setConnState(events.ConnDisconnected)
		return fmt.Errorf("cannot reconnect: never connected")
	}

	conn, err := dialer(ctx)
	if err != nil {
		s.setConnState(events.ConnDisconnected)
		return fmt.Errorf("reconnect dial: %w", err)
	}
	cc := control.New(conn, token)
	if err := cc.SyncClient(ctx, s.st); err != nil {
		_ = conn.Close()
		s.setConnState(events.ConnDisconnected)
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
	s.startStreamLocked(streamCtx, cc)
	s.mu.Unlock()
	s.setConnState(events.ConnConnected)
	return nil
}

// UpdateRadioInfo pushes a radio change via the live control client.
func (s *Session) UpdateRadioInfo(ctx context.Context, info *srspb.RadioInfo) error {
	s.mu.Lock()
	cc := s.control
	s.mu.Unlock()
	if cc == nil {
		return fmt.Errorf("not connected")
	}
	return cc.UpdateRadioInfo(ctx, info)
}

// Control returns the live control client (nil if not connected).
func (s *Session) Control() *control.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.control
}
