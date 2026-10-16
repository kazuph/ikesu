package client

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/render"
)

// Feedback belongs to the terminal, not the endpoint snapshot protocol. Input
// still reaches the server's existing gesture handler, which owns the move.
type tabFeedback struct {
	machine                    string
	workspace, tab, x, y, drop int
	moved                      bool
}

func (m *machineClient) tabDragInput(ev input.Event) (bool, error) {
	ev = m.tabMouse.cells(ev)
	if d := m.tabFeedback; d != nil {
		if ev.Kind != input.KindMouseMotion && ev.Kind != input.KindMouseRelease {
			return false, nil
		}
		if ev.Mouse.X != d.x || ev.Mouse.Y != d.y {
			d.moved = true
		}
		d.drop = render.TabDropIndex(m.regions.TabHits, ev.Mouse.X, ev.Mouse.Y)
		if ev.Kind == input.KindMouseRelease {
			m.tabFeedback = nil
		}
		if err := m.sendInput(d.machine, eventBytes(ev)); err != nil {
			return true, err
		}
		return true, m.paint()
	}
	e := m.endpoints[m.active]
	if ev.Kind != input.KindMouseClick || ev.Mouse.Button != uv.MouseLeft || e.snapshot == nil || e.snapshot.Menu != nil || e.snapshot.Dialog != nil || e.snapshot.Help != nil || e.snapshot.Settings != nil {
		return false, nil
	}
	ws := e.snapshot.State.ActiveWorkspace()
	if ws == nil {
		return false, nil
	}
	for i, r := range m.regions.TabHits {
		if i < len(ws.Tabs) && contains(r.X, r.Y, r.W, r.H, ev.Mouse.X, ev.Mouse.Y) {
			m.tabFeedback = &tabFeedback{machine: m.active, workspace: ws.ID, tab: ws.Tabs[i].ID, x: ev.Mouse.X, y: ev.Mouse.Y, drop: -1}
			break
		}
	}
	return false, nil
}

func (m *machineClient) drawTabFeedback(out *render.Output) {
	d := m.tabFeedback
	if d == nil {
		return
	}
	e := m.endpoints[m.active]
	ws := e.snapshot.State.ActiveWorkspace()
	valid := d.machine == m.active && ws != nil && ws.ID == d.workspace
	if valid {
		valid = false
		for _, tab := range ws.Tabs {
			valid = valid || tab.ID == d.tab
		}
	}
	if !valid {
		m.tabFeedback = nil
		return
	}
	if d.moved {
		render.DrawTabDrop(out, d.drop)
	}
}
