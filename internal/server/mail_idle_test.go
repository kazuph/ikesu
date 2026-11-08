package server

import (
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

// A real key requires the full ten-second quiet period before mail can resume.
func TestDeferredMailRetriesOnlyItsPane(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "mail-deferred")
	s.mailCh = make(chan mailWrite, 8)
	s.mailInFlight = map[int64]bool{}
	s.jobInFlight = map[string]bool{}
	pane, err := s.sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	s.app.SetReported(1, "codex", "working")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.InsertPaneMessage("room", "", "sender", "p1", "held mail", nil, false, s.app.State().PaneGeneration(1))
	if err != nil {
		t.Fatal(err)
	}
	sentinel, err := store.InsertMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, "p999999", "unrelated pending", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	pane.RecordKeyInput()
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	workerDone := make(chan struct{})
	go func() { s.mailWorker(); close(workerDone) }()
	defer func() { close(s.mailCh); <-workerDone; s.app.Stop(); <-appDone }()
	s.app.Do(func() { s.flushMailForPane(1) })
	deadline := time.Now().Add(runtime.MailInputQuiet + 5*time.Second)
	for {
		regular, err := store.PendingForAgentCreationOrder("p1")
		if err != nil {
			t.Fatal(err)
		}
		jobs, err := store.PendingJobMessages()
		if err != nil || len(jobs) != 1 || jobs[0].ID != sentinel.ID {
			t.Fatalf("unrelated pane swept: %v err=%v", jobs, err)
		}
		if len(regular) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("held pane did not resume")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
