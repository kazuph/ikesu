package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

type sidebarScrollDrag struct {
	agents bool
	grab   int
}

func (a *App) sidebarView() render.SidebarView {
	in := render.Input{ServerUI: a.serverUI, State: a.st, Panes: a.paneSnaps(), SidebarConfig: a.sidebarConfig, SidebarScroll: a.sidebarScroll, AgentScroll: a.agentScroll, Jobs: a.jobRows, JobsPanel: a.jobsPanel, JobScroll: a.jobScroll}
	if a.workspaceNavigate {
		in.SelectedWorkspace = &a.navigateSelected
	}
	return render.SidebarViewForInput(in)
}

func (a *App) revealServerWorkspace() {
	if a.st.SidebarArea().W == 0 {
		return
	}
	view := a.sidebarView()
	for _, pos := range view.Rows {
		if pos.Row.Kind == render.RowWorkspace && pos.Row.Ws == a.st.Active {
			return
		}
	}
	headings, cards := 0, 0
	for _, row := range render.SidebarRows(a.st, a.paneSnaps(), a.sidebarConfig)[2:] {
		if row.Kind == render.RowSep {
			return
		}
		if row.Kind == render.RowSection || row.Kind == render.RowRule {
			headings++
			continue
		}
		if row.Kind == render.RowWorkspace && row.Ws == a.st.Active {
			available := max(1, view.Workspace.Rect.H-headings-1)
			if cards < view.Workspace.Offset {
				a.sidebarScroll = cards
			} else {
				a.sidebarScroll = max(0, cards+1-available)
			}
			return
		}
		cards++
	}
}

func (a *App) sidebarWheel(m uv.Mouse, up bool) {
	if a.legacyCompactSidebar() && pointIn(m.X, m.Y, a.st.RightSidebarArea()) {
		if up {
			a.agentScroll--
		} else {
			a.agentScroll++
		}
		a.agentScroll = max(0, min(a.agentScroll, max(0, len(render.LegacyRightSidebarRows(a.rightSidebarInput()))-a.st.RightSidebarArea().H)))
		return
	}
	view := a.sidebarView()
	v, off := view.Workspace, &a.sidebarScroll
	step := 3
	if a.st.WorkspaceDensity == "slim" && a.sidebarConfig.UI.Sidebar.Spaces.Rows == nil {
		step = 2 // The observed slim workspace card has a name row and a git row.
	}
	if pointIn(m.X, m.Y, view.Agents.Rect) {
		v, off = view.Agents, &a.agentScroll
		if a.serverUI {
			step = 1
		}
		if a.jobsPanel {
			off = &a.jobScroll
		}
	}
	if !pointIn(m.X, m.Y, v.Rect) {
		return
	}
	if up {
		*off -= step
	} else {
		*off += step
	}
	*off = max(0, min(*off, max(0, v.Total-v.Rect.H)))
}

func (a *App) sidebarScrollbarPress(m uv.Mouse) bool {
	if a.serverUI {
		return false
	}
	view := a.sidebarView()
	for i, v := range []render.SidebarViewport{view.Workspace, view.Agents} {
		if v.Rect.W < model.SidebarMinDefault-1 || v.Total <= v.Rect.H || v.Rect.H <= 0 || v.Rect.W <= 1 || m.X != v.Rect.X+v.Rect.W-1 || !pointIn(m.X, m.Y, v.Rect) {
			continue
		}
		h, top := render.SidebarThumb(v)
		rel := m.Y - v.Rect.Y
		grab := rel - top
		if rel < top || rel >= top+h {
			grab = h / 2
		}
		a.sidebarBar = &sidebarScrollDrag{agents: i == 1, grab: grab}
		a.sidebarScrollbarMouse(input.Event{Kind: input.KindMouseMotion, Mouse: m})
		return true
	}
	return false
}

func (a *App) sidebarScrollbarMouse(ev input.Event) {
	if ev.Kind == input.KindMouseRelease {
		a.sidebarBar = nil
		return
	}
	if ev.Kind != input.KindMouseMotion || a.sidebarBar == nil {
		return
	}
	view := a.sidebarView()
	v, off := view.Workspace, &a.sidebarScroll
	if a.sidebarBar.agents {
		v, off = view.Agents, &a.agentScroll
		if a.jobsPanel {
			off = &a.jobScroll
		}
	}
	h, _ := render.SidebarThumb(v)
	travel := v.Rect.H - h
	if travel <= 0 {
		*off = 0
		return
	}
	top := max(0, min(ev.Mouse.Y-v.Rect.Y-a.sidebarBar.grab, travel))
	*off = top * (v.Total - v.Rect.H) / travel
}
