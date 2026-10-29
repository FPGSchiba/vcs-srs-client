package lgtest

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
)

// ResetProxy is a TCP proxy that can abort one proxied connection with a
// RST, the failure the load run reported as "connection reset by peer".
type ResetProxy struct {
	ln net.Listener

	mu     sync.Mutex
	conns  []net.Conn // downstream side, in accept order
	remote []string   // downstream remote addresses, in accept order
	closed bool
	wg     sync.WaitGroup
}

// TCPResetProxy starts a TCP proxy on 127.0.0.1:0 in front of upstream.
//
// Connections are numbered 0, 1, 2... in ACCEPT order, which is not the bot
// id: bots dial concurrently. Tests must assert counts, not specific bot ids.
// The proxy closes on t.Cleanup.
func TCPResetProxy(t testing.TB, upstream string) *ResetProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("lgtest: listen: %v", err)
	}
	p := &ResetProxy{ln: ln}
	t.Cleanup(p.close)

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			down, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", upstream)
			if err != nil {
				_ = down.Close()
				continue
			}
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				_ = down.Close()
				_ = up.Close()
				return
			}
			p.conns = append(p.conns, down)
			p.remote = append(p.remote, down.RemoteAddr().String())
			p.mu.Unlock()

			p.wg.Add(2)
			go func() {
				defer p.wg.Done()
				_, _ = io.Copy(up, down)
				_ = up.Close()
			}()
			go func() {
				defer p.wg.Done()
				_, _ = io.Copy(down, up)
				_ = down.Close()
			}()
		}
	}()
	return p
}

// Addr is the proxy's listen address.
func (p *ResetProxy) Addr() string { return p.ln.Addr().String() }

// Count is the number of connections accepted so far.
func (p *ResetProxy) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

// RemoteAddrs returns the downstream remote address of every accepted
// connection, in accept order.
func (p *ResetProxy) RemoteAddrs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.remote...)
}

// Reset aborts connection i: it sets SO_LINGER 0 on the downstream side and
// closes it, so the client reads "connection reset by peer".
func (p *ResetProxy) Reset(i int) error {
	p.mu.Lock()
	if i < 0 || i >= len(p.conns) {
		n := len(p.conns)
		p.mu.Unlock()
		return fmt.Errorf("lgtest: no connection %d (have %d)", i, n)
	}
	c := p.conns[i]
	p.mu.Unlock()
	if tc, ok := c.(*net.TCPConn); ok {
		if err := tc.SetLinger(0); err != nil {
			return err
		}
	}
	return c.Close()
}

func (p *ResetProxy) close() {
	p.mu.Lock()
	p.closed = true
	_ = p.ln.Close()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
}
