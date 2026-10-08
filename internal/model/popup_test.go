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
