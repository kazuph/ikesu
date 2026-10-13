package dispatch

import (
	"path/filepath"
	"testing"
)

func TestImmutableJobCallerAndFailedMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertCommand("j1", "test", "true", "/work", "p1", "duplicate", "summary", ""); err != nil {
		t.Fatal(err)
	}
	actor, name, pane := "agent", "duplicate", "p99"
	s.UpsertActor(&actor, &name, nil, nil, nil, &pane)
	job, err := s.CommandRow("j1")
	if err != nil || job.CallerPane != "p1" {
		t.Fatal(job, err)
	}
	s.InsertCommand("j2", "old", "true", "/work", "", "duplicate", "summary", "")
	job, _ = s.CommandRow("j2")
	if job.CallerPane != "" {
		t.Fatal("legacy caller fabricated")
	}
	legacy, _ := s.InsertMessage(JobsRoom, "", RunShellActor, "duplicate", "old completion", nil, false)
	exact, _ := s.InsertMessage(JobsRoom, "", RunShellActor, "p1", "exact completion", nil, false)
	s.Close()
	for range 2 {
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		pending, _ := s.PendingJobMessages()
		if len(pending) != 1 || pending[0].ID != exact.ID {
			t.Fatal(pending)
		}
		s.mu.Lock()
		row, e := s.queryLocked(`SELECT status,body,completion,read_at FROM dispatches WHERE id=?`, legacy.ID)
		if e != nil || !row.Step() || row.ColumnText(0) != "failed" || row.ColumnText(1) != "old completion" || row.ColumnText(2) == "" || row.ColumnText(3) != "" {
			t.Fatal("legacy failure evidence")
		}
		row.Close()
		s.mu.Unlock()
		s.Close()
	}
}
func TestFailedMessageKeepsBodyTargetReason(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "failed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.InsertFailedMessage("room", "/work", "sender", "duplicate", "unchanged body", "ambiguous recipient", nil); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.History("room", nil, 10)
	if len(rows) != 1 || rows[0].Body != "unchanged body" || rows[0].ToAgent != "duplicate" || rows[0].DeliveredAt != nil || rows[0].ReadAt != nil {
		t.Fatal(rows)
	}
	pending, _ := s.PendingMessagesForAgent("room", "duplicate")
	if len(pending) != 0 {
		t.Fatal("failure queued")
	}
}
