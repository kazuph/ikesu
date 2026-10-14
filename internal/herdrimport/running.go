package herdrimport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// Running never creates a lock or sends a request to herdr. Both session
// directory and default-session sockets are checked, using existing files.
func Running(dir string) (bool, error) {
	for _, sub := range []string{"", "default"} {
		for _, name := range []string{"herdr.sock", "herdr-client.sock"} {
			path := filepath.Join(dir, sub, name)
			if _, err := os.Lstat(path); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return false, err
			}
			conn, err := net.Dial("unix", path)
			if err == nil {
				conn.Close()
				return true, nil
			}
			if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, syscall.ENOENT) {
				return false, fmt.Errorf("cannot determine herdr liveness: %w", err)
			}
		}
		for _, name := range []string{"herdr.lock", "server.lock"} {
			f, err := os.Open(filepath.Join(dir, sub, name))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return false, err
			}
			err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
			if err == nil {
				unix.Flock(int(f.Fd()), unix.LOCK_UN)
			}
			f.Close()
			if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
		}
	}
	return false, nil
}
