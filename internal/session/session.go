// Package session orchestrates the guest connect sequence and owns the gRPC
// connection lifecycle.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

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
	// Dialer opens the connection. If nil, Connect builds one from serverURL
	// and TLSCAFile via dialerFor: insecure only for a loopback host, TLS
	// (system roots, or the pinned CA when TLSCAFile is set) otherwise.
	Dialer  Dialer
	Version string
	// TLSCAFile is config.Config.TLSCAFile, threaded here so dialerFor can
	// reach it. Connect and Reconnect both build dialers, so it belongs on
	// the dependency struct rather than on the Connect call signature.
	TLSCAFile string

	// OnControlState observes every control-link transition, alongside the
	// events.EventControlConnection emission. It exists so the connection-
	// health model is fed from the SAME call sites that emit the lifecycle
	// event -- which is what makes the two incapable of disagreeing about
	// the link. Optional; nil is a no-op.
	//
	// Called synchronously on the transitioning goroutine, so it must not
	// block. connhealth.Monitor.SetControlState satisfies that.
	OnControlState func(events.ConnectionState)

	// OnSessionEnded is called (no Session lock held) when the server has
	// ended this session, before the store is cleared. Optional; set late
	// through SetSessionEndedHook.
	OnSessionEnded func(reason string)

	// PostDialTimeout bounds everything Connect does after the dial
	// (InitAuth, the bcrypt hash, GuestLogin, SyncClient) and Reconnect's
	// SyncClient. 0 means defaultPostDialTimeout. Overridable for tests.
	PostDialTimeout time.Duration
}

const (
	// defaultPostDialTimeout is D1 (user decision): a server that completes
	// the dial and then never answers fails the connect after 15 s instead
	// of hanging the Welcome screen forever. The dial keeps its own 10 s
	// bound (dialTimeout), so the worst case is 25 s.
	defaultPostDialTimeout = 15 * time.Second

	// cancelDisconnectTimeout bounds the best-effort Disconnect a cancelled
	// or timed-out Connect sends when GuestLogin had already issued a token
	// (D3), so the server frees the session at once instead of reaping it.
	cancelDisconnectTimeout = 2 * time.Second
)

// Session orchestrates connect/disconnect and owns the live connection.
type Session struct {
	st  *state.Store
	em  events.Emitter
	dep Deps

	// lifeMu serialises Connect, Reconnect and Disconnect with each other.
	// Lock order is lifeMu -> mu only. CancelConnect, endSession, resync and
	// markControlLost never take it: CancelConnect must not wait behind the
	// attempt it is cancelling.
	lifeMu sync.Mutex

	mu      sync.Mutex
	conn    *grpc.ClientConn
	control *control.Client
	cancel  context.CancelFunc

	lastDialer Dialer
	lastToken  string

	// attemptCancel cancels the Connect/Reconnect attempt in flight, nil
	// when none is. Guarded by mu.
	attemptCancel context.CancelFunc

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
	s.mu.Lock()
	current := gen == s.streamGen
	s.mu.Unlock()
	if !current {
		return // a replaced connection's stream
	}
	if err != nil {
		s.log("update stream ended", err)
	}
	// Branch on the status code only (classifyRPCError); see spec §3.4.
	switch v, reason := classifyRPCError(err); v {
	case verdictSessionEnded:
		// SV1 C5/C7: removed, kicked, banned, stale, unknown session.
		_ = s.endSession(reason)
	case verdictSuperseded:
		// SV1 C6 (D9): another subscriber took our stream with our token.
		// Not a logout: an ordinary control loss the user can RECONNECT from.
		slog.Default().Warn("session: update stream superseded by another subscriber", "err", err)
		s.markControlLost(gen)
	case verdictResync:
		s.markControlLost(gen) // replaced by the C13 resync in Task 7
	default:
		s.markControlLost(gen)
	}
}

