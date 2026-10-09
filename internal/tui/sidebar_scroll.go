package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/render"
)

type sidebarScrollDrag struct {
	agents bool
	grab   int
}

func (a *App) sidebarView() render.SidebarView {
	return render.SidebarViewForInput(render.Input{ServerUI: a.serverUI, State: a.st, Panes: a.paneSnaps(), SidebarConfig: a.sidebarConfig, SidebarScroll: a.sidebarScroll, AgentScroll: a.agentScroll, Jobs: a.jobRows, JobsPanel: a.jobsPanel, JobScroll: a.jobScroll})
}

func (a *App) sidebarWheel(m uv.Mouse, up bool) {
	if !a.st.SidebarVisible {
		if up {
			a.sidebarScroll -= 3
		} else {
			a.sidebarScroll += 3
		}
		a.sidebarScroll = a.clampCollapsedScroll(len(a.st.Workspaces), max(0, a.st.SidebarArea().H-1))
		return
	}
	view := a.sidebarView()
	v, off := view.Workspace, &a.sidebarScroll
	step := 3
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
		if v.Total <= v.Rect.H || v.Rect.H <= 0 || m.X != v.Rect.X+v.Rect.W-1 || !pointIn(m.X, m.Y, v.Rect) {
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
