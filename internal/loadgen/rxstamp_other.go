//go:build !linux

package loadgen

import "net"

// OOBSize is kept for API parity with Linux.
const OOBSize = 64

// EnableRxStamps is a no-op off Linux.
func EnableRxStamps(c *net.UDPConn) error { return nil }

// ReadStamped reads a plain datagram; stamped is always false off Linux.
func ReadStamped(c *net.UDPConn, buf, oob []byte) (n int, kernelNs int64, drops uint32, stamped bool, err error) {
	n, err = c.Read(buf)
	return n, 0, 0, false, err
}
