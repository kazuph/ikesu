package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

// Real input requires waiting through the ten-second quiet interval.
func TestPendingMailAndJobWaitWhileBlocked(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "mail-pending-blocked")
	s.mailCh = make(chan mailWrite, 8)
	s.mailInFlight = map[int64]bool{}
	s.jobInFlight = map[string]bool{}
	capture := filepath.Join(t.TempDir(), "pty")
	script := fmt.Sprintf("stty raw -echo; printf READY; exec cat > %q", capture)
	p, err := s.sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(p.ScreenText(), "READY") {
		if time.Now().After(deadline) {
			t.Fatal("PTY not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	regular, err := store.InsertPaneMessage("room", "", "sender", "p1", "regular", nil, false, s.app.State().PaneGeneration(1))
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.InsertPaneMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, "p1", "job", nil, false, s.app.State().PaneGeneration(1))
	if err != nil {
		t.Fatal(err)
	}
	s.app.SetReported(1, "codex", "working")
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	if !s.enqueueMail(mailWrite{pane: 1, owner: p, payload: []byte("regular\r"), msgID: regular.ID}) || !s.enqueueMail(mailWrite{pane: 1, owner: p, payload: []byte("job\r"), msgIDs: []int64{job.ID}, room: dispatch.JobsRoom, toAgent: "p1"}) {
		t.Fatal("enqueue failed")
	}
	p.RecordKeyInput()
	workerDone := make(chan struct{})
	go func() { s.mailWorker(); close(workerDone) }()
	defer func() { close(s.mailCh); <-workerDone; s.app.Stop(); <-appDone }()
	deadline = time.Now().Add(5 * time.Second)
	for {
		s.mailMu.Lock()
		_, deferred := s.mailDeferred[1]
		s.mailMu.Unlock()
		if deferred {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("write was not held")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.app.Do(func() { s.app.SetReported(1, "codex", "blocked") })
	// Observe at least one retry after the quiet interval.
	time.Sleep(runtime.MailInputQuiet + 2*jobActiveRefresh)
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("blocked pane received %q", data)
	}
	s.app.Do(func() { s.app.SetReported(1, "codex", "working") })
	deadline = time.Now().Add(5 * time.Second)
	for {
		data, err = os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > len("regular\rjob\r") {
			t.Fatalf("wrong order or count: %q", data)
		}
		regularPending, err := store.PendingForAgentCreationOrder("p1")
		if err != nil {
			t.Fatal(err)
		}
		jobs, err := store.PendingJobMessages()
		if err != nil {
			t.Fatal(err)
		}
		if string(data) == "regular\rjob\r" && len(regularPending) == 0 && len(jobs) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("data=%q regular pending=%v jobs pending=%v", data, regularPending, jobs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
