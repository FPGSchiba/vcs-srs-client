package loadgen

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	"github.com/google/uuid"
)

// DefaultHelloLadder holds cumulative HELLO deadlines (design §4.3): one HELLO
// is sent at t=0 and again at each ladder entry that passes without an ack.
var DefaultHelloLadder = []time.Duration{
	1500 * time.Millisecond, 3000 * time.Millisecond, 4500 * time.Millisecond,
	6000 * time.Millisecond, 7500 * time.Millisecond,
}

const recvErrBackoff = 10 * time.Millisecond

// FastHelloStep is the step of the fast ladder used to measure secret
// propagation (design §4.3).
const FastHelloStep = 100 * time.Millisecond

// FastHelloLadder returns n cumulative deadlines spaced FastHelloStep apart.
func FastHelloLadder(n int) []time.Duration {
	l := make([]time.Duration, n)
	for i := range l {
		l[i] = time.Duration(i+1) * FastHelloStep
	}
	return l
}

type ledgerKey struct {
	sender uuid.UUID
	freq   voice.KHz
}

// VoiceConn is one bot's connected UDP voice socket.
type VoiceConn struct {
	ID uuid.UUID
	// KeepaliveEvery is the keepalive period; set before Start. Default 5 s.
	KeepaliveEvery time.Duration
	RxLag          *Hist // kernel receive lag; only fed when stamps are available
	RTT            *Hist // keepalive round trip
	Drops          atomic.Uint32
	Unstamped      atomic.Uint64
	// KeepaliveLost counts probes whose ack never came within KeepaliveEvery.
	KeepaliveLost atomic.Uint64
	RecvErrors    atomic.Uint64 // non-close socket read errors

	conn *net.UDPConn

	mu      sync.Mutex // guards ledgers and the Marks made into them
	ledgers map[ledgerKey]*RecvLedger

	sendMu  sync.Mutex
	sendBuf []byte

	sentAt   atomic.Int64 // UnixNano of the outstanding keepalive, 0 = none
	lastEcho atomic.Int64 // server timestamp to echo next
	wg       sync.WaitGroup
	closed   atomic.Bool
	done     chan struct{} // closed by Close
	watcher  sync.WaitGroup
}

// DialVoice opens a connected UDP socket to addr with receive stamping.
func DialVoice(addr string, id uuid.UUID) (*VoiceConn, error) {
	ra, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	c, err := net.DialUDP("udp", nil, ra)
	if err != nil {
		return nil, err
	}
	if err := EnableRxStamps(c); err != nil {
		c.Close()
		return nil, fmt.Errorf("rx stamps: %w", err)
	}
	_ = c.SetReadBuffer(1 << 20)
	return &VoiceConn{
		ID: id, KeepaliveEvery: 5 * time.Second,
		RxLag: &Hist{}, RTT: &Hist{},
		done: make(chan struct{}), conn: c, ledgers: map[ledgerKey]*RecvLedger{},
	}, nil
}

