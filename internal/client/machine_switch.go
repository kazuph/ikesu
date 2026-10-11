package client

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/render"
)

func (m *machineClient) requestPending() {
	if m.pending == nil {
		return
	}
	e := m.endpoints[m.pending.id]
	if e.connection == nil {
		return
	}
	if m.pending.requested {
		return
	}
	if err := e.connection.enqueue(resizeMessage(m.size)); err != nil {
		m.fail(m.pending.id, err)
		return
	}
	if !endpoint.Allows(e.connection.writer.available, endpoint.MachineGeneration, e.connection.writer.methods, "workspace_select") {
		if e.snapshot == nil {
			return
		}
		if ws := e.snapshot.State.ActiveWorkspace(); ws != nil && ws.ID == m.pending.workspace {
			m.finishSwitch()
			return
		}
		for _, position := range render.SidebarViewForInput(*e.snapshot).Rows {
			row := position.Row
			if row.Kind == render.RowWorkspace && e.snapshot.State.Workspaces[row.Ws].ID == m.pending.workspace {
				_ = m.sendInput(m.pending.id, []byte(fmt.Sprintf("\x1b[<0;2;%dM\x1b[<0;2;%dm", position.Y+1, position.Y+1)))
				m.pending.requested = true
				return
			}
		}
		m.fail(m.pending.id, fmt.Errorf("endpoint_action_unsupported: workspace outside Local viewport"))
		return
	}
	if err := e.connection.enqueue(msg{Type: "workspace_select", Workspace: m.pending.workspace}); err != nil {
		m.fail(m.pending.id, err)
	} else {
		m.pending.requested = true
	}
}

func (m *machineClient) finishSwitch() {
	p := m.pending
	if p == nil {
		return
	}
	e := m.endpoints[p.id]
	if e.connection == nil || e.snapshot == nil {
		return
	}
	s := e.snapshot.State
	ws := s.ActiveWorkspace()
	if ws == nil || (!p.create && ws.ID != p.workspace) || (p.create && p.previousWorkspaces[ws.ID]) || s.Cols != m.size.Cols || s.Rows != m.size.Rows {
		return
	}
	old := m.endpoints[m.active]
	oldID := m.active
	visible := false
	if old.connection != nil {
		if endpoint.Allows(old.connection.writer.available, endpoint.MachineGeneration, old.connection.writer.methods, "visibility") {
			_ = old.connection.enqueue(msg{Type: "visibility", Visible: &visible})
		} else {
			// Local's existing generation-one focus input remains supported.
			_ = m.sendInput(m.active, []byte("\x1b[O"))
		}
	}
	m.active = p.id
	if old.removed {
		delete(m.endpoints, oldID)
	}
	m.pending = nil
	m.regions = nil
	visible = !m.blurred
	if endpoint.Allows(e.connection.writer.available, endpoint.MachineGeneration, e.connection.writer.methods, "visibility") {
		_ = e.connection.enqueue(msg{Type: "visibility", Visible: &visible})
	} else if visible {
		_ = m.sendInput(m.active, []byte("\x1b[I"))
	}
}
