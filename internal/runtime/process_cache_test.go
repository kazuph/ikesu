package runtime

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestG9ForegroundCacheReusesScanAndInvalidatesCwdAndProgram(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(dir, "next")
	if err := os.Mkdir(next, 0700); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.KillAll()
	p, err := s.Spawn(1, dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	wait := func(condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatal("process probe did not converge", p.processCache.key)
			}
			time.Sleep(time.Millisecond)
		}
	}
	wait(func() bool { p.PollProcess(); return p.Snapshot().Cwd == dir && len(p.processCache.job.Processes) > 0 })
	readAt := p.processCache.readAt
	keyBefore := p.processCache.key
	for i := 0; i < 10; i++ {
		_ = p.Snapshot()
		_ = p.SpawnCwd()
	}
	if !p.processCache.readAt.Equal(readAt) {
		t.Fatal("cached session pane reads repeated a foreground job scan")
	}
	p.PollProcess()
	if p.processCache.key == keyBefore && !p.processCache.readAt.Equal(readAt) {
		t.Fatal("unchanged process group scan not reused")
	}
	if p.processCache.key.shellPID != p.ShellPID() || p.processCache.key.shellCwd != dir || p.processCache.key.leaderCwd != dir {
		t.Fatal(p.processCache.key)
	}
	if _, err := p.WriteToPTY([]byte("cd '" + next + "'\n")); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		p.PollProcess()
		return p.Snapshot().Cwd == next && p.processCache.key.shellCwd == next && p.processCache.key.leaderCwd == next
	})
	if p.processCache.readAt.Equal(readAt) {
		t.Fatal("cwd change reused old scan")
	}
	readAt = p.processCache.readAt
	if _, err := p.WriteToPTY([]byte("/bin/sleep 60\n")); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		p.PollProcess()
		return p.processCache.key.groupID != p.ShellPID() && p.processCache.key.leaderName == "sleep"
	})
	if p.processCache.readAt.Equal(readAt) {
		t.Fatal("new foreground process group reused old scan")
	}
	group := p.processCache.key.groupID
	pgid, groupErr := unix.Getpgid(group)
	if err := unix.Kill(-group, unix.SIGTERM); err != nil {
		t.Fatal("foreground group termination failed", err, "key", p.processCache.key, "job", p.processCache.job, "leader pgid", pgid, groupErr, "screen", p.ScreenText())
	}
	wait(func() bool { p.PollProcess(); return p.processCache.key.groupID == p.ShellPID() })
	readAt = p.processCache.readAt
	if _, err := p.WriteToPTY([]byte("exec /bin/sleep 60\n")); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { p.PollProcess(); return p.processCache.key.leaderName == "sleep" })
	if p.processCache.readAt.Equal(readAt) {
		t.Fatal("exec reused old foreground process")
	}
	readAt = p.processCache.readAt
	time.Sleep(foregroundJobCacheLifetime)
	p.PollProcess()
	if p.processCache.readAt.Equal(readAt) {
		t.Fatal("scan retained after 1.5 seconds")
	}
}
