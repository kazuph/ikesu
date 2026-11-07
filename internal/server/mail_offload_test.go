package server

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestMailQueryDoesNotBlockAppLoop(t *testing.T) {
	for _, sweep := range []bool{false, true} {
		name := "pane"
		if sweep {
			name = "sweep"
		}
		t.Run(name, func(t *testing.T) { checkMailQueryDoesNotBlockAppLoop(t, sweep) })
	}
}

func checkMailQueryDoesNotBlockAppLoop(t *testing.T, sweep bool) {
	t.Helper()
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
		s.app.Do(func() {
			close(flushStarted)
			if sweep {
				s.flushAllMail()
			} else {
				s.flushMailForPane(1)
			}
		})
		close(flushDone)
	}()
	<-flushStarted
	// Confirm the real SQLite store read is waiting on its held mutex before
	// probing the app loop; merely starting a goroutine would not prove this.
	deadline := time.Now().Add(time.Second)
	stack := make([]byte, 1<<20)
	for {
		n := runtime.Stack(stack, true)
		if bytes.Contains(stack[:n], []byte("internal/dispatch.(*Store).Pending")) {
			break
		}
		if time.Now().After(deadline) {
			t.Error("mail database read did not reach the held store lock")
			break
		}
		runtime.Gosched()
	}
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
	if !s.applyPaneMailSnapshot(1, mailQueryTarget{owner: pane, generation: s.app.State().PaneGeneration(1)}, snapshot, nil) || len(s.mailCh) != 0 {
		t.Fatal("snapshot consumed by inbox was applied")
	}
}

func TestPaneMailSnapshotRejectsReplacement(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
	s := newTestServer(t, "mail-snapshot-replacement")
	s.mailCh = make(chan mailWrite, 4)
	s.mailInFlight = map[int64]bool{}
	s.jobInFlight = map[string]bool{}
	s.app.State().Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	pid := s.app.State().FocusedPane()
	pane, err := s.sup.SpawnWith(pid, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	generation := s.app.State().PaneGeneration(pid)
	for _, room := range []string{"room", dispatch.JobsRoom} {
		if _, err := store.InsertPaneMessage(room, "", "sender", fmtPaneID(pid), "body", nil, false, generation); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := s.readPaneMail(pid)
	s.app.State().Apply(model.CmdClosePane{Pane: pid})
	if s.applyPaneMailSnapshot(pid, mailQueryTarget{owner: pane, generation: generation}, snapshot, nil) || len(s.mailCh) != 0 {
		t.Fatal("closed pane received stale snapshot")
	}
	s.sup.Kill(pid)
	s.app.State().Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	if s.app.State().FocusedPane() != pid || s.app.State().PaneGeneration(pid) == generation {
		t.Fatal("pane was not replaced")
	}
	if _, err := s.sup.SpawnWith(pid, t.TempDir(), 80, 24, nil, []string{"/bin/cat"}); err != nil {
		t.Fatal(err)
	}
	if s.applyPaneMailSnapshot(pid, mailQueryTarget{owner: pane, generation: generation}, snapshot, nil) || len(s.mailCh) != 0 {
		t.Fatal("replacement pane received stale snapshot")
	}
}

func TestSendKeepsNudgedWhenAnotherFlushQueuesFirst(t *testing.T) {
	for _, room := range []string{"room", dispatch.JobsRoom} {
		t.Run(room, func(t *testing.T) {
			teststate.Lock(t)
			teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
			s := newTestServer(t, "mail-send-overlap")
			s.mailCh = make(chan mailWrite, 4)
			s.mailInFlight = map[int64]bool{}
			s.jobInFlight = map[string]bool{}
			if _, err := s.sup.SpawnWith(1, t.TempDir(), 80, 24, nil, []string{"/bin/cat"}); err != nil {
				t.Fatal(err)
			}
			defer s.sup.KillAll()
			s.app.SetReported(1, "codex", "working")
			var query mailSendQuery
			response := s.prepareMsgSend("send", api.MsgSendParams{Room: room, FromAgent: "sender", To: "p1", Body: "body"}, &query)
			if response.Err != nil || !query.ready {
				t.Fatalf("response=%+v", response)
			}
			defer s.store.Close()
			snapshot := s.readPaneMail(1)
			if s.applyPaneMailSnapshot(1, query.target, snapshot, nil) {
				t.Fatal("unexpected stale snapshot")
			}
			write := <-s.mailCh
			if _, err := s.store.MarkDeliveredByIDs([]int64{query.message.ID}); err != nil {
				t.Fatal(err)
			}
			s.mailDone(write)
			if !s.applyPaneMailSnapshot(1, query.target, snapshot, nil) || len(s.mailCh) != 0 {
				t.Fatal("snapshot queued a message already delivered by another flush")
			}
			if !query.nudged || len(s.readPaneMail(1).regular) != 0 || len(s.readPaneMail(1).jobs) != 0 {
				t.Fatal("concurrent flush lost send's nudged result")
			}
		})
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

func TestOtherPaneDeliveryDoesNotInvalidateSnapshot(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, dispatch.DBPathEnvVar, filepath.Join(t.TempDir(), "mail.db"))
	s := newTestServer(t, "mail-other-pane")
	s.mailCh = make(chan mailWrite, 2)
	s.mailInFlight = map[int64]bool{}
	s.jobInFlight = map[string]bool{}
	s.app.State().Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	other := s.app.State().FocusedPane()
	pane, err := s.sup.SpawnWith(other, t.TempDir(), 80, 24, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	message, err := store.InsertPaneMessage("room", "", "sender", "p1", "first", nil, false, s.app.State().PaneGeneration(1))
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, err := store.InsertPaneMessage("room", "", "sender", fmtPaneID(other), "second", nil, false, s.app.State().PaneGeneration(other))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := s.readPaneMail(1)
	write := mailWrite{pane: other, owner: pane, msgID: otherMessage.ID, payload: []byte("second\r")}
	if !s.enqueueMail(write) {
		t.Fatal("other pane's message was not queued")
	}
	<-s.mailCh
	if written, err := s.deliverMailWrite(write); !written || err != nil {
		t.Fatalf("other pane delivery: written=%v err=%v", written, err)
	}
	s.mailDone(write)
	if ok, stale := s.enqueueMailAtEpoch(mailWrite{pane: 1, msgID: message.ID}, &snapshot.epoch); !ok || stale {
		t.Fatalf("another pane invalidated the snapshot: ok=%v stale=%v", ok, stale)
	}
}
