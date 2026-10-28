//go:build linux

package loadgen

import (
	"net"
	"testing"
	"time"
)

func TestReadStampedLoopback(t *testing.T) {
	srv, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cli, err := net.DialUDP("udp", nil, srv.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	if err := EnableRxStamps(srv); err != nil {
		t.Fatal(err)
	}
	_ = srv.SetReadDeadline(time.Now().Add(2 * time.Second))

	before := time.Now().UnixNano()
	if _, err := cli.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	oob := make([]byte, OOBSize)
	n, kernelNs, drops, stamped, err := ReadStamped(srv, buf, oob)
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().UnixNano()
	if n != 4 {
		t.Fatalf("n=%d", n)
	}
	if !stamped {
		t.Fatal("not stamped")
	}
	if kernelNs < before || kernelNs > after {
		t.Fatalf("kernelNs %d outside [%d,%d]", kernelNs, before, after)
	}
	if drops != 0 {
		t.Fatalf("drops=%d", drops)
	}
}
