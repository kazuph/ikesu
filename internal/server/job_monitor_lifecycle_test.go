package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRetiredServerRejectsCancelMonitor(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "retired-monitor")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	s.closing.Store(2)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	s.resumeJobCancel("j0001", jobRunnerRecord{PID: 1})
	deadline := time.Now().Add(time.Second)
	for {
		s.jobProcs.mu.Lock()
		active := s.jobProcs.cancelling["j0001"]
		s.jobProcs.mu.Unlock()
		if !active {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("retired server admitted monitor")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCancelMonitorDrainOwnsDBUntilLastRead(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "drain-monitor")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	ready, release, readDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.startJobMonitor("first", func(<-chan struct{}) error {
		close(ready)
		<-release
		_, err := store.CommandRows(0)
		close(readDone)
		return err
	})
	<-ready
	closed := make(chan struct{})
	go func() { s.stopJobMonitors(); _ = store.Close(); close(closed) }()
	deadline := time.Now().Add(time.Second)
	for {
		s.jobProcs.mu.Lock()
		stopping := s.jobProcs.stopping
		s.jobProcs.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown did not start")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-closed:
		t.Fatal("DB closed before the admitted monitor finished")
	default:
	}
	late := make(chan struct{}, 1)
	s.startJobMonitor("late", func(<-chan struct{}) error { late <- struct{}{}; return nil })
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("monitor drain did not finish")
	}
	select {
	case <-readDone:
	default:
		t.Fatal("last DB read was not completed")
	}
	select {
	case <-late:
		t.Fatal("new monitor admitted after shutdown")
	default:
	}
}

func TestManagedSignalRejectsMismatchAndUnavailableStart(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := signalManagedJob(jobRunnerRecord{PID: pid, Identity: "different start"}, syscall.SIGTERM); err == nil {
		t.Fatal("mismatched process signalled")
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatal("unrelated live process was killed", err)
	}
	if err := signalCancellationGroup(jobRunnerRecord{PID: pid, Identity: "different start"}, jobCancelState{VerifiedPGID: pid, VerifiedIdentity: "different start"}, syscall.SIGTERM); err == nil {
		t.Fatal("saved cancellation bypassed live identity mismatch")
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = cmd.Wait()
	if err := signalManagedJob(jobRunnerRecord{PID: pid, Identity: "unavailable"}, syscall.SIGTERM); err == nil {
		t.Fatal("missing start identity accepted")
	}
	if err := signalCancellationGroup(jobRunnerRecord{PID: pid, Identity: "unavailable"}, jobCancelState{}, syscall.SIGTERM); err == nil {
		t.Fatal("unverified missing leader accepted")
	}
}

func TestLegacyCancelReapedLeaderWithSurvivingChild(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "legacy-reaped-leader")
	defer s.stopJobMonitors()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.CreateTemp(t.TempDir(), "legacy-log")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command("/bin/sh", "-c", `/bin/sh -c 'trap "" TERM; printf READY; exec sleep 60' & wait`)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() { _ = syscall.Kill(-pid, syscall.SIGKILL); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(log.Name())
		if strings.Contains(string(data), "READY") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child not ready")
		}
		time.Sleep(time.Millisecond)
	}
	if err := store.InsertCommand("legacy", "legacy", "sh", t.TempDir(), "p1", "owner", "none", log.Name()); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCommandRunning("legacy", int64(pid)); err != nil {
		t.Fatal(err)
	}
	response := s.runCancel("cancel", api.RunCancelParams{JobID: "legacy"})
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	_ = cmd.Wait()
	if !processGroupAlive(pid) {
		t.Fatal("child did not survive TERM")
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		row, err := store.CommandRow("legacy")
		if err != nil {
			t.Fatal(err)
		}
		if row != nil && row.Status == "cancelled" && row.ExitCode == nil && !processGroupAlive(pid) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("legacy cancellation did not finish", row)
		}
		time.Sleep(time.Millisecond)
	}
}
