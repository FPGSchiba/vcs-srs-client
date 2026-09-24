package voice

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// State is the voice session's lifecycle state, as drawn in the phase design
// doc, section 7.
type State int

const (
	StateIdle State = iota
	StateResolving
	StateHandshaking
	StateConnected
	StateRebinding
	StateRetrying
	StateClosed
)

// String returns the lowercase name of the state.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateResolving:
		return "resolving"
	case StateHandshaking:
		return "handshaking"
	case StateConnected:
		return "connected"
	case StateRebinding:
		return "rebinding"
	case StateRetrying:
		return "retrying"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// helloLadder is the HELLO retry schedule: five attempts, resending HELLO on
// each one, with these timeouts between them. Taken from the C# peer so the
// two clients behave the same way in front of the same server.
var helloLadder = [...]time.Duration{
	1500 * time.Millisecond,
	3000 * time.Millisecond,
	4500 * time.Millisecond,
	6000 * time.Millisecond,
	7500 * time.Millisecond,
}

const (
	// defaultKeepalive is the keepalive period. The server's liveness
	// threshold is 60 s with a 30 s sweep, so this gives roughly twelve
	// chances to survive a bad patch.
	defaultKeepalive = 5 * time.Second

	// defaultJitterMS is the default target jitter-buffer depth, carried
	// here for the RX path that lands in a later task.
	defaultJitterMS = 60

	// retryInterval is how long an exhausted INITIAL ladder waits before
	// dialing a fresh socket and running the whole ladder again. It does not
	// apply to the binding-loss path: see lifecycleLoop.
	retryInterval = 15 * time.Second

	// bindingLossThreshold is how many consecutive unanswered keepalives are
	// read as a dead source-address binding. The server answers every
	// keepalive at the bound address, so silence is the only signal a
	// NAT rebind, network switch or VPN toggle ever produces: our writes
	// keep succeeding and the server keeps dropping everything.
	bindingLossThreshold = 3

	// defaultPoll bounds how long a wait sleeps before re-reading the clock.
	// With the real clock the wait is exact anyway -- each iteration sleeps
	// the lesser of the remaining time and this -- so it only costs a few
	// idle wakeups per second. Tests shrink it so an injected clock can jump
	// a whole rung without any real sleeping.
	defaultPoll = 250 * time.Millisecond

	// eventBuffer is the depth of the rx-to-lifecycle event queue. Only
	// control packets are queued, at a handful per second, so this is far
	// more headroom than the protocol can use.
	eventBuffer = 16

	// maxPendingKA bounds the number of unanswered keepalive send times kept
	// for RTT matching. Binding loss is declared long before this fills, so
	// it only exists so a pathological server that answers nothing cannot
	// grow the slice without limit.
	maxPendingKA = 8

	// resolveTimeout bounds one name lookup. It is REAL time, deliberately
	// not the injected clock: it bounds an I/O call, not a protocol
	// interval. See resolveUDPAddr for why the bound has to exist at all.
	resolveTimeout = 10 * time.Second
)

var (
	errSessionClosed = errors.New("voice: session closed")

	// ErrSecretLength reports a voice secret that is not exactly
	// VoiceSecretLen bytes. The server reads payload[0:VoiceSecretLen] and
	// compares that slice, so a short or long secret can never match: it
	// would fail as an endless, indistinguishable "no HELLO_ACK" retry loop
	// rather than as the configuration error it is.
	ErrSecretLength = errors.New("voice: secret has the wrong length")
)

