package server

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestCancelZombieLeaderWithStoppedParentKillsSurvivingGrandchild(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "zombie-cancel")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	defer s.stopJobMonitors()
	script := filepath.Join(t.TempDir(), "stopped-parent.py")
	code := `import os, signal, json
r,w=os.pipe()
pid=os.fork()
if pid==0:
    os.close(r)
    os.setpgid(0,0)
    signal.signal(signal.SIGTERM,signal.SIG_DFL)
    grandchild=os.fork()
    if grandchild==0:
        signal.signal(signal.SIGTERM,signal.SIG_IGN)
        os.write(w,str(os.getpid()).encode())
        os.close(w)
        os.read(0,1)
        os._exit(0)
    os.close(w)
    while True: signal.pause()
os.close(w)
grandchild=int(os.read(r,100))
os.close(r)
print(json.dumps(dict(Leader=pid,Grandchild=grandchild)),flush=True)
os.kill(os.getpid(),signal.SIGSTOP)
os.waitpid(pid,0)
`
	if err = os.WriteFile(script, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	monitor := exec.Command("python3", script)
	monitor.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	input, err := monitor.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := monitor.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = monitor.Start(); err != nil {
		t.Fatal(err)
	}
	var processes struct{ Leader, Grandchild int }
	if err = json.NewDecoder(bufio.NewReader(output)).Decode(&processes); err != nil {
		t.Fatal(err)
	}
	signalOwned := func(pid int, sig syscall.Signal) {
		t.Helper()
		out, e := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", script).Output()
		if e != nil || !strings.Contains("\n"+string(out), "\n"+strconv.Itoa(pid)+"\n") {
			t.Fatal("process ownership unavailable", pid, string(out), e)
		}
		if e = syscall.Kill(pid, sig); e != nil {
			t.Fatal(e)
		}
	}
	resumed := false
	defer func() {
		if !resumed {
			signalOwned(monitor.Process.Pid, syscall.SIGCONT)
		}
		owned, _ := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", script).Output()
		for _, pid := range []int{processes.Leader, processes.Grandchild} {
			if strings.Contains("\n"+string(owned), "\n"+strconv.Itoa(pid)+"\n") {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		input.Close()
		monitor.Wait()
	}()
	state := func(pid int, prefix string) bool {
		b, e := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		return e == nil && strings.HasPrefix(strings.TrimSpace(string(b)), prefix)
	}
	waitPromptCondition(t, func() bool { return state(monitor.Process.Pid, "T") })
	identity, err := jobProcessIdentity(processes.Leader)
	if err != nil {
		t.Fatal(err)
	}
	monitorIdentity, err := jobProcessIdentity(monitor.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "job.log")
	if err = os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.InsertCommand("zombie-cancel", "zombie-cancel", "real process tree", "/", "", "", "none", log); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkCommandRunning("zombie-cancel", int64(processes.Leader)); err != nil {
		t.Fatal(err)
	}
	job, err := store.CommandRow("zombie-cancel")
	if err != nil {
		t.Fatal(err)
	}
	boot, err := jobBootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	record := jobRunnerRecord{JobID: job.ID, PID: processes.Leader, Identity: identity, MonitorPID: monitor.Process.Pid, MonitorIdentity: monitorIdentity, StartedMs: *job.StartedMs, Token: "real-tree", BootID: boot}
	if err = writeJobRecord(log+".runner", record); err != nil {
		t.Fatal(err)
	}
	response := s.runCancel("cancel", api.RunCancelParams{JobID: job.ID})
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	waitPromptCondition(t, func() bool { return state(processes.Leader, "Z") })
	waitPromptCondition(t, func() bool { return !processGroupAlive(processes.Leader) })
	if !state(monitor.Process.Pid, "T") {
		t.Fatal("parent resumed before KILL reached grandchild")
	}
	if row, e := store.CommandRow(job.ID); e != nil || row.Status != "cancelling" {
		t.Fatal("stopped monitor contract changed", row, e)
	}
	signalOwned(monitor.Process.Pid, syscall.SIGCONT)
	resumed = true
	if err = monitor.Wait(); err != nil {
		t.Fatal(err)
	}
	job, err = store.CommandRow(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.recordCancellation(store, *job, true); err != nil {
		t.Fatal(err)
	}
	if row, e := store.CommandRow(job.ID); e != nil || row.Status != "cancelled" || row.ExitCode != nil {
		t.Fatal(row, e)
	}
}
