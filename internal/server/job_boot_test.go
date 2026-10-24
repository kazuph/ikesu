package server

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestSavedDifferentBootNeverSignalsLiveProcess(t *testing.T) {
	// A saved record from another boot is input data; all PID/group probes and
	// signal attempts below operate on a real locally running process.
	child := exec.Command("/bin/cat")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { in.Close(); child.Wait() }()
	pid := child.Process.Pid
	identity, err := jobProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	m := jobRunnerRecord{PID: pid, Identity: identity, BootID: "saved-previous-boot"}
	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		if err := signalManagedJob(m, signal); err == nil {
			t.Fatal("managed signal crossed boot")
		}
		if err := signalCancellationGroup(m, jobCancelState{VerifiedPGID: pid, VerifiedIdentity: identity}, signal); err == nil {
			t.Fatal("resumed cancellation crossed boot")
		}
	}
	state, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
		t.Fatal("unrelated process was stopped", string(state), err)
	}
	if alive, err := managedJobGroupAlive(m); err != nil || alive {
		t.Fatal(alive, err)
	}
}
