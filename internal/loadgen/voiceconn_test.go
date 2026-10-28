package loadgen

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	"github.com/google/uuid"
)

// fakeVoice is a loopback UDP server: acks HELLO (after dropHello drops),
// acks KEEPALIVE with an 8-byte timestamp, relays VOICE to every other
// registered address.
type fakeVoice struct {
	conn      *net.UDPConn
	dropHello atomic.Int32
	never     bool
	dropKA    atomic.Int32 // keepalives to swallow
	kaEchoes  chan int64   // timestamps carried by received keepalives
	mu        sync.Mutex
	peers     map[uuid.UUID]*net.UDPAddr
}

func newFakeVoice(t *testing.T, drop int32, never bool) *fakeVoice {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeVoice{conn: c, never: never, peers: map[uuid.UUID]*net.UDPAddr{}, kaEchoes: make(chan int64, 64)}
	f.dropHello.Store(drop)
	go f.loop()
	t.Cleanup(func() { c.Close() })
	return f
}

func (f *fakeVoice) addr() string { return f.conn.LocalAddr().String() }

func (f *fakeVoice) loop() {
	buf := make([]byte, 2048)
	for {
		n, from, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		p, err := voice.Parse(buf[:n])
		if err != nil {
			continue
		}
		switch p.Type {
		case voice.PacketTypeHello:
			if f.never || f.dropHello.Add(-1) >= 0 {
				continue
			}
			f.mu.Lock()
			f.peers[p.SenderID] = from
			f.mu.Unlock()
			ack := &voice.Packet{Type: voice.PacketTypeHelloAck, SenderID: p.SenderID}
			_, _ = f.conn.WriteToUDP(ack.AppendTo(nil), from)
		case voice.PacketTypeKeepalive:
			select {
			case f.kaEchoes <- voice.KeepaliveTimestamp(p.Payload):
			default:
			}
			if f.dropKA.Add(-1) >= 0 {
				continue
			}
			ack := voice.NewKeepalive(p.SenderID, time.Now().UnixNano())
			_, _ = f.conn.WriteToUDP(ack.AppendTo(nil), from)
		case voice.PacketTypeVoice:
			f.mu.Lock()
			for id, a := range f.peers {
				if id != p.SenderID {
					_, _ = f.conn.WriteToUDP(buf[:n], a)
				}
			}
			f.mu.Unlock()
		}
	}
}

func dialHello(t *testing.T, f *fakeVoice) *VoiceConn {
	t.Helper()
	vc, err := DialVoice(f.addr(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vc.Close() })
	return vc
}

func TestHelloLadderRetries(t *testing.T) {
	f := newFakeVoice(t, 2, false)
	vc := dialHello(t, f)
	ladder := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 150 * time.Millisecond, 200 * time.Millisecond, 250 * time.Millisecond}
	tries, err := vc.Hello(context.Background(), "secret", ladder)
	if err != nil || tries != 3 {
		t.Fatalf("tries=%d err=%v, want 3 nil", tries, err)
	}
}

func TestHelloGivesUpAfterLadder(t *testing.T) {
	f := newFakeVoice(t, 0, true)
	vc := dialHello(t, f)
	start := time.Now()
	tries, err := vc.Hello(context.Background(), "secret", []time.Duration{50 * time.Millisecond, 100 * time.Millisecond})
	if err == nil {
		t.Fatal("want error")
	}
	if d := time.Since(start); d > 400*time.Millisecond {
		t.Fatalf("took %v, want <= 400ms", d)
	}
	if tries != 2 {
		t.Fatalf("tries=%d, want 2", tries)
	}
}

func TestHelloHonoursContext(t *testing.T) {
	f := newFakeVoice(t, 0, true)
	vc := dialHello(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := vc.Hello(ctx, "s", DefaultHelloLadder); err == nil {
		t.Fatal("want ctx error")
	}
}

func TestReceiveMarksLedger(t *testing.T) {
	f := newFakeVoice(t, 0, false)
	a, b := dialHello(t, f), dialHello(t, f)
	ladder := []time.Duration{500 * time.Millisecond}
	for _, vc := range []*VoiceConn{a, b} {
		if _, err := vc.Hello(context.Background(), "s", ladder); err != nil {
			t.Fatal(err)
		}
	}
	const freq = voice.KHz(251000)
	b.ExpectFrom(a.ID, freq, 1, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Start(ctx)
	payload := make([]byte, 80)
	for seq := uint32(1); seq <= 10; seq++ {
		if err := a.Send(seq, freq, payload); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		r, ok := b.LedgerResult(a.ID, freq)
		if !ok {
			t.Fatal("no ledger")
		}
		if r.Unique == 10 {
			if r.Lost != 0 {
				t.Fatalf("lost %d", r.Lost)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d unique", r.Unique)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestKeepaliveRTTRecorded(t *testing.T) {
	f := newFakeVoice(t, 0, false)
	vc := dialHello(t, f)
	if _, err := vc.Hello(context.Background(), "s", []time.Duration{500 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	vc.KeepaliveEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vc.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for vc.RTT.Count() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("no RTT recorded")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestKeepaliveRTTRecorded_EchoesServerTimestamp(t *testing.T) {
	f := newFakeVoice(t, 0, false)
	vc := dialHello(t, f)
	if _, err := vc.Hello(context.Background(), "s", []time.Duration{500 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	vc.KeepaliveEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vc.Start(ctx)
	first := <-f.kaEchoes
	second := <-f.kaEchoes
	if first != 0 {
		t.Fatalf("first keepalive echoed %d, want 0", first)
	}
	if second == 0 {
		t.Fatal("second keepalive did not echo the server timestamp")
	}
}

func TestKeepaliveRecoversFromLostProbe(t *testing.T) {
	f := newFakeVoice(t, 0, false)
	f.dropKA.Store(1)
	vc := dialHello(t, f)
	if _, err := vc.Hello(context.Background(), "s", []time.Duration{500 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	vc.KeepaliveEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vc.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for vc.RTT.Count() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("bot never keepalived again after a lost probe")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := vc.KeepaliveLost.Load(); got != 1 {
		t.Fatalf("KeepaliveLost=%d, want 1", got)
	}
}

func TestCloseWithoutCancelDoesNotLeakWatcher(t *testing.T) {
	f := newFakeVoice(t, 0, false)
	vc := dialHello(t, f)
	vc.Start(context.Background())
	vc.Close()
	select {
	case <-vc.done:
	default:
		t.Fatal("done not closed after Close")
	}
	vc.watcher.Wait()
}

func TestFastHelloLadder(t *testing.T) {
	l := FastHelloLadder(3)
	if len(l) != 3 || l[0] != FastHelloStep || l[2] != 3*FastHelloStep {
		t.Fatalf("got %v", l)
	}
}
