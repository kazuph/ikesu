package tui

import (
	"fmt"
	"path/filepath"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/worktree"
)

type WorktreeOutcome struct {
	Index       int
	Worktree    api.WorktreeInfo
	AlreadyOpen bool
}

func value(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

func (a *App) openWorktreeItems(src api.WorktreeSourceInfo, path string, label *string, focus bool, items []api.WorktreeInfo) (WorktreeOutcome, error) {
	var info *api.WorktreeInfo
	for i := range items {
		if worktree.SamePath(items[i].Path, path) {
			info = &items[i]
			break
		}
	}
	if info == nil || info.IsBare {
		return WorktreeOutcome{}, fmt.Errorf("checkout not found")
	}
	path = info.Path
	// The main checkout anchors the repo group. Bare repos can anchor a
	// group too: their pane starts in the repository directory.
	parent := -1
	for i, w := range a.st.Workspaces {
		if worktree.SamePath(w.Cwd, src.RepoRoot) {
			parent = i
			break
		}
	}
	if parent < 0 {
		a.exec(model.CmdNewWorkspace{Cwd: src.RepoRoot, NoFocus: true})
		parent = len(a.st.Workspaces) - 1
		a.exec(model.CmdRenameWorkspace{Index: parent, Name: src.RepoName})
	}
	a.exec(model.CmdSetWorktree{Index: parent, Info: worktree.Metadata(src, src.RepoRoot)})
	for i, w := range a.st.Workspaces {
		if worktree.SamePath(w.Cwd, path) {
			a.exec(model.CmdSetWorktree{Index: i, Info: worktree.Metadata(src, path)})
			if focus {
				a.exec(model.CmdSelectWorkspace{Index: i})
			}
			return WorktreeOutcome{Index: i, Worktree: *info, AlreadyOpen: true}, nil
		}
	}
	a.exec(model.CmdNewWorkspace{Cwd: path, NoFocus: !focus})
	idx := len(a.st.Workspaces) - 1
	name := filepath.Base(path)
	if label != nil && *label != "" {
		name = *label
	}
	a.exec(model.CmdRenameWorkspace{Index: idx, Name: name})
	a.exec(model.CmdSetWorktree{Index: idx, Info: worktree.Metadata(src, path)})
	id := fmt.Sprintf("s%d", a.st.Workspaces[idx].ID)
	info.OpenWorkspaceID = &id
	target := parent + 1
	for target < idx && a.st.Workspaces[target].Worktree != nil && a.st.Workspaces[target].Worktree.RepoKey == src.RepoKey {
		target++
	}
	if target != idx {
		a.exec(model.CmdMoveWorkspace{Index: idx, InsertIndex: target})
		idx = target
	}
	a.st.Workspaces[parent].GroupCollapsed = false
	return WorktreeOutcome{Index: idx, Worktree: *info}, nil
}

func (a *App) emitWorktree(kind string, out WorktreeOutcome, force bool) {
	if a.worktreeHook != nil {
		a.worktreeHook(kind, out, force)
	}
}

func (a *App) SetWorktreeHook(f func(string, WorktreeOutcome, bool)) { a.worktreeHook = f }
