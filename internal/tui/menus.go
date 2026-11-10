package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"

	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
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
		render.MenuItem{Label: "New grok agent"},
		render.MenuItem{Label: "New letta agent"},
		render.MenuItem{Label: "New qwen agent"},
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
	items = append(items,
		render.MenuItem{Label: menuMovePaneToTab},
		render.MenuItem{Label: menuMovePaneToSpace},
		render.MenuItem{Separator: true},
		render.MenuItem{Label: "Close pane"})
	a.menu = &uiMenu{justOpened: true,
		kind: menuPane,
		pane: pid,
		m: render.Menu{
			X: x, Y: y, Hover: -1, Sel: 0,
			Items: items,
		},
	}
}

// openRenamePaneDialog prompts for a name for the clicked pane.
func (a *App) openRenamePaneDialog(pid int) {
	if a.st.Overlay != nil && pid == a.st.Overlay.PaneID {
		a.ShowToast("rename pane", "unsupported: overlay pane names are transient", "error", -1)
		return
	}
	name := a.st.PaneLabels[model.PaneID(pid)]
	a.dialog = &uiDialog{
		input: &name,
		d: render.Dialog{
			Title:   "Rename pane",
			Confirm: "rename", Cancel: "cancel",
			Input: name, InputCursor: len([]rune(name)), InputEdit: true,
		},
		run: func(a *App) {
			a.exec(model.CmdRenamePane{Pane: model.PaneID(pid), Label: name})
		},
	}
	a.dirty = true
}

// agentPane splits the focused pane's tab to the right and runs argv in the
// new pane (menu "New * agent" items).
func (a *App) agentPane(agent string) {
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return
	}
	argv, configured := a.sidebarConfig.AgentStart.Commands[agent]
	if !configured {
		argv = []string{agent}
	}
	if len(argv) == 0 || argv[0] == "" {
		a.worktreeError(fmt.Errorf("agent preset %s has no command", agent))
		return
	}
	cwd := ws.Cwd
	if p := a.sup.Get(a.focusedPane()); p != nil {
		cwd = p.SpawnCwd()
	}
	target := a.focusedPane()
	env := make(map[string]string, len(a.sidebarConfig.Agent.Env))
	for k, v := range a.sidebarConfig.Agent.Env {
		env[k] = v
	}
	argv = append([]string(nil), argv...)
	launch := func() {
		wi, _ := a.st.FindPane(target)
		if wi < 0 {
			a.worktreeError(fmt.Errorf("agent target pane no longer exists"))
			return
		}
		a.lastSpawnError = nil
		a.exec(model.CmdSplitPane{
			Workspace: wi, Pane: target, Dir: layout.Vertical,
			Ratio: 0.5, Focus: true, Cwd: cwd, Env: env, Argv: append([]string{}, argv...), AgentCommand: true,
		})
		pid := a.focusedPane()
		if a.lastSpawnError != nil {
			err := a.lastSpawnError
			a.exec(model.CmdClosePane{Pane: pid})
			a.worktreeError(err)
			return
		}
		if p := a.sup.Get(pid); p != nil {
			p.SetAgent(agent)
		}
		a.SetReported(pid, agent, "unknown")
	}
	if _, err := a.sup.LoginPath(); err == nil {
		launch()
		return
	}
	go func() {
		err := a.sup.WaitLoginPath()
		a.Do(func() {
			if err != nil {
				a.worktreeError(err)
				return
			}
			launch()
		})
	}()
}

// openWorkspaceMenu shows the workspace menu (clicking the active row).
func (a *App) setWorkspaceMenuSection(index int, section model.Section) {
	if index < 0 || index >= len(a.st.Workspaces) {
		a.worktreeError(fmt.Errorf("workspace no longer exists"))
		return
	}
	ws := a.st.Workspaces[index]
	selected := a.isOyagoiWorkspace(ws)
	a.setSection(index, section)
	if ws.Section != section {
		a.worktreeError(fmt.Errorf("workspace section change failed"))
		return
	}
	if selected && a.machineRequest != nil {
		if err := a.machineRequest(fmt.Sprintf("oyagoi:unset:%d:%s", ws.ID, ws.Generation)); err != nil {
			a.worktreeError(err)
		}
	}
}

