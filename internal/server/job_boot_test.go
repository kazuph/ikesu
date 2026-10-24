package server

import (
	"github.com/kazuph/ikesu/internal/api"
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

func TestProcessGroupGoneAfterRealChildExit(t *testing.T) {
	child := exec.Command("/bin/cat")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	if !processGroupAlive(pid) {
		in.Close()
		child.Wait()
		t.Fatal("real child group missing")
	}
	in.Close()
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if processGroupAlive(pid) {
		t.Fatal("reaped process group still live")
	}
	if gone, err := monitorProcessGone(pid, "real-start"); err != nil || !gone {
		t.Fatal("reaped monitor not recognized", gone, err)
	}
}

func TestSavedDifferentBootCancellationDoesNotTouchForeignGroup(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "old-boot-cancel")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	defer s.stopJobMonitors()
	child := exec.Command("/bin/cat")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { in.Close(); child.Wait() }()
	pid := child.Process.Pid
	identity, err := jobProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "old.log")
	if err = os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.InsertCommand("old-boot", "old-boot", "cat", "/", "", "", "none", log); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkCommandRunning("old-boot", int64(pid)); err != nil {
		t.Fatal(err)
	}
	job, err := store.CommandRow("old-boot")
	if err != nil {
		t.Fatal(err)
	}
	m := jobRunnerRecord{JobID: job.ID, PID: pid, Identity: identity, MonitorPID: pid, MonitorIdentity: identity, Token: "saved-identity", StartedMs: *job.StartedMs, BootID: "saved-previous-boot"}
	if err = writeJobRecord(log+".runner", m); err != nil {
		t.Fatal(err)
	}
	if err = writeJobRecord(log+".cancel", jobCancelState{TermSentMs: time.Now().UnixMilli(), VerifiedPGID: pid, VerifiedIdentity: identity}); err != nil {
		t.Fatal(err)
	}
	response := s.runCancel("cancel", api.RunCancelParams{JobID: job.ID})
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	waitPromptCondition(t, func() bool { r, _ := store.CommandRow(job.ID); return r != nil && r.Status == "cancelled" })
	state, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
		t.Fatal("foreign group received cancellation", string(state), err)
	}
}