// Options configures a Session. The zero value is usable: every field falls
// back to a documented default.
type Options struct {
	Log *slog.Logger

	// OnState receives every lifecycle transition. Calls are delivered one
	// at a time, in order, from a single goroutine dedicated to that job, so
	// the callback never runs concurrently with itself. It may call any
	// Session method, INCLUDING Close: the delivery goroutine is not one of
	// the goroutines Close joins.
	//
	// It is delivered synchronously with nothing else, so a slow callback
	// delays later callbacks but never the state machine itself.
	OnState func(State, error)

	Clock     func() time.Time // nil means time.Now
	Keepalive time.Duration    // 0 means 5s
	JitterMS  int              // 0 means 60

	// poll overrides defaultPoll. Unexported: it is a test seam for driving
	// the state machine with an injected clock, not part of the API.
	poll time.Duration

	// txEncode replaces the Opus encoder on the transmit path. Unexported:
	// it is a test seam for driving the packet-ceiling and back-pressure
	// paths deterministically, not part of the API.
	txEncode func(pcm []float32, dst []byte) (int, error)

	// rxNewDecoder replaces the Opus decoder FACTORY on the receive path.
	// Unexported, and a factory rather than a single decode function because
	// each stream owns its own decoder: a shared one would smear every
	// talker into every other, and a shared stub would hide exactly that
	// defect.
	rxNewDecoder func() (rxDecoder, error)
}

// eventKind identifies what rxLoop saw.
type eventKind int

const (
	evHelloAck eventKind = iota
	evKeepaliveReply
)

// event is a control-packet arrival handed from rxLoop to the lifecycle
// goroutine. gen identifies the socket it arrived on, so a datagram that
// raced a re-dial cannot satisfy a wait belonging to the new socket.
type event struct {
	kind     eventKind
	gen      uint64
	at       time.Time
	serverTS int64
}

// stateChange is one queued OnState delivery.
type stateChange struct {
	st  State
	err error
}

// Session owns one connected UDP socket for its whole life, plus the state
// machine that keeps that socket's source address bound on the server.
//
// Five goroutines run per session: rxLoop, which does a blocking read,
// parses and dispatches; lifecycleLoop, which owns the HELLO ladder and the
// keepalive schedule; txLoop, which encodes captured audio and writes it;
// decodeLoop, which decodes and colours received audio; and deliverLoop,
// which delivers OnState callbacks. Close joins the first four (rxLoop also
// exits via the socket being closed under it) and does NOT join
// deliverLoop, which is what makes it safe for OnState to call Close.
type Session struct {
	src    Sources
	self   uuid.UUID
	secret string

	log       *slog.Logger
	onState   func(State, error)
	clock     func() time.Time
	keepalive time.Duration
	poll      time.Duration

	// jitterMS is the configured jitter-buffer target, in milliseconds. It
	// sets both each stream's priming delay and how much decoded PCM the
	// decode loop keeps ahead of playback -- see rxState.aheadSamples.
	jitterMS int

	// tx is the transmit path: see tx.go. It is a field of Session because
	// its packets go out on Session's socket, through the same lock that
	// serialises every other write against Close's BYE.
	tx txState

	// rx is the receive path: see rx.go. It is a field of Session because
	// its packets arrive on Session's socket, dispatched by rxLoop below.
	rx rxState

	events chan event
	done   chan struct{}
	wg     sync.WaitGroup

	closeOnce sync.Once
	closeErr  error

	// cbSignal wakes deliverLoop. It carries no data -- the queue below does
	// -- so a full buffer means "already awake", and the send can be dropped
	// rather than blocking the transition that triggered it.
	cbSignal chan struct{}

	mu           sync.Mutex
	state        State
	conn         *net.UDPConn
	gen          uint64
	closed       bool
	rtt          time.Duration
	lastServerTS int64

	// pendingKA holds the send time of every keepalive still unanswered,
	// oldest first. A reply is matched to the OLDEST outstanding send, not
	// to the most recent one: on a link slow enough for a reply to arrive
	// after the next keepalive went out -- exactly where the number matters
	// -- matching the most recent send measures a residue, or a negative.
	pendingKA []time.Time

	// cbQueue holds transitions waiting for deliverLoop; cbFinal records
	// that StateClosed has been queued, after which nothing more may be
	// published. Both are guarded by mu together with state itself, so a
	// transition and its delivery cannot be reordered by a racing caller.
	cbQueue []stateChange
	cbFinal bool

	// waitSeq and waitDeadline publish what the lifecycle goroutine is
	// currently parked on. See waitingOn.
	waitSeq      uint64
	waitDeadline time.Time
}

