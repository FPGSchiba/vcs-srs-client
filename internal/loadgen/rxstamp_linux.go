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
// The Stamp says which of them were actually present, so a caller can tell
// "no stamp" from "stamp lost" and never mistake a missing drop cmsg for 0.
func ReadStamped(c *net.UDPConn, buf, oob []byte) (int, Stamp, error) {
	n, oobn, flags, _, err := c.ReadMsgUDP(buf, oob)
	if err != nil {
		return n, Stamp{}, err
	}
	return n, decodeStamp(oob[:oobn], flags), nil
}

// decodeStamp parses the control buffer. The numeric option constants above
// hold on amd64/arm64/arm/386 only (mips, sparc, alpha, parisc differ), which
// covers every target this tool ships for.
func decodeStamp(oob []byte, flags int) Stamp {
	st := Stamp{Truncated: flags&syscall.MSG_CTRUNC != 0}
	msgs, perr := syscall.ParseSocketControlMessage(oob)
	if perr != nil {
		st.ParseErr = true
		return st
	}
	for _, m := range msgs {
		if m.Header.Level != syscall.SOL_SOCKET {
			continue
		}
		switch m.Header.Type {
		case soTimestampNS: // SCM_TIMESTAMPNS == SO_TIMESTAMPNS
			// Timespec is two native words; decode without an unaligned cast.
			w := int(unsafe.Sizeof(syscall.Timespec{}.Sec))
			if len(m.Data) < 2*w {
				continue
			}
			var sec, nsec int64
			switch w {
			case 8:
				sec = int64(binary.NativeEndian.Uint64(m.Data[:8]))
				nsec = int64(binary.NativeEndian.Uint64(m.Data[8:16]))
			case 4:
				sec = int64(int32(binary.NativeEndian.Uint32(m.Data[:4])))
				nsec = int64(int32(binary.NativeEndian.Uint32(m.Data[4:8])))
			default:
				continue
			}
			st.KernelNs = sec*1e9 + nsec
			st.Stamped = true
		case soRxqOvfl:
			if len(m.Data) >= 4 {
				st.Drops = binary.NativeEndian.Uint32(m.Data[:4])
				st.DropsSeen = true
			}
		}
	}
	return st
}
