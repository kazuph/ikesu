package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/api"

	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

// openPaneMenu shows the per-pane right-click menu at (x, y). Layout ops
// appear only when the tab holds more than one pane (SPEC G1 pane menu).
func (a *App) openPaneMenu(x, y, pid int) {
	multi := false
	zoomed := false
	if ws := a.st.ActiveWorkspace(); ws != nil {
		if t := ws.TabRef(); t != nil {
			multi = len(t.Layout.PaneIDs()) > 1
			zoomed = t.Zoomed == model.PaneID(pid)
		}
	}
	items := []render.MenuItem{
		{Label: "Split vertical"},
		{Label: "Split horizontal"},
		{Separator: true},
		{Label: "Rename pane"},
	}
	if a.st.PaneLabels[model.PaneID(pid)] != "" {
		items = append(items, render.MenuItem{Label: "Clear pane name"})
	}
	items = append(items,
		render.MenuItem{Separator: true},
		render.MenuItem{Label: "New Claude Code agent"},
		render.MenuItem{Label: "New Codex agent"},
		render.MenuItem{Label: "New agy agent"},
		render.MenuItem{Separator: true},
	)
	if multi {
		items = append(items,
			render.MenuItem{Label: "Move to left split"},
			render.MenuItem{Label: "Move to right split"},
			render.MenuItem{Label: "Move to upper split"},
			render.MenuItem{Label: "Move to lower split"},
			render.MenuItem{Label: "Equalize pane sizes"},
			render.MenuItem{Label: "Cycle pane layout"},
			render.MenuItem{Label: "Rotate panes"},
			render.MenuItem{Label: "Rotate panes reverse"},
			render.MenuItem{Separator: true},
		)
		if zoomed {
			items = append(items, render.MenuItem{Label: "Unzoom"})
		} else {
			items = append(items, render.MenuItem{Label: "Zoom"})
		}
	}
	items = append(items, render.MenuItem{Label: "Close pane"})
	a.menu = &uiMenu{justOpened: true,
		kind: menuPane,
		pane: pid,
		m: render.Menu{
			X: x, Y: y, Hover: -1, Sel: -1,
			Items: items,
		},
	}
}

// openRenamePaneDialog prompts for a name for the clicked pane.
func (a *App) openRenamePaneDialog(pid int) {
	name := a.st.PaneLabels[model.PaneID(pid)]
	a.dialog = &uiDialog{
		input: &name,
		d: render.Dialog{
			Title:   "Rename pane",
			Confirm: "rename", Cancel: "cancel",
			Input: name, InputEdit: true,
		},
		run: func(a *App) {
			a.exec(model.CmdRenamePane{Pane: model.PaneID(pid), Label: name})
		},
	}
	a.dirty = true
}

// agentPane splits the focused pane's tab to the right and runs argv in the
// new pane (menu "New * agent" items).
func (a *App) agentPane(argv ...string) {
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return
	}
	a.exec(model.CmdSplitImplicit{
		Workspace: a.st.Active, Tab: ws.ActiveTab,
		Dir: layout.Vertical, Focus: true,
		Argv: argv,
	})
}

// openWorkspaceMenu shows the workspace menu (clicking the active row).
func (a *App) openWorkspaceMenu(x, y, ws int) {
	items := []render.MenuItem{{Label: "New Claude Code agent"}, {Label: "New Codex agent"}, {Label: "New agy agent"}, {Separator: true}}
	wt := a.workspaceWorktreeItems(ws)
	var group []render.MenuItem
	for _, it := range wt {
		if it.Label == "Collapse" || it.Label == "Expand" {
			group = append(group, it)
		} else {
			items = append(items, it)
		}
	}
	items = append(items, render.MenuItem{Label: "Duplicate"}, render.MenuItem{Separator: true}, render.MenuItem{Label: "Rename"}, render.MenuItem{Label: "Close"})
	items = append(items, group...)
	items = append(items, render.MenuItem{Separator: true}, render.MenuItem{Label: "⭐ favorite"}, render.MenuItem{Label: "💼 work"}, render.MenuItem{Label: "🏠 personal"}, render.MenuItem{Label: "No section"})
	a.menu = &uiMenu{justOpened: true, kind: menuWorkspace, ws: ws, m: render.Menu{X: x, Y: y, Hover: -1, Sel: -1, Items: items}}
}

