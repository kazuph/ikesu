package model

import "testing"

func TestFocuslessMovePreservesSelectionIDs(t *testing.T) {
	for _, scope := range []string{"workspace", "tab"} {
		t.Run(scope, func(t *testing.T) {
			s := newState()
			moved := s.FocusedPane()
			if scope == "workspace" {
				s.Apply(CmdNewWorkspace{})
				s.Apply(CmdNewWorkspace{})
			} else {
				s.Apply(CmdNewTab{Workspace: 0})
				s.Apply(CmdNewTab{Workspace: 0})
			}
			if scope == "workspace" {
				s.Active = 1
			} else {
				s.Workspaces[0].ActiveTab = 1
			}
			ws, tab := s.ActiveWorkspace(), s.ActiveWorkspace().TabRef()
			s.Apply(CmdMovePane{Pane: moved, Dest: MoveDest{Kind: MoveDestNewWorkspace}})
			if s.ActiveWorkspace().ID != ws.ID || s.ActiveWorkspace().TabRef().ID != tab.ID {
				t.Fatalf("selection changed: want ws=%d tab=%d, got ws=%d tab=%d", ws.ID, tab.ID, s.ActiveWorkspace().ID, s.ActiveWorkspace().TabRef().ID)
			}
		})
	}
}
