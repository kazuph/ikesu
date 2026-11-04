package latency

import (
	"golang.org/x/sys/unix"
	"net"
	"unsafe"
)

type PeerIdentity struct {
	PID     int
	Version uint32
}

// PeerIdentityOf obtains the kernel's last socket operator and its audit-token
// generation; inconsistent or unavailable values cannot identify a peer.
func PeerIdentityOf(conn net.Conn) PeerIdentity {
	c, ok := conn.(*net.UnixConn)
	if !ok {
		return PeerIdentity{}
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return PeerIdentity{}
	}
	var result PeerIdentity
	_ = raw.Control(func(fd uintptr) {
		pid, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		if err != nil || pid <= 1 {
			return
		}
		var token [8]uint32
		size := uint32(unsafe.Sizeof(token))
		_, _, errno := unix.Syscall6(unix.SYS_GETSOCKOPT, fd, unix.SOL_LOCAL, unix.LOCAL_PEERTOKEN, uintptr(unsafe.Pointer(&token[0])), uintptr(unsafe.Pointer(&size)), 0)
		if errno != 0 || size != uint32(unsafe.Sizeof(token)) || int(token[5]) != pid || token[7] == 0 {
			return
		}
		result = PeerIdentity{PID: pid, Version: token[7]}
	})
	return result
}

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
