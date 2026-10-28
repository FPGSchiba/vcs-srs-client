//go:build linux

package loadgen

import (
	"encoding/binary"
	"net"
	"syscall"
	"unsafe"
)

// OOBSize is enough for one SCM_TIMESTAMPNS and one SO_RXQ_OVFL cmsg.
const OOBSize = 64

const (
	soTimestampNS = 0x23 // SO_TIMESTAMPNS
	soRxqOvfl     = 0x28 // SO_RXQ_OVFL
)

// EnableRxStamps asks the kernel for receive timestamps and the cumulative
// socket drop counter on c.
func EnableRxStamps(c *net.UDPConn) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) {
		if serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soTimestampNS, 1); serr != nil {
			return
		}
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soRxqOvfl, 1)
	}); err != nil {
		return err
	}
	return serr
}

// ReadStamped reads one datagram and decodes the kernel receive time (Unix ns,
// CLOCK_REALTIME) and the cumulative drop count from the control messages.
func ReadStamped(c *net.UDPConn, buf, oob []byte) (n int, kernelNs int64, drops uint32, stamped bool, err error) {
	n, oobn, _, _, err := c.ReadMsgUDP(buf, oob)
	if err != nil {
		return n, 0, 0, false, err
	}
	msgs, perr := syscall.ParseSocketControlMessage(oob[:oobn])
	if perr != nil {
		return n, 0, 0, false, nil
	}
	for _, m := range msgs {
		if m.Header.Level != syscall.SOL_SOCKET {
			continue
		}
		switch m.Header.Type {
		case soTimestampNS: // SCM_TIMESTAMPNS == SO_TIMESTAMPNS
			if len(m.Data) >= int(unsafe.Sizeof(syscall.Timespec{})) {
				ts := *(*syscall.Timespec)(unsafe.Pointer(&m.Data[0]))
				kernelNs = int64(ts.Sec)*1e9 + int64(ts.Nsec)
				stamped = true
			}
		case soRxqOvfl:
			if len(m.Data) >= 4 {
				drops = binary.NativeEndian.Uint32(m.Data[:4])
			}
		}
	}
	return n, kernelNs, drops, stamped, nil
}
