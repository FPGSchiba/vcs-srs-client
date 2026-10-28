// Package session orchestrates the guest connect sequence and owns the gRPC
// connection lifecycle.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// (InitAuth, the bcrypt hash, GuestLogin, the subscription ack,
	// SyncClient) and Reconnect's subscription ack and SyncClient. 0 means
	// defaultPostDialTimeout. Overridable for tests.
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

	// pendingUpdateCap bounds the updates a new update stream holds while
	// the SyncClient snapshot it was opened ahead of is still loading. It
	// mirrors the server's own per-subscription buffer (vcs-srs-server
	// srs/session.go updateStreamBuffer = 256, SV2): a client that has had
	// to hold more than the server itself would queue has fallen behind by
	// the server's own measure, and is treated exactly like the server's
	// fell-behind end (C13): resync.
	pendingUpdateCap = 256

	// subscribeAckTimeout bounds the wait for the server's subscription ack
	// (SV2 C17, see awaitSubscribed) on Connect, Reconnect and the resync
	// (user decision 2026-10-27). A C17 server acks within one round trip;
	// a server from before C17 never does, and past this bound the attempt
	// logs a Warn and goes on to SyncClient without the ordering guarantee,
	// so such a server still connects, about 2 s slower. Inside Connect and
	// Reconnect the wait is also capped by what is left of the post-dial
	// bound, which keeps covering everything after the dial.
	subscribeAckTimeout = 2 * time.Second
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

	// cancelSeq counts CancelConnect calls. An attempt reads it before it
	// waits for lifeMu; if it has moved by the time the attempt begins, a
	// CANCEL arrived while the attempt was still queued and it is cancelled
	// at once. Guarded by mu.
	cancelSeq uint64

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

// pendingStream is an update stream opened AHEAD of the SyncClient snapshot
// it must not be overtaken by: registered with the server (awaitSubscribed)
// before the snapshot is requested, so nothing published after the snapshot
// is lost, and held (control.Held) until the snapshot is applied, so nothing
// delivered before it is wiped by it.
//
// Its goroutine starts at once but owns no stream generation until install
// hands it one: an end of the stream before install is kept by the Held and
// reported only after Release, through handleStreamEnd, exactly like the
// end of an installed stream. An attempt that never installs it calls
// abandon (or cancels the connection context), and the goroutine exits
// without reporting anything.
type pendingStream struct {
	held    *control.Held
	gen     chan uint64 // receives the generation install assigns
	abandon context.CancelFunc
}

// openHeldStream subscribes on ctx (the connection's stream context, which
// the installed stream keeps), waits for the server's subscription ack
// (awaitSubscribed, under waitCtx), and starts holding the stream. On error
// nothing is left running.
func (s *Session) openHeldStream(ctx, waitCtx context.Context, cc *control.Client) (*pendingStream, error) {
	// subCtx is this stream's own: cancelling it on exit ends a stream the
	// consumer stopped reading (an overflowed hold) without touching the
	// connection-wide context the next subscription reuses.
	subCtx, subCancel := context.WithCancel(ctx)
	stream, err := cc.Subscribe(subCtx)
	if err != nil {
		subCancel()
		return nil, err
	}
	if err := s.awaitSubscribed(waitCtx, stream); err != nil {
		subCancel()
		return nil, err
	}
	p := &pendingStream{
		held:    control.NewHeld(s.st, s.em, pendingUpdateCap),
		gen:     make(chan uint64, 1),
		abandon: subCancel,
	}
	go func() {
		defer subCancel()
		err := p.held.Consume(subCtx, stream)
		select {
		case gen := <-p.gen:
			s.handleStreamEnd(ctx, gen, err)
		default:
			// Never installed: the attempt that opened it failed or was
			// superseded, and that path owns the state.
		}
	}()
	return p, nil
}

// installLocked gives p the next stream generation. Caller holds s.mu and
// calls p.held.Release after unlocking: Release routes into the store, whose
// observers must not run under s.mu.
func (s *Session) installLocked(p *pendingStream) {
	s.streamGen++
	p.gen <- s.streamGen
}

