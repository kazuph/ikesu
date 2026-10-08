package tui

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/worktree"
)

func (a *App) worktreeError(err error) {
	a.dialog = &uiDialog{d: render.Dialog{Title: "Worktree operation failed", Detail: err.Error(), Confirm: "OK", Cancel: "close"}, run: func(*App) {}}
	a.dirty = true
}

func (a *App) newWorktreeDialog(index int) {
	if index < 0 || index >= len(a.st.Workspaces) {
		return
	}
	id := fmt.Sprintf("s%d", a.st.Workspaces[index].ID)
	branch := ""
	a.dialog = &uiDialog{input: &branch, d: render.Dialog{Title: "New worktree", Detail: "New or existing branch name", InputEdit: true, Confirm: "create", Cancel: "cancel"}, run: func(a *App) {
		if branch == "" {
			a.worktreeError(fmt.Errorf("enter a branch name"))
			return
		}
		_, err := a.CreateWorktree(api.WorktreeCreateParams{WorkspaceID: &id, Branch: &branch, Focus: true})
		if err != nil {
			a.worktreeError(err)
		}
	}}
	a.dirty = true
}

func (a *App) openWorktreeMenu(index int) {
	id := fmt.Sprintf("s%d", a.st.Workspaces[index].ID)
	src, err := a.WorktreeSource("", &id)
	if err != nil {
		a.worktreeError(err)
		return
	}
	items, err := a.WorktreeList(src)
	if err != nil {
		a.worktreeError(err)
		return
	}
	menu := []render.MenuItem{}
	paths := []string{}
	for _, w := range items {
		if w.IsBare || w.IsPrunable {
			continue
		}
		label := w.Path
		if w.Branch != nil {
			label = *w.Branch
		}
		menu = append(menu, render.MenuItem{Label: label})
		paths = append(paths, w.Path)
	}
	a.menu = &uiMenu{kind: menuWorktree, ws: index, worktreePaths: paths, m: render.Menu{X: 2, Y: 2, Hover: -1, Sel: -1, Items: menu}}
	a.dirty = true
}

func (a *App) deleteWorktreeDialog(index int, force bool) {
	if index < 0 || index >= len(a.st.Workspaces) {
		return
	}
	ws := a.st.Workspaces[index]
	id := ws.ID
	title := "Delete worktree checkout?"
	detail := ws.Cwd
	if force {
		title = "Discard changes and delete checkout?"
		detail = "Git refused the safe removal. Force removal discards uncommitted files: " + ws.Cwd
	}
	a.dialog = &uiDialog{d: render.Dialog{Title: title, Detail: detail, Confirm: "delete", Cancel: "cancel"}, run: func(a *App) {
		for i, w := range a.st.Workspaces {
			if w.ID != id {
				continue
			}
			_, err := a.RemoveWorktree(i, force)
			if err != nil {
				a.worktreeError(err)
			}
			return
		}
	}}
	a.dirty = true
}

func (a *App) groupWorktree(index int) {
	ws := a.st.Workspaces[index]
	if ws.Worktree == nil {
		return
	}
	for i, w := range a.st.Workspaces {
		if w.Worktree != nil && w.Worktree.RepoKey == ws.Worktree.RepoKey && !w.Worktree.IsLinkedWorktree {
			target := i + 1
			if target >= len(a.st.Workspaces) {
				target = len(a.st.Workspaces) - 1
			}
			a.exec(model.CmdMoveWorkspace{Index: index, InsertIndex: target})
			return
		}
	}
}

func (a *App) workspaceWorktreeItems(index int) []render.MenuItem {
	ws := a.st.Workspaces[index]
	src, err := worktree.Source(ws.Cwd)
	if err != nil {
		return nil
	}
	a.exec(model.CmdSetWorktree{Index: index, Info: worktree.Metadata(src, src.SourceCheckoutPath)})
	items := []render.MenuItem{{Label: "New worktree"}, {Label: "Open worktree..."}}
	if ws.Worktree.IsLinkedWorktree {
		items = append(items, render.MenuItem{Label: "Delete worktree checkout..."}, render.MenuItem{Label: "Move to worktree group"})
	} else {
		label := "Collapse"
		if ws.GroupCollapsed {
			label = "Expand"
		}
		items = append(items, render.MenuItem{Label: label})
	}
	return items
}
