package model

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
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

func TestLegacyGenerationMarkerSurvivesRestoreButNotReuse(t *testing.T) {
	legacy := New("/tmp", 80, 24).Snapshot()
	legacy.Workspaces[0].Tabs[0].Panes[0].Generation = ""
	r := Restore(legacy)
	data, err := json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"legacy_generation":true`)) {
		t.Fatalf("legacy marker missing: %s", data)
	}
	r.Apply(CmdSplitFocused{Dir: layout.Vertical})
	r.Apply(CmdClosePane{Pane: 1})
	r.Apply(CmdSplitFocused{Dir: layout.Vertical})
	if r.FocusedPane() != 1 {
		t.Fatal("expected reused pane")
	}
	data, err = json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"legacy_generation":true`)) {
		t.Fatalf("replacement inherited legacy marker: %s", data)
	}
}

func TestPaneAllocationAvoidsAllTemporaryPanes(t *testing.T) {
	s := New("/tmp", 80, 24)
	firstWorkspace := s.ActiveWorkspace().ID
	s.Apply(CmdOpenPopup{WorkspaceID: firstWorkspace, Width: api.PopupSize{Cells: 20}, Height: api.PopupSize{Cells: 10}})
	if s.Popup.PaneID != 2 {
		t.Fatalf("first popup=%d", s.Popup.PaneID)
	}
	s.Apply(CmdNewWorkspace{Cwd: "/tmp"})
	if s.FocusedPane() != 3 {
		t.Fatalf("workspace pane=%d", s.FocusedPane())
	}
	s.Apply(CmdOpenPopup{WorkspaceID: s.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 20}, Height: api.PopupSize{Cells: 10}})
	if s.Popup.PaneID != 4 {
		t.Fatalf("second popup=%d", s.Popup.PaneID)
	}
	s.Apply(CmdOpenOverlay{TargetPaneID: 3})
	if s.Overlay == nil || s.Overlay.PaneID != 5 {
		t.Fatalf("overlay=%v", s.Overlay)
	}
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	if s.FocusedPane() != 6 {
		t.Fatalf("new pane=%d", s.FocusedPane())
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