func (a *App) openWorkspaceMenu(x, y, ws int) {
	if ws < 0 || ws >= len(a.st.Workspaces) {
		return
	}
	req, _ := a.workspaceWorktreeRequest(ws, "source")
	a.buildWorkspaceMenu(x, y, ws, nil)
	menu := a.menu
	a.startWorktree(req, func(result WorktreeResult, err error) {
		if err != nil || a.menu != menu {
			return
		}
		for i, w := range a.st.Workspaces {
			if w.ID == menu.wsID && w.Generation == menu.wsGeneration && w.Cwd == req.ExpectedCwd {
				selected := ""
				if menu.m.Sel >= 0 && menu.m.Sel < len(menu.m.Items) {
					selected = menu.m.Items[menu.m.Sel].Label
				}
				a.buildWorkspaceMenu(x, y, i, &result.Source)
				a.menu.justOpened = menu.justOpened
				for j, item := range a.menu.m.Items {
					if item.Label == selected && !item.Separator {
						a.menu.m.Sel = j
						break
					}
				}
				a.dirty = true
				return
			}
		}
	})
}

func (a *App) buildWorkspaceMenu(x, y, ws int, source *api.WorktreeSourceInfo) {
	items := []render.MenuItem{{Label: "New Claude Code agent"}, {Label: "New Codex agent"}, {Label: "New agy agent"}, {Label: "New grok agent"}, {Label: "New letta agent"}, {Label: "New qwen agent"}, {Separator: true}}
	var wt []render.MenuItem
	if source != nil {
		wt = a.workspaceWorktreeItems(ws, *source)
	}
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
	items = append(items, render.MenuItem{Separator: true})
	if a.machineRequest != nil {
		items = append(items, render.MenuItem{Label: "🎏 HQ"})
	}
	items = append(items, render.MenuItem{Label: "⭐ favorite"}, render.MenuItem{Label: "💼 work"}, render.MenuItem{Label: "🏠 personal"}, render.MenuItem{Label: "No section"})
	target := a.st.Workspaces[ws]
	a.menu = &uiMenu{justOpened: true, kind: menuWorkspace, ws: ws, wsID: target.ID, wsGeneration: target.Generation, m: render.Menu{X: x, Y: y, Hover: -1, Sel: 0, Items: items}}
}

// openTabMenu shows the right-click menu for a tab cell.
func (a *App) openTabMenu(x, y, ws, ti int) {
	a.menu = &uiMenu{justOpened: true,
		kind: menuTab,
		ws:   ws, tab: ti,
		m: render.Menu{
			// The recorded 0.2.24 tab menu is fourteen columns with New tab selected.
			X: x, Y: y, Hover: -1, Sel: 0, MinWidth: 14,
			Items: a.tabMenuItems(ws, ti),
		},
	}
}

// tabMenuItems: the recorded New tab/Rename/Close plus the move entries.
func (a *App) tabMenuItems(ws, ti int) []render.MenuItem {
	n := 0
	if ws >= 0 && ws < len(a.st.Workspaces) {
		n = len(a.st.Workspaces[ws].Tabs)
	}
	return []render.MenuItem{
		{Label: "New tab"},
		{Label: "Rename"},
		{Separator: true},
		{Label: menuMoveTabLeft, Disabled: ti <= 0},
		{Label: menuMoveTabRight, Disabled: ti >= n-1},
		{Label: menuMoveTabToSpace, Disabled: len(a.st.Workspaces) < 2},
		{Separator: true},
		{Label: "Close"},
	}
}

