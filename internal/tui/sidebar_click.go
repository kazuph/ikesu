package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

// sidebarClick handles clicks inside the sidebar region.
func (a *App) sidebarClick(m uv.Mouse) {
	if m.Button == uv.MouseRight {
		a.sidebarRightClick(m)
		return
	}
	if m.Button != uv.MouseLeft {
		return
	}
	if a.sidebarPress(m) {
		return
	}
	s := a.st
	if !a.serverUI && pointIn(m.X, m.Y, render.SidebarToggleRect(s)) {
		a.exec(model.CmdToggleSidebar{})
		return
	}
	if !s.SidebarVisible {
		in := render.Input{State: s, ServerUI: a.serverUI, Panes: a.paneSnaps(), SidebarConfig: a.sidebarConfig, SidebarScroll: a.sidebarScroll, AgentScroll: a.agentScroll}
		// Collapsed strip: one row per workspace.
		viewport := render.CollapsedWorkspaceViewport(in)
		if !pointIn(m.X, m.Y, viewport.Rect) {
			return
		}
		i := m.Y - viewport.Rect.Y + viewport.Offset
		if i >= 0 && i < len(s.Workspaces) {
			a.exec(model.CmdSelectWorkspace{Index: i})
		}
		return
	}
	// Footer buttons.
	if pointIn(m.X, m.Y, render.SidebarMenuRect(s)) {
		a.openMainMenu(0, render.SidebarMenuRect(s).Y)
		_, h := render.MenuSize(&a.menu.m)
		a.menu.m.Y -= h
		return
	}
	if pointIn(m.X, m.Y, render.SidebarNewRect(s)) {
		a.newWorkspace()
		return
	}
	r, ok := a.sidebarRowAt(m.X, m.Y)
	if !ok {
		return
	}

	switch r.Kind {
	case render.RowWorkspace:
		if r.Ws == s.Active {
			a.openWorkspaceMenu(m.X, m.Y, r.Ws)
		} else {
			a.exec(model.CmdSelectWorkspace{Index: r.Ws})
		}
	case render.RowAgent:
		a.exec(model.CmdSelectWorkspace{Index: r.Ws})
		a.exec(model.CmdFocusPane{Pane: r.Pane})
	}
}

// Card padding retains its workspace owner; empty panel rows stay inert.
func (a *App) sidebarRightClick(m uv.Mouse) {
	s := a.st
	if !s.SidebarVisible {
		return
	}
	if pointIn(m.X, m.Y, render.SidebarMenuRect(s)) || pointIn(m.X, m.Y, render.SidebarNewRect(s)) {
		return
	}
	r, ok := a.sidebarRowAt(m.X, m.Y)
	if ok && (r.Kind == render.RowWorkspace || r.Kind == render.RowGit || r.Kind == render.RowCardBlank) {
		a.openWorkspaceMenu(m.X, m.Y, r.Ws)
	}
}
