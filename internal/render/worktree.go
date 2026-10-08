package render

import "github.com/kazuph/goterrace/internal/model"

func WorkspaceHidden(s *model.AppState, index int) bool {
	child := s.Workspaces[index].Worktree
	if child == nil || !child.IsLinkedWorktree {
		return false
	}
	for _, w := range s.Workspaces {
		if w.Worktree != nil && !w.Worktree.IsLinkedWorktree && w.Worktree.RepoKey == child.RepoKey {
			return w.GroupCollapsed
		}
	}
	return false
}
