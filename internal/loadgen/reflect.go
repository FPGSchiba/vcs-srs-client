package loadgen

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	"github.com/google/uuid"
)

// DeterministicUUID returns a version-4 UUID derived from sha256(seed || i),
// so calibration runs name their bots identically every time.
func DeterministicUUID(seed int64, i int) uuid.UUID {
	var in [16]byte
	binary.BigEndian.PutUint64(in[:8], uint64(seed))
	binary.BigEndian.PutUint64(in[8:], uint64(i))
	sum := sha256.Sum256(in[:])
	var u uuid.UUID
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return u
}

// SplitDests splits n destinations into `senders` contiguous [lo, hi) ranges
// covering 0..n whose sizes differ by at most one. Ranges may be empty.
func SplitDests(n, senders int) [][2]int {
	if senders < 1 {
		senders = 1
	}
	out := make([][2]int, senders)
	base, extra := n/senders, n%senders
	lo := 0
	for i := range out {
		sz := base
		if i < extra {
			sz++
		}
		out[i] = [2]int{lo, lo + sz}
		lo += sz
	}
	return out
}

type reflPeer struct {
	id uuid.UUID
	ap netip.AddrPort
}

type reflJob struct {
	pkt   []byte
	peers []reflPeer
	from  uuid.UUID
}

// Reflector is the calibration peer: it accepts any HELLO and fans every
// VOICE frame out to all other registered addresses. One UDP socket carries
// all traffic (bots use connected sockets, which accept datagrams only from
// the address they dialled, udp(7)); several goroutines write to it.
type Reflector struct {
	conn    *net.UDPConn
	senders int
	sendErr atomic.Uint64

	mu    sync.Mutex
	peers atomic.Pointer[[]reflPeer] // copy-on-write snapshot
}

// NewReflector listens on addr (port 0 for an ephemeral port) with at least
// two sender goroutines.
func NewReflector(addr string, senders int) (*Reflector, error) {
	if senders < 2 {
		return nil, fmt.Errorf("reflector: senders %d must be >= 2", senders)
	}
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	c, err := net.ListenUDP("udp", ua)
	if err != nil {
		return nil, err
	}
	_ = c.SetReadBuffer(4 << 20)
	_ = c.SetWriteBuffer(4 << 20)
	r := &Reflector{conn: c, senders: senders}
	empty := []reflPeer{}
	r.peers.Store(&empty)
	return r, nil
}

// Addr is the bound host:port.
func (r *Reflector) Addr() string { return r.conn.LocalAddr().String() }

// SendErrors counts failed fan-out writes.
func (r *Reflector) SendErrors() uint64 { return r.sendErr.Load() }

func (r *Reflector) register(id uuid.UUID, ap netip.AddrPort) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old := *r.peers.Load()
	next := make([]reflPeer, 0, len(old)+1)
	found := false
	for _, p := range old {
		if p.id == id {
			p.ap = ap
			found = true
		}
		next = append(next, p)
	}
	if !found {
		next = append(next, reflPeer{id, ap})
	}
	r.peers.Store(&next)
}

// Serve reads until ctx is done or the socket fails, then closes the socket.
func (r *Reflector) Serve(ctx context.Context) error {
	jobs := make([]chan reflJob, r.senders)
	var wg sync.WaitGroup
	for i := range jobs {
		jobs[i] = make(chan reflJob, 4096)
		wg.Add(1)
		go func(ch chan reflJob) {
			defer wg.Done()
			for j := range ch {
				for _, p := range j.peers {
					if p.id == j.from {
						continue
					}
					if _, err := r.conn.WriteToUDPAddrPort(j.pkt, p.ap); err != nil {
						r.sendErr.Add(1)
					}
				}
			}
		}(jobs[i])
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = r.conn.Close()
		case <-stop:
		}
	}()
	defer func() {
		close(stop)
		for _, ch := range jobs {
			close(ch)
		}
		wg.Wait()
		_ = r.conn.Close()
	}()

	buf := make([]byte, voice.MaxDatagram*2)
	for {
		n, from, err := r.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		p, perr := voice.Parse(buf[:n])
		if perr != nil {
			continue
		}
		switch p.Type {
		case voice.PacketTypeHello:
			r.register(p.SenderID, from)
			ack := &voice.Packet{Type: voice.PacketTypeHelloAck, SenderID: p.SenderID}
			if _, err := r.conn.WriteToUDPAddrPort(ack.AppendTo(nil), from); err != nil {
				r.sendErr.Add(1)
			}
		case voice.PacketTypeKeepalive:
			ack := voice.NewKeepalive(p.SenderID, time.Now().UnixNano())
			if _, err := r.conn.WriteToUDPAddrPort(ack.AppendTo(nil), from); err != nil {
				r.sendErr.Add(1)
			}
		case voice.PacketTypeVoice:
			peers := *r.peers.Load()
			pkt := append([]byte(nil), buf[:n]...) // workers outlive buf
			for i, rg := range SplitDests(len(peers), r.senders) {
				if rg[0] == rg[1] {
					continue
				}
				select {
				case jobs[i] <- reflJob{pkt: pkt, peers: peers[rg[0]:rg[1]], from: p.SenderID}:
				default:
					r.sendErr.Add(uint64(rg[1] - rg[0])) // queue full: reflector-side loss
				}
			}
		}
	}
}
