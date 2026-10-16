package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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
	src, err := a.WorktreeSource("", &id)
	if err != nil {
		a.SetConfigWarning("gtx worktree actions require a workspace inside a Git work tree.")
		a.configWarningBanner = true
		return
	}
	branch := ""
	d := &uiDialog{input: &branch, replaceDefault: true, d: render.Dialog{Title: "New worktree", WorktreeCreate: true, Confirm: "create and open", Cancel: "cancel"}}
	d.changed = func() { d.d.Checkout = filepath.Join(*src.CheckoutPathPrefix, worktree.Slug(branch)) }
	d.changed()
	d.run = func(a *App) {
		d.d.Input = branch
		d.changed()
		_, err := a.CreateWorktree(api.WorktreeCreateParams{WorkspaceID: &id, Branch: &branch, Focus: true})
		if err != nil {
			var gitErr *worktree.GitError
			d.d.Detail = err.Error()
			if errors.As(err, &gitErr) {
				d.d.Detail = gitErr.Output
			}
			a.dialog = d
		}
	}
	a.dialog = d
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
	a.menu = &uiMenu{kind: menuWorktree, ws: index, worktreeItems: items, m: render.Menu{Worktree: true, Hover: -1, Sel: 0}}
	a.filterWorktrees()
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
			if index < i {
				target--
			}
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

func (a *App) filterWorktrees() {
	m := a.menu
	m.m.Items = nil
	m.worktreePaths = nil
	for _, item := range m.worktreeItems {
		if item.IsBare || item.IsPrunable {
			continue
		}
		label := item.Path
		if item.Branch != nil {
			label = *item.Branch
		}
		if !strings.Contains(strings.ToLower(label+" "+item.Path), strings.ToLower(m.m.Filter)) {
			continue
		}
		status := ""
		if item.OpenWorkspaceID != nil {
			status = "open"
		}
		m.m.Items = append(m.m.Items, render.MenuItem{Label: label, Detail: item.Path, Status: status})
		m.worktreePaths = append(m.worktreePaths, item.Path)
	}
	m.m.Sel = 0
	if len(m.m.Items) == 0 {
		m.m.Sel = -1
	}
	m.m.Scroll = 0
}
