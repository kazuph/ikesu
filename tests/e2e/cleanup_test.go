package e2e

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"golang.org/x/sys/unix"
)

// The stop response precedes teardown. Wait for the session lock to be
// released before testing.TempDir removes the shell's history directory.
func (s *session) stopServer() {
	dir := filepath.Join(s.stateDir, s.name)
	if c, err := net.Dial("unix", api.APISocketPath(dir)); err == nil {
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		_, _ = c.Write([]byte(`{"id":"e2e","method":"server.stop","params":{}}` + "\n"))
		_, _ = io.Copy(io.Discard, c)
		c.Close()
	}
	lock, err := os.OpenFile(api.LockPath(dir), os.O_RDWR, 0600)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		s.t.Errorf("open session cleanup lock: %v", err)
		return
	}
	defer lock.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
			return
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN || time.Now().After(deadline) {
			s.t.Errorf("test server did not finish teardown: %v", err)
			return
		}
		time.Sleep(time.Millisecond)
	}
}
