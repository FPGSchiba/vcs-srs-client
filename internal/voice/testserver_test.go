package voice

import (
	"net"
	"sync"
	"testing"
	"time"
)

// keepaliveObservation is one keepalive as the fixture saw it, paired with
// the timestamp the fixture had most recently sent out before it arrived.
// The pairing is captured under the fixture's lock, so a test can assert
// "this keepalive echoed the timestamp we last sent" without racing the
// reply the fixture is about to send for this very packet.
type keepaliveObservation struct {
	payload []byte // the payload exactly as received
	prevTS  int64  // the last timestamp this server sent before this arrived
}

// testServer is a minimal UDP responder for SESSION STATE-MACHINE tests
// only: the retry ladder, the unanswered-keepalive counter, and re-HELLO
// recovery, none of which can be provoked against a real server without
// sleeping for minutes.
//
// It is deliberately NOT a protocol authority. It encodes our own reading of
// the server, so it can never catch a MISREADING of it -- which is exactly
// why the design doc (D11) puts the real headless server behind the
// integration tests in Task 12. Do not grow this into a second server
// implementation; if you find yourself wanting to, write an integration test
// instead.
type testServer struct {
	conn *net.UDPConn

	mu         sync.Mutex
	bound      map[string]bool // source addr -> bound
	acceptSec  string          // the only secret it accepts
	dropHello  bool            // refuse to ACK, to drive the retry ladder
	dropKeep   bool            // ignore keepalives, to drive binding-loss detection
	helloes    int
	keepalives int
	byes       int
	byesBound  int    // BYEs that arrived from an address this server had bound
	lastHello  string // source address of the most recent HELLO
	lastSentTS int64  // the timestamp most recently sent in a keepalive reply
	kaSeen     []keepaliveObservation

	// voices records every VOICE datagram in arrival order. The real server
	// relays them; this fixture only counts and keeps them, which is all the
	// TX tests need to see.
	voices []*Packet
}

func newTestServer(t *testing.T, secret string) *testServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ts := &testServer{conn: conn, bound: map[string]bool{}, acceptSec: secret}
	go ts.loop()
	t.Cleanup(func() { conn.Close() })
	return ts
}

func (ts *testServer) addr() string { return ts.conn.LocalAddr().String() }

func (ts *testServer) setDropHello(v bool)     { ts.mu.Lock(); ts.dropHello = v; ts.mu.Unlock() }
func (ts *testServer) setDropKeepalive(v bool) { ts.mu.Lock(); ts.dropKeep = v; ts.mu.Unlock() }

func (ts *testServer) helloCount() int { ts.mu.Lock(); defer ts.mu.Unlock(); return ts.helloes }

// lastHelloFrom returns the source address the most recent HELLO arrived
// from. The server binds a session to its source address, so this is how a
// test tells a re-HELLO on the SAME socket apart from one on a freshly
// dialed socket.
func (ts *testServer) lastHelloFrom() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastHello
}

// voiceCount returns how many VOICE datagrams have arrived.
func (ts *testServer) voiceCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.voices)
}

// voicePackets returns a copy of every VOICE datagram seen so far, in
// arrival order.
func (ts *testServer) voicePackets() []*Packet {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]*Packet(nil), ts.voices...)
}

func (ts *testServer) keepaliveCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.keepalives
}

// observedKeepalives returns a copy of every keepalive seen so far, in
// arrival order.
func (ts *testServer) observedKeepalives() []keepaliveObservation {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]keepaliveObservation(nil), ts.kaSeen...)
}

// byeCounts returns the total number of BYEs seen and how many of those
// arrived from a source address the server had bound via a verified HELLO.
func (ts *testServer) byeCounts() (total, fromBound int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.byes, ts.byesBound
}

func (ts *testServer) loop() {
	buf := make([]byte, MaxDatagram)
	for {
		n, from, err := ts.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt, err := Parse(buf[:n])
		if err != nil {
			continue
		}
		ts.mu.Lock()
		switch pkt.Type {
		case PacketTypeHello:
			ts.helloes++
			ts.lastHello = from.String()
			ok := len(pkt.Payload) >= VoiceSecretLen &&
				string(pkt.Payload[:VoiceSecretLen]) == ts.acceptSec
			drop := ts.dropHello
			ts.mu.Unlock()
			if ok && !drop {
				ts.mu.Lock()
				ts.bound[from.String()] = true
				ts.mu.Unlock()
				ack := &Packet{Type: PacketTypeHelloAck, SenderID: pkt.SenderID}
				ts.conn.WriteToUDP(ack.AppendTo(nil), from)
			}
			continue
		case PacketTypeKeepalive:
			ts.keepalives++
			ts.kaSeen = append(ts.kaSeen, keepaliveObservation{
				payload: append([]byte(nil), pkt.Payload...),
				prevTS:  ts.lastSentTS,
			})
			bound, drop := ts.bound[from.String()], ts.dropKeep
			if bound && !drop {
				sent := time.Now().UnixMilli()
				ts.lastSentTS = sent
				ts.mu.Unlock()
				reply := NewKeepalive(pkt.SenderID, sent)
				ts.conn.WriteToUDP(reply.AppendTo(nil), from)
				continue
			}
			ts.mu.Unlock()
			continue
		case PacketTypeVoice:
			ts.voices = append(ts.voices, pkt)
			ts.mu.Unlock()
			continue
		case PacketTypeBye:
			ts.byes++
			if ts.bound[from.String()] {
				ts.byesBound++
			}
			ts.mu.Unlock()
			continue
		}
		ts.mu.Unlock()
	}
}
