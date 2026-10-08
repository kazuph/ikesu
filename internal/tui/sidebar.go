package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func (a *App) SetSidebarConfig(c config.Config, startup bool) {
	a.sidebarConfig = c
	lo, hi, n, err := c.SidebarBounds()
	a.st.SidebarMin, a.st.SidebarMax, a.st.SidebarDefault = lo, hi, n
	if !a.st.SidebarManual {
		a.st.SidebarSize = n
	}
	a.st.WorkspaceDensity = c.WorkspaceDensity()
	a.st.AgentScope = c.AgentScope()
	if startup && c.UI.SidebarStartCollapsed {
		a.st.SidebarVisible = false
	}
	if err != nil {
		a.worktreeError(err)
	}
	a.dirty = true
}
func (a *App) sidebarPreset(p string) { a.exec(model.CmdSidebarPreset{Preset: p}) }
func (a *App) cycleWorkspace(dir int) {
	order := a.st.VisibleWorkspaceOrder()
	if len(order) == 0 {
		return
	}
	at := -1
	for i, w := range order {
		if w == a.st.Active {
			at = i
			break
		}
	}
	if at < 0 {
		if dir > 0 {
			at = -1
		} else {
			at = 0
		}
	}
	a.exec(model.CmdSelectWorkspace{Index: order[(at+dir+len(order))%len(order)]})
}
func (a *App) DuplicateWorkspace(index int, focus bool) (int, error) {
	if index < 0 || index >= len(a.st.Workspaces) {
		return -1, fmt.Errorf("workspace not found")
	}
	original := a.st.ActiveWorkspace().ID
	a.lastSpawnError = nil
	cwds := map[int]string{}
	for _, t := range a.st.Workspaces[index].Tabs {
		for _, id := range t.Layout.PaneIDs() {
			if p := a.sup.Get(id); p != nil {
				cwds[id] = p.SpawnCwd()
				if cwd := p.Snapshot().Cwd; cwd != "" {
					cwds[id] = cwd
				}
			}
		}
	}
	a.exec(model.CmdDuplicateWorkspace{Index: index, Focus: focus, Cwds: cwds})
	if a.lastSpawnError != nil {
		err := a.lastSpawnError
		a.exec(model.CmdCloseWorkspace{Index: len(a.st.Workspaces) - 1})
		for i, w := range a.st.Workspaces {
			if w.ID == original {
				a.exec(model.CmdSelectWorkspace{Index: i})
				break
			}
		}
		return -1, err
	}
	return len(a.st.Workspaces) - 1, nil
}
func (a *App) setSection(index int, section model.Section) {
	a.exec(model.CmdSetSection{Index: index, Section: section})
	a.sidebarScroll = 0
}
func (a *App) toggleSection(section model.Section) {
	a.exec(model.CmdToggleSection{Section: section})
	a.sidebarScroll = 0
}
func (a *App) newSectionWorkspace(section model.Section) {
	a.newWorkspace()
	a.setSection(a.st.Active, section)
}
func (a *App) toggleSidebarSetting(density bool) {
	key, value := "agent_panel_scope", "sort"
	if a.st.AgentScope == "sort" {
		value = "all"
	}
	if density {
		key = "workspace_panel_density"
		value = "slim"
		if a.st.WorkspaceDensity == "slim" {
			value = "full"
		}
	}
	if err := config.SaveSidebarSetting(key, value); err != nil {
		a.worktreeError(err)
		return
	}
	if density {
		a.st.WorkspaceDensity = value
	} else {
		a.st.AgentScope = value
	}
	a.sidebarScroll = 0
	a.changed()
	a.dirty = true
}
func (a *App) duplicateDialog(index int) {
	if _, err := a.DuplicateWorkspace(index, true); err != nil {
		a.dialog = &uiDialog{d: render.Dialog{Title: "duplicate workspace failed: " + err.Error(), Confirm: "OK", Cancel: "close"}, run: func(*App) {}}
	}
}

func (a *App) renameWorkspaceDialog(index int) {
	name := a.st.Workspaces[index].Name
	a.dialog = &uiDialog{input: &name, d: render.Dialog{Title: "Rename workspace", Input: name, InputEdit: true, Confirm: "rename", Cancel: "cancel"}, run: func(a *App) { a.exec(model.CmdRenameWorkspace{Index: index, Name: name}) }}
}
func (a *App) newTabNameDialog() {
	name := ""
	a.dialog = &uiDialog{input: &name, d: render.Dialog{Title: "New tab", InputEdit: true, Confirm: "create", Cancel: "cancel"}, run: func(a *App) {
		a.exec(model.CmdNewTab{Workspace: a.st.Active})
		w := a.st.ActiveWorkspace()
		if w != nil {
			a.exec(model.CmdRenameTab{Workspace: a.st.Active, Tab: w.ActiveTab, Label: name})
		}
	}}
}
func (a *App) unsupportedSidebar(feature string) {
	a.worktreeError(fmt.Errorf("unsupported: %s", feature))
}
func (a *App) reloadSidebarConfig() {
	c, err := config.Load()
	if err != nil {
		a.worktreeError(err)
		return
	}
	a.SetSidebarConfig(c, false)
	a.SetUIFlags(c.ShowTabBar(), c.HideTabBarWhenSingleTab(), c.PaneBorders())
	a.SetKeybinds(c.CopyKeys(), c.SplitVerticalKeys(), c.SplitHorizontalKeys())
	a.SetPaneKeybinds(c)
	a.syncLayout()
	a.changed()
}
func (a *App) SetWorkspaceTokenSource(source func(int) map[string]string) {
	a.workspaceTokenSource = source
}
