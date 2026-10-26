//go:build darwin

package runtime

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessOutputDoesNotProlongSettling(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "output.py")
	if err = os.WriteFile(script, []byte("import time\nprint('OUTPUT_SCHEDULE_READY',flush=True)\nwhile True:\n print('OUTPUT_SCHEDULE_TICK',flush=True)\n time.sleep(.1)\n"), 0600); err != nil {
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
	ready := time.Now().Add(5 * time.Second)
	for !strings.Contains(p.ScreenText(), "\nOUTPUT_SCHEDULE_READY\n") || p.Snapshot().FgPID == p.ShellPID() {
		s.PollPendingProcesses(time.Now())
		if time.Now().After(ready) {
			t.Fatal("real output fixture did not start", p.Snapshot())
		}
		time.Sleep(10 * time.Millisecond)
	}
	until := time.Now().Add(2 * foregroundJobCacheLifetime)
	for time.Now().Before(until) {
		s.PollPendingProcesses(time.Now())
		time.Sleep(10 * time.Millisecond)
	}
	p.mu.Lock()
	settling := p.processSettlingUntil
	p.mu.Unlock()
	if !settling.IsZero() {
		t.Fatal("PTY output prolonged process settling", settling)
	}
	p.processCache.mu.Lock()
	readAt := p.processCache.readAt
	p.processCache.mu.Unlock()
	// The approved policy retains light per-tick work, not full PID/argv scans.
	until = time.Now().Add(2 * foregroundJobCacheLifetime)
	for time.Now().Before(until) {
		s.PollPendingProcesses(time.Now())
		time.Sleep(10 * time.Millisecond)
	}
	p.processCache.mu.Lock()
	after := p.processCache.readAt
	p.processCache.mu.Unlock()
	if !after.Equal(readAt) {
		t.Fatal("unchanged output repeated full PID/argv scan", readAt, after)
	}
	if strings.Count(p.ScreenText(), "OUTPUT_SCHEDULE_TICK") < 2 {
		t.Fatal("output fixture stopped while checking quiescence")
	}
}

func TestProcessOutputKeepsDelayedSilentForegroundSwitch(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "switch.py")
	childFile := filepath.Join(dir, "child")
	switched := filepath.Join(dir, "switched")
	body := `import os,signal,time,sys
child=os.fork()
if child==0:
 os.setpgid(0,0)
 os.chdir('/')
 while True:signal.pause()
os.setpgid(child,child)
open(sys.argv[1],'w').write(str(child))
def switch(*_):
 os.write(1,b'FOREGROUND_SWITCH_TRIGGER\r\n')
 time.sleep(.1)
 os.tcsetpgrp(0,child)
 open(sys.argv[2],'w').write(str(time.time_ns()))
signal.signal(signal.SIGUSR1,switch)
print('FOREGROUND_SWITCH_READY',flush=True)
while True:signal.pause()
`
	if err = os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor()
	defer s.KillAll()
	p, err := s.Spawn(1, dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.WriteToPTY([]byte(fmt.Sprintf("'%s' '%s' '%s' '%s'\n", python, script, childFile, switched))); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for {
		s.PollPendingProcesses(time.Now())
		p.mu.Lock()
		settling := p.processSettlingUntil
		p.mu.Unlock()
		if strings.Contains(p.ScreenText(), "\nFOREGROUND_SWITCH_READY\n") && p.Snapshot().FgPID != p.ShellPID() && settling.IsZero() {
			break
		}
		if time.Now().After(until) {
			t.Fatal("silent foreground fixture did not settle", p.Snapshot())
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, err := os.ReadFile(childFile)
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(string(raw))
	if err != nil || child <= 0 {
		t.Fatal("invalid fixture child", string(raw), err)
	}
	defer syscall.Kill(child, syscall.SIGTERM)
	if err = syscall.Kill(p.Snapshot().FgPID, syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	until = time.Now().Add(5 * time.Second)
	for p.Snapshot().FgPID != child {
		s.PollPendingProcesses(time.Now())
		if time.Now().After(until) {
			t.Fatal("silent foreground switch was missed", p.Snapshot())
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, err = os.ReadFile(switched)
	if err != nil {
		t.Fatal(err)
	}
	at, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	latency := time.Since(time.Unix(0, at))
	t.Logf("output followed by silent tcsetpgrp observed after %s", latency)
	// The UI's existing tick is 50ms; allow one extra tick of shared-host jitter.
	if latency > 2*50*time.Millisecond {
		t.Fatal("output-only policy delayed the existing foreground observation", latency)
	}
}