// waitingOn reports the sequence number and deadline of the wait the
// lifecycle goroutine is currently parked on. It exists for tests driving an
// injected clock: they need to know the session has observed the previous
// deadline and computed the next one before they move the clock again,
// otherwise an advance can land inside that window and be counted against
// the wrong interval.
func (s *Session) waitingOn() (seq uint64, deadline time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitSeq, s.waitDeadline
}

// Dial resolves the endpoint, opens a connected UDP socket and runs the
// HELLO handshake in the background. It returns as soon as the socket is
// open; handshake progress arrives through OnState.
//
// The socket is connected rather than unconnected on purpose: the server
// binds a session to its source address on a verified HELLO and authenticates
// everything afterwards by that binding, and a connected socket also surfaces
// ICMP port-unreachable as a write error.
func Dial(src Sources, self uuid.UUID, secret string, opt Options) (*Session, error) {
	if len(secret) != VoiceSecretLen {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrSecretLength, len(secret), VoiceSecretLen)
	}

	s := &Session{
		src:       src,
		self:      self,
		secret:    secret,
		log:       opt.Log,
		onState:   opt.OnState,
		clock:     opt.Clock,
		keepalive: opt.Keepalive,
		poll:      opt.poll,
		jitterMS:  opt.JitterMS,
		events:    make(chan event, eventBuffer),
		done:      make(chan struct{}),
		cbSignal:  make(chan struct{}, 1),
		state:     StateIdle,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.clock == nil {
		s.clock = time.Now
	}
	if s.keepalive <= 0 {
		s.keepalive = defaultKeepalive
	}
	if s.poll <= 0 {
		s.poll = defaultPoll
	}
	if s.jitterMS <= 0 {
		s.jitterMS = defaultJitterMS
	}

	s.tx.init(opt.txEncode, s.log)
	s.rx.init(opt.rxNewDecoder, s.jitterMS)

	// Started before the first transition so no callback is ever dropped,
	// and deliberately outside s.wg: Close joins s.wg, and OnState is
	// allowed to call Close.
	go s.deliverLoop()

	s.setState(StateResolving, nil)
	if err := s.openSocket(); err != nil {
		// No goroutine has touched the codec yet, and the caller gets no
		// Session to Close, so it has to be freed here or its C memory
		// leaks for the life of the process.
		s.tx.close()
		s.rx.close()
		s.setState(StateClosed, err)
		return nil, err
	}

	s.wg.Add(3)
	go s.lifecycleLoop()
	go s.txLoop()
	go s.decodeLoop()
	return s, nil
}

// State returns the session's current lifecycle state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// RTT returns the most recent keepalive round-trip time, or zero if no
// keepalive has been answered on the current binding. It is reset whenever
// the binding is re-established, so it never reports a pre-outage figure
// while the session is rebinding.
func (s *Session) RTT() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rtt
}

