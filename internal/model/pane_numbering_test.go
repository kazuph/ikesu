package model

import (
	"bytes"
	"encoding/gob"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/layout"
)

func TestPaneAllocationUsesLedgerAndFailsClosed(t *testing.T) {
	db, err := dispatch.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := New("/tmp", 80, 24)
	if err := db.PutPaneSession(dispatch.PaneSession{Session: "test", PaneID: 2}); err != nil {
		t.Fatal(err)
	}
	s.SetPaneReservations(func() (map[int]bool, error) { return db.ReservedPaneIDs("test") })
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	if id := s.FocusedPane(); id != 3 {
		t.Fatalf("got %d, want 3", id)
	}
	db.Close()
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	if id := s.FocusedPane(); id != 4 {
		t.Fatalf("unreadable database: got %d, want high water + 1", id)
	}
}

func TestPaneGenerationAndHighWaterSurviveHandoff(t *testing.T) {
	s := New("/tmp", 80, 24)
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	id, generation := s.FocusedPane(), s.PaneGeneration(s.FocusedPane())
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(s); err != nil {
		t.Fatal(err)
	}
	var restored AppState
	if err := gob.NewDecoder(&wire).Decode(&restored); err != nil {
		t.Fatal(err)
	}
	restored.RestoreHandoffCounters(s.HandoffCounters())
	if restored.FocusedPane() != id || restored.PaneGeneration(id) != generation {
		t.Fatal("handoff changed pane identity")
	}
	restored.Apply(CmdSplitFocused{Dir: layout.Vertical})
	if restored.FocusedPane() == id {
		t.Fatal("new pane collided with restored pane")
	}
}

func TestPaneGenerationSurvivesRestoreAndChangesOnReuse(t *testing.T) {
	s := New("/tmp", 80, 24)
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	id := s.FocusedPane()
	old := s.PaneGeneration(id)
	if old == "" {
		t.Fatal("missing generation")
	}
	r := Restore(s.Snapshot())
	if got := r.PaneGeneration(id); got != old {
		t.Fatalf("restored generation %q, want %q", got, old)
	}
	r.Apply(CmdClosePane{Pane: id})
	r.Apply(CmdSplitFocused{Dir: layout.Vertical})
	if r.FocusedPane() != id || r.PaneGeneration(id) == old || r.PaneGeneration(id) == "" {
		t.Fatal("reused pane did not acquire a new generation")
	}
}
