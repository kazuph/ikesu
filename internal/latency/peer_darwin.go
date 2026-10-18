package latency

import (
	"golang.org/x/sys/unix"
	"net"
)

func PeerPID(conn net.Conn) int {
	c, ok := conn.(*net.UnixConn)
	if !ok {
		return 0
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return 0
	}
	pid := 0
	_ = raw.Control(func(fd uintptr) {
		if value, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID); err == nil {
			pid = value
		}
	})
	return pid
}
