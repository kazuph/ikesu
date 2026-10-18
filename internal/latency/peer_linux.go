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
		if value, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED); err == nil {
			pid = int(value.Pid)
		}
	})
	return pid
}
