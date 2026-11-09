package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// peerContext observes hangup without consuming bytes from a persistent API
// connection. The pipe stops the poll before the duplicated descriptor closes.
func peerContext(conn net.Conn) (context.Context, func(), error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return nil, nil, fmt.Errorf("API connection has no socket descriptor")
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	fd := -1
	var dupErr error
	if err := raw.Control(func(original uintptr) { fd, dupErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 0) }); err != nil {
		return nil, nil, err
	}
	if dupErr != nil {
		return nil, nil, dupErr
	}
	read, write, err := os.Pipe()
	if err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		waitPeerHangup(fd, int(read.Fd()))
	}()
	stop := func() {
		_, _ = write.Write([]byte{1})
		<-done
		write.Close()
		read.Close()
		unix.Close(fd)
		cancel()
	}
	return ctx, stop, nil
}
