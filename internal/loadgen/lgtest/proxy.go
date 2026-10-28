// Package lgtest holds test support for the load generator. It is an ordinary
// package, not a _test.go file, so tests in cmd/vcs-loadbot can import it too.
// It has no tests of its own and must never be imported by non-test code.
package lgtest

import (
	"net"
	"sync"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
)

// HelloDropProxy starts a UDP proxy on 127.0.0.1:0 in front of upstream and
// returns its address.
//
// Every new downstream source address is numbered 0, 1, 2... in order of its
// first packet and gets its own connected upstream socket, so the server
// binds a distinct address per bot. Both directions are forwarded. Every
// HELLO from a source whose index satisfies drop is discarded; everything
// else, including later keepalives and voice, passes through untouched.
//
// The proxy closes on t.Cleanup.
func HelloDropProxy(t testing.TB, upstream string, drop func(srcIndex int) bool) (addr string) {
	t.Helper()
	ua, err := net.ResolveUDPAddr("udp", upstream)
	if err != nil {
		t.Fatalf("lgtest: resolve %q: %v", upstream, err)
	}
	ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("lgtest: listen: %v", err)
	}
	_ = ln.SetReadBuffer(1 << 20)

	type peer struct {
		idx  int
		conn *net.UDPConn
	}
	var (
		mu     sync.Mutex
		peers  = map[string]*peer{}
		closed bool
		wg     sync.WaitGroup
	)

	t.Cleanup(func() {
		mu.Lock()
		closed = true
		_ = ln.Close()
		for _, p := range peers {
			_ = p.conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	})

	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 64*1024)
		for {
			n, src, err := ln.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			key := src.String()
			mu.Lock()
			if closed {
				mu.Unlock()
				return
			}
			p := peers[key]
			if p == nil {
				c, derr := net.DialUDP("udp", nil, ua)
				if derr != nil {
					mu.Unlock()
					continue
				}
				p = &peer{idx: len(peers), conn: c}
				peers[key] = p
				wg.Add(1)
				go func() {
					defer wg.Done()
					rb := make([]byte, 64*1024)
					for {
						m, rerr := c.Read(rb)
						if rerr != nil {
							return
						}
						_, _ = ln.WriteToUDPAddrPort(rb[:m], src)
					}
				}()
			}
			mu.Unlock()

			if pk, perr := voice.Parse(buf[:n]); perr == nil && pk.Type == voice.PacketTypeHello && drop != nil && drop(p.idx) {
				continue
			}
			_, _ = p.conn.Write(buf[:n])
		}
	}()
	return ln.LocalAddr().String()
}
