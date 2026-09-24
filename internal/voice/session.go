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

	// retryInterval is how long an exhausted ladder waits before dialing a
	// fresh socket and running the whole ladder again.
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
)

var errSessionClosed = errors.New("voice: session closed")

// Options configures a Session. The zero value is usable: every field falls
// back to a documented default.
type Options struct {
	Log       *slog.Logger
	OnState   func(State, error) // may be called concurrently
	Clock     func() time.Time   // nil means time.Now
	Keepalive time.Duration      // 0 means 5s
	JitterMS  int                // 0 means 60

	// poll overrides defaultPoll. Unexported: it is a test seam for driving
	// the state machine with an injected clock, not part of the API.
	poll time.Duration
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

// Session owns one connected UDP socket for its whole life, plus the state
// machine that keeps that socket's source address bound on the server.
//
// Two goroutines run per session: rxLoop, which does a blocking read, parses
// and dispatches, and lifecycleLoop, which owns the HELLO ladder and the
// keepalive schedule. Both exit via done (and, for rxLoop, via the socket
// being closed under it).
//
// OnState must not call Close: Close waits for those goroutines, and one of
// them is what delivers the callback.
type Session struct {
	src    Sources
	self   uuid.UUID
	secret string

	log       *slog.Logger
	onState   func(State, error)
	clock     func() time.Time
	keepalive time.Duration
	poll      time.Duration

	// jitterMS is the configured jitter-buffer target. Task 7 only records
	// it; the RX path that consumes it lands in a later task.
	jitterMS int

	events chan event
	done   chan struct{}
	wg     sync.WaitGroup

	closeOnce sync.Once
	closeErr  error

	// cbMu serialises OnState callbacks so they are delivered in the order
	// the transitions happened.
	cbMu sync.Mutex

	mu           sync.Mutex
	state        State
	conn         *net.UDPConn
	gen          uint64
	closed       bool
	rtt          time.Duration
	lastServerTS int64
	lastKASent   time.Time

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

	s.setState(StateResolving, nil)
	if err := s.openSocket(); err != nil {
		s.setState(StateClosed, err)
		return nil, err
	}

	s.wg.Add(1)
	go s.lifecycleLoop()
	return s, nil
}

// State returns the session's current lifecycle state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// RTT returns the most recent keepalive round-trip time, or zero if no
// keepalive has been answered yet.
func (s *Session) RTT() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rtt
}

// Close sends a best-effort BYE, closes the socket and stops the session's
// goroutines. It is safe to call more than once; only the first call does
// anything. A BYE from an unbound address is inert server-side, which is why
// this is best-effort: the server's liveness sweep is the backstop.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		conn := s.conn
		s.conn = nil
		s.mu.Unlock()

		close(s.done)

		if conn != nil {
			if _, err := conn.Write(NewBye(s.self).AppendTo(nil)); err != nil {
				s.log.Debug("voice: BYE not sent", "err", err)
			}
			s.closeErr = conn.Close()
		}
		s.wg.Wait()
		s.setState(StateClosed, nil)
	})
	return s.closeErr
}

// now reads the injected clock.
func (s *Session) now() time.Time { return s.clock() }

// setState records the new state and delivers it to OnState. The callback
// runs outside s.mu so it may call State() or RTT(), and under cbMu so
// callbacks cannot be reordered relative to each other.
func (s *Session) setState(st State, err error) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()

	if err != nil {
		s.log.Info("voice: session state", "state", st.String(), "err", err)
	} else {
		s.log.Debug("voice: session state", "state", st.String())
	}

	s.cbMu.Lock()
	defer s.cbMu.Unlock()
	if s.onState != nil {
		s.onState(st, err)
	}
}

// currentGen returns the generation number of the live socket.
func (s *Session) currentGen() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
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
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("voice: resolve %q: %w", addr, err)
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
	s.rtt = 0
	s.lastServerTS = 0
	s.lastKASent = time.Time{}
	s.mu.Unlock()

	if old != nil {
		old.Close()
	}

	s.wg.Add(1)
	go s.rxLoop(conn, gen)
	return nil
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
		default:
			// VOICE and BYE are not this task's business; the RX audio path
			// picks them up in a later task.
		}
	}
}

// emit queues a control event without ever blocking the read loop.
func (s *Session) emit(ev event) {
	select {
	case s.events <- ev:
	case <-s.done:
	default:
		s.log.Warn("voice: control event queue full, dropping", "kind", ev.kind)
	}
}

// lifecycleLoop owns the handshake ladder and the keepalive schedule for the
// life of the session.
func (s *Session) lifecycleLoop() {
	defer s.wg.Done()

	for {
		acked, closed := s.runLadder()
		if closed {
			return
		}
		if acked {
			s.setState(StateConnected, nil)
			lost, closed := s.serve()
			if closed {
				return
			}
			if lost {
				// Re-HELLO on the SAME socket first: if the binding died
				// server-side (a restart, a cleanup sweep) the existing
				// source address is still perfectly good.
				s.setState(StateRebinding, nil)
				continue
			}
			return
		}

		// The ladder is exhausted. The C# peer logs "proceeding with
		// keepalive path" here; that cannot work against this server,
		// because keepalives from an unbound address are dropped. Report the
		// reason and re-run the whole ladder on a fresh socket instead --
		// the usual causes (server restarting, secret not yet propagated to
		// a voice node) are transient, so surrender is the wrong response.
		s.setState(StateRetrying, fmt.Errorf(
			"voice: no HELLO_ACK after %d attempts; retrying in %s", len(helloLadder), retryInterval))

		for {
			if _, closed := s.wait(retryInterval, nil); closed {
				return
			}
			s.setState(StateResolving, nil)
			if err := s.openSocket(); err != nil {
				if errors.Is(err, errSessionClosed) {
					return
				}
				s.setState(StateRetrying, err)
				continue
			}
			break
		}
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
func (s *Session) recordKeepalive(ev event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.serverTS != 0 {
		s.lastServerTS = ev.serverTS
	}
	if !s.lastKASent.IsZero() {
		if rtt := ev.at.Sub(s.lastKASent); rtt > 0 {
			s.rtt = rtt
		}
	}
}

// sendKeepalive writes a keepalive echoing the server's last timestamp. The
// echo is the only input to the server's per-client latency map, so it is not
// optional decoration; before the first reply there is nothing to echo and
// the payload is empty.
func (s *Session) sendKeepalive() error {
	s.mu.Lock()
	echo := s.lastServerTS
	s.lastKASent = s.now()
	s.mu.Unlock()
	return s.send(NewKeepalive(s.self, echo))
}

// send writes one packet on the live socket.
func (s *Session) send(p *Packet) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return errSessionClosed
	}
	_, err := conn.Write(p.AppendTo(nil))
	return err
}
