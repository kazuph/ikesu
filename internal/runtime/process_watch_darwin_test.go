//go:build darwin

package runtime

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
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
		return until.IsZero()
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

func TestQuietForegroundUsesEventsAndObservesSilentCwdAndChildExec(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "quiet.py")
	if err := os.WriteFile(script, []byte("import os,signal,time\nprint('QUIET_FOREGROUND_READY',flush=True)\nsignal.signal(signal.SIGUSR1,lambda *_:os.chdir('/'))\ndef child(*_):\n if os.fork()==0:os.execl('/bin/sleep','sleep','60')\nsignal.signal(signal.SIGUSR2,child)\nwhile True:signal.pause()\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.KillAll()
	p, err := s.Spawn(1, dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.WriteToPTY([]byte(fmt.Sprintf("'%s' '%s'\n", python, script))); err != nil {
		t.Fatal(err)
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
				t.Fatal("event-driven foreground did not converge", p.Snapshot())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait(func() bool {
		return strings.Contains(p.ScreenText(), "\nQUIET_FOREGROUND_READY\n") && p.Snapshot().FgPID != p.ShellPID()
	})
	wait(func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.processSettlingUntil.IsZero()
	})
	at := p.processCache.readAt
	group := p.Snapshot().FgPID
	deadline := time.Now().Add(foregroundJobCacheLifetime + 50*time.Millisecond)
	for time.Now().Before(deadline) {
		p.PollProcessChanged()
		time.Sleep(10 * time.Millisecond)
	}
	if !p.processCache.readAt.Equal(at) {
		t.Fatal("quiet foreground repeated the full group scan")
	}
	if err := unix.Kill(group, unix.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return p.Snapshot().Cwd == "/" })
	if err := unix.Kill(group, unix.SIGUSR2); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		for _, pr := range p.processCache.job.Processes {
			if pr.Name == "sleep" {
				return true
			}
		}
		return false
	})
}
