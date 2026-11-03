package model

import (
	"bytes"
	"encoding/gob"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/session"
)

func TestReusedPaneDoesNotInheritLabel(t *testing.T) {
	for _, kind := range []string{"split", "workspace", "popup", "overlay", "import"} {
		t.Run(kind, func(t *testing.T) {
			s := New("/tmp", 80, 24)
			s.Apply(CmdSplitFocused{Dir: layout.Vertical})
			s.Apply(CmdRenamePane{Pane: 2, Label: "reviewer"})
			s.Apply(CmdClosePane{Pane: 2})
			id := 0
			switch kind {
			case "split":
				s.Apply(CmdSplitFocused{Dir: layout.Vertical})
				id = s.FocusedPane()
			case "workspace":
				s.Apply(CmdNewWorkspace{Cwd: "/tmp"})
				id = s.FocusedPane()
			case "popup":
				s.Apply(CmdOpenPopup{WorkspaceID: s.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 20}, Height: api.PopupSize{Cells: 10}})
				id = s.Popup.PaneID
			case "overlay":
				s.Apply(CmdOpenOverlay{TargetPaneID: 1})
				id = s.Overlay.PaneID
			case "import":
				merged, added := session.Append(s.Snapshot(), New("/tmp", 80, 24).Snapshot(), nil, false)
				s.AppendSession(merged, added)
				id = added.Workspaces[0].Tabs[0].Panes[0].ID
			}
			if id != 2 {
				t.Fatalf("allocated p%d, want p2", id)
			}
			if label := s.PaneLabels[id]; label != "" {
				t.Fatalf("new pane p%d inherited label %q", id, label)
			}
		})
	}
}

func TestExistingPaneLabelSurvivesMoveRestoreHandoff(t *testing.T) {
	s := New("/tmp", 80, 24)
	s.Apply(CmdSplitFocused{Dir: layout.Vertical})
	s.Apply(CmdRenamePane{Pane: 2, Label: "reviewer"})
	s.Apply(CmdMovePane{Pane: 2, Dest: MoveDest{Kind: MoveDestNewTab, Workspace: 0}})
	if s.PaneLabels[2] != "reviewer" {
		t.Fatal("move lost the label")
	}
	restored := Restore(s.Snapshot())
	if restored.PaneLabels[2] != "reviewer" {
		t.Fatal("restore lost the label")
	}
	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(s); err != nil {
		t.Fatal(err)
	}
	var handoff AppState
	if err := gob.NewDecoder(&wire).Decode(&handoff); err != nil {
		t.Fatal(err)
	}
	if handoff.PaneLabels[2] != "reviewer" {
		t.Fatal("handoff lost the label")
	}
}
