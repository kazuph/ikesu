package model

import "github.com/kazuph/goterrace/internal/api"

type CmdSetWorktree struct {
	Index int
	Info  *api.WorkspaceWorktreeInfo
}

func (CmdSetWorktree) isCommand() {}

type CmdMoveWorkspace struct{ Index, InsertIndex int }

func (CmdMoveWorkspace) isCommand() {}

func (s *AppState) moveWorkspace(from, to int) []Effect {
	if from < 0 || from >= len(s.Workspaces) || to < 0 || to >= len(s.Workspaces) || from == to {
		return nil
	}
	active := s.ActiveWorkspace()
	ws := s.Workspaces[from]
	s.Workspaces = append(s.Workspaces[:from], s.Workspaces[from+1:]...)
	s.Workspaces = append(s.Workspaces, nil)
	copy(s.Workspaces[to+1:], s.Workspaces[to:])
	s.Workspaces[to] = ws
	for i, w := range s.Workspaces {
		if w == active {
			s.Active = i
		}
	}
	return []Effect{EffSyncLayout{}}
}

type CmdToggleWorktreeGroup struct{ Index int }

func (CmdToggleWorktreeGroup) isCommand() {}
