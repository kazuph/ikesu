package model

import (
	"github.com/kazuph/goterrace/internal/api"
	"testing"
)

func TestG2PopupStableOwnerReorderAndShutdownExactlyOnce(t *testing.T) {
	s := New("/tmp", 120, 50)
	owner := s.ActiveWorkspace().ID
	s.Apply(CmdOpenPopup{WorkspaceID: owner, Width: api.PopupSize{Cells: 60}, Height: api.PopupSize{Cells: 20}})
	pid := s.Popup.PaneID
	s.Apply(CmdPopupResizeLock{Locked: true})
	s.Apply(CmdNewWorkspace{Cwd: "/tmp/B"})
	if s.PopupVisible() || s.Popup.WorkspaceID != owner {
		t.Fatal("owner changed")
	}
	s.Apply(CmdMoveWorkspace{Index: 0, InsertIndex: 1})
	s.Apply(CmdSelectWorkspace{Index: 1})
	if !s.PopupVisible() || s.Popup.PaneID != pid {
		t.Fatal("reorder moved popup")
	}
	for _, effect := range s.Apply(CmdCloseWorkspace{Index: 0}) {
		if k, ok := effect.(EffKillPane); ok && k.Pane == pid {
			t.Fatal("background close killed popup")
		}
	}
	if s.Popup == nil || !s.Popup.ResizeLocked {
		t.Fatal("background close cleared popup state")
	}
	count := 0
	for _, effect := range s.Apply(CmdCloseWorkspace{Index: 0}) {
		if k, ok := effect.(EffKillPane); ok && k.Pane == pid {
			count++
		}
	}
	if s.Popup != nil || count != 1 || len(s.Apply(CmdClosePopup{})) != 0 {
		t.Fatal("owner cleanup was not once", count)
	}
}

func TestParityPopupCenteredAboveActionBar(t *testing.T) {
	for _, dimensions := range []struct{ cols, rows, x, y int }{{160, 45, 63, 12}, {100, 25, 33, 2}} {
		s := New("/tmp", dimensions.cols, dimensions.rows)
		s.Apply(CmdOpenPopup{WorkspaceID: s.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 60}, Height: api.PopupSize{Cells: 20}})
		r := s.PopupRect()
		if r.X != dimensions.x || r.Y != dimensions.y || r.W != 60 || r.H != 20 {
			t.Fatal("distribution geometry", dimensions, r)
		}
	}
}

func TestStage6aClosePopupRequiresActiveOwner(t *testing.T) {
	s := New("/tmp", 120, 40)
	owner := s.ActiveWorkspace().ID
	s.Apply(CmdOpenPopup{WorkspaceID: owner, Width: api.PopupSize{Cells: 40}, Height: api.PopupSize{Cells: 12}})
	pop := s.Popup
	s.Apply(CmdNewWorkspace{Cwd: "/tmp/other"})
	if effects := s.Apply(CmdClosePopup{}); len(effects) != 0 {
		t.Fatal("closed another workspace popup", effects)
	}
	if s.PopupByPane(pop.PaneID) != pop {
		t.Fatal("lost background popup")
	}
	s.Apply(CmdSelectWorkspace{Index: 0})
	if !s.PopupVisible() || s.Popup != pop {
		t.Fatal("owner popup did not survive")
	}
}
