package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

type sidebarGesture struct {
	ws, x, y               int
	active, moved, divider bool
	panelDivider           bool
	gitRow                 bool
	section                model.Section
	drop                   int
}

func (a *App) sidebarRowAt(x, y int) (render.SidebarRow, bool) {
	area := a.st.SidebarArea()
	if x < area.X || x >= area.X+area.W-1 {
		return render.SidebarRow{}, false
	}
	view := a.sidebarView()
	for _, pos := range view.Rows {
		if y == pos.Y && !pos.Right {
			return pos.Row, true
		}
	}
	return render.SidebarRow{}, false
}
func (a *App) sidebarPress(m uv.Mouse) bool {
	s := a.st
	if !s.SidebarVisible {
		if m.X == s.SidebarArea().W-1 && m.Button == uv.MouseLeft {
			s.SidebarVisible = true
			s.SidebarHidden = false
			a.syncLayout()
			a.saveViewPreferences()
			a.changed()
			return true
		}
		return false
	}
	if m.X == s.SidebarArea().W-1 && m.Button == uv.MouseLeft {
		a.sidebarGesture = &sidebarGesture{divider: true, x: m.X, y: m.Y}
		return true
	}
	if m.Button != uv.MouseLeft {
		return false
	}
	if a.sidebarScrollbarPress(m) {
		return true
	}
	if pointIn(m.X, m.Y, render.SidebarWidthRect(s)) {
		a.sidebarPreset(s.CyclePreset())
		return true
	}
	r, ok := a.sidebarRowAt(m.X, m.Y)
	if !ok {
		return false
	}
	switch r.Kind {
	case render.RowJob:
		a.openJobLog(r.Line)
		return true
	case render.RowTitle:
		if a.serverUI {
			return true
		}
		_, _, w, _ := render.SidebarContent(s.SidebarArea())
		if m.X >= s.SidebarArea().X+w-6 {
			a.toggleSidebarSetting(true)
		}
		return true
	case render.RowSep:
		if !a.serverUI && !a.jobsPanel && pointIn(m.X, m.Y, render.SidebarAgentSortRect(s, m.Y)) {
			a.toggleSidebarSetting(false)
			return true
		}
		if pointIn(m.X, m.Y, render.SidebarJobsTab(s.SidebarArea().W, m.Y)) {
			if !a.jobsPanel {
				a.agentScroll = 0
			}
			a.jobsPanel = true
			a.changed()
			return true
		}
		if m.X >= 1 && m.X < 1+len("[agents]") {
			if a.jobsPanel {
				a.agentScroll = 0
			}
			a.jobsPanel = false
			a.changed()
			return true
		}
		return true
	case render.RowSection:
		ix, _, w, _ := render.SidebarContent(s.SidebarArea())
		if pointIn(m.X, m.Y, render.SectionNewRect(ix, m.Y, w)) {
			a.newSectionWorkspace(r.Section)
		} else {
			a.toggleSection(r.Section)
		}
		return true
	case render.RowWorkspace:
		a.sidebarGesture = &sidebarGesture{ws: r.Ws, x: m.X, y: m.Y, active: r.Ws == s.Active, drop: -1}
		if a.snapshotSink != nil && r.Ws != s.Active {
			a.exec(model.CmdSelectWorkspace{Index: r.Ws})
		}
		return true
	case render.RowGit, render.RowCardBlank:
		a.sidebarGesture = &sidebarGesture{ws: r.Ws, x: m.X, y: m.Y, active: r.Ws == s.Active, drop: -1, gitRow: r.Kind == render.RowGit}
		if a.snapshotSink != nil && r.Ws != s.Active {
			a.exec(model.CmdSelectWorkspace{Index: r.Ws})
		}
		if a.serverUI && s.WorkspaceDensity == "slim" && r.Kind == render.RowGit {
			a.exec(model.CmdSelectWorkspace{Index: r.Ws})
		}
		return true
	case render.RowRule, render.RowBlank:
		return true
	}
	return false
}
func (a *App) sidebarGestureMouse(ev input.Event) {
	g := a.sidebarGesture
	if g == nil {
		return
	}
	m := ev.Mouse
	if ev.Kind == input.KindMouseMotion {
		if m.X != g.x || m.Y != g.y {
			g.moved = true
		}
		if g.divider {
			lo, hi, _ := a.st.SidebarBounds()
			a.st.SidebarSize = max(lo, min(hi, m.X+1))
			a.st.SidebarManual = true
			a.syncLayout()
			a.changed()
			return
		}
		g.section = ""
		g.drop = -1
		r, ok := a.sidebarRowAt(m.X, m.Y)
		if !ok {
			return
		}
		source := a.st.Workspaces[g.ws].SidebarSection()
		if r.Kind == render.RowSection || r.Kind == render.RowBlank {
			if r.Section != source {
				g.section = r.Section
			}
			return
		}
		if r.Kind == render.RowWorkspace || r.Kind == render.RowGit || r.Kind == render.RowCardBlank {
			dest := a.st.Workspaces[r.Ws].SidebarSection()
			if dest != source {
				g.section = dest
				if a.snapshotSink != nil {
					g.drop = r.Ws
				}
			} else {
				g.drop = r.Ws
				if m.Y > g.y {
					g.drop++
				}
			}
		}
	}
	if ev.Kind == input.KindMouseRelease {
		a.sidebarGesture = nil
		if g.divider {
			if !g.moved {
				a.st.SidebarVisible = false
				a.st.SidebarHidden = false
				a.syncLayout()
				a.changed()
			}
			a.saveViewPreferences()
			return
		}
		if g.moved {
			if g.section != "" {
				a.setSection(g.ws, g.section)
			}
			if g.drop >= 0 && (g.section == "" || a.snapshotSink != nil) {
				to := g.drop
				if to > g.ws {
					to--
				}
				a.exec(model.CmdMoveWorkspace{Index: g.ws, InsertIndex: to})
			}
			return
		}
		r, ok := a.sidebarRowAt(m.X, m.Y)
		if !ok || m.Y != g.y || r.Ws != g.ws || (r.Kind != render.RowWorkspace && r.Kind != render.RowGit) {
			return
		}
		if g.active {
			if a.serverUI && a.st.WorkspaceDensity == "slim" && r.Kind == render.RowGit {
				return
			}
			a.openWorkspaceMenu(m.X, m.Y, g.ws)
			a.menu.justOpened = false
		} else {
			a.exec(model.CmdSelectWorkspace{Index: g.ws})
		}
	}
}
