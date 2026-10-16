package client

import (
	"reflect"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/render"
)

func (m *machineClient) paint() error {
	if m.inputBatch || m.outputFence.Waiting(time.Now()) {
		m.paintQueued = true
		return nil
	}
	m.paintQueued = false
	e := m.endpoints[m.active]
	if e.snapshot == nil {
		return nil
	}
	in := *e.snapshot
	if err := m.inputSource.Prefix(in.Prefix, in.SidebarConfig.Experimental.SwitchASCIIInputSourceInPrefix); err != nil {
		m.notify("input source: " + err.Error())
	}
	if m.forms != nil {
		m.forms.Overlay(&in)
	}
	m.sidebar.PaneStatus = nil
	if e.connection == nil {
		m.sidebar.PaneStatus = []string{e.profile.Label + ": " + e.status, e.reason, "手元のspaceは使える"}
	}
	m.sidebar.Groups = m.sidebar.Groups[:0]
	m.sidebar.Active = m.active
	m.sidebar.DropMachine, m.sidebar.DropIndex = "", nil
	m.sidebar.DropSection = ""
	if m.drag != nil && m.drag.started {
		m.sidebar.DropMachine, m.sidebar.DropSection = m.drag.id, m.drag.targetSection
		if m.drag.drop >= 0 {
			m.sidebar.DropIndex = &m.drag.drop
		}
	}
	for _, id := range m.order {
		group := m.endpoints[id]
		input := group.snapshot
		if !group.profile.Enabled {
			input = nil
		}
		m.sidebar.Groups = append(m.sidebar.Groups, render.MachineGroup{ID: id, Label: group.profile.Label, Status: group.status, Reason: group.reason, Input: input})
	}
	var composed *render.Output
	if len(m.sidebar.Groups) == 1 && e.connection != nil {
		composed = render.Compose(in)
		m.sidebar.Hits = m.sidebar.Hits[:0]
	} else {
		composed = render.ComposeMachines(in, &m.sidebar)
	}
	m.drawTabFeedback(composed)
	f := composed.Frame
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
	if in.SidebarConfig.UI.HostCursor == "drawn" && composed.AnchorVisible && composed.AnchorShape == 0 {
		if cell := f.Cell(composed.AnchorX, composed.AnchorY); cell != nil {
			cell.Style.Attrs |= uv.AttrReverse
			composed.AnchorVisible = false
		}
	}
	if f.W != m.size.Cols || f.H != m.size.Rows {
		clipped := f.Viewport(m.size.Cols, m.size.Rows)
		composed.Frame = clipped
		composed.AnchorVisible = composed.AnchorVisible && composed.AnchorX < clipped.W && composed.AnchorY < clipped.H
	}
	composed.Frame.GraphicsNamespace = m.active
	data := m.frames.Frame(composed.Frame, composed.AnchorX, composed.AnchorY, composed.AnchorVisible, composed.AnchorShape)
	if _, err := m.out.Write(data); err != nil {
		return err
	}
	if m.outputFence.Written(data, time.Now()) {
		if _, err := m.out.WriteString(render.TerminalStatusQuery); err != nil {
			return err
		}
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
