package dispatch

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestCloseWaitsForStoreOperationAndRejectsFurtherOperations(t *testing.T) {
	s := testStore(t)
	if err := s.InsertCommand("lock", "lock", "true", t.TempDir(), "p1", "p1", "none", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCommandRunning("lock", int64(os.Getpid())); err != nil {
		t.Fatal(err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	operationDone := make(chan error, 1)
	logDir := t.TempDir()
	go func() {
		operationDone <- s.CleanupJobs(time.Now(), logDir, func(pid int) bool {
			close(locked)
			<-release
			return syscall.Kill(pid, 0) == nil
		})
	}()
	<-locked
	closeDone := make(chan error, 1)
	go func() { closeDone <- s.Close() }()
	select {
	case err := <-closeDone:
		t.Errorf("Close returned before the SQLite operation finished: %v", err)
		closeDone = nil
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-operationDone; err != nil {
		t.Fatal(err)
	}
	if closeDone != nil {
		if err := <-closeDone; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PendingForAgentCreationOrder("p1"); !errors.Is(err, errStoreClosed) {
		t.Errorf("pending after Close: %v", err)
	}
	if _, err := s.MessagesForInboxRecipients("room", nil, nil, nil); !errors.Is(err, errStoreClosed) {
		t.Errorf("empty inbox after Close: %v", err)
	}
	if _, err := s.MarkDeliveredByIDs(nil); !errors.Is(err, errStoreClosed) {
		t.Errorf("empty acknowledgement after Close: %v", err)
	}
	if _, err := s.InsertMessage("room", "", "sender", "p1", "body", nil, false); !errors.Is(err, errStoreClosed) {
		t.Errorf("insert after Close: %v", err)
	}
	if _, err := s.CompleteJob("lock", "running", "exited", nil, 0, "p1", "body"); !errors.Is(err, errStoreClosed) {
		t.Errorf("completion after Close: %v", err)
	}
	if err := s.CleanupJobs(time.Now(), logDir, func(pid int) bool { return syscall.Kill(pid, 0) == nil }); !errors.Is(err, errStoreClosed) {
		t.Errorf("cleanup after Close: %v", err)
	}
}
