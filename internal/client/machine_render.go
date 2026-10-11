package client

import (
	"reflect"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/render"
)

func (m *machineClient) paint() error {
	e := m.endpoints[m.active]
	if e.snapshot == nil {
		return nil
	}
	in := *e.snapshot
	m.sidebar.Groups = m.sidebar.Groups[:0]
	m.sidebar.Active = m.active
	for _, id := range m.order {
		group := m.endpoints[id]
		input := group.snapshot
		if !group.profile.Enabled {
			input = nil
		}
		m.sidebar.Groups = append(m.sidebar.Groups, render.MachineGroup{ID: id, Label: group.profile.Label, Status: group.status, Reason: group.reason, Input: input})
	}
	var composed *render.Output
	if len(m.sidebar.Groups) == 1 {
		composed = render.Compose(in)
		m.sidebar.Hits = m.sidebar.Hits[:0]
	} else {
		composed = render.ComposeMachines(in, &m.sidebar)
	}
	f := composed.Frame
	if e.connection == nil {
		area := in.State.PanesArea()
		f.Fill(area.X, area.Y, area.W, area.H, " ", uv.Style{Fg: f.Palette.Text})
		lines := []string{e.profile.Label + ": " + e.status, e.reason, "手元のspaceは使える"}
		y := area.Y
		for _, line := range lines {
			y = frameLines(f, area.X, y, area.W, line)
		}
		composed.AnchorVisible = false
	}
	if m.notice != "" {
		x, y, end := composed.ActionBar.Copy.X+composed.ActionBar.Copy.W, composed.ActionBar.Area.Y, composed.ActionBar.Cycle.X
		if end-x < len([]rune(m.notice)) {
			x = 1
			y = f.H - 1
			end = max(x, in.State.SidebarArea().W-1)
		}
		for _, r := range m.notice {
			if x >= end {
				break
			}
			if cell := f.Cell(x, y); cell != nil {
				style := cell.Style
				style.Fg = f.Palette.Text
				f.Set(x, y, string(r), style)
			}
			x++
		}
	}
	if in.SidebarConfig.UI.HostCursor == "drawn" && composed.AnchorVisible {
		if cell := f.Cell(composed.AnchorX, composed.AnchorY); cell != nil {
			cell.Style.Attrs |= uv.AttrReverse
			composed.AnchorVisible = false
		}
	}
	if f.W != m.size.Cols || f.H != m.size.Rows {
		clipped := render.NewFrame(m.size.Cols, m.size.Rows)
		clipped.Palette = f.Palette
		for y := 0; y < min(f.H, clipped.H); y++ {
			copy(clipped.Cells[y*clipped.W:y*clipped.W+min(f.W, clipped.W)], f.Cells[y*f.W:y*f.W+min(f.W, clipped.W)])
		}
		composed.Frame = clipped
		composed.AnchorVisible = composed.AnchorVisible && composed.AnchorX < clipped.W && composed.AnchorY < clipped.H
	}
	data := m.frames.Frame(composed.Frame, composed.AnchorX, composed.AnchorY, composed.AnchorVisible)
	if _, err := m.out.Write(data); err != nil {
		return err
	}
	if err := m.title.painted(m.out, in); err != nil {
		return err
	}
	composed.Frame = nil
	if e.connection != nil && (e.requestAck || m.regions == nil || !reflect.DeepEqual(m.regions, composed)) {
		if err := e.connection.enqueue(msg{Type: "rendered", Rendered: composed, FocusEpoch: e.epoch}); err != nil {
			return err
		}
		e.requestAck = false
	}
	m.regions = composed
	held := m.held
	m.held = nil
	for _, event := range held {
		if err := m.input(event); err != nil {
			return err
		}
	}
	return nil
}

func frameLines(f *render.Frame, x, y, width int, text string) int {
	start := x
	for _, r := range text {
		w := ansi.StringWidth(string(r))
		if r == '\n' || x+w > start+width {
			y++
			x = start
			if r == '\n' {
				continue
			}
		}
		f.Set(x, y, string(r), uv.Style{Fg: f.Palette.Text})
		x += w
	}
	return y + 1
}