// resync answers SV2's C13 ("update stream fell behind: resubscribe and call
// SyncClient") on the same connection, from the ended stream's goroutine:
//
//  1. re-check that gen is still current and the connection live;
//  2. resubscribe FIRST and wait for the server's subscription ack (SV2 C17,
//     see awaitSubscribed); the new stream holds what it receives
//     (pendingStream);
//  3. then SyncClient, bounded by the post-dial timeout, which replaces the
//     roster;
//  4. on success install the new stream under a new generation, replay what
//     it held on top of the snapshot, and publish connected again
//     (consumers rehydrate on it);
//  5. on failure, a session-ended verdict ends the session, anything else is
//     an ordinary control loss.
//
// streamCtx is the ended stream's context, which the new stream reuses: the
// connection's stream cancel (s.cancel) still governs it. There is no rate
// cap; the server sends C13 only after its buffer of stalled events
// overflows, and the client's own hold overflows only past the same size.
func (s *Session) resync(streamCtx context.Context, gen uint64) {
	s.mu.Lock()
	if gen != s.streamGen || s.conn == nil || s.control == nil {
		s.mu.Unlock()
		return
	}
	cc := s.control
	s.mu.Unlock()

	fail := func(err error) {
		if streamCtx.Err() != nil {
			return // Disconnect/Reconnect/endSession ended it on purpose
		}
		if v, reason := classifyRPCError(err); v == verdictSessionEnded {
			_ = s.endSession(reason)
			return
		}
		s.log("resync failed", err)
		s.markControlLost(gen)
	}
	// stillCurrent reports whether gen still owns the live connection.
	stillCurrent := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return gen == s.streamGen && s.conn != nil
	}

	p, err := s.openHeldStream(streamCtx, streamCtx, cc)
	if err != nil {
		fail(err)
		return
	}
	if !stillCurrent() {
		p.abandon()
		return // replaced while subscribing; that path owns the state
	}

	sctx, cancel := context.WithTimeout(streamCtx, s.postDialTimeout())
	defer cancel()
	if _, err := cc.SyncClient(sctx, s.st); err != nil {
		p.abandon()
		fail(err)
		return
	}
	s.mu.Lock()
	if gen != s.streamGen || s.conn == nil {
		s.mu.Unlock()
		p.abandon()
		return
	}
	s.installLocked(p)
	s.mu.Unlock()
	p.held.Release()
	s.setConnState(events.ConnConnected)
}

// awaitSubscribed waits until the server has registered the subscription
// that stream opened. grpc-go returns from a server-streaming call once the
// request is sent, and SyncClient travels on its own HTTP/2 stream, so
// without this the server can serve SyncClient before the subscriber exists
// and an update published in between is in neither the snapshot nor the
// stream. SV2 C17 closes that: the server sends (empty) response headers
// right after registering, and Header() returns on them.
//
// The wait is bounded by subscribeAckTimeout, or by what is left of ctx's
// deadline when that is sooner (Connect and Reconnect pass their post-dial
// context). A server from before C17 sends its headers only with the first
// update; past the bound this logs a Warn and returns nil, so the caller
// proceeds to SyncClient as it did before C17 instead of wedging or failing.
// A stream that ends before any header (the server refused it) returns that
// stream's error, like a failed Subscribe. A cancelled ctx returns its error.
//
// On the fallback, the Header goroutine outlives this call until the first
// update or the stream's end; grpc-go permits Header() alongside RecvMsg.
func (s *Session) awaitSubscribed(ctx context.Context, stream srspb.SRSService_SubscribeToUpdatesClient) error {
	acked := make(chan bool, 1)
	go func() {
		// clientStream.Header never returns an error: a stream that ended
		// without headers yields (nil, nil) and its status is on Recv.
		md, err := stream.Header()
		acked <- err == nil && md != nil
	}()
	bound := subscribeAckTimeout
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl); left < bound {
			bound = left
		}
	}
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case ok := <-acked:
		if ok {
			return nil
		}
		if _, err := stream.Recv(); err != nil && err != io.EOF {
			return err
		}
		return errors.New("update stream ended before the server acknowledged the subscription")
	case <-timer.C:
		slog.Default().Warn("session: server did not acknowledge the update subscription; continuing without the ordering guarantee (server predates SV2 C17?)",
			"after", bound)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
		// SV2 C13 (D6): fell behind, not a session loss.
		s.resync(ctx, gen)
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
	return s.endSessionIf(reason, nil)
}

