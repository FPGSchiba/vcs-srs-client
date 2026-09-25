package voice

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice/testdata"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// This file is Task 12: the only fixture in this phase that drives a REAL
// vcs-srs-server rather than our own reading of it. Every other test in
// this package (and its siblings across the phase) is checked against
// ../testserver_test.go, a fixture we wrote ourselves from our own reading
// of the protocol -- which means it can never catch a MISREADING of that
// protocol; it would encode the same mistake and pass regardless. These
// tests are what can.
//
// Every test below calls testdata.Start, which calls testdata.RequireServer
// and skips cleanly when VCS_SERVER_BIN is unset, so `go test ./...` stays
// green on a machine with no server. See internal/voice/testdata for the
// fixture itself and .superpowers/sdd/task-12-report.md for what running
// this against the real binary found.

// nopEmitter satisfies events.Emitter (internal/events) without importing
// that package: session.New only needs the method, and every event these
// tests care about is read back from state.Store directly, not from the
// emitted event stream.
type nopEmitter struct{}

func (nopEmitter) Emit(string, any) {}

// guestUnitID satisfies the real server's `^[A-Z0-9]{2,4}$` UnitId
// validation (srs/utils.go checkUnitId). Every guest in this file uses the
// same one; nothing here asserts on unit identity.
const guestUnitID = "TST"

// testTone returns a deterministic, non-silent one-DSP-tick (480-sample)
// PCM buffer.
//
// Pure digital silence (an all-zero buffer) is exactly what a naive
// integration test would reach for, and it is wrong: libopus encodes true
// zero-signal PCM down to a 2-3 BYTE frame even with DTX disabled (nothing
// here enables DTX -- see tx.go's txBitrate comment -- this is ordinary
// entropy coding doing its job on a maximally compressible input). The real
// server's relay floor (voice/server.go's handleVoicePacket:
// `len(packet.Payload) > 5`, mirrored client-side as MinVoicePayload in
// packet.go) then drops every one of those frames SILENTLY: no error, no
// counter, nothing -- see task-12-report.md. Our own hand-written fixture
// (testserver_test.go) applies no such floor, so this could never have been
// caught without the real server. An actual signal avoids it entirely.
func testTone() []float32 {
	buf := make([]float32, audio.FrameSamples)
	for i := range buf {
		buf[i] = float32(0.2 * math.Sin(2*math.Pi*440*float64(i)/float64(audio.SampleRate)))
	}
	return buf
}

// silentLogger is the Options.Log every dialed session in this file uses:
// real, not nil (nil would panic on first use), just discarded, matching
// testOptions' Log in session_test.go.
func silentLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// integrationClient bundles a REAL, fully-connected guest control-plane
// session (InitAuth -> GuestLogin -> SyncClient, exactly what a real login
// does) with the identity and secret voice.Dial needs. Building this is
// what makes these tests drive the real client stack rather than a
// hand-rolled substitute for the control plane.
type integrationClient struct {
	sess   *session.Session
	self   uuid.UUID
	secret string
}

