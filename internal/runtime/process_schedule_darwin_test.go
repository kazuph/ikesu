//go:build darwin

package runtime

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPendingProcessSchedulePreservesQuietAndSilentCwd(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "silent_cwd.py")
	if err = os.WriteFile(script, []byte("import os,signal\nsignal.signal(signal.SIGUSR1,lambda *_:os.chdir('/'))\ndef child(*_):\n if os.fork()==0:os.execl('/bin/sleep','sleep','60')\nsignal.signal(signal.SIGUSR2,child)\nprint('PROCESS_SCHEDULE_READY',flush=True)\nwhile True:signal.pause()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.KillAll()
	p, err := s.Spawn(1, dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.WriteToPTY([]byte(fmt.Sprintf("'%s' '%s'\n", python, script))); err != nil {
		t.Fatal(err)
	}
	changedIDs := map[int]bool{}
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			for _, id := range s.PollPendingProcesses(time.Now()) {
				changedIDs[id] = true
			}
			if check() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("shared process schedule did not converge", p.Snapshot())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait(func() bool {
		p.mu.Lock()
		until := p.processSettlingUntil
		outputUntil := p.processOutputUntil
		p.mu.Unlock()
		return strings.Contains(p.ScreenText(), "\nPROCESS_SCHEDULE_READY\n") && p.Snapshot().FgPID != p.ShellPID() && until.IsZero() && outputUntil.IsZero()
	})
	s.processMu.Lock()
	periodic := s.processTicks[p]
	due := s.processDue[p]
	s.processMu.Unlock()
	if periodic || due.IsZero() {
		t.Fatal("quiet foreground did not leave the per-tick work set", periodic, due)
	}
	at := p.processCache.readAt
	if changed := s.PollPendingProcesses(time.Now()); len(changed) != 0 {
		t.Fatal("quiet process reported a change", changed)
	}
	if !p.processCache.readAt.Equal(at) {
		t.Fatal("quiet schedule repeated the full process scan")
	}
	pid := p.Snapshot().FgPID
	delete(changedIDs, p.ID)
	sent := time.Now()
	if err = syscall.Kill(pid, syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return p.Snapshot().Cwd == "/" })
	t.Logf("silent cwd observed after %s with the existing cwd deadline", time.Since(sent))
	if !changedIDs[p.ID] {
		t.Fatal("silent cwd change did not invalidate detection")
	}
	if err = syscall.Kill(pid, syscall.SIGUSR2); err != nil {
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
	if err = syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return p.Snapshot().FgPID == p.ShellPID() })
	if _, err = p.WriteToPTY([]byte("exec /bin/sleep 60\n")); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return p.Snapshot().ProcName == "sleep" })
	s.Kill(p.ID)
	s.PollPendingProcesses(time.Now())
	s.processMu.Lock()
	_, queued := s.processWork[p]
	_, ticking := s.processTicks[p]
	_, scheduled := s.processDue[p]
	s.processMu.Unlock()
	if queued || ticking || scheduled {
		t.Fatal("closed pane retained process work", queued, ticking, scheduled)
	}
}