// Close sends a best-effort BYE, closes the socket and stops the session's
// goroutines. It is safe to call more than once and from any goroutine,
// including from inside OnState; only the first call does anything. A BYE
// from an unbound address is inert server-side, which is why this is
// best-effort: the server's liveness sweep is the backstop.
//
// The final StateClosed callback may be delivered shortly after Close
// returns. Close cannot wait for it without deadlocking the case that
// matters most -- OnState calling Close -- so State() is the synchronous
// answer and the callback is the notification.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		// The BYE is written and the session marked closed under one hold
		// of mu, and every other write takes the same lock for its whole
		// check-and-write. A ladder HELLO therefore lands strictly before
		// the BYE or not at all -- it can never re-bind the session
		// server-side after we said goodbye, leaving a ghost client behind
		// until the 60 s sweep.
		s.mu.Lock()
		byeErr := s.sendLocked(NewBye(s.self), nil)
		s.closed = true
		conn := s.conn
		s.conn = nil
		s.mu.Unlock()

		if byeErr != nil && !errors.Is(byeErr, errSessionClosed) {
			s.log.Debug("voice: BYE not sent", "err", byeErr)
		}

		close(s.done)

		if conn != nil {
			s.closeErr = conn.Close()
		}
		s.wg.Wait()
		// After the join, so nothing can be inside an encode or decode call.
		s.tx.close()
		s.rx.close()
		s.setState(StateClosed, nil)
	})
	return s.closeErr
}

// now reads the injected clock.
func (s *Session) now() time.Time { return s.clock() }

// setState records the new state and queues it for delivery. The state and
// its queue entry are written under one hold of mu, and deliverLoop is the
// only consumer, so callbacks are delivered exactly in the order the
// transitions happened.
//
// It never blocks: queueing is a slice append, so a slow or absent consumer
// cannot stall the state machine. Nothing is published after StateClosed.
func (s *Session) setState(st State, err error) {
	s.mu.Lock()
	if s.cbFinal {
		s.mu.Unlock()
		return
	}
	s.state = st
	if st == StateClosed {
		s.cbFinal = true
	}
	s.cbQueue = append(s.cbQueue, stateChange{st: st, err: err})
	s.mu.Unlock()

	if err != nil {
		s.log.Info("voice: session state", "state", st.String(), "err", err)
	} else {
		s.log.Debug("voice: session state", "state", st.String())
	}

	select {
	case s.cbSignal <- struct{}{}:
	default:
	}
}

// deliverLoop is the sole caller of OnState. Running deliveries on a
// goroutine of their own is what lets OnState call Close: Close joins the rx
// and lifecycle goroutines, and if a callback ran on either of those, a
// Close from inside it would wait for itself forever.
//
// It exits after delivering StateClosed, which every terminated session
// publishes exactly once.
func (s *Session) deliverLoop() {
	for range s.cbSignal {
		for {
			s.mu.Lock()
			if len(s.cbQueue) == 0 {
				s.mu.Unlock()
				break
			}
			ch := s.cbQueue[0]
			s.cbQueue = s.cbQueue[1:]
			s.mu.Unlock()

			if s.onState != nil {
				s.onState(ch.st, ch.err)
			}
			if ch.st == StateClosed {
				return
			}
		}
	}
}

// currentGen returns the generation number of the live socket.
func (s *Session) currentGen() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// resolveUDPAddr resolves addr, bounded in time and cancellable by Close.
//
// net.ResolveUDPAddr takes no context, and Close joins the loop that calls
// it, so an unbounded system resolver -- a blackholed DNS server after a VPN
// toggle, precisely the case this state machine exists for -- would block
// Close for as long as the resolver blocked. The lookup therefore runs on a
// goroutine of its own, writing to a buffered channel nobody has to read; it
// outlives this call at most until the resolver returns.
func (s *Session) resolveUDPAddr(addr string) (*net.UDPAddr, error) {
	type result struct {
		addr *net.UDPAddr
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		ua, err := net.ResolveUDPAddr("udp", addr)
		ch <- result{addr: ua, err: err}
	}()

	timer := time.NewTimer(resolveTimeout)
	defer timer.Stop()

	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("voice: resolve %q: %w", addr, r.err)
		}
		return r.addr, nil
	case <-s.done:
		return nil, errSessionClosed
	case <-timer.C:
		return nil, fmt.Errorf("voice: resolve %q: no answer within %s", addr, resolveTimeout)
	}
}

