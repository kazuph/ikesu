package model

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"testing"
)

func TestPopupPerWorkspaceAndPersistentSnapshot(t *testing.T) {
	s := New("/tmp", 120, 40)
	before, _ := json.Marshal(s.Snapshot())
	s.Apply(CmdOpenPopup{WorkspaceID: s.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 40}, Height: api.PopupSize{Cells: 20}})
	first := s.Popup.PaneID
	after, _ := json.Marshal(s.Snapshot())
	if string(before) != string(after) {
		t.Fatalf("popup changed snapshot %s -> %s", before, after)
	}
	s.Apply(CmdNewWorkspace{Cwd: "/tmp"})
	s.Apply(CmdOpenPopup{WorkspaceID: s.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 40}, Height: api.PopupSize{Cells: 20}})
	second := s.Popup.PaneID
	if first == second || len(s.Popups) != 2 {
		t.Fatal(s.Popups)
	}
	s.Apply(CmdSelectWorkspace{Index: 0})
	if s.Popup.PaneID != first || !s.PopupVisible() {
		t.Fatal(s.Popup)
	}
	s.Apply(CmdClosePopup{Pane: second})
	if s.PopupByPane(second) != nil || s.Popup.PaneID != first || len(s.Popups) != 1 {
		t.Fatal(s.Popups)
	}
}
