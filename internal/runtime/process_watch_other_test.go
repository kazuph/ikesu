//go:build !darwin

package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestPollingHandoffKeepsQuietCwdAndExec(t *testing.T) {
	teststate.Lock(t)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "quiet.py")
	body := "import os,signal\nsignal.signal(signal.SIGUSR1,lambda *_:os.chdir('/'))\nsignal.signal(signal.SIGUSR2,lambda *_:os.execl('/bin/sleep','sleep','60'))\nprint('POLL_HANDOFF_READY',flush=True)\nwhile True:signal.pause()\n"
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	s, next := NewSupervisor(), NewSupervisor()
	defer s.KillAll()
	defer next.KillAll()
	p, err := s.SpawnWith(1, dir, 80, 24, nil, []string{python, file})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for !strings.Contains(p.ScreenText(), "POLL_HANDOFF_READY") {
		if time.Now().After(until) {
			t.Fatal("quiet PTY did not start")
		}
		time.Sleep(time.Millisecond)
	}
	p.PollProcess()
	states, files, err := s.PauseHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if err = next.ImportHandoff(states, files); err != nil {
		t.Fatal(err)
	}
	s.ReleaseHandoff()
	next.StartImported()
	q := next.Get(1)
	next.PollPendingProcesses(time.Now())
	next.processMu.Lock()
	scheduled := next.processTicks[q]
	next.processMu.Unlock()
	if !scheduled {
		t.Fatal("imported quiet pane was not registered for non-native process observation")
	}
	wait := func(check func() bool) {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for {
			next.PollPendingProcesses(time.Now())
			if check() {
				return
			}
			if time.Now().After(until) {
				t.Fatal("quiet imported process change was missed", q.Snapshot())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err = syscall.Kill(q.Snapshot().FgPID, syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return q.Snapshot().Cwd == "/" })
	if err = syscall.Kill(q.Snapshot().FgPID, syscall.SIGUSR2); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return q.Snapshot().ProcName == "sleep" })
	next.Kill(1)
	next.PollPendingProcesses(time.Now())
	next.processMu.Lock()
	defer next.processMu.Unlock()
	if next.processTicks[q] || next.processWork[q] || !next.processDue[q].IsZero() {
		t.Fatal("closed imported pane retained process schedule")
	}
}
