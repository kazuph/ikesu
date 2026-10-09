package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"time"
)

type sidebarGesture struct {
	ws, x, y               int
	active, moved, divider bool
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
		if y == pos.Y {
			return pos.Row, true
		}
	}
	return render.SidebarRow{}, false
}
func (a *App) sidebarPress(m uv.Mouse) bool {
	s := a.st
	if !s.SidebarVisible {
		return false
	}
	if m.X == s.SidebarArea().W-1 && m.Button == uv.MouseLeft {
		now := time.Now()
		if !a.dividerAt.IsZero() && now.Sub(a.dividerAt) <= 350*time.Millisecond {
			a.sidebarPreset("normal")
			a.sidebarGesture = nil
			a.dividerAt = time.Time{}
			return true
		}
		a.dividerAt = now
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
	case render.RowTitle:
		_, _, w, _ := render.SidebarContent(s.SidebarArea())
		if m.X >= s.SidebarArea().X+w-6 {
			a.toggleSidebarSetting(true)
		}
		return true
	case render.RowSep:
		if !a.jobsPanel && pointIn(m.X, m.Y, render.SidebarAgentSortRect(s, m.Y)) {
			a.toggleSidebarSetting(false)
			return true
		}
		if pointIn(m.X, m.Y, render.SidebarJobsTab(s.SidebarArea().W, m.Y)) {
			a.jobsPanel = true
			a.changed()
			return true
		}
		if m.X >= 1 && m.X < 1+len("[agents]") {
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
		return true
	case render.RowGit, render.RowCardBlank:
		if s.WorkspaceDensity != "slim" {
			a.sidebarGesture = &sidebarGesture{ws: r.Ws, x: m.X, y: m.Y, active: r.Ws == s.Active, drop: -1}
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
			return
		}
		if g.moved {
			if g.section != "" {
				a.setSection(g.ws, g.section)
			} else if g.drop >= 0 {
				to := g.drop
				if to > g.ws {
					to--
				}
				a.exec(model.CmdMoveWorkspace{Index: g.ws, InsertIndex: to})
			}
			return
		}
		r, ok := a.sidebarRowAt(m.X, m.Y)
		if !ok || m.Y != g.y || r.Ws != g.ws || (r.Kind != render.RowWorkspace && (r.Kind != render.RowGit || a.st.WorkspaceDensity == "slim")) {
			return
		}
		if g.active {
			a.openWorkspaceMenu(m.X, m.Y, g.ws)
			a.menu.justOpened = false
		} else {
			a.exec(model.CmdSelectWorkspace{Index: g.ws})
		}
	}
}