// SetSessionEndedHook installs the hook endSession calls (with no Session
// lock held) before it clears the store: App stops the voice session there,
// so the cleared voice credentials cannot trigger a redial. Same late-binding
// discipline as SetControlStateObserver: set it before Connect.
func (s *Session) SetSessionEndedHook(fn func(reason string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dep.OnSessionEnded = fn
}

// endSession reacts to the server having ended this client's session: it
// tears the connection down, forgets the token, calls the session-ended hook,
// clears the session's store state, publishes disconnected, and emits
// auth:session_ended{reason} followed by auth:session_changed=logged_out.
// It returns sessionEndedError(reason) either way.
//
// It runs at most once per session: if the token and the connection are both
// already gone (an earlier endSession, or the user's Disconnect), it has no
// side effects. It never takes lifeMu, so it is safe from the stream
// goroutine, the ping ticker, and callers inside Reconnect.
func (s *Session) endSession(reason string) error {
	err, _ := s.endSessionOnce(reason)
	return err
}

// endSessionOnce is endSession, also reporting whether this call did the
// work (false: someone else already had).
func (s *Session) endSessionOnce(reason string) (error, bool) {
	s.mu.Lock()
	if s.lastToken == "" && s.conn == nil {
		s.mu.Unlock()
		return sessionEndedError(reason), false
	}
	s.streamGen++
	conn, cancel := s.conn, s.cancel
	s.conn, s.control, s.cancel = nil, nil, nil
	s.lastDialer, s.lastToken = nil, ""
	hook := s.dep.OnSessionEnded
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	slog.Default().Warn("session: the server ended this session", "reason", reason)
	// Voice stops BEFORE the credentials are cleared (spec §3.3).
	if hook != nil {
		hook(reason)
	}
	s.st.ResetSession()
	s.st.ClearSelf()
	s.setConnState(events.ConnDisconnected)
	tagged := events.New(s.em)
	tagged.SessionEnded(reason)
	tagged.SessionChanged("logged_out")
	return sessionEndedError(reason), true
}

// checkSessionEnded routes a unary RPC error: a session-ended verdict ends
// the session and returns its error; anything else is returned unchanged.
func (s *Session) checkSessionEnded(err error) error {
	if v, reason := classifyRPCError(err); v == verdictSessionEnded {
		return s.endSession(reason)
	}
	return err
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
	rtt, err := cc.PingOnce(ctx, lastRTTMs)
	if err != nil {
		// SV1 C1. Called with no connhealth lock held, so ending the
		// session from here is safe.
		return rtt, s.checkSessionEnded(err)
	}
	return rtt, nil
}

// postDialTimeout is the configured post-dial bound.
func (s *Session) postDialTimeout() time.Duration {
	if s.dep.PostDialTimeout > 0 {
		return s.dep.PostDialTimeout
	}
	return defaultPostDialTimeout
}

// beginAttempt derives the cancellable context of one Connect/Reconnect
// attempt and publishes its cancel for CancelConnect. Caller holds lifeMu.
// The returned func unpublishes and releases it.
func (s *Session) beginAttempt(ctx context.Context) (context.Context, func()) {
	attemptCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.attemptCancel = cancel
	s.mu.Unlock()
	return attemptCtx, func() {
		s.mu.Lock()
		s.attemptCancel = nil
		s.mu.Unlock()
		cancel()
	}
}

// CancelConnect aborts the Connect or Reconnect in flight: the dial or the
// post-dial RPC returns at once and the attempt fails with
// ErrConnectCancelled. A no-op when no attempt is in flight -- in particular
// once Connect has returned, so a CANCEL that lands the instant a connect
// succeeded leaves the new session alone.
//
// It takes only s.mu, never lifeMu, which the attempt itself holds.
func (s *Session) CancelConnect() {
	s.mu.Lock()
	cancel := s.attemptCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// attemptError maps a failed attempt's error. A cancelled attempt is
// ErrConnectCancelled whatever the RPC said; an expired post-dial bound is
// the server-not-responding error (wrapping the RPC error); anything else is
// returned as is. pctx is nil when the failure was in the dial.
func (s *Session) attemptError(attemptCtx, pctx context.Context, err error) error {
	if errors.Is(attemptCtx.Err(), context.Canceled) {
		return ErrConnectCancelled
	}
	if pctx != nil && errors.Is(pctx.Err(), context.DeadlineExceeded) {
		return &serverNotRespondingError{after: s.postDialTimeout(), cause: err}
	}
	return err
}

// teardownLive retires the current stream generation, cancels the stream and
// closes the connection, if any, without publishing a state change. Caller
// holds lifeMu.
func (s *Session) teardownLive() {
	s.mu.Lock()
	if s.conn == nil && s.cancel == nil {
		s.mu.Unlock()
		return
	}
	s.streamGen++
	conn, cancel := s.conn, s.cancel
	s.conn, s.control, s.cancel = nil, nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
}

// Connect runs the full guest sequence. Returns a typed error synchronously for
// any pre-connected failure; post-connected transitions flow through events.
//
// The dial is bounded by dialTimeout and everything after it by the
// post-dial timeout; both run under an attempt context that CancelConnect
// cancels. Every failure ends in a published ConnDisconnected (GO-01).
func (s *Session) Connect(ctx context.Context, serverURL, name, password, unitID string) (err error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	attemptCtx, endAttempt := s.beginAttempt(ctx)
	defer endAttempt()

	tagged := events.New(s.em)
	s.setConnState(events.ConnReconnecting) // transient "connecting" surfaced to UI
	defer func() {
		if err != nil {
			s.setConnState(events.ConnDisconnected)
		}
	}()

	// GO-02: a live connection is torn down before the new one is dialled,
	// exactly as Reconnect replaces one -- otherwise its conn and update
	// stream leak, one per extra Connect.
	s.teardownLive()

	dialer := s.dep.Dialer
	if dialer == nil {
		d, derr := dialerFor(serverURL, s.dep.TLSCAFile)
		if derr != nil {
			return derr
		}
		dialer = d
	}

	conn, err := dialer(attemptCtx)
	if err != nil {
		if mapped := s.attemptError(attemptCtx, nil, err); mapped != err {
			return mapped
		}
		return fmt.Errorf("dial: %w", err)
	}

	pctx, pcancel := context.WithTimeout(attemptCtx, s.postDialTimeout())
	defer pcancel()

	// token is set once GuestLogin has issued one. abort sends the D3
	// best-effort Disconnect only then, and only for a cancel or timeout:
	// the server would otherwise hold the session until its reaper runs.
	var token string
	abort := func(cause error) error {
		mapped := s.attemptError(attemptCtx, pctx, cause)
		if token != "" && mapped != cause {
			dctx, dcancel := context.WithTimeout(context.Background(), cancelDisconnectTimeout)
			_ = control.New(conn, token).Disconnect(dctx)
			dcancel()
		}
		_ = conn.Close()
		return mapped
	}

	ac := auth.New(conn)
	init, err := ac.InitAuth(pctx, s.dep.Version)
	if err != nil {
		return abort(err)
	}
	if !init.HasGuest {
		return abort(auth.ErrGuestUnavailable)
	}
	// The server matches the coalition by bcrypt-comparing the client-supplied
	// hash against its stored coalition passwords, so we never send plaintext.
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return abort(fmt.Errorf("hash password: %w", err))
	}
	guest, err := ac.GuestLogin(pctx, name, string(hashed), unitID, init.ClientGUID)
	if err != nil {
		return abort(err)
	}
	token = guest.Token

	cc := control.New(conn, guest.Token)
	if _, err := cc.SyncClient(pctx, s.st); err != nil {
		return abort(err)
	}
	if cerr := pctx.Err(); cerr != nil {
		// Cancelled or timed out after the last RPC answered: honour it
		// rather than install a session the user already walked away from.
		return abort(cerr)
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
	// Abort a connect still negotiating BEFORE waiting for lifeMu, which
	// that connect holds: a Logout must not wait out the post-dial bound.
	s.CancelConnect()
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	s.mu.Lock()
	conn, cc, cancel := s.conn, s.control, s.cancel
	s.conn, s.control, s.cancel = nil, nil, nil
	s.streamGen++ // retire this stream's generation
	// GO-10: forget the session, so a stale RECONNECT cannot resurrect it.
	s.lastDialer, s.lastToken = nil, ""
	s.mu.Unlock()

	// Order kept for SV1 C12: the stream ctx is cancelled before the
	// Disconnect RPC, so the server ending our stream is ignored here.
	if cancel != nil {
		cancel()
	}
	if cc != nil {
		_ = cc.Disconnect(ctx)
	}
	if conn != nil {
		_ = conn.Close()
	}
	// CON-07: drop the roster and the voice secret with the session.
	s.st.ResetSession()
	s.st.ClearSelf()
	tagged := events.New(s.em)
	s.setConnState(events.ConnDisconnected)
	tagged.SessionChanged("logged_out")
	return nil
}

// Reconnect re-establishes the control session reusing the stored token (no
// re-auth). Emits reconnecting → connected on success, or disconnected on
// failure (exactly once). Bounded and cancellable exactly like Connect.
func (s *Session) Reconnect(ctx context.Context) (err error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	attemptCtx, endAttempt := s.beginAttempt(ctx)
	defer endAttempt()

	s.setConnState(events.ConnReconnecting)
	// ended is set when this call's endSession did the teardown; it has
	// already published disconnected, and the one-emission rule holds.
	ended := false
	defer func() {
		if err != nil && !ended {
			s.setConnState(events.ConnDisconnected)
		}
	}()
	// endWith ends the session (SV1 C1 / an empty voice secret) and returns
	// its error.
	endWith := func(reason string) error {
		e, did := s.endSessionOnce(reason)
		ended = did
		return e
	}

	s.mu.Lock()
	dialer, token := s.lastDialer, s.lastToken
	s.mu.Unlock()
	if dialer == nil {
		return fmt.Errorf("cannot reconnect: never connected")
	}

	conn, err := dialer(attemptCtx)
	if err != nil {
		if mapped := s.attemptError(attemptCtx, nil, err); mapped != err {
			return mapped
		}
		return fmt.Errorf("reconnect dial: %w", err)
	}
	pctx, pcancel := context.WithTimeout(attemptCtx, s.postDialTimeout())
	defer pcancel()

	cc := control.New(conn, token)
	hadSecret, err := cc.SyncClient(pctx, s.st)
	if err != nil {
		_ = conn.Close()
		if mapped := s.attemptError(attemptCtx, pctx, err); mapped != err {
			return mapped
		}
		if v, reason := classifyRPCError(err); v == verdictSessionEnded {
			return endWith(reason)
		}
		return fmt.Errorf("reconnect sync: %w", err)
	}
	if !hadSecret {
		// The server answered but no longer has a voice secret for us: the
		// session was removed while the RPC was in flight (SRV-06).
		_ = conn.Close()
		return endWith("expired")
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.lastToken != token {
		// The session ended (stream end, ping) while this Reconnect was
		// syncing: do not resurrect it.
		s.mu.Unlock()
		cancel()
		_ = conn.Close()
		return sessionEndedError("expired")
	}
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
	if err := cc.UpdateRadioInfo(ctx, info); err != nil {
		return s.checkSessionEnded(err) // SV1 C1
	}
	return nil
}

// Control returns the live control client (nil if not connected).
func (s *Session) Control() *control.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.control
}