func (a *App) mainMenuItems() []render.MenuItem {
	koiLabel := "koi: off"
	if a.sidebarConfig.KoiEnabled() {
		koiLabel = "koi: on"
	}
	items := []render.MenuItem{
		{Label: "new workspace"}, {Label: "new tab"}, {Label: "delete idle panes…"}, {Separator: true}, {Label: "settings"}, {Label: "keybinds"}, {Label: "reload config"}, {Label: koiLabel}, {Label: "sidebar narrow"}, {Label: "sidebar normal"}, {Label: "sidebar wide"}, {Separator: true}, {Label: "restore agents...", Disabled: a.restoreHook == nil}, {Separator: true}, {Label: "detach", Disabled: a.detacher == nil}, {Separator: true}, {Label: "add machine..."}, {Label: "remove machine..."}, {Separator: true}, {Label: "stop server"}, {Label: "restart", Disabled: a.restartHook == nil},
	}
	if a.clientMachines || a.machineRequest != nil {
		var base, machines []render.MenuItem
		for _, item := range items {
			if item.Label == "add machine..." || item.Label == "remove machine..." {
				machines = append(machines, item)
			} else if !item.Separator || len(base) == 0 || !base[len(base)-1].Separator {
				base = append(base, item)
			}
		}
		items = append(append(base, render.MenuItem{Separator: true}), machines...)
	}
	return items
}

// openMainMenu shows the [menu] global menu.
func (a *App) openMainMenu(x, y int) {
	a.menu = &uiMenu{justOpened: true, kind: menuMain, m: render.Menu{X: x, Y: y, Hover: -1, Sel: 0, Items: a.mainMenuItems(), Global: true}}
}

