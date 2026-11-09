package server

import (
	"testing"
	"time"

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

func TestLedgerFailedWriteIsNotExposedAsSaved(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "ledger-error")
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
