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
	n, st, err := ReadStamped(srv, buf, oob)
	kernelNs := st.KernelNs
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().UnixNano()
	if n != 4 {
		t.Fatalf("n=%d", n)
	}
	if !st.Stamped {
		t.Fatal("not stamped")
	}
	if kernelNs < before || kernelNs > after {
		t.Fatalf("kernelNs %d outside [%d,%d]", kernelNs, before, after)
	}
	if st.Truncated || st.ParseErr {
		t.Fatalf("unexpected %+v", st)
	}
	// The kernel omits the SO_RXQ_OVFL cmsg while its counter is zero, so
	// DropsSeen is not asserted here.
	if st.Drops != 0 {
		t.Fatalf("drops=%d", st.Drops)
	}
}

// A control buffer too small for the cmsgs must be reported, not swallowed.
func TestReadStampedTruncatedControl(t *testing.T) {
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
	if _, err := cli.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_, st, err := ReadStamped(srv, make([]byte, 64), make([]byte, 8))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Truncated {
		t.Fatalf("MSG_CTRUNC not reported: %+v", st)
	}
	if st.DropsSeen {
		t.Fatalf("drops cannot be seen with a truncated control buffer: %+v", st)
	}
}
