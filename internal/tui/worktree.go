package tui

import (
	"crypto/rand"
	"fmt"
	"path/filepath"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/worktree"
)

type WorktreeOutcome struct {
	Index       int
	Worktree    api.WorktreeInfo
	AlreadyOpen bool
}

func (a *App) WorktreeSource(cwd string, ws *string) (api.WorktreeSourceInfo, error) {
	if ws != nil && cwd != "" {
		return api.WorktreeSourceInfo{}, fmt.Errorf("workspace_id and cwd are mutually exclusive")
	}
	if ws != nil {
		ref, ok := api.ParseWorkspaceRef(*ws)
		if !ok {
			return api.WorktreeSourceInfo{}, fmt.Errorf("workspace not found")
		}
		found := false
		for i, w := range a.st.Workspaces {
			if w.ID == ref.Number || ref.Index == i+1 {
				cwd = w.Cwd
				found = true
				break
			}
		}
		if !found {
			return api.WorktreeSourceInfo{}, fmt.Errorf("workspace not found")
		}
	}
	if cwd == "" {
		if w := a.st.ActiveWorkspace(); w != nil {
			cwd = w.Cwd
		} else {
			cwd = a.cwd
		}
	}
	src, err := worktree.Source(cwd)
	if err != nil {
		return src, err
	}
	for _, w := range a.st.Workspaces {
		if worktree.SamePath(w.Cwd, src.SourceCheckoutPath) {
			id := fmt.Sprintf("s%d", w.ID)
			src.SourceWorkspaceID = &id
			break
		}
	}
	return src, nil
}

func (a *App) WorktreeList(src api.WorktreeSourceInfo) ([]api.WorktreeInfo, error) {
	items, err := worktree.List(src.RepoRoot)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Label = src.RepoName
		for _, w := range a.st.Workspaces {
			if worktree.SamePath(w.Cwd, items[i].Path) {
				id := fmt.Sprintf("s%d", w.ID)
				items[i].OpenWorkspaceID = &id
				break
			}
		}
	}
	return items, nil
}

func value(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

func (a *App) CreateWorktree(p api.WorktreeCreateParams) (WorktreeOutcome, error) {
	src, err := a.WorktreeSource(value(p.Cwd), p.WorkspaceID)
	if err != nil {
		return WorktreeOutcome{}, err
	}
	branch := value(p.Branch)
	if branch == "" {
		branch = "worktree/" + rand.Text()
	}
	path := value(p.Path)
	if path == "" {
		path = filepath.Join(*src.CheckoutPathPrefix, worktree.Slug(branch))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(src.SourceCheckoutPath, path)
	}
	path = filepath.Clean(path)
	if err = worktree.Create(src.RepoRoot, path, branch, value(p.Base)); err != nil {
		return WorktreeOutcome{}, err
	}
	out, err := a.openWorktree(src, path, p.Label, p.Focus)
	if err == nil {
		a.emitWorktree("worktree_created", out, false)
	}
	return out, err
}

func (a *App) OpenWorktree(p api.WorktreeOpenParams) (WorktreeOutcome, error) {
	if (p.Path == nil) == (p.Branch == nil) {
		return WorktreeOutcome{}, fmt.Errorf("exactly one of path or branch is required")
	}
	src, err := a.WorktreeSource(value(p.Cwd), p.WorkspaceID)
	if err != nil {
		return WorktreeOutcome{}, err
	}
	items, err := a.WorktreeList(src)
	if err != nil {
		return WorktreeOutcome{}, err
	}
	path := value(p.Path)
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(src.SourceCheckoutPath, path)
	}
	for _, w := range items {
		if path != "" && worktree.SamePath(path, w.Path) || p.Branch != nil && w.Branch != nil && *p.Branch == *w.Branch {
			if w.IsBare {
				return WorktreeOutcome{}, fmt.Errorf("cannot open a bare repository as a checkout")
			}
			out, err := a.openWorktree(src, w.Path, p.Label, p.Focus)
			if err == nil {
				a.emitWorktree("worktree_opened", out, false)
			}
			return out, err
		}
	}
	return WorktreeOutcome{}, fmt.Errorf("worktree not found; use create to check out an existing branch")
}

func (a *App) openWorktree(src api.WorktreeSourceInfo, path string, label *string, focus bool) (WorktreeOutcome, error) {
	items, err := a.WorktreeList(src)
	if err != nil {
		return WorktreeOutcome{}, err
	}
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

func (a *App) RemoveWorktree(index int, force bool) (api.WorktreeInfo, error) {
	if index < 0 || index >= len(a.st.Workspaces) {
		return api.WorktreeInfo{}, fmt.Errorf("workspace not found")
	}
	ws := a.st.Workspaces[index]
	src, err := worktree.Source(ws.Cwd)
	if err != nil {
		return api.WorktreeInfo{}, err
	}
	items, err := a.WorktreeList(src)
	if err != nil {
		return api.WorktreeInfo{}, err
	}
	var info *api.WorktreeInfo
	for i := range items {
		if worktree.SamePath(items[i].Path, src.SourceCheckoutPath) {
			info = &items[i]
			break
		}
	}
	if info == nil {
		return api.WorktreeInfo{}, fmt.Errorf("worktree not found")
	}
	if err := worktree.Remove(src.RepoRoot, info.Path, force); err != nil {
		return api.WorktreeInfo{}, err
	}
	a.emitWorktree("worktree_removed", WorktreeOutcome{Index: index, Worktree: *info}, force)
	a.exec(model.CmdCloseWorkspace{Index: index})
	return *info, nil
}

func (a *App) emitWorktree(kind string, out WorktreeOutcome, force bool) {
	if a.worktreeHook != nil {
		a.worktreeHook(kind, out, force)
	}
}

func (a *App) SetWorktreeHook(f func(string, WorktreeOutcome, bool)) { a.worktreeHook = f }
