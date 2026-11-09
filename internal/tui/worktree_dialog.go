package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/worktree"
)

func (a *App) worktreeError(err error) {
	a.dialog = &uiDialog{d: render.Dialog{Title: "Worktree operation failed", Detail: err.Error(), Confirm: "OK", Cancel: "close"}, run: func(*App) {}}
	a.dirty = true
}

func (a *App) worktreeScreenUnchanged() func() bool {
	a.worktreeScreenRequest++
	request := a.worktreeScreenRequest
	dialog, menu, settings, help := a.dialog, a.menu, a.settings, a.help
	navigator, copyMode, selection := a.navigator, a.cm, a.sel
	return func() bool {
		return request == a.worktreeScreenRequest && a.dialog == dialog && a.menu == menu && a.settings == settings && a.help == help && a.navigator == navigator && a.cm == copyMode && a.sel == selection
	}
}

func (a *App) newWorktreeDialog(index int) {
	if index < 0 || index >= len(a.st.Workspaces) {
		return
	}
	req, ok := a.workspaceWorktreeRequest(index, "source")
	if !ok {
		return
	}
	current := a.worktreeScreenUnchanged()
	a.startWorktree(req, func(result WorktreeResult, err error) {
		if !current() {
			return
		}
		if err != nil {
			a.SetConfigWarning("ikesu worktree actions require a workspace inside a Git work tree.")
			a.configWarningBanner = true
			return
		}
		a.showNewWorktreeDialog(req, result.Source)
	})
}

func (a *App) showNewWorktreeDialog(req WorktreeRequest, src api.WorktreeSourceInfo) {
	branch := ""
	d := &uiDialog{input: &branch, replaceDefault: true, d: render.Dialog{Title: "New worktree", WorktreeCreate: true, Confirm: "create and open", Cancel: "cancel"}}
	d.changed = func() { d.d.Checkout = filepath.Join(*src.CheckoutPathPrefix, worktree.Slug(branch)) }
	d.changed()
	d.run = func(a *App) {
		d.d.Input = branch
		d.changed()
		if branch == "" {
			d.d.Detail = "enter a branch name"
			a.dialog = d
			return
		}
		create := req
		chosen := branch
		create.Operation, create.Branch, create.Focus = "create", &chosen, true
		current := a.worktreeScreenUnchanged()
		a.startWorktree(create, func(_ WorktreeResult, err error) {
			if !current() {
				return
			}
			if err != nil {
				var gitErr *worktree.GitError
				d.d.Detail = err.Error()
				if errors.As(err, &gitErr) {
					d.d.Detail = gitErr.Output
				}
				a.dialog = d
			}
		})
	}
	a.dialog = d
	a.dirty = true
}

func (a *App) openWorktreeMenu(index int) {
	req, ok := a.workspaceWorktreeRequest(index, "list")
	if !ok {
		return
	}
	current := a.worktreeScreenUnchanged()
	a.startWorktree(req, func(result WorktreeResult, err error) {
		if !current() {
			return
		}
		if err != nil {
			a.worktreeError(err)
			return
		}
		for i, w := range a.st.Workspaces {
			if w.Generation == req.ExpectedGeneration && w.Cwd == req.ExpectedCwd && fmt.Sprintf("s%d", w.ID) == *req.WorkspaceID {
				a.menu = &uiMenu{kind: menuWorktree, ws: i, wsID: w.ID, wsGeneration: w.Generation, worktreeItems: result.Items, m: render.Menu{Worktree: true, Total: len(result.Items), Hover: -1, Sel: 0}}
				a.filterWorktrees()
				a.dirty = true
				return
			}
		}
	})
}

func (a *App) deleteWorktreeDialog(index int, force bool) {
	if index < 0 || index >= len(a.st.Workspaces) {
		return
	}
	ws := a.st.Workspaces[index]
	id, shown, generation := ws.ID, ws.Cwd, ws.Generation
	title := "Delete worktree checkout?"
	detail := ws.Cwd
	if force {
		title = "Discard changes and delete checkout?"
		detail = "Git refused the safe removal. Force removal discards uncommitted files: " + ws.Cwd
	}
	dialog := render.Dialog{Title: title, Detail: detail, Confirm: "delete", Cancel: "cancel"}
	if !force {
		dialog = render.Dialog{Title: "delete worktree checkout?", Detail: "This removes the checkout folder:", Lines: []string{ws.Cwd, "The branch is not deleted. The IKESU workspace will close.", "", "", ""}, Width: 72, CompactButtons: true, CenterTerminal: true, Confirm: "remove", Cancel: "cancel"}
	}
	a.dialog = &uiDialog{d: dialog, run: func(a *App) {
		for i, w := range a.st.Workspaces {
			if w.ID != id || w.Generation != generation {
				continue
			}
			// The space follows its first pane's shell, so it may point at
			// another checkout than the one this dialog showed.
			if !worktree.SamePath(w.Cwd, shown) {
				a.worktreeError(fmt.Errorf("not deleted: this space moved to %s after the dialog opened; open delete again", w.Cwd))
				return
			}
			req, _ := a.workspaceWorktreeRequest(i, "remove")
			req.ExpectedCwd, req.Force = shown, force
			a.startWorktree(req, func(_ WorktreeResult, err error) {
				if err != nil {
					a.worktreeError(err)
				}
			})
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

func (a *App) workspaceWorktreeItems(index int, src api.WorktreeSourceInfo) []render.MenuItem {
	ws := a.st.Workspaces[index]
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

func (a *App) closeWorkspaceTargets(index int) []model.WorkspaceID {
	ws := a.st.Workspaces[index]
	ids := []model.WorkspaceID{ws.ID}
	if ws.Worktree == nil || ws.Worktree.IsLinkedWorktree {
		return ids
	}
	for _, other := range a.st.Workspaces {
		if other.ID != ws.ID && other.Worktree != nil && other.Worktree.RepoKey == ws.Worktree.RepoKey {
			ids = append(ids, other.ID)
		}
	}
	return ids
}
