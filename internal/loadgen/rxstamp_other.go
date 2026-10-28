//go:build !linux

package loadgen

import "net"

// OOBSize is kept for API parity with Linux.
const OOBSize = 64

// EnableRxStamps is a no-op off Linux.
func EnableRxStamps(c *net.UDPConn) error { return nil }

// ReadStamped reads a plain datagram; the Stamp is always empty off Linux.
func ReadStamped(c *net.UDPConn, buf, oob []byte) (int, Stamp, error) {
	n, err := c.Read(buf)
	return n, Stamp{}, err
}
