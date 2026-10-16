package server

import (
	"testing"

	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	sqlite3 "github.com/ncruces/go-sqlite3"
)

func TestStage4SavedUnknownExitIsNotSuccessful(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "stage4-unknown-exit")
	s.app.SetJobSource(s.jobSnapshot)
	store, err := dispatch.Open(dispatch.DBPath(s.session))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InsertCommand("unknown-exit", "UNKNOWN", "true", t.TempDir(), "p1", "", "none", ""); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite3.Open(dispatch.DBPath(s.session))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec("UPDATE dispatches SET status='exited', exit_code=NULL, started_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'), finished_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE external_id='unknown-exit'"); err != nil {
		t.Fatal(err)
	}
	s.app.TestRefreshJobDots()
	jobs := s.app.Jobs()
	if len(jobs) != 1 || jobs[0].Color != render.Red || jobs[0].ExitCode != nil {
		t.Fatalf("unknown exit presented as success: %+v", jobs)
	}
	dots := s.app.JobDots()[0]
	if len(dots) != 1 || dots[0].Color != render.Red {
		t.Fatalf("unknown exit dot: %+v", dots)
	}
}
