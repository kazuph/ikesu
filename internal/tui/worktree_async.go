package tui

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/worktree"
)

type WorktreeRequest struct {
	Operation                 string
	Cwd                       string
	WorkspaceID               *string
	ExpectedGeneration        string
	ExpectedCwd               string
	Branch, Path, Base, Label *string
	Focus, Force              bool
}

type WorktreeResult struct {
	Source              api.WorktreeSourceInfo
	Items               []api.WorktreeInfo
	Outcome             WorktreeOutcome
	Removed             api.WorktreeInfo
	WorkspaceID         int
	WorkspaceGeneration string
}

type workspaceIdentity struct {
	id         int
	generation string
}

func (a *App) workspaceWorktreeRequest(index int, operation string) (WorktreeRequest, bool) {
	if index < 0 || index >= len(a.st.Workspaces) {
		return WorktreeRequest{}, false
	}
	w := a.st.Workspaces[index]
	id := fmt.Sprintf("s%d", w.ID)
	return WorktreeRequest{Operation: operation, WorkspaceID: &id, ExpectedGeneration: w.Generation, ExpectedCwd: w.Cwd}, true
}

func (a *App) startWorktree(req WorktreeRequest, complete func(WorktreeResult, error)) {
	go func() {
		result, err := a.ExecuteWorktree(req)
		a.Do(func() { complete(result, err) })
	}()
}

