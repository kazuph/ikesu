package server

import (
	"os"
	"testing"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestLegacyGenerationBindsOnlyRestoredOwner(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "legacy-generation")
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		t.Fatal(err)
	}
	f := s.app.State().Snapshot()
	f.Workspaces[0].Tabs[0].Panes[0].Generation = ""
	s.app.RestoreSession(f)
	defer s.sup.Kill(1)
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.InsertMessage("legacy", "", "sender", "p1", "old", nil, false); err != nil {
		t.Fatal(err)
	}
	s.migrateLegacyPaneGenerations()
	rows, err := store.PendingMessagesForAgent("legacy", "p1")
	if err != nil || len(rows) != 1 || rows[0].PaneGeneration != s.app.State().PaneGeneration(1) {
		t.Fatalf("legacy rows %v err %v", rows, err)
	}
	saved, err := session.Load(session.Path(s.dir))
	if err != nil || saved == nil || saved.Workspaces[0].Tabs[0].Panes[0].Generation != rows[0].PaneGeneration || saved.Workspaces[0].Tabs[0].Panes[0].LegacyGeneration {
		t.Fatalf("saved pane %v err %v", saved, err)
	}
	if _, err := store.InsertMessage(dispatch.JobsRoom, "", "sender", "p1", "unbound", nil, false); err != nil {
		t.Fatal(err)
	}
	s.migrateLegacyPaneGenerations()
	rows, err = store.PendingMessagesForAgent(dispatch.JobsRoom, "p1")
	if err != nil || len(rows) != 1 || rows[0].PaneGeneration != "" {
		t.Fatalf("new untagged row was rebound: %v, %v", rows, err)
	}
}
