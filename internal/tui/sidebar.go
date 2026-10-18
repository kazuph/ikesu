package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"strings"
)

func (a *App) SetSidebarConfig(c config.Config, startup bool) {
	a.sup.SetGraphics(c.Experimental.KittyGraphics)
	a.sup.SetShell(c.Terminal.DefaultShell, c.Terminal.ShellMode)
	a.sup.SetGuard(c.Guard, api.StateDir())
	a.sup.SetScrollbackBytes(c)
	a.setSidebarConfig(c, startup)
}

func (a *App) setSidebarConfig(c config.Config, startup bool) {
	a.sidebarConfig = c
	a.st.PaneGaps = c.UI.PaneGaps == nil || *c.UI.PaneGaps
	a.st.SidebarCollapsedHidden = c.UI.SidebarCollapsedMode == "hidden"
	a.setStartupConfigWarning(c.Warnings)
	a.BeginHostTheme()
	p := render.ConfigPalette(c)
	a.panePalette = &p
	a.st.MobileWidthThreshold = c.MobileWidthThreshold()
	lo, hi, n, err := c.SidebarBounds()
	a.st.SidebarMin, a.st.SidebarMax, a.st.SidebarDefault = lo, hi, n
	if !a.st.SidebarManual {
		a.st.SidebarSize = n
	}
	a.st.RightSidebarSize = 0
	if c.UI.RightSidebarWidth != nil {
		a.st.RightSidebarSize = max(lo, min(hi, *c.UI.RightSidebarWidth))
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

func (a *App) setStartupConfigWarning(warnings []string) {
	message := ""
	for _, warning := range warnings {
		if strings.Contains(warning, "unsupported:") {
			key := strings.SplitN(warning, ":", 2)[0]
			message = "config.toml: " + key + " unsupported; gtx config check"
			break
		}
		if !a.serverUI {
			if strings.HasPrefix(warning, "invalid keybinding:") {
				if message == "" {
					message = "config.toml; gtx config check"
				}
			} else {
				message = "config.toml invalid; using defaults; gtx config check"
			}
		}
	}
	a.SetConfigWarning(message)
	a.configWarningBanner = message != ""
}
func (a *App) sidebarPreset(p string) {
	a.exec(model.CmdSidebarPreset{Preset: p})
	a.saveViewPreferences()
}
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
	var edit config.Edit
	if density {
		edit = config.Edit{Kind: "sidebar", Key: key, Value: value}
	} else {
		sort := "spaces"
		if value == "sort" {
			sort = "priority"
		}
		edit = config.Edit{Kind: "agent_sort", Value: sort}
	}
	a.editConfiguration(edit, func(_ config.Config, err error) {
		if err != nil {
			a.worktreeError(err)
			return
		}
		if density {
			a.st.WorkspaceDensity = value
		} else {
			a.st.AgentScope = value
			a.agentScroll = 0
		}
		a.sidebarScroll = 0
		a.changed()
		a.dirty = true
	})
}
func (a *App) duplicateDialog(index int) {
	if _, err := a.DuplicateWorkspace(index, true); err != nil {
		a.dialog = &uiDialog{d: render.Dialog{Title: "duplicate workspace failed: " + err.Error(), Confirm: "OK", Cancel: "close"}, run: func(*App) {}}
	}
}

func (a *App) renameWorkspaceDialog(index int) {
	name := render.WorkspaceDisplayName(a.st.Workspaces[index], a.paneSnaps())
	a.dialog = &uiDialog{input: &name, d: render.Dialog{Title: "Rename workspace", Input: name, InputCursor: len([]rune(name)), InputEdit: true, Confirm: "rename", Cancel: "cancel"}, run: func(a *App) { a.exec(model.CmdRenameWorkspace{Index: index, Name: name}) }}
}
func (a *App) newTabNameDialog() {
	w := a.st.ActiveWorkspace()
	if w == nil {
		return
	}
	defaultName := fmt.Sprint(len(w.Tabs) + 1)
	name := defaultName
	a.dialog = &uiDialog{input: &name, replaceDefault: true, d: render.Dialog{Title: "New tab", Input: name, InputCursor: len([]rune(name)), InputEdit: true, Confirm: "create", Cancel: "cancel"}, run: func(a *App) {
		a.exec(model.CmdNewTab{Workspace: a.st.Active})
		w := a.st.ActiveWorkspace()
		if w != nil && name != defaultName {
			a.exec(model.CmdRenameTab{Workspace: a.st.Active, Tab: w.ActiveTab, Label: name})
		}
	}}
}
func (a *App) unsupportedSidebar(feature string) {
	a.worktreeError(fmt.Errorf("unsupported: %s", feature))
}
func (a *App) reloadSidebarConfig() {
	a.editConfiguration(config.Edit{Kind: "reload"}, func(c config.Config, err error) {
		if err != nil {
			a.SetConfigWarning(err.Error())
			a.worktreeError(err)
			return
		}
		a.ApplyReloadedConfig(c)
	})
}
func (a *App) SetWorkspaceTokenSource(source func(int) map[string]string) {
	a.workspaceTokenSource = source
}

// CloseWorkspace returns the replacement-shell error when the last workspace must remain open.
func (a *App) CloseWorkspace(index int) error {
	a.lastSpawnError = nil
	a.exec(model.CmdCloseWorkspace{Index: index})
	return a.lastSpawnError
}