// ExecuteWorktree must be called outside the app loop. Git runs between two
// app-loop snapshots; a changed workspace never receives the old result.
func (a *App) ExecuteWorktree(req WorktreeRequest) (result WorktreeResult, err error) {
	var target *workspaceGitTarget
	var directory, appCwd string
	var reserved *workspaceIdentity
	prepared := false
	a.Do(func() {
		prepared = true
		if req.WorkspaceID != nil && req.Cwd != "" {
			err = fmt.Errorf("workspace_id and cwd are mutually exclusive")
			return
		}
		var w *model.Workspace
		if req.WorkspaceID != nil {
			ref, ok := api.ParseWorkspaceRef(*req.WorkspaceID)
			if !ok {
				err = fmt.Errorf("workspace not found")
				return
			}
			for i, candidate := range a.st.Workspaces {
				if candidate.ID == ref.Number || ref.Index == i+1 {
					w = candidate
					break
				}
			}
			if w == nil {
				err = fmt.Errorf("workspace not found")
				return
			}
		} else if req.Cwd == "" {
			w = a.st.ActiveWorkspace()
		}
		if w != nil {
			if req.ExpectedGeneration != "" && (w.Generation != req.ExpectedGeneration || w.Cwd != req.ExpectedCwd) {
				err = fmt.Errorf("workspace changed during worktree operation")
				return
			}
			t := a.captureWorkspaceGitTarget(w)
			target = &t
			req.Cwd = w.Cwd
			if req.Operation == "create" || req.Operation == "remove" {
				key := workspaceIdentity{id: w.ID, generation: w.Generation}
				if a.worktreePending == nil {
					a.worktreePending = map[workspaceIdentity]int{}
				}
				a.worktreePending[key]++
				reserved = &key
			}
		}
		if req.Cwd == "" {
			req.Cwd = a.cwd
		}
		directory, appCwd = a.sidebarConfig.Worktrees.Directory, a.cwd
	})
	if !prepared {
		return result, fmt.Errorf("app stopped")
	}
	if err != nil {
		return result, err
	}
	if reserved != nil {
		defer a.Do(func() {
			a.worktreePending[*reserved]--
			if a.worktreePending[*reserved] == 0 {
				delete(a.worktreePending, *reserved)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-a.doneCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	src, err := worktree.SourceContext(ctx, req.Cwd)
	if err != nil {
		return result, err
	}
	if directory != "" {
		if directory == "~" || strings.HasPrefix(directory, "~/") {
			home, _ := os.UserHomeDir()
			directory = filepath.Join(home, strings.TrimPrefix(directory, "~/"))
		}
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(appCwd, directory)
		}
		prefix := filepath.Join(directory, src.RepoName)
		src.CheckoutPathPrefix = &prefix
	}
	items, err := worktree.ListContext(ctx, src.RepoRoot)
	if err != nil {
		return result, err
	}
	path := value(req.Path)
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(src.SourceCheckoutPath, path)
	}
	var selected *api.WorktreeInfo
	if req.Operation == "open" || req.Operation == "remove" {
		if req.Operation == "remove" {
			path = src.SourceCheckoutPath
		}
		if req.Operation == "open" && (req.Path == nil) == (req.Branch == nil) {
			return result, fmt.Errorf("exactly one of path or branch is required")
		}
		for i := range items {
			if path != "" && worktree.SamePath(path, items[i].Path) || req.Branch != nil && items[i].Branch != nil && *req.Branch == *items[i].Branch {
				selected = &items[i]
				break
			}
		}
		if selected == nil {
			return result, fmt.Errorf("worktree not found")
		}
		if selected.IsBare {
			return result, fmt.Errorf("cannot open a bare repository as a checkout")
		}
		path = selected.Path
	}
	valid := func() bool {
		if target == nil {
			return true
		}
		for _, w := range a.st.Workspaces {
			if a.currentWorkspaceGitTarget(w, *target) {
				return true
			}
		}
		return false
	}
	sameWorkspace := func(checkCwd bool) bool {
		if target == nil {
			return true
		}
		for _, w := range a.st.Workspaces {
			if w.ID == target.id && w.Generation == target.generation && (!checkCwd || w.Cwd == target.cwd) {
				return true
			}
		}
		return false
	}
	if req.Operation == "create" || req.Operation == "remove" {
		ok := false
		a.Do(func() { ok = sameWorkspace(true) })
		if !ok {
			return result, fmt.Errorf("workspace changed during worktree operation")
		}
		if req.Operation == "create" {
			branch := value(req.Branch)
			if branch == "" {
				branch = "worktree/" + rand.Text()
			}
			if path == "" {
				path = filepath.Join(*src.CheckoutPathPrefix, worktree.Slug(branch))
			}
			path = filepath.Clean(path)
			err = worktree.CreateContext(ctx, src.RepoRoot, path, branch, value(req.Base))
		} else {
			err = worktree.RemoveContext(ctx, src.RepoRoot, path, req.Force)
		}
		if err != nil {
			return result, err
		}
		if req.Operation == "create" {
			items, err = worktree.ListContext(ctx, src.RepoRoot)
			if err != nil {
				return result, err
			}
		}
	}
	applied := false
	a.Do(func() {
		applied = true
		matches := valid()
		if req.Operation == "remove" {
			matches = sameWorkspace(false)
		}
		if req.Operation == "create" {
			matches = sameWorkspace(true)
		}
		if !matches {
			if req.Operation == "create" || req.Operation == "remove" {
				err = fmt.Errorf("Git completed, but workspace changed; checkout operation was not applied to the workspace: %s", path)
			} else {
				err = fmt.Errorf("workspace changed during worktree operation")
			}
			return
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
		for _, w := range a.st.Workspaces {
			if worktree.SamePath(w.Cwd, src.SourceCheckoutPath) {
				id := fmt.Sprintf("s%d", w.ID)
				src.SourceWorkspaceID = &id
				break
			}
		}
		result.Source, result.Items = src, items
		switch req.Operation {
		case "create", "open":
			result.Outcome, err = a.openWorktreeItems(src, path, req.Label, req.Focus, items)
			if err == nil {
				w := a.st.Workspaces[result.Outcome.Index]
				result.WorkspaceID, result.WorkspaceGeneration = w.ID, w.Generation
				kind := "worktree_opened"
				if req.Operation == "create" {
					kind = "worktree_created"
				}
				a.emitWorktree(kind, result.Outcome, false)
			}
		case "remove":
			for i, w := range a.st.Workspaces {
				if target != nil && w.ID == target.id && w.Generation == target.generation {
					result.Removed = *selected
					a.emitWorktree("worktree_removed", WorktreeOutcome{Index: i, Worktree: *selected}, req.Force)
					a.exec(model.CmdCloseWorkspace{Index: i})
					return
				}
			}
			err = fmt.Errorf("workspace not found")
		}
	})
	if !applied {
		return result, fmt.Errorf("app stopped")
	}
	return result, err
}