// openSocket resolves the endpoint afresh and dials a new connected socket,
// replacing and closing any previous one. Closing the old socket unblocks its
// rxLoop, which then exits; a new rxLoop is started for the new socket.
//
// Re-resolving on every dial is deliberate: a VOICE_ADDRESS_UPDATE may have
// changed the endpoint since the last attempt.
func (s *Session) openSocket() error {
	addr, err := Resolve(s.src)
	if err != nil {
		return fmt.Errorf("voice: resolve endpoint: %w", err)
	}
	raddr, err := s.resolveUDPAddr(addr)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return fmt.Errorf("voice: dial %q: %w", addr, err)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		conn.Close()
		return errSessionClosed
	}
	old := s.conn
	s.conn = conn
	s.gen++
	gen := s.gen
	// A fresh socket means a fresh source address; nothing measured against
	// the old binding carries over.
	s.resetBindingLocked()
	s.mu.Unlock()

	if old != nil {
		old.Close()
	}

	s.wg.Add(1)
	go s.rxLoop(conn, gen)
	return nil
}

// resetBindingLocked drops everything measured against a binding that is
// gone. Caller holds s.mu.
//
// lastServerTS in particular MUST go: echoing a pre-outage timestamp back
// after recovery records one hugely inflated sample in the server's
// per-client latency map, and a stale rtt would show the UI a healthy ping
// for a session that is in fact broken.
func (s *Session) resetBindingLocked() {
	s.rtt = 0
	s.lastServerTS = 0
	s.pendingKA = nil
}

// rxLoop reads datagrams off one socket until that socket is closed. It does
// the minimum possible per packet -- parse and dispatch -- so it never blocks
// the socket. Anything slower belongs on the lifecycle goroutine.
func (s *Session) rxLoop(conn *net.UDPConn, gen uint64) {
	defer s.wg.Done()

	buf := make([]byte, MaxDatagram)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		pkt, err := Parse(buf[:n])
		if err != nil {
			s.log.Debug("voice: dropping unparseable datagram", "err", err)
			continue
		}
		switch pkt.Type {
		case PacketTypeHelloAck:
			s.emit(event{kind: evHelloAck, gen: gen, at: s.now()})
		case PacketTypeKeepalive:
			s.emit(event{
				kind:     evKeepaliveReply,
				gen:      gen,
				at:       s.now(),
				serverTS: KeepaliveTimestamp(pkt.Payload),
			})
		case PacketTypeVoice:
			// rxVoice filters and enqueues, and does nothing that can
			// block. Everything slower -- decode, effects, playout timing --
			// happens on the decode goroutine.
			s.rxVoice(pkt, s.now())
		case PacketTypeBye:
			// The server never sends one. Counted rather than ignored so a
			// server that starts to is visible rather than mysterious.
			s.rx.byes.Add(1)
		}
	}
}

// emit queues a control event without ever blocking the read loop.
func (s *Session) emit(ev event) {
	select {
	case s.events <- ev:
	default:
		s.log.Warn("voice: control event queue full, dropping", "kind", ev.kind)
	}
}