// connectGuest logs in as name against srv's coalition and blocks until
// SyncClient has returned a voice secret, exactly the precondition the
// design doc's session lifecycle (§7) names for starting a voice session at
// all.
func connectGuest(t *testing.T, srv *testdata.Server, name string) *integrationClient {
	t.Helper()

	st := state.New()
	// The real server's checkVersion (srs/utils.go) does NOT do a semver
	// range check as our own unit tests assume nothing about at all -- it
	// is a hardcoded literal match against the server's own build version
	// ("return version == \"0.1.0\""), so any other string is rejected with
	// "Unsupported version" before GuestLogin is even reachable. See
	// task-12-report.md.
	sess := session.New(st, nopEmitter{}, session.Deps{Version: "0.1.0"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The real server also validates UnitId against `^[A-Z0-9]{2,4}$`
	// (srs/utils.go checkUnitId) -- an empty string, which nothing on the
	// client side rejects, fails GuestLogin with "Invalid UnitId". See
	// task-12-report.md.
	if err := sess.Connect(ctx, srv.ControlAddr, name, srv.Password, guestUnitID); err != nil {
		t.Fatalf("guest connect (%s): %v", name, err)
	}
	t.Cleanup(func() { _ = sess.Disconnect(context.Background()) })

	secret, _, _ := st.VoiceCredentials()
	if secret == "" {
		t.Fatalf("guest connect (%s): SyncClient returned no voice secret", name)
	}
	selfGUID := st.Snapshot().SelfGUID
	self, err := uuid.Parse(selfGUID)
	if err != nil {
		t.Fatalf("guest connect (%s): self GUID %q did not parse: %v", name, selfGUID, err)
	}
	return &integrationClient{sess: sess, self: self, secret: secret}
}

// pushRadio publishes one enabled radio on freq through the real control
// plane. The server's relay decision (voice/server.go's
// GetListeningClients -> IsListeningOnFrequency) requires an exact float32
// match against a radio a client pushed exactly this way, in the SAME
// coalition as the sender -- so every non-test, non-global relay case needs
// this on the receiving end (and, for the happy path, on the sending end
// too, since the server does not relay to a client it does not know about).
func (c *integrationClient) pushRadio(t *testing.T, id uint32, freq KHz) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info := &srspb.RadioInfo{Radios: []*srspb.Radio{{
		Id:        id,
		Name:      "radio",
		Frequency: freq.MHz32(),
		Enabled:   true,
	}}}
	if err := c.sess.UpdateRadioInfo(ctx, info); err != nil {
		t.Fatalf("UpdateRadioInfo: %v", err)
	}
}

// dialVoice dials the real voice.Session for c against srv. It always uses
// ConfigHost/ConfigPort, never Sync/Update: SyncClient's
// coalition_voice_addr is unconditionally "" on a standalone server (design
// doc §6 -- that field is populated only by a distributed voice node's
// registry entry), so config-supplied host/port is the only source that
// ever resolves here, exactly as it is in every standalone production
// deployment today.
func dialVoice(t *testing.T, srv *testdata.Server, c *integrationClient, rec *stateRecorder, mutate ...func(*Options)) *Session {
	t.Helper()
	opt := Options{Log: silentLogger(), OnState: rec.on}
	for _, m := range mutate {
		m(&opt)
	}
	s, err := Dial(Sources{ConfigHost: srv.VoiceHost, ConfigPort: srv.VoicePort}, c.self, c.secret, opt)
	if err != nil {
		t.Fatalf("voice.Dial: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// pollUntil polls cond against REAL wall-clock time. Unlike session_test.go's
// fake-clock suite, there is no clock to advance by hand here: the session
// under test is a real process talking over a real (loopback) socket, so
// forward progress can only come from real elapsed time.
func pollUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

// transmitUntil repeatedly hands s silent PCM -- two audio.FrameSamples
// halves per call, matching WriteFrame's own 960-sample accumulator (tx.go)
// -- until cond is satisfied or timeout elapses. Retrying rather than
// sending once is what keeps a single dropped UDP datagram from flaking an
// otherwise-correct test.
func transmitUntil(t *testing.T, s *Session, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	tone := testTone()
	// rx.go's decode path paces itself against ring drainage by design
	// (rxState.aheadSamples, topUp's doc): it decodes only far enough ahead
	// to keep a small ring topped up and then STOPS until something reads
	// from that ring. In production that reader is the DSP goroutine's
	// ReadInto call, once per 10 ms tick; nothing in this test process runs
	// that pipeline, so without calling ReadInto here too, decode legitimately
	// (and correctly) stops after filling the ring once, forever, no matter
	// how much longer audio keeps arriving -- discovered the hard way while
	// writing case 7 (see task-12-report.md). Draining it here is what a
	// real playback consumer would be doing throughout this whole test.
	sink := make([]float32, audio.FrameSamples)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.WriteFrame(tone)
		s.WriteFrame(tone)
		s.ReadInto(sink)
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

// rawVoiceSocket dials a plain, unmanaged UDP socket at the fixture's voice
// endpoint. It exists for the handful of cases that need to speak (or
// deliberately misuse) the wire protocol directly, outside a Session's own
// state machine: an address a HELLO never bound (case 3), a BYE sent
// without also tearing down the local socket that observes what happens
// next (case 6). It is built from this package's own Packet
// encode/decode -- Parse, NewHello, NewVoice, NewBye -- so the wire format
// itself is still exercised through the real code under test, only the
// state machine around it is bypassed.
func rawVoiceSocket(t *testing.T, srv *testdata.Server) *net.UDPConn {
	t.Helper()
	raddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", srv.VoiceHost, srv.VoicePort))
	if err != nil {
		t.Fatalf("resolve voice addr: %v", err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		t.Fatalf("dial raw voice socket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readUntilType reads datagrams off conn until one parses as want or timeout
// elapses, reporting which happened.
func readUntilType(conn *net.UDPConn, timeout time.Duration, want PacketType) bool {
	deadline := time.Now().Add(timeout)
	buf := make([]byte, MaxDatagram)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := conn.Read(buf)
		if err != nil {
			continue
		}
		if p, perr := Parse(buf[:n]); perr == nil && p.Type == want {
			return true
		}
	}
	return false
}

// drainVoiceCount transmits silence on sa while reading raw datagrams off
// conn, for up to timeout, and reports how many parsed as a VOICE packet.
// Used both to prove a baseline relay is working (expect > 0) and, after a
// BYE, that it has stopped (expect == 0) -- driving the transmitter inside
// the same loop is what lets both assertions share one bounded wait rather
// than racing a fixed sleep against delivery.
func drainVoiceCount(sa *Session, conn *net.UDPConn, timeout time.Duration) int {
	tone := testTone()
	buf := make([]byte, MaxDatagram)
	count := 0
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sa.WriteFrame(tone)
		sa.WriteFrame(tone)
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, err := conn.Read(buf)
		if err != nil {
			continue
		}
		if p, perr := Parse(buf[:n]); perr == nil && p.Type == PacketTypeVoice {
			count++
		}
	}
	return count
}

// corruptSecret flips one bit of secret's first byte, keeping the length
// (and so voice.Dial's own length validation) unchanged while guaranteeing
// the result never equals secret.
func corruptSecret(secret string) string {
	b := []byte(secret)
	b[0] ^= 0x01
	return string(b)
}

// ---------------------------------------------------------------------------
// 1. Happy-path relay between two clients on a shared frequency.
// ---------------------------------------------------------------------------

func TestIntegrationRelayBetweenTwoClients(t *testing.T) {
	srv := testdata.Start(t)

	alice := connectGuest(t, srv, "Alice")
	bob := connectGuest(t, srv, "Bob")

	const freq = KHz(251000) // outside both fixture frequencies (100.000, 200.000)
	alice.pushRadio(t, 1, freq)
	bob.pushRadio(t, 1, freq)

	recA, recB := newStateRecorder(), newStateRecorder()
	sa := dialVoice(t, srv, alice, recA)
	sb := dialVoice(t, srv, bob, recB)

	pollUntil(t, 10*time.Second, "both sessions Connected", func() bool {
		return sa.State() == StateConnected && sb.State() == StateConnected
	})

	sb.SetRXContext([]KHz{freq}, nil, nil)
	sa.SetTXFrequencies([]TXTarget{{Freq: freq}})

	transmitUntil(t, sa, 10*time.Second, "Bob to decode Alice's relayed audio", func() bool {
		return sb.RXStats().Decoded > 0
	})

	if got := sb.RXStats().DroppedFreq; got != 0 {
		t.Errorf("Bob dropped %d packets as off-frequency; the server relayed something unexpected", got)
	}
}

// ---------------------------------------------------------------------------
// 2. A wrong secret is rejected: no binding, no ACK, Connected never reached.
// ---------------------------------------------------------------------------

func TestIntegrationWrongSecretRejected(t *testing.T) {
	srv := testdata.Start(t)
	c := connectGuest(t, srv, "Eve")

	rec := newStateRecorder()
	s, err := Dial(Sources{ConfigHost: srv.VoiceHost, ConfigPort: srv.VoicePort}, c.self, corruptSecret(c.secret), Options{
		Log: silentLogger(), OnState: rec.on,
	})
	if err != nil {
		t.Fatalf("voice.Dial: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Several real HELLO-ladder rungs' worth of time. The server's
	// handleHelloPacket rejects an invalid secret without acknowledging it
	// or binding the address (voice/server.go, rejectInvalidSecret) -- there
	// is no event to poll for, only the absence of one, so this is a bounded
	// wait followed by an assertion, not a poll.
	time.Sleep(4 * time.Second)

	if s.State() == StateConnected || rec.saw(StateConnected) {
		t.Fatal("session reached Connected with a wrong secret; the server must never ACK an invalid HELLO")
	}
}

// ---------------------------------------------------------------------------
// 3. VOICE from an address a HELLO never bound is dropped, even carrying a
//    currently-bound client's own id.
// ---------------------------------------------------------------------------

func TestIntegrationVoiceFromUnboundAddressDropped(t *testing.T) {
	srv := testdata.Start(t)

	alice := connectGuest(t, srv, "Alice")
	bob := connectGuest(t, srv, "Bob")

	const freq = KHz(252000)
	alice.pushRadio(t, 1, freq)
	bob.pushRadio(t, 1, freq)

	recA, recB := newStateRecorder(), newStateRecorder()
	sa := dialVoice(t, srv, alice, recA) // Alice's REAL, bound session
	sb := dialVoice(t, srv, bob, recB)
	pollUntil(t, 10*time.Second, "both sessions Connected", func() bool {
		return sa.State() == StateConnected && sb.State() == StateConnected
	})
	sb.SetRXContext([]KHz{freq}, nil, nil)
	sa.SetTXFrequencies([]TXTarget{{Freq: freq}})

	// Positive control: prove Alice's REAL, bound session actually reaches
	// Bob before trusting the negative assertion below. Without this, the
	// "0 datagrams" assertion would pass just as well if relay were broken
	// outright for this fixture instance, since nothing would ever have
	// been relayed either way -- see TestIntegrationByeDisconnects, which
	// uses the identical shape ("cannot test that BYE stops it").
	transmitUntil(t, sa, 10*time.Second, "Bob to decode Alice's real, bound relay -- cannot test that a spoofed address is rejected without this working first", func() bool {
		return sb.RXStats().Decoded > 0
	})
	// Let Alice's transmit queue (up to txQueueDepth frames, ~160 ms of
	// buffered audio -- see tx.go) fully drain before taking the baseline,
	// so an in-flight legitimate datagram is never mistaken for the
	// spoofed one landing.
	time.Sleep(300 * time.Millisecond)
	baseline := sb.RXStats().Received

	// A SECOND, unrelated socket presenting Alice's already-bound identity.
	// voice/server.go's isBoundAddr keys on (clientID, source address)
	// TOGETHER, so a currently valid client id from any OTHER address must
	// still be rejected -- this is the property that makes source-address
	// spoofing (and the recovery path in case 7) meaningful at all.
	spoof := rawVoiceSocket(t, srv)
	pkt := NewVoice(alice.self, 0, freq, []byte("spoofed-payload"), true, false)
	if _, err := spoof.Write(pkt.AppendTo(nil)); err != nil {
		t.Fatalf("write spoofed VOICE: %v", err)
	}

	// Bounded wait, not sleep-then-check: give the server every chance to
	// relay it wrongly before concluding it did not.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	if got := sb.RXStats().Received - baseline; got != 0 {
		t.Fatalf("Bob's rxLoop saw %d MORE datagrams after the spoof (relay from Alice's real session already proven working above); the server must not have relayed a spoofed, unbound-address VOICE packet at all", got)
	}
}

// ---------------------------------------------------------------------------
// 4. Keepalive echo feeds the session's own RTT measurement.
// ---------------------------------------------------------------------------

func TestIntegrationKeepaliveEchoFeedsLatency(t *testing.T) {
	srv := testdata.Start(t)
	c := connectGuest(t, srv, "Solo")
	rec := newStateRecorder()
	s := dialVoice(t, srv, c, rec)

	pollUntil(t, 10*time.Second, "Connected", func() bool { return s.State() == StateConnected })
	pollUntil(t, 5*time.Second, "a non-zero RTT from the keepalive echo", func() bool { return s.RTT() > 0 })
}

// ---------------------------------------------------------------------------
// 5. A packet sent on the server's test frequency echoes back to its sender.
// ---------------------------------------------------------------------------

func TestIntegrationTestFrequencyLoopback(t *testing.T) {
	srv := testdata.Start(t)
	c := connectGuest(t, srv, "Echo")
	rec := newStateRecorder()
	s := dialVoice(t, srv, c, rec)
	pollUntil(t, 10*time.Second, "Connected", func() bool { return s.State() == StateConnected })

	testFreq := KHzFromMHz32(testdata.TestFrequencyMHz)
	s.SetRXContext(nil, nil, []KHz{testFreq})
	s.SetTXFrequencies([]TXTarget{{Freq: testFreq}})

	// No RadioInfo push needed: voice/server.go's handleVoicePacket special-
	// cases a test frequency BEFORE the coalition/radio match, echoing
	// straight back to the sender's bound address (handleTestFrequencyPacket).
	transmitUntil(t, s, 10*time.Second, "our own echo back from the server's test frequency", func() bool {
		return s.RXStats().Decoded > 0
	})
}

// ---------------------------------------------------------------------------
// 6. BYE from the bound address disconnects the session; the server stops
//    relaying to it.
// ---------------------------------------------------------------------------

func TestIntegrationByeDisconnects(t *testing.T) {
	srv := testdata.Start(t)

	alice := connectGuest(t, srv, "Alice")
	bob := connectGuest(t, srv, "Bob")

	const freq = KHz(253000)
	alice.pushRadio(t, 1, freq)
	bob.pushRadio(t, 1, freq)

	recA := newStateRecorder()
	sa := dialVoice(t, srv, alice, recA)
	pollUntil(t, 10*time.Second, "Alice Connected", func() bool { return sa.State() == StateConnected })
	sa.SetTXFrequencies([]TXTarget{{Freq: freq}})

	// Bob is a raw socket speaking the protocol directly, not a second
	// voice.Session: session.Close() both sends BYE and tears down the
	// local socket in one call, which would make "did the SERVER stop
	// relaying" and "is our own socket even still open to notice"
	// indistinguishable. Driving the handshake by hand keeps them separate.
	bobConn := rawVoiceSocket(t, srv)
	if _, err := bobConn.Write(NewHello(bob.self, bob.secret).AppendTo(nil)); err != nil {
		t.Fatalf("write Bob's HELLO: %v", err)
	}
	if !readUntilType(bobConn, 5*time.Second, PacketTypeHelloAck) {
		t.Fatal("Bob's raw socket never received a HELLO_ACK")
	}

	if baseline := drainVoiceCount(sa, bobConn, 3*time.Second); baseline == 0 {
		t.Fatal("Bob never received Alice's relayed audio while bound; cannot test that BYE stops it")
	}

	if _, err := bobConn.Write(NewBye(bob.self).AppendTo(nil)); err != nil {
		t.Fatalf("write Bob's BYE: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // let the server process the disconnect

	if after := drainVoiceCount(sa, bobConn, 1500*time.Millisecond); after != 0 {
		t.Fatalf("Bob received %d VOICE packets after sending BYE; the server must stop relaying to a disconnected client", after)
	}
}

// ---------------------------------------------------------------------------
// 7. The one worth the whole exercise: after the client's own socket rebinds
//    to a new source address -- otherwise completely silent, per §7.3 -- the
//    session detects it and re-HELLOs without any action from the caller.
// ---------------------------------------------------------------------------

func TestIntegrationReHelloAfterSourceAddressChange(t *testing.T) {
	srv := testdata.Start(t)
	c := connectGuest(t, srv, "Roamer")
	rec := newStateRecorder()

	s := dialVoice(t, srv, c, rec, func(o *Options) {
		// A short keepalive interval keeps the three-consecutive-unanswered
		// binding-loss threshold (§7.3) well under a second instead of
		// production's 15 s, without touching any package constant --
		// Keepalive is ordinary public Options, exactly what a caller with
		// unusual latency requirements would also tune.
		o.Keepalive = 200 * time.Millisecond
	})
	pollUntil(t, 10*time.Second, "Connected", func() bool { return s.State() == StateConnected })

	testFreq := KHzFromMHz32(testdata.TestFrequencyMHz)
	s.SetRXContext(nil, nil, []KHz{testFreq})
	s.SetTXFrequencies([]TXTarget{{Freq: testFreq}})

	transmitUntil(t, s, 10*time.Second, "baseline echo before the source address changes", func() bool {
		return s.RXStats().Decoded > 0
	})
	baseline := s.RXStats().Decoded

	// Provoke exactly the failure §7.3 exists for. openSocket is the same
	// call Session's own reopen() makes internally on a fresh dial; calling
	// it directly from the test IS "rebinding the client socket" -- nothing
	// external can rebind loopback's own address mapping the way a real NAT
	// or network switch would, so this is the faithful local equivalent: a
	// new source port, the same identity and secret, and (until detected)
	// complete silence from the server, which is still relaying to the
	// old, now-abandoned address.
	if err := s.openSocket(); err != nil {
		t.Fatalf("forcing a socket rebind: %v", err)
	}

	pollUntil(t, 5*time.Second, "StateRebinding after the source address change", func() bool {
		return rec.saw(StateRebinding)
	})
	pollUntil(t, 10*time.Second, "recovery to StateConnected", func() bool {
		return s.State() == StateConnected
	})

	// And, without any action from the caller, audio flows again.
	transmitUntil(t, s, 10*time.Second, "the echo to resume after recovery", func() bool {
		return s.RXStats().Decoded > baseline
	})
}