// openTabMenu shows the right-click menu for a tab cell.
func (a *App) openTabMenu(x, y, ws, ti int) {
	a.menu = &uiMenu{justOpened: true,
		kind: menuTab,
		ws:   ws, tab: ti,
		m: render.Menu{
			X: x, Y: y, Hover: -1, Sel: -1,
			Items: []render.MenuItem{
				{Label: "Rename tab…"},
				{Label: "New tab"},
				{Separator: true},
				{Label: "Close tab"},
			},
		},
	}
}

// openMainMenu shows the [menu] global menu.
func (a *App) openMainMenu(x, y int) {
	a.menu = &uiMenu{justOpened: true, kind: menuMain, m: render.Menu{X: x, Y: y, Hover: -1, Sel: -1, Items: []render.MenuItem{
		{Label: "new workspace"}, {Label: "new tab"}, {Separator: true}, {Label: "settings"}, {Label: "keybinds"}, {Label: "reload config"}, {Label: "sidebar narrow"}, {Label: "sidebar normal"}, {Label: "sidebar wide"}, {Separator: true}, {Label: "restore agents...", Disabled: a.restoreHook == nil}, {Separator: true}, {Label: "detach", Disabled: a.detacher == nil}, {Separator: true}, {Label: "stop server"}, {Label: "restart", Disabled: a.restartHook == nil},
	}}}
}

// activateMenu runs the command for menu item i and closes the menu.
func (a *App) activateMenu(i int) {
	m := a.menu
	a.menu = nil
	if m == nil || i < 0 || i >= len(m.m.Items) || m.m.Items[i].Separator || m.m.Items[i].Disabled {
		return
	}
	switch m.kind {
	case menuPane:
		pid := m.pane
		a.exec(model.CmdFocusPane{Pane: pid})
		switch m.m.Items[i].Label {
		case "Split vertical":
			a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
		case "Split horizontal":
			a.exec(model.CmdSplitFocused{Dir: layout.Horizontal})
		case "Rename pane":
			a.openRenamePaneDialog(pid)
		case "Clear pane name":
			a.exec(model.CmdRenamePane{Pane: model.PaneID(pid)})
		case "New Claude Code agent":
			a.agentPane("claude")
		case "New Codex agent":
			a.agentPane("codex")
		case "New agy agent":
			a.agentPane("agy")
		case "Move to left split":
			a.exec(model.CmdArrange{Pane: model.PaneID(pid), Op: "move_left"})
		case "Move to right split":
			a.exec(model.CmdArrange{Pane: model.PaneID(pid), Op: "move_right"})
		case "Move to upper split":
			a.exec(model.CmdArrange{Pane: model.PaneID(pid), Op: "move_up"})
		case "Move to lower split":
			a.exec(model.CmdArrange{Pane: model.PaneID(pid), Op: "move_down"})
		case "Equalize pane sizes":
			a.exec(model.CmdArrange{Pane: model.PaneID(pid), Op: "equalize"})
		case "Cycle pane layout":
			a.exec(model.CmdArrange{Pane: model.PaneID(pid), Op: "cycle"})
		case "Rotate panes":
			a.exec(model.CmdArrange{Pane: model.PaneID(pid), Op: "rotate_forward"})
		case "Rotate panes reverse":
			a.exec(model.CmdArrange{Pane: model.PaneID(pid), Op: "rotate_backward"})
		case "Zoom", "Unzoom":
			a.exec(model.CmdZoomPane{Pane: model.PaneID(pid), Mode: "toggle"})
		case "Close pane":
			a.closeFocusedPane()
		}
	case menuWorktree:
		if i < len(m.worktreePaths) {
			id := fmt.Sprintf("s%d", a.st.Workspaces[m.ws].ID)
			path := m.worktreePaths[i]
			_, err := a.OpenWorktree(api.WorktreeOpenParams{WorkspaceID: &id, Path: &path, Focus: true})
			if err != nil {
				a.worktreeError(err)
			}
		}
	case menuWorkspace:
		switch m.m.Items[i].Label {
		case "New Claude Code agent", "New Codex agent", "New agy agent":
			a.exec(model.CmdSelectWorkspace{Index: m.ws})
			agent := "claude"
			if m.m.Items[i].Label == "New Codex agent" {
				agent = "codex"
			}
			if m.m.Items[i].Label == "New agy agent" {
				agent = "agy"
			}
			a.agentPane(agent)
		case "Duplicate":
			a.duplicateDialog(m.ws)
		case "Rename":
			a.renameWorkspaceDialog(m.ws)
		case "⭐ favorite":
			a.setSection(m.ws, model.SectionFavorite)
		case "💼 work":
			a.setSection(m.ws, model.SectionWork)
		case "🏠 personal":
			a.setSection(m.ws, model.SectionPersonal)
		case "No section":
			a.setSection(m.ws, model.SectionNone)
		case "New workspace":
			a.newWorkspace()
		case "Close workspace…", "Close":
			a.openCloseWorkspaceDialog(m.ws)
		case "New worktree":
			a.newWorktreeDialog(m.ws)
		case "Open worktree...":
			a.openWorktreeMenu(m.ws)
		case "Delete worktree checkout...":
			a.deleteWorktreeDialog(m.ws, false)
		case "Move to worktree group":
			a.groupWorktree(m.ws)
		case "Collapse", "Expand":
			a.exec(model.CmdToggleWorktreeGroup{Index: m.ws})
		}
	case menuTab:
		switch i {
		case 0:
			a.openRenameTabDialog(m.ws, m.tab)
		case 1:
			a.exec(model.CmdNewTab{Workspace: m.ws})
		case 3:
			a.closeTabAt(m.ws, m.tab)
		}
	case menuMain:
		switch m.m.Items[i].Label {
		case "new workspace":
			a.newWorkspace()
		case "new tab":
			a.newTabNameDialog()
		case "settings":
			a.unsupportedSidebar("settings popup")
		case "keybinds":
			a.openHelp()
		case "reload config":
			a.reloadSidebarConfig()
		case "sidebar narrow":
			a.sidebarPreset("narrow")
		case "sidebar normal":
			a.sidebarPreset("normal")
		case "sidebar wide":
			a.sidebarPreset("wide")
		case "restore agents...":
			a.openRestoreDialog()
		case "detach":
			if a.detacher != nil {
				a.detacher("detached")
			}
		case "stop server":
			a.openQuitDialog()
		case "restart":
			a.openRestartDialog()
		}
	}

}