// lifecycleLoop owns the handshake ladder and the keepalive schedule for the
// life of the session.
func (s *Session) lifecycleLoop() {
	defer s.wg.Done()

	// rebinding records that the ladder about to run is a re-HELLO on an
	// existing socket after binding loss, rather than a first handshake on a
	// fresh one. The two exhaust differently: see below.
	rebinding := false

	for {
		acked, closed := s.runLadder()
		if closed {
			return
		}
		if acked {
			rebinding = false
			s.setState(StateConnected, nil)
			lost, closed := s.serve()
			if closed {
				return
			}
			if !lost {
				return
			}
			// Re-HELLO on the SAME socket first: if the binding died
			// server-side (a restart, a cleanup sweep) the existing
			// source address is still perfectly good.
			s.mu.Lock()
			s.resetBindingLocked()
			s.mu.Unlock()
			s.setState(StateRebinding, nil)
			rebinding = true
			continue
		}

		if rebinding {
			// §7.3: "the retry ladder also failing -> close the socket, dial
			// a fresh one (new source port), HELLO again" -- with no
			// interval, and for good reason. By this point the caller has
			// already lost 3 unanswered keepalives (15 s) plus the whole
			// re-HELLO ladder (22.5 s) of audio. Sitting out another 15 s
			// idle before even trying would push the outage past the
			// server's 60 s liveness sweep, so the client could be culled
			// while waiting to recover from a NAT rebind.
			rebinding = false
			if stop := s.reopen(); stop {
				return
			}
			continue
		}

		// The INITIAL ladder is exhausted. The C# peer logs "proceeding with
		// keepalive path" here; that cannot work against this server,
		// because keepalives from an unbound address are dropped. Report the
		// reason and re-run the whole ladder on a fresh socket instead --
		// the usual causes (server restarting, secret not yet propagated to
		// a voice node) are transient and want a pause, so surrender is the
		// wrong response and so is hammering.
		s.setState(StateRetrying, fmt.Errorf(
			"voice: no HELLO_ACK after %d attempts; retrying in %s", len(helloLadder), retryInterval))
		if _, closed := s.wait(retryInterval, nil); closed {
			return
		}
		if stop := s.reopen(); stop {
			return
		}
	}
}

// reopen dials a fresh socket -- a fresh source port -- retrying every
// retryInterval for as long as dialing itself keeps failing. It reports
// whether the session was closed while trying.
func (s *Session) reopen() (stop bool) {
	for {
		s.setState(StateResolving, nil)
		if err := s.openSocket(); err != nil {
			if errors.Is(err, errSessionClosed) {
				return true
			}
			s.setState(StateRetrying, err)
			if _, closed := s.wait(retryInterval, nil); closed {
				return true
			}
			continue
		}
		return false
	}
}

// runLadder sends HELLO and waits for a HELLO_ACK, five times, at the
// ladder's timings, resending on every attempt. It reports whether the
// handshake completed, and whether the session was closed while waiting.
func (s *Session) runLadder() (acked, closed bool) {
	s.setState(StateHandshaking, nil)
	gen := s.currentGen()

	for attempt, rung := range helloLadder {
		if err := s.send(NewHello(s.self, s.secret)); err != nil {
			s.log.Warn("voice: HELLO not sent", "attempt", attempt+1, "err", err)
		}
		matched, closed := s.wait(rung, func(ev event) bool {
			if ev.gen != gen {
				return false
			}
			switch ev.kind {
			case evHelloAck:
				return true
			case evKeepaliveReply:
				// A reply to a keepalive sent before the binding was lost can
				// still be in flight; record it, but it does not stand in for
				// an ACK.
				s.recordKeepalive(ev)
			}
			return false
		})
		if closed {
			return false, true
		}
		if matched {
			return true, false
		}
	}
	return false, false
}

// serve runs the keepalive schedule until the binding looks dead or the
// session is closed. It reports (bindingLost, closed).
func (s *Session) serve() (lost, closed bool) {
	gen := s.currentGen()
	unanswered := 0

	send := func() {
		if err := s.sendKeepalive(); err != nil {
			s.log.Warn("voice: keepalive not sent", "err", err)
		}
		unanswered++
	}

	// Prime the server's latency map immediately rather than five seconds
	// after the handshake, and start the liveness clock from the handshake.
	send()

	for {
		_, closed := s.wait(s.keepalive, func(ev event) bool {
			if ev.gen != gen {
				return false
			}
			if ev.kind == evKeepaliveReply {
				s.recordKeepalive(ev)
				unanswered = 0
			}
			return false
		})
		if closed {
			return false, true
		}
		if unanswered >= bindingLossThreshold {
			s.log.Warn("voice: binding appears lost",
				"unanswered_keepalives", unanswered)
			return true, false
		}
		send()
	}
}