// Hello sends HELLO and waits for HELLO_ACK, resending at each ladder
// deadline. It reads the socket itself, so call it before Start. tries is the
// number of HELLOs sent.
func (v *VoiceConn) Hello(ctx context.Context, secret string, ladder []time.Duration) (tries int, err error) {
	pkt := voice.NewHello(v.ID, secret).AppendTo(nil)
	buf := make([]byte, voice.MaxDatagram*2)
	start := time.Now()
	defer func() { _ = v.conn.SetReadDeadline(time.Time{}) }()
	for _, d := range ladder {
		if err := ctx.Err(); err != nil {
			return tries, err
		}
		if _, err := v.conn.Write(pkt); err != nil {
			return tries, err
		}
		tries++
		end := start.Add(d)
		if cd, ok := ctx.Deadline(); ok && cd.Before(end) {
			end = cd
		}
		_ = v.conn.SetReadDeadline(end)
		for {
			n, rerr := v.conn.Read(buf)
			if rerr != nil {
				var ne net.Error
				if errors.As(rerr, &ne) && ne.Timeout() {
					break // next rung
				}
				return tries, rerr
			}
			if p, perr := voice.Parse(buf[:n]); perr == nil && p.Type == voice.PacketTypeHelloAck {
				return tries, nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return tries, err
	}
	return tries, fmt.Errorf("no HELLO_ACK after %d tries", tries)
}

// ExpectFrom installs the ledger for frames from sender on freq, covering
// sequences [base, base+n). Call before a hold; it replaces any earlier one.
func (v *VoiceConn) ExpectFrom(sender uuid.UUID, freq voice.KHz, base uint64, n int) *RecvLedger {
	l := NewRecvLedger(base, n)
	v.mu.Lock()
	v.ledgers[ledgerKey{sender, freq}] = l
	v.mu.Unlock()
	return l
}

// LedgerResult snapshots a ledger safely against the receive loop.
func (v *VoiceConn) LedgerResult(sender uuid.UUID, freq voice.KHz) (LedgerResult, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	l, ok := v.ledgers[ledgerKey{sender, freq}]
	if !ok {
		return LedgerResult{}, false
	}
	return l.Result(), true
}

// Start launches the receive and keepalive goroutines. Both end on Close, or
// when ctx is done.
func (v *VoiceConn) Start(ctx context.Context) {
	v.wg.Add(2)
	go v.recvLoop()
	go v.keepaliveLoop(ctx)
	v.watcher.Add(1)
	go func() {
		defer v.watcher.Done()
		select {
		case <-ctx.Done():
			v.Close()
		case <-v.done:
		}
	}()
}

func (v *VoiceConn) recvLoop() {
	defer v.wg.Done()
	buf := make([]byte, voice.MaxDatagram*2)
	oob := make([]byte, OOBSize)
	for {
		n, st, err := ReadStamped(v.conn, buf, oob)
		now := time.Now()
		if err != nil {
			if v.closed.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			// e.g. ICMP-induced ECONNREFUSED on a connected socket; back off
			// so a persistent error cannot spin a core.
			v.RecvErrors.Add(1)
			select {
			case <-v.done:
				return
			case <-time.After(recvErrBackoff):
			}
			continue
		}
		if st.DropsSeen {
			v.Drops.Store(st.Drops)
		}
		p, perr := voice.Parse(buf[:n])
		if perr != nil {
			continue
		}
		switch p.Type {
		case voice.PacketTypeVoice:
			v.mu.Lock()
			l := v.ledgers[ledgerKey{p.SenderID, p.Frequency}]
			if l != nil {
				l.Mark(p.Sequence)
			}
			v.mu.Unlock()
			if st.Stamped {
				lag := now.UnixNano() - st.KernelNs
				if lag < 0 {
					lag = 0
				}
				v.RxLag.Record(time.Duration(lag))
			} else {
				v.Unstamped.Add(1)
			}
		case voice.PacketTypeKeepalive:
			if s := v.sentAt.Swap(0); s != 0 {
				v.RTT.Record(time.Duration(now.UnixNano() - s))
			}
			if ts := voice.KeepaliveTimestamp(p.Payload); ts != 0 {
				v.lastEcho.Store(ts)
			}
		}
	}
}

func (v *VoiceConn) keepaliveLoop(ctx context.Context) {
	defer v.wg.Done()
	every := v.KeepaliveEvery
	if every <= 0 {
		every = 5 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-v.done:
			return
		case <-t.C:
			if v.closed.Load() {
				return
			}
			if s := v.sentAt.Load(); s != 0 {
				if time.Since(time.Unix(0, s)) < every {
					continue // one outstanding at a time
				}
				// Probe or ack lost: count it and move on, or the bot would
				// go silent and be reaped by the server.
				if v.sentAt.CompareAndSwap(s, 0) {
					v.KeepaliveLost.Add(1)
				}
			}
			pkt := voice.NewKeepalive(v.ID, v.lastEcho.Load()).AppendTo(nil)
			v.sentAt.Store(time.Now().UnixNano())
			if _, err := v.conn.Write(pkt); err != nil {
				v.sentAt.Store(0)
			}
		}
	}
}

// Send transmits one VOICE frame with PTT set.
func (v *VoiceConn) Send(seq uint32, freq voice.KHz, payload []byte) error {
	v.sendMu.Lock()
	v.sendBuf = voice.NewVoice(v.ID, seq, freq, payload, true, false).AppendTo(v.sendBuf[:0])
	_, err := v.conn.Write(v.sendBuf)
	v.sendMu.Unlock()
	return err
}

// Bye sends a best-effort BYE.
func (v *VoiceConn) Bye() { _, _ = v.conn.Write(voice.NewBye(v.ID).AppendTo(nil)) }

// Close stops the goroutines and closes the socket. Safe to call repeatedly.
func (v *VoiceConn) Close() {
	if v.closed.Swap(true) {
		return
	}
	close(v.done)
	_ = v.conn.Close()
	v.wg.Wait()
}