// endSessionIf is endSessionOnce with an extra guard evaluated under s.mu:
// when still is non-nil and reports false, the call has no side effects.
func (s *Session) endSessionIf(reason string, still func() bool) (error, bool) {
	s.mu.Lock()
	if (s.lastToken == "" && s.conn == nil) || (still != nil && !still()) {
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
//
// tok is the token of the session the caller's control client belonged to,
// captured together with that client. When a later Connect has installed a
// different session in the meantime, the verdict is about the old one and
// the current session is left alone (the error is still returned).
func (s *Session) checkSessionEnded(tok string, err error) error {
	if v, reason := classifyRPCError(err); v == verdictSessionEnded {
		e, _ := s.endSessionIf(reason, func() bool { return s.lastToken == tok })
		return e
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
	cc, tok := s.control, s.lastToken
	s.mu.Unlock()
	if cc == nil {
		return 0, fmt.Errorf("ping: not connected")
	}
	rtt, err := cc.PingOnce(ctx, lastRTTMs)
	if err != nil {
		// SV1 C1. Called with no connhealth lock held, so ending the
		// session from here is safe.
		return rtt, s.checkSessionEnded(tok, err)
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

// queueAttempt is called by Connect/Reconnect BEFORE they wait for lifeMu.
// It returns the ticket beginAttempt checks for a CANCEL that arrived while
// the attempt was queued.
func (s *Session) queueAttempt() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelSeq
}

// beginAttempt derives the cancellable context of one Connect/Reconnect
// attempt and publishes its cancel for CancelConnect. Caller holds lifeMu.
// If CancelConnect ran since queueAttempt issued ticket, the attempt starts
// already cancelled. The returned func unpublishes and releases it.
func (s *Session) beginAttempt(ctx context.Context, ticket uint64) (context.Context, func()) {
	attemptCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.attemptCancel = cancel
	if s.cancelSeq != ticket {
		cancel()
	}
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
// An attempt still queued behind lifeMu (for example behind a Disconnect)
// is cancelled too, through cancelSeq; one that queues after this call is
// not.
//
// It takes only s.mu, never lifeMu, which the attempt itself holds.
func (s *Session) CancelConnect() {
	s.mu.Lock()
	s.cancelSeq++
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
	ticket := s.queueAttempt()
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	attemptCtx, endAttempt := s.beginAttempt(ctx, ticket)
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
	// The replaced (or control-lost) session is forgotten here, not on
	// success: if this Connect fails, RECONNECT must not resume it.
	s.mu.Lock()
	s.lastDialer, s.lastToken = nil, ""
	s.mu.Unlock()

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
	// Subscribe BEFORE SyncClient and wait for the server to register the
	// subscription (SV2 C17), so an update published after the snapshot is
	// taken reaches the stream; the stream holds it until the snapshot is
	// applied. A refusal fails the Connect the way a failed SyncClient did
	// when SyncClient was the first call after GuestLogin.
	streamCtx, streamCancel := context.WithCancel(context.Background())
	p, err := s.openHeldStream(streamCtx, pctx, cc)
	if err != nil {
		streamCancel()
		return abort(fmt.Errorf("subscribe to updates: %w", err))
	}
	if _, err := cc.SyncClient(pctx, s.st); err != nil {
		streamCancel()
		return abort(err)
	}
	if cerr := pctx.Err(); cerr != nil {
		// Cancelled or timed out after the last RPC answered: honour it
		// rather than install a session the user already walked away from.
		streamCancel()
		return abort(cerr)
	}

	// Record the local client identity for the UI (callsign/FFID from the form,
	// coalition resolved by the server from the password).
	s.st.SetSelf(init.ClientGUID, &srspb.ClientInfo{
		Name:      name,
		Coalition: guest.Coalition,
		UnitId:    unitID,
	})

	s.mu.Lock()
	s.conn, s.control, s.cancel = conn, cc, streamCancel
	s.lastDialer = dialer
	s.lastToken = guest.Token
	s.installLocked(p)
	s.mu.Unlock()
	// The snapshot is applied: replay what the stream held, then connected.
	p.held.Release()

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
	ticket := s.queueAttempt()
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	attemptCtx, endAttempt := s.beginAttempt(ctx, ticket)
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
	// Subscribe first and hold, exactly as Connect does. A refusal is routed
	// like a failed SyncClient: Unauthenticated ends the session.
	streamCtx, streamCancel := context.WithCancel(context.Background())
	p, err := s.openHeldStream(streamCtx, pctx, cc)
	if err != nil {
		streamCancel()
		_ = conn.Close()
		if mapped := s.attemptError(attemptCtx, pctx, err); mapped != err {
			return mapped
		}
		if v, reason := classifyRPCError(err); v == verdictSessionEnded {
			return endWith(reason)
		}
		return fmt.Errorf("reconnect subscribe: %w", err)
	}
	hadSecret, err := cc.SyncClient(pctx, s.st)
	if err != nil {
		streamCancel()
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
		streamCancel()
		_ = conn.Close()
		return endWith("expired")
	}

	s.mu.Lock()
	if s.lastToken != token {
		// The session ended (stream end, ping) while this Reconnect was
		// syncing: do not resurrect it.
		s.mu.Unlock()
		streamCancel()
		_ = conn.Close()
		return sessionEndedError("expired")
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn, s.control, s.cancel = conn, cc, streamCancel
	s.installLocked(p)
	s.mu.Unlock()
	p.held.Release()
	s.setConnState(events.ConnConnected)
	return nil
}

// UpdateRadioInfo pushes a radio change via the live control client.
func (s *Session) UpdateRadioInfo(ctx context.Context, info *srspb.RadioInfo) error {
	s.mu.Lock()
	cc, tok := s.control, s.lastToken
	s.mu.Unlock()
	if cc == nil {
		return fmt.Errorf("not connected")
	}
	if err := cc.UpdateRadioInfo(ctx, info); err != nil {
		return s.checkSessionEnded(tok, err) // SV1 C1
	}
	return nil
}

// Control returns the live control client (nil if not connected).
func (s *Session) Control() *control.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.control
}
