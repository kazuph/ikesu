package server

import (
	"golang.org/x/sys/unix"
	"net"
	"os"
)

func sameUserPeer(conn net.Conn) bool {
	c, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return false
	}
	allowed := false
	if raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		allowed = err == nil && cred.Uid == uint32(os.Geteuid())
	}) != nil {
		return false
	}
	return allowed
}