// wait blocks for d of clock time, handing every control event to onEvent as
// it arrives. It returns matched=true as soon as onEvent returns true, and
// closed=true if the session is closed while waiting.
//
// Each iteration sleeps the lesser of the remaining time and s.poll, so with
// the real clock the wakeup lands exactly on the deadline, and with an
// injected clock a test can jump the deadline and have it observed within one
// poll interval.
func (s *Session) wait(d time.Duration, onEvent func(event) bool) (matched, closed bool) {
	deadline := s.now().Add(d)
	s.mu.Lock()
	s.waitSeq++
	s.waitDeadline = deadline
	s.mu.Unlock()

	for {
		remaining := deadline.Sub(s.now())
		if remaining <= 0 {
			return false, false
		}
		if remaining > s.poll {
			remaining = s.poll
		}
		timer := time.NewTimer(remaining)
		select {
		case <-s.done:
			timer.Stop()
			return false, true
		case ev := <-s.events:
			timer.Stop()
			if onEvent != nil && onEvent(ev) {
				return true, false
			}
		case <-timer.C:
		}
	}
}

// recordKeepalive stores the server's echo timestamp for the next keepalive
// payload and updates the locally measured round-trip time.
//
// The reply is matched to the OLDEST unanswered send, not to the most recent
// one. Keepalives go out strictly in order and the server answers each in
// turn, so on a link slow enough that a reply arrives after the next
// keepalive was sent, the oldest outstanding send is the one it answers.
func (s *Session) recordKeepalive(ev event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.serverTS != 0 {
		s.lastServerTS = ev.serverTS
	}
	if len(s.pendingKA) == 0 {
		// Nothing outstanding: a duplicate, or a reply that survived a
		// re-dial. It carries no usable timing, and guessing one would be
		// worse than reporting the last good measurement.
		return
	}
	sent := s.pendingKA[0]
	s.pendingKA = s.pendingKA[1:]
	if rtt := ev.at.Sub(sent); rtt > 0 {
		s.rtt = rtt
	}
}

// noteKeepaliveSentLocked remembers when a keepalive went out so its reply
// can be timed against it. Caller holds s.mu.
func (s *Session) noteKeepaliveSentLocked(at time.Time) {
	if len(s.pendingKA) >= maxPendingKA {
		s.pendingKA = s.pendingKA[1:]
	}
	s.pendingKA = append(s.pendingKA, at)
}

// sendKeepalive writes a keepalive echoing the server's last timestamp. The
// echo is the only input to the server's per-client latency map, so it is not
// optional decoration; before the first reply there is nothing to echo and
// the payload is empty.
func (s *Session) sendKeepalive() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	echo := s.lastServerTS
	at := s.now()
	err := s.sendLocked(NewKeepalive(s.self, echo), nil)
	if err == nil {
		// Only a keepalive that actually went out can be answered; timing a
		// reply against a send that failed would mis-date the next one.
		s.noteKeepaliveSentLocked(at)
	}
	return err
}

// send writes one packet on the live socket.
func (s *Session) send(p *Packet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendLocked(p, nil)
}

// sendScratch writes one packet on the live socket, serialising it into a
// caller-owned buffer. txLoop uses it to avoid an allocation per packet per
// frequency, fifty times a second; a scratch with cap >= MaxDatagram is
// never regrown.
func (s *Session) sendScratch(p *Packet, scratch []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendLocked(p, scratch)
}

// sendLocked writes one packet on the live socket, serialising it into
// scratch (nil means allocate). Caller holds s.mu, which is what serialises
// it against Close's BYE. A connected UDP write does not block on the
// network, so holding the lock across it costs nothing.
func (s *Session) sendLocked(p *Packet, scratch []byte) error {
	if s.closed || s.conn == nil {
		return errSessionClosed
	}
	_, err := s.conn.Write(p.AppendTo(scratch[:0]))
	return err
}
