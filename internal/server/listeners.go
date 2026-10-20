package server

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"golang.org/x/sys/unix"
	"net"
	"os"
)

func openSessionSockets(session, dir string) (*os.File, net.Listener, net.Listener, error) {
	// One server per session: flock the lock file for the server's lifetime.
	lockPath := api.LockPath(dir)
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open lock: %w", err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, nil, nil, fmt.Errorf("a gtx server is already running for session %q", session)
	}

	apiSock := api.APISocketPath(dir)
	clientSock := api.ClientSocketPath(dir)
	// Stale sockets from a crashed server are safe to replace while we hold
	// the lock.
	os.Remove(apiSock)
	os.Remove(clientSock)
	apiLn, err := net.Listen("unix", apiSock)
	if err != nil {
		lock.Close()
		return nil, nil, nil, fmt.Errorf("listen %s: %w", apiSock, err)
	}
	clientLn, err := net.Listen("unix", clientSock)
	if err != nil {
		apiLn.Close()
		os.Remove(apiSock)
		lock.Close()
		return nil, nil, nil, fmt.Errorf("listen %s: %w", clientSock, err)
	}
	os.Chmod(apiSock, 0o600)
	os.Chmod(clientSock, 0o600)

	return lock, apiLn, clientLn, nil
}
