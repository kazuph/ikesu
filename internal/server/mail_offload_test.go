package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestMailQueryDoesNotBlockAppLoop(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
	s := newTestServer(t, "mail-offload")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InsertCommand("lock", "lock", "true", t.TempDir(), "p1", "p1", "none", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCommandRunning("lock", int64(os.Getpid())); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	locked, release, cleanupDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		cleanupDone <- store.CleanupJobs(time.Now(), logDir, func(pid int) bool {
			close(locked)
			<-release
			return processGroupAlive(pid)
		})
	}()
	<-locked
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	flushStarted, flushDone := make(chan struct{}), make(chan struct{})
	go func() {
		s.app.Do(func() { close(flushStarted); s.flushMailForPane(1) })
		close(flushDone)
	}()
	<-flushStarted
	responsive := make(chan struct{})
	go s.app.Do(func() { close(responsive) })
	select {
	case <-responsive:
	case <-time.After(time.Second):
		t.Error("app loop blocked while mail database was locked")
	}
	close(release)
	if err := <-cleanupDone; err != nil {
		t.Error(err)
	}
	<-flushDone
	<-responsive
	s.app.Stop()
	<-appDone
	s.mailQueryWG.Wait()
	s.sup.KillAll()
}

func TestInboxInvalidatesMailSnapshot(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
	s := newTestServer(t, "mail-snapshot-inbox")
	s.mailCh = make(chan mailWrite, 1)
	s.mailInFlight = map[int64]bool{}
	s.jobInFlight = map[string]bool{}
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	message, err := store.InsertPaneMessage("room", "", "sender", "p1", "body", nil, false, s.app.State().PaneGeneration(1))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := s.readPaneMail(1)
	if len(snapshot.regular) != 1 {
		t.Fatalf("pending: %+v", snapshot.regular)
	}
	response := s.msgInbox("inbox", api.MsgInboxParams{Room: "room", ToAgent: "p1"})
	if response.Err != nil {
		t.Fatal(response.Err)
	}
	if ok, stale := s.enqueueMailAtEpoch(mailWrite{pane: 1, msgID: message.ID}, &snapshot.epoch); ok || !stale || len(s.mailCh) != 0 {
		t.Fatalf("stale snapshot was queued: ok=%v stale=%v", ok, stale)
	}
}

func TestReadPaneMailPreservesPendingOrder(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
	s := newTestServer(t, "mail-snapshot")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, entry := range []struct{ room, body string }{
		{"first", "one"}, {dispatch.JobsRoom, "job one"},
		{"second", "two"}, {dispatch.JobsRoom, "job two"},
	} {
		if _, err := store.InsertPaneMessage(entry.room, "", "sender", "p1", entry.body, nil, false, "generation"); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := s.readPaneMail(1)
	if len(snapshot.regular) != 2 || snapshot.regular[0].Body != "one" || snapshot.regular[1].Body != "two" {
		t.Fatalf("regular messages: %+v", snapshot.regular)
	}
	if len(snapshot.jobs) != 2 || snapshot.jobs[0].Body != "job one" || snapshot.jobs[1].Body != "job two" {
		t.Fatalf("job messages: %+v", snapshot.jobs)
	}
}
