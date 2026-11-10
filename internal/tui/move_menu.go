package tui

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

// Move menus: the tab and pane right-click menus open a second menu at the
// same spot that lists where to move. Targets are kept by id and resolved
// when chosen, so a list opened before another change still moves the
// right tab or pane (or does nothing when the target is gone).

const (
	menuMoveTabToSpace  = "Move to space…"
	menuMoveTabLeft     = "Move tab left"
	menuMoveTabRight    = "Move tab right"
	menuMovePaneToTab   = "Move to tab…"
	menuMovePaneToSpace = "Move pane to space…"
	menuMoveNewTab      = "New tab"
)

func (a *App) openMoveMenu(x, y int, title string, labels []string, actions []func()) {
	items := make([]render.MenuItem, len(labels))
	for i, l := range labels {
		items[i] = render.MenuItem{Label: l}
	}
	a.menu = &uiMenu{kind: menuMove, actions: actions, m: render.Menu{X: x, Y: y, Title: title, Hover: -1, Sel: 0, Items: items}}
	a.dirty = true
}

// wsIndexByID returns the current index of workspace id, or -1.
func (a *App) wsIndexByID(id int) int {
	for i, w := range a.st.Workspaces {
		if w.ID == id {
			return i
		}
	}
	return -1
}

// tabIndexByID returns the indices of tab id, or -1, -1.
func (a *App) tabIndexByID(id int) (int, int) {
	for i, w := range a.st.Workspaces {
		for j, t := range w.Tabs {
			if t.ID == id {
				return i, j
			}
		}
	}
	return -1, -1
}

// openTabSpaceMenu lists the other spaces for the tab at (wi, ti).
func (a *App) openTabSpaceMenu(x, y, wi, ti int) {
	if wi < 0 || wi >= len(a.st.Workspaces) || ti < 0 || ti >= len(a.st.Workspaces[wi].Tabs) {
		return
	}
	tabID := a.st.Workspaces[wi].Tabs[ti].ID
	var labels []string
	var actions []func()
	for i, w := range a.st.Workspaces {
		if i == wi {
			continue
		}
		destID := w.ID
		labels = append(labels, fmt.Sprintf("%d %s", i+1, a.WorkspaceLabel(i)))
		actions = append(actions, func() {
			sw, st := a.tabIndexByID(tabID)
			dw := a.wsIndexByID(destID)
			if sw < 0 || dw < 0 {
				a.ShowToast("tab move failed", "the tab or space no longer exists", "error", a.focusedPane())
				return
			}
			a.exec(model.CmdMoveTabToWorkspace{Workspace: sw, Index: st, Dest: dw, Focus: true})
		})
	}
	if len(labels) == 0 {
		a.ShowToast("tab move", "there is no other space", "info", a.focusedPane())
		return
	}
	a.openMoveMenu(x, y, "move tab to", labels, actions)
}

// openPaneTabMenu lists the tabs of space wsID (other than the pane's own
// tab) and "New tab" as destinations for pane pid.
func (a *App) openPaneTabMenu(x, y, pid, wsID int) {
	wi := a.wsIndexByID(wsID)
	if wi < 0 {
		return
	}
	pw, pt := a.st.FindPane(model.PaneID(pid))
	var labels []string
	var actions []func()
	move := func(dest func() (model.MoveDest, bool)) func() {
		return func() {
			d, ok := dest()
			if !ok {
				a.ShowToast("pane move failed", "the tab or space no longer exists", "error", a.focusedPane())
				return
			}
			out := &model.PaneMoveOutcome{}
			a.exec(model.CmdMovePane{Pane: model.PaneID(pid), Dest: d, Focus: true, Out: out})
			if !out.Changed {
				reason := out.Reason
				if reason == "zoomed_tab" {
					reason = "unzoom the tab first"
				} else if reason == "" {
					reason = "the pane no longer exists"
				}
				a.ShowToast("pane move failed", reason, "error", a.focusedPane())
			}
		}
	}
	for j, t := range a.st.Workspaces[wi].Tabs {
		if wi == pw && j == pt {
			continue
		}
		tabID := t.ID
		label := fmt.Sprintf("tab %d", j+1) // same number as the tab strip
		if t.Name != "" {
			label += " " + t.Name
		}
		labels = append(labels, label)
		actions = append(actions, move(func() (model.MoveDest, bool) {
			w, ti := a.tabIndexByID(tabID)
			return model.MoveDest{Kind: model.MoveDestTab, TabW: w, TabT: ti, Split: layout.Vertical}, w >= 0
		}))
	}
	labels = append(labels, menuMoveNewTab)
	actions = append(actions, move(func() (model.MoveDest, bool) {
		w := a.wsIndexByID(wsID)
		return model.MoveDest{Kind: model.MoveDestNewTab, Workspace: w}, w >= 0
	}))
	title := "move pane to"
	if wi != pw {
		title = "move pane to " + a.WorkspaceLabel(wi)
	}
	a.openMoveMenu(x, y, title, labels, actions)
}

// openPaneSpaceMenu lists the other spaces; choosing one opens its tab list.
func (a *App) openPaneSpaceMenu(x, y, pid int) {
	pw, _ := a.st.FindPane(model.PaneID(pid))
	var labels []string
	var actions []func()
	for i, w := range a.st.Workspaces {
		if i == pw {
			continue
		}
		id := w.ID
		labels = append(labels, fmt.Sprintf("%d %s", i+1, a.WorkspaceLabel(i)))
		actions = append(actions, func() { a.openPaneTabMenu(x, y, pid, id) })
	}
	if len(labels) == 0 {
		a.ShowToast("pane move", "there is no other space", "info", a.focusedPane())
		return
	}
	a.openMoveMenu(x, y, "move pane to space", labels, actions)
}