// activateMenu runs the command for menu item i and closes the menu.
func (a *App) activateMenu(i int) {
	m := a.menu
	a.menu = nil
	if m == nil || i < 0 || i >= len(m.m.Items) || m.m.Items[i].Separator || m.m.Items[i].Disabled {
		return
	}
	switch m.kind {
	case menuOyagoiUnset:
		m.unsetOyagoi()
	case menuMachineManage:
		if m.m.Items[i].Label == "add machine..." {
			a.addMachineDialog()
		} else {
			a.removeMachineDialog(m.machines[0])
		}
	case menuMachineRemove:
		if i < len(m.machines) {
			a.removeMachineDialog(m.machines[i])
		}
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
		case "New grok agent":
			a.agentPane("grok")
		case "New letta agent":
			a.agentPane("letta")
		case "New qwen agent":
			a.agentPane("qwen")
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
		case menuMovePaneToTab:
			if wi, _ := a.st.FindPane(model.PaneID(pid)); wi >= 0 {
				a.openPaneTabMenu(m.m.X, m.m.Y, pid, a.st.Workspaces[wi].ID)
			}
		case menuMovePaneToSpace:
			a.openPaneSpaceMenu(m.m.X, m.m.Y, pid)
		}
	case menuWorktree:
		if i < len(m.worktreePaths) {
			path := m.worktreePaths[i]
			index := -1
			for j, w := range a.st.Workspaces {
				if w.ID == m.wsID && w.Generation == m.wsGeneration {
					index = j
					break
				}
			}
			if index < 0 {
				a.worktreeError(fmt.Errorf("workspace no longer exists"))
				break
			}
			req, _ := a.workspaceWorktreeRequest(index, "open")
			req.Path, req.Focus = &path, true
			a.startWorktree(req, func(_ WorktreeResult, err error) {
				if err != nil {
					a.worktreeError(err)
				}
			})
		}
	case menuWorkspace:
		switch m.m.Items[i].Label {
		case "🎏 HQ", "⭐ favorite", "💼 work", "🏠 personal", "No section", "New worktree", "Open worktree...", "Delete worktree checkout...", "Move to worktree group", "Collapse", "Expand":
			index := -1
			for j, ws := range a.st.Workspaces {
				if ws.ID == m.wsID && ws.Generation == m.wsGeneration {
					index = j
					break
				}
			}
			if index < 0 {
				a.worktreeError(fmt.Errorf("workspace no longer exists"))
				return
			}
			m.ws = index
		}
		switch m.m.Items[i].Label {
		case "🎏 HQ":
			if a.machineRequest != nil && m.ws >= 0 && m.ws < len(a.st.Workspaces) {
				ws := a.st.Workspaces[m.ws]
				if err := a.machineRequest(fmt.Sprintf("oyagoi:set:%d:%s", ws.ID, ws.Generation)); err != nil {
					a.worktreeError(err)
				}
			}
		case "New Claude Code agent", "New Codex agent", "New agy agent", "New grok agent", "New letta agent", "New qwen agent":
			a.exec(model.CmdSelectWorkspace{Index: m.ws})
			agent := "claude"
			if m.m.Items[i].Label == "New Codex agent" {
				agent = "codex"
			}
			if m.m.Items[i].Label == "New agy agent" {
				agent = "agy"
			}
			if m.m.Items[i].Label == "New grok agent" {
				agent = "grok"
			}
			if m.m.Items[i].Label == "New qwen agent" {
				agent = "qwen"
			}
			if m.m.Items[i].Label == "New letta agent" {
				agent = "letta"
			}
			a.agentPane(agent)
		case "Duplicate":
			a.duplicateDialog(m.ws)
		case "Rename":
			a.renameWorkspaceDialog(m.ws)
		case "⭐ favorite":
			a.setWorkspaceMenuSection(m.ws, model.SectionFavorite)
		case "💼 work":
			a.setWorkspaceMenuSection(m.ws, model.SectionWork)
		case "🏠 personal":
			a.setWorkspaceMenuSection(m.ws, model.SectionPersonal)
		case "No section":
			a.setWorkspaceMenuSection(m.ws, model.SectionNone)
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
		switch m.m.Items[i].Label {
		case "Rename":
			a.exec(model.CmdSelectTab{Workspace: m.ws, Tab: m.tab})
			a.openRenameTabDialog(m.ws, m.tab)
		case "New tab":
			a.exec(model.CmdNewTab{Workspace: m.ws})
		case menuMoveTabLeft:
			a.exec(model.CmdMoveTab{Workspace: m.ws, Index: m.tab, InsertIndex: m.tab - 1})
		case menuMoveTabRight:
			a.exec(model.CmdMoveTab{Workspace: m.ws, Index: m.tab, InsertIndex: m.tab + 2})
		case menuMoveTabToSpace:
			a.openTabSpaceMenu(m.m.X, m.m.Y, m.ws, m.tab)
		case "Close":
			a.closeTabAt(m.ws, m.tab)
		}
	case menuMove:
		if i < len(m.actions) && m.actions[i] != nil {
			m.actions[i]()
		}
	case menuMain:
		if a.clientMachines && m.m.Items[i].Label != "koi: on" && m.m.Items[i].Label != "koi: off" && m.m.Items[i].Label != "add machine..." && m.m.Items[i].Label != "remove machine..." && m.m.Items[i].Label != "delete idle panes…" {
			a.machineError(fmt.Errorf("machine is disconnected; select a connected space to use %s", m.m.Items[i].Label))
			return
		}
		switch m.m.Items[i].Label {
		case "delete idle panes…":
			if a.machineRequest != nil {
				if err := a.machineRequest("idle_panes"); err != nil {
					a.machineError(err)
				}
			}
		case "add machine...":
			a.addMachineDialog()
		case "remove machine...":
			a.removeMachineMenu()
		case "new workspace":
			a.newWorkspace()
		case "new tab":
			a.newTabNameDialog()
		case "settings":
			a.openSettings()
		case "keybinds":
			a.openHelp()
		case "reload config":
			a.reloadSidebarConfig()
		case "koi: on", "koi: off":
			a.toggleKoi()
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
	if a.menu.m.Worktree {
		return render.WorktreeMenuItemAt(&a.menu.m, a.st.Cols, a.st.Rows, x, y)
	}
	r := render.MenuRect(&a.menu.m, a.st.Cols, a.st.Rows)
	row := y - r.Y - 1
	i := row + render.MenuScroll(&a.menu.m, a.st.Rows)
	if x < r.X || x >= r.X+r.W || row < 0 || row >= r.H-2 || i >= len(a.menu.m.Items) {
		return -1
	}
	return i
}

// menuMouse handles hover and click/release inside a menu.
func (a *App) menuMouse(ev input.Event) {
	m := ev.Mouse
	i := a.menuItemAt(m.X, m.Y)
	switch ev.Kind {
	case input.KindMouseClick:
		if !pointIn(m.X, m.Y, render.MenuRect(&a.menu.m, a.st.Cols, a.st.Rows)) {
			if m.Button == uv.MouseRight && (a.menu.kind == menuMain || a.serverUI && (a.menu.kind == menuTab || a.menu.kind == menuPane || a.menu.kind == menuWorkspace)) {
				return
			}
			a.menu = nil
			// Outside left presses only dismiss; right presses retarget the menu.
			if m.Button == uv.MouseRight {
				a.click(m)
			}
		}
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
	if m.m.Worktree {
		if k.Code == '/' && !m.m.Filtering {
			m.m.Filtering = true
			a.dirty = true
			return
		}
		if m.m.Filtering && (k.Text != "" || k.Code == uv.KeyBackspace) {
			if k.Code == uv.KeyBackspace {
				r := []rune(m.m.Filter)
				if len(r) > 0 {
					m.m.Filter = string(r[:len(r)-1])
				}
			} else {
				m.m.Filter += k.Text
			}
			a.filterWorktrees()
			a.dirty = true
			return
		}
	}
	step := func(dir int) {
		n := len(m.m.Items)
		sel := m.m.Sel
		for j := 0; j < n; j++ {
			if m.kind == menuMain {
				sel = (sel + dir + n) % n
			} else {
				sel += dir
				if sel < 0 || sel >= n {
					return
				}
			}
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
	if a.dialog.d.WorktreeCreate {
		if ev.Kind != input.KindMouseClick || ev.Mouse.Button != uv.MouseLeft {
			return
		}
		r := render.WorktreeCreateRect(a.st.Cols, a.st.Rows)
		confirm, cancel := render.WorktreeCreateButtons(r)
		if pointIn(ev.Mouse.X, ev.Mouse.Y, confirm) {
			d := a.dialog
			a.dialog = nil
			d.run(a)
		} else if pointIn(ev.Mouse.X, ev.Mouse.Y, cancel) || !pointIn(ev.Mouse.X, ev.Mouse.Y, r) {
			a.cancelDialog()
		}
		a.dirty = true
		return
	}
	if a.dialog.d.InputEdit && ev.Kind == input.KindMouseRelease {
		return
	}
	if ev.Kind != input.KindMouseClick && ev.Kind != input.KindMouseRelease {
		return
	}
	m := ev.Mouse
	if a.dialog.d.InputEdit {
		r, _, _ := render.DialogRects(&a.dialog.d, layout.Rect{W: a.st.Cols, H: a.st.Rows})
		buttons := render.NameInputButtons(r)
		if m.Button == uv.MouseLeft && pointIn(m.X, m.Y, buttons[0]) {
			d := a.dialog
			a.dialog = nil
			d.run(a)
		} else if m.Button == uv.MouseLeft && pointIn(m.X, m.Y, buttons[1]) {
			a.dialogKey(uv.Key{Code: 'c', Mod: uv.ModCtrl})
		} else if m.Button == uv.MouseLeft && pointIn(m.X, m.Y, buttons[2]) && !a.dialog.machineForm {
			a.dialog = nil
		}
		return
	}
	if m.Button == uv.MouseRight {
		return
	}
	area := a.st.PanesArea()
	if a.dialog.d.CenterTerminal {
		area = layout.Rect{W: a.st.Cols, H: a.st.Rows}
	}
	outer, cancel, confirm := render.DialogRects(&a.dialog.d, area)
	switch {
	case pointIn(m.X, m.Y, confirm):
		if ev.Kind == input.KindMouseClick {
			d := a.dialog
			a.dialog = nil
			d.run(a)
		}
	case pointIn(m.X, m.Y, cancel):
		a.cancelDialog()
	default:
		if a.dialog.d.CenterTerminal && pointIn(m.X, m.Y, outer) {
			return
		}
		a.cancelDialog()
	}
}
