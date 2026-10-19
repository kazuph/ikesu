package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

func (a *App) rightSidebarInput() render.Input {
	return render.Input{State: a.st, ServerUI: a.serverUI, Panes: a.paneSnaps(), SidebarConfig: a.sidebarConfig, AgentScroll: a.agentScroll, Jobs: a.jobRows, JobsPanel: a.jobsPanel, JobScroll: a.jobScroll}
}
func (a *App) rightSidebarClick(m uv.Mouse) {
	area := a.st.RightSidebarArea()
	if !pointIn(m.X, m.Y, area) {
		return
	}
	if m.Button == uv.MouseLeft && m.X == area.X {
		if area.W == model.SidebarCollapsedWidth {
			a.st.RightSidebarMode = ""
		} else {
			a.st.RightSidebarMode = "compact"
		}
		a.syncLayout()
		a.saveViewPreferences()
		a.changed()
		return
	}
	if m.Button == uv.MouseLeft && area.W > model.SidebarCollapsedWidth && a.sidebarScrollbarPress(m) {
		return
	}
	if m.Button == uv.MouseLeft && area.W > model.SidebarCollapsedWidth {
		h := render.RightSidebarHeaderFor(a.st, 0)
		if pointIn(m.X, m.Y, h.Sort) {
			a.toggleSidebarSetting(false)
			return
		}
		if pointIn(m.X, m.Y, h.Agents) || pointIn(m.X, m.Y, h.Jobs) {
			jobs := pointIn(m.X, m.Y, h.Jobs)
			if a.jobsPanel != jobs {
				a.agentScroll = 0
			}
			a.jobsPanel = jobs
			a.changed()
			return
		}
	}
	var rows []render.SidebarPosition
	if area.W == model.SidebarCollapsedWidth {
		rows = render.CollapsedAgentRows(a.rightSidebarInput())
	} else {
		rows = a.sidebarView().Rows
	}
	for _, pos := range rows {
		r := pos.Row
		if !pos.Right || m.Y != pos.Y || (r.Kind != render.RowAgent && r.Kind != render.RowJob && r.Kind != render.RowSep) {
			continue
		}
		if m.Button == uv.MouseRight {
			if r.Kind == render.RowAgent {
				a.exec(model.CmdSelectWorkspace{Index: r.Ws})
				a.exec(model.CmdFocusPane{Pane: r.Pane})
				a.openPaneMenu(m.X, m.Y, r.Pane)
			}
			return
		}
		if m.Button != uv.MouseLeft {
			return
		}
		switch r.Kind {
		case render.RowAgent:
			a.exec(model.CmdSelectWorkspace{Index: r.Ws})
			a.exec(model.CmdFocusPane{Pane: r.Pane})
		case render.RowJob:
			if area.W == model.SidebarCollapsedWidth {
				a.focusJobCaller(r.Line)
			} else {
				a.openJobLog(r.Line)
			}
		case render.RowSep:
			if area.W == model.SidebarCollapsedWidth {
				return
			}

			a.changed()
		}
		return
	}
}
func (a *App) focusJobCaller(index int) {
	if index < 0 || index >= len(a.jobRows) {
		return
	}
	caller := a.jobRows[index].CallerPane
	for wi, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			for _, id := range tab.Layout.PaneIDs() {
				if caller == fmt.Sprintf("p%d", id) {
					a.exec(model.CmdSelectWorkspace{Index: wi})
					a.exec(model.CmdFocusPane{Pane: id})
					return
				}
			}
		}
	}
}
