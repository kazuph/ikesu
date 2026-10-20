package client

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

// A cross-machine press selects its viewer first. Drop requests wait for that
// selection's snapshot, so a delayed switch cannot reorder a hidden viewer.
type machineDrag struct {
	id                string
	workspace, source int
	pressHit          render.MachineHit
	pressEvent        input.Event
	selected          bool
	x, y              int
	drop              int
	section           model.Section
	targetSection     model.Section
	started, released bool
	divider           bool
}

func (m *machineClient) dragInput(event input.Event) error {
	d := m.drag
	if d.divider {
		if event.Kind == input.KindMouseRelease {
			m.drag = nil
		}
		return m.sendInput(d.id, eventBytes(event))
	}
	if event.Kind == input.KindMouseRelease {
		m.suppressRelease = false
		if !d.started {
			m.drag = nil
			if d.selected && event.Mouse.Y == d.y && contains(d.pressHit.Rect.X, d.pressHit.Rect.Y, d.pressHit.Rect.W, d.pressHit.Rect.H, event.Mouse.X, event.Mouse.Y) {
				press, err := m.sidebarBytes(d.pressHit, d.pressEvent)
				if err != nil {
					return err
				}
				release, err := m.sidebarBytes(d.pressHit, event)
				if err != nil {
					return err
				}
				return m.sendInput(d.id, append(press, release...))
			}
			return nil
		}
		d.released = true
	} else if !d.started && (event.Mouse.X != d.x || event.Mouse.Y != d.y) {
		d.started = true
	}
	if !d.started {
		return nil
	}
	for _, hit := range m.sidebar.Hits {
		if hit.Machine == d.id && !hit.Header && !hit.New && contains(hit.Rect.X, hit.Rect.Y, hit.Rect.W, hit.Rect.H, event.Mouse.X, event.Mouse.Y) {
			d.drop = -1
			d.targetSection = ""
			if (hit.Row.Kind == render.RowSection || hit.Row.Kind == render.RowBlank) && hit.Row.Section != d.section {
				d.targetSection = hit.Row.Section
			}
			if hit.Row.Kind == render.RowWorkspace || hit.Row.Kind == render.RowGit || hit.Row.Kind == render.RowCardBlank {
				state := m.endpoints[d.id].snapshot.State
				if state.Workspaces[hit.Row.Ws].SidebarSection() == d.section {
					d.drop = hit.Row.Ws
					if event.Mouse.Y > d.y {
						d.drop++
					}
				} else {
					d.targetSection = state.Workspaces[hit.Row.Ws].SidebarSection()
					d.drop = hit.Row.Ws
				}
			}
			if err := m.flushDrag(); err != nil {
				return err
			}
			return m.paint()
		}
	}
	if d.released {
		if d.workspace == 0 || (d.drop < 0 && d.targetSection == "") {
			m.drag = nil
			return nil
		}
		return m.flushDrag()
	}
	return nil
}

func (m *machineClient) flushDrag() error {
	d := m.drag
	if d == nil || m.pending != nil || m.active != d.id {
		return nil
	}
	if !d.released {
		return nil
	}
	m.drag = nil
	var requests []api.Request
	if d.targetSection != "" {
		requests = append(requests, api.Request{ID: "sidebar-section", Method: api.MethodWorkspaceSetSection, Params: api.MarshalParams(api.WorkspaceSetSectionParams{WorkspaceID: api.FormatWorkspaceID(d.workspace), Section: string(d.targetSection)})})
	}
	if d.drop >= 0 {
		index := d.drop
		if index > d.source {
			index--
		}
		requests = append(requests, api.Request{ID: "sidebar-move", Method: "workspace.move", Params: api.MarshalParams(map[string]any{"workspace_id": api.FormatWorkspaceID(d.workspace), "insert_index": index})})
	}
	if len(requests) > 0 {
		m.sidebarRequests(d.id, requests)
	}
	return nil
}
