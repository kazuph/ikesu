//go:build darwin

package runtime

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuietShellUsesProcessEventsAndKeepsCwdForkExecExit(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.KillAll()
	p, err := s.Spawn(1, dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if !processEventQueue() {
		t.Fatal("native process event queue unavailable")
	}
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			p.PollProcessChanged()
			if check() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("process event did not update state", p.Snapshot())
			}
			time.Sleep(time.Millisecond)
		}
	}
	wait(func() bool { return p.Snapshot().Cwd == dir && p.processCache.key.groupID == p.ShellPID() })
	// Let the real shell finish its startup prompt before testing quiescence.
	_, err = p.WriteToPTY([]byte("printf '\\nWATCH_READY_DONE\\n'\n"))
	if err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		return strings.Contains(p.ScreenText(), "\nWATCH_READY_DONE\n") && p.processCache.key.groupID == p.ShellPID()
	})
	// Wait for the existing transition-observation window, keeping the real
	// process observations active until it ends.
	wait(func() bool {
		p.mu.Lock()
		until := p.processSettlingUntil
		p.mu.Unlock()
		return time.Now().After(until)
	})
	// Polling past the old cache lifetime must not rescan a quiet shell.
	p.PollProcess()
	at := p.processCache.readAt
	key := p.processCache.key
	deadline := time.Now().Add(foregroundJobCacheLifetime + 50*time.Millisecond)
	for time.Now().Before(deadline) {
		p.PollProcessChanged()
		time.Sleep(10 * time.Millisecond)
	}
	if !p.processCache.readAt.Equal(at) {
		t.Fatal("quiet shell repeated foreground group scan", key, p.processCache.key, p.Snapshot())
	}
	// A builtin can change cwd after the event-settling window without
	// emitting PTY output or a fork/exec event.
	_, err = p.WriteToPTY([]byte("PS1=''; read -t 2 unused; cd /\n"))
	if err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return p.Snapshot().Cwd == "/" })
	_, err = p.WriteToPTY([]byte("/bin/sleep 60\n"))
	if err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return p.Snapshot().ProcName == "sleep" })
	group := p.processCache.key.groupID
	if err := unix.Kill(-group, unix.SIGTERM); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return p.processCache.key.groupID == p.ShellPID() })
	_, err = p.WriteToPTY([]byte("cd /; exec /bin/sleep 60\n"))
	if err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return p.Snapshot().ProcName == "sleep" && p.Snapshot().Cwd == "/" })
}