// menuItemAt returns the item index under (x, y) or -1.
func (a *App) menuItemAt(x, y int) int {
	if a.menu == nil {
		return -1
	}
	r := render.MenuRect(&a.menu.m, a.st.Cols, a.st.Rows)
	i := y - r.Y - 1
	if x < r.X || x >= r.X+r.W || i < 0 || i >= len(a.menu.m.Items) {
		return -1
	}
	return i
}

// menuMouse handles hover and click/release inside a menu.
func (a *App) menuMouse(ev input.Event) {
	m := ev.Mouse
	i := a.menuItemAt(m.X, m.Y)
	switch ev.Kind {
	case input.KindMouseMotion:
		if i >= 0 && !a.menu.m.Items[i].Separator {
			a.menu.m.Hover = i
			a.menu.m.Sel = i
		} else {
			a.menu.m.Hover = -1
		}
	case input.KindMouseRelease:
		if a.menu.justOpened {
			a.menu.justOpened = false
			return
		}
		if i >= 0 {
			a.activateMenu(i)
		}
	}
}

// menuKey handles Up/Down/Enter/Esc while a menu is open.
func (a *App) menuKey(k uv.Key) {
	m := a.menu
	if m == nil {
		return
	}
	step := func(dir int) {
		n := len(m.m.Items)
		sel := m.m.Sel
		for j := 0; j < n; j++ {
			sel = (sel + dir + n) % n
			if !m.m.Items[sel].Separator && !m.m.Items[sel].Disabled {
				m.m.Sel = sel
				return
			}
		}
	}
	switch {
	case k.Code == uv.KeyEscape:
		a.menu = nil
	case k.Code == uv.KeyUp:
		if m.m.Sel < 0 {
			m.m.Sel = len(m.m.Items)
		}
		step(-1)
	case k.Code == uv.KeyDown:
		if m.m.Sel < 0 {
			m.m.Sel = -1
		}
		step(1)
	case k.Code == uv.KeyEnter:
		if m.m.Sel >= 0 {
			a.activateMenu(m.m.Sel)
		}
	}
	a.dirty = true
}

// dialogMouse resolves clicks on the confirmation dialog.
func (a *App) dialogMouse(ev input.Event) {
	if ev.Kind != input.KindMouseClick && ev.Kind != input.KindMouseRelease {
		return
	}
	m := ev.Mouse
	_, cancel, confirm := render.DialogRects(&a.dialog.d, a.st.Cols, a.st.Rows)
	switch {
	case pointIn(m.X, m.Y, confirm):
		if ev.Kind == input.KindMouseClick {
			d := a.dialog
			a.dialog = nil
			d.run(a)
		}
	case pointIn(m.X, m.Y, cancel):
		a.dialog = nil
	default:
		a.dialog = nil
	}
}
