package server

import (
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/session"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"testing"
)

func TestObservedResumeSourcePersistsCurrentIDInSnapshotLedgerAndAPI(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "resume-source")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	job := platform.ForegroundJob{Processes: []platform.ForegroundProcess{{Name: "claude", Argv: []string{"claude", "--resume", "old-id"}}}}
	rec := observedSessionRecord("claude", "current-id", "task", job)
	if rec.Value != "current-id" || rec.SessionStartSource != "resume" {
		t.Fatal(rec)
	}
	s.app.RecordSession(1, rec)
	s.ledgerPut(1, rec)
	s.saveSession()
	if s.store != nil {
		defer s.store.Close()
	}
	f, err := session.Load(session.Path(s.dir))
	if err != nil {
		t.Fatal(err)
	}
	f.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		if p.SessionID != "current-id" || p.SessionStartSource != "resume" {
			t.Fatal(p)
		}
	})
	ledger, ok := s.ledgerEntry(1)
	if !ok || ledger.SessionID != "current-id" || ledger.SessionStartSource != "resume" {
		t.Fatal(ledger, ok)
	}
	info, ok := s.paneInfo(1)
	if !ok || info.AgentSession == nil || info.AgentSession.SessionStartSource != "resume" || info.AgentSession.Value != "current-id" {
		t.Fatal(info, ok)
	}
	s.app.RestoreSession(f)
	defer s.sup.KillAll()
	restored, _ := s.app.SessionRecord(1)
	if restored.SessionStartSource != "resume" {
		t.Fatal(restored)
	}
	fresh := observedSessionRecord("claude", "fresh-id", "", platform.ForegroundJob{})
	if fresh.SessionStartSource != "" {
		t.Fatal(fresh)
	}
}
