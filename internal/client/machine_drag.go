package client

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
)

// A cross-machine press selects its viewer first. Drag bytes wait for that
// selection's snapshot, so a delayed switch cannot reorder a hidden viewer.
type machineDrag struct {
	id                string
	press             []byte
	x, y              int
	queued            [][]byte
	last              []byte
	drop              int
	section           model.Section
	started, released bool
}

func (m *machineClient) dragInput(event input.Event) error {
	d := m.drag
	if event.Kind == input.KindMouseRelease {
		m.suppressRelease = false
		if !d.started {
			m.drag = nil
			return nil
		}
		d.released = true
	} else if !d.started && (event.Mouse.X != d.x || event.Mouse.Y != d.y) {
		d.started = true
		d.queued = append(d.queued, d.press)
	}
	if !d.started {
		return nil
	}
	for _, hit := range m.sidebar.Hits {
		if hit.Machine == d.id && !hit.Header && !hit.New && contains(hit.Rect.X, hit.Rect.Y, hit.Rect.W, hit.Rect.H, event.Mouse.X, event.Mouse.Y) {
			data, err := m.sidebarBytes(hit, event)
			if err != nil {
				return err
			}
			d.queued = append(d.queued, data)
			d.last = data
			d.drop = -1
			if hit.Row.Kind == render.RowWorkspace || hit.Row.Kind == render.RowGit || hit.Row.Kind == render.RowCardBlank {
				state := m.endpoints[d.id].snapshot.State
				if state.Workspaces[hit.Row.Ws].SidebarSection() == d.section {
					d.drop = hit.Row.Ws
					if event.Mouse.Y > d.y {
						d.drop++
					}
				}
			}
			if err := m.flushDrag(); err != nil {
				return err
			}
			if m.pending != nil {
				return m.paint()
			}
			return nil
		}
	}
	if d.released {
		if len(d.last) == 0 {
			m.drag = nil
			return nil
		}
		// The observed endpoint keeps the last valid drop target when the
		// pointer leaves the sidebar; release must still finish its gesture.
		var button, x, y int
		var final byte
		if _, err := fmt.Sscanf(string(d.last), "\x1b[<%d;%d;%d%c", &button, &x, &y, &final); err != nil {
			m.drag = nil
			return err
		}
		d.queued = append(d.queued, []byte(fmt.Sprintf("\x1b[<0;%d;%dm", x, y)))
		return m.flushDrag()
	}
	return nil
}

func (m *machineClient) flushDrag() error {
	d := m.drag
	if d == nil || m.pending != nil || m.active != d.id {
		return nil
	}
	for _, data := range d.queued {
		if err := m.sendInput(d.id, data); err != nil {
			return err
		}
	}
	d.queued = nil
	if d.released {
		m.drag = nil
	}
	return nil
}
