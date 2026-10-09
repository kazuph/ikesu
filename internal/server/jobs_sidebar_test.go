package server

import (
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	sqlite3 "github.com/ncruces/go-sqlite3"
	"testing"
)

func TestG2JobsHistoryRowsRemainWhenExpiredAndDeadRunnerDotsDisappear(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "history")
	s.app.SetJobSource(s.jobSnapshot)
	store, err := dispatch.Open(dispatch.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"expired", "dead", "queued"} {
		if err := store.InsertCommand(id, id, "true", "/tmp", "p1", "", "none", "/tmp/unused"); err != nil {
			t.Fatal(err)
		}
	}
	// Write actual historical data to the isolated SQLite DB, without replacing a dependency.
	db, err := sqlite3.Open(dispatch.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Exec("UPDATE dispatches SET status='exited', exit_code=0, finished_at='2000-01-01T00:00:00.000Z' WHERE label='expired'; UPDATE dispatches SET status='running', runner_pid=NULL WHERE label='dead'"); err != nil {
		t.Fatal(err)
	}
	s.app.TestRefreshJobDots()
	if len(s.app.JobDots()[0]) != 1 {
		t.Fatal("expired or dead row left a dot", s.app.JobDots())
	}
	in := render.Input{State: s.app.State(), Jobs: s.app.Jobs(), JobsPanel: true}
	view := render.SidebarViewForInput(in)
	count := 0
	for _, pos := range view.Rows {
		if pos.Row.Kind == render.RowJob {
			count++
		}
	}
	if len(in.Jobs) != 3 || view.Agents.Total != 3 || count != 3 {
		t.Fatal("history rows changed", in.Jobs, view.Agents.Total, count)
	}
	for _, j := range in.Jobs {
		if j.ID == "queued" && j.Color != s.app.JobDots()[0][0].Color {
			t.Fatal("dot/panel colors differ")
		}
	}
}
