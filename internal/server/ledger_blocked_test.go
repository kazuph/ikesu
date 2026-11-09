package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/tui"
	sqlite3 "github.com/ncruces/go-sqlite3"
)

func TestLedgerWriteDoesNotBlockAppOnSQLiteLock(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "ledger-blocked")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	writer, err := sqlite3.Open(dispatch.DBPath(s.session))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		s.ledgerPut(1, tui.AgentRecord{Agent: "codex", Kind: "id", Value: "session", Source: "report:test"})
		close(finished)
	}()
	var blocked bool
	select {
	case <-finished:
	case <-time.After(250 * time.Millisecond):
		blocked = true
	}
	if err := writer.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	<-finished
	s.ledgerWG.Wait()
	if blocked {
		t.Fatal("ledgerPut waited for SQLite while on the app loop")
	}
	row, ok := s.ledgerEntry(1)
	if !ok || row.SessionID != "session" {
		t.Fatalf("write after unlock: %+v %v", row, ok)
	}
}

func TestLedgerSQLiteLockDoesNotStopOtherPaneInput(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "ledger-input")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	writer, err := sqlite3.Open(dispatch.DBPath(s.session))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	path := filepath.Join(t.TempDir(), "input-seen")
	entered := make(chan struct{})
	ledgerDone := make(chan struct{})
	go func() {
		defer close(ledgerDone)
		s.app.Do(func() {
			close(entered)
			s.ledgerPut(pid, tui.AgentRecord{Agent: "codex", Kind: "id", Value: "session", Source: "report:test"})
		})
	}()
	<-entered
	go s.app.Do(func() { s.sup.Input(pid, []byte(fmt.Sprintf("printf ready > %q\n", path))) })
	deadline := time.After(time.Second)
	seen := false
	for !seen {
		if _, err := os.Stat(path); err == nil {
			seen = true
			break
		}
		select {
		case <-deadline:
			goto unlocked
		case <-time.After(time.Millisecond):
		}
	}
unlocked:
	if err := writer.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	<-ledgerDone
	s.ledgerWG.Wait()
	if !seen {
		t.Fatal("pane input did not run while SQLite was locked")
	}
}

func TestLedgerFailedWriteIsNotExposedAsSaved(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "ledger-error")
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	writer, err := sqlite3.Open(dispatch.DBPath(s.session))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Exec("DROP TABLE pane_sessions"); err != nil {
		t.Fatal(err)
	}
	s.ledgerPut(1, tui.AgentRecord{Agent: "codex", Kind: "id", Value: "failed", Source: "report:test"})
	s.ledgerWG.Wait()
	if row, ok := s.ledgerEntry(1); ok {
		t.Fatalf("failed write exposed: %+v", row)
	}
}

func TestServerShutdownDrainsLockedLedgerWrite(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	stopped := false
	defer func() {
		if !stopped {
			stopServer(t, dir, done)
		}
	}()
	st, err := dispatch.Open(dispatch.DBPath("test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	writer, err := sqlite3.Open(dispatch.DBPath("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_ = writer.Exec("ROLLBACK")
		}
	}()
	id := "shutdown-session"
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Agent: "codex", State: "idle", Source: "test", SessionID: &id}, nil)
	apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil)
	if err := writer.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	locked = false
	select {
	case code := <-done:
		stopped = true
		if code != 0 {
			t.Fatalf("server exit %d", code)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("server did not drain ledger and exit")
	}
	st, err = dispatch.Open(dispatch.DBPath("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.PaneSessions("test")
	if err != nil || len(rows) != 1 || rows[0].SessionID != id {
		t.Fatalf("ledger after shutdown: %+v %v", rows, err)
	}
}
