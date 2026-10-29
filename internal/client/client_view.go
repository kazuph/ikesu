package client

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
	"image"
	"reflect"
	"sort"
)

func (m *machineClient) currentClientView() api.ClientView {
	view := api.ClientView{MachineID: m.active, PaneIDs: []string{}}
	if m.focusKnown {
		focused := !m.blurred
		view.Focused = &focused
	}
	e := m.endpoints[m.active]
	if e == nil || e.connection == nil || e.snapshot == nil || e.snapshotAttempt != e.attempt {
		return view
	}
	view.Session = e.profile.Session
	if m.active == "local" {
		view.Session = m.session
	}
	if ws := e.snapshot.State.ActiveWorkspace(); ws != nil {
		view.WorkspaceID = fmt.Sprintf("s%d", ws.ID)
		state := e.snapshot.State
		for _, pane := range state.DisplayPaneRects() {
			if state.PopupVisible() {
				p, r := state.PopupRect(), pane.Rect
				if !image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(image.Rect(p.X, p.Y, p.X+p.W, p.Y+p.H)).Empty() {
					continue
				}
			}
			if pane.Rect.W > 0 && pane.Rect.H > 0 {
				view.PaneIDs = append(view.PaneIDs, fmt.Sprintf("p%d", pane.ID))
			}
		}
		if state.PopupVisible() {
			view.PaneIDs = append(view.PaneIDs, fmt.Sprintf("p%d", state.Popup.PaneID))
		}
		sort.Strings(view.PaneIDs)
	}
	return view
}
func (m *machineClient) reportClientView(painted ...render.Input) {
	view := m.currentClientView()
	if len(painted) > 0 {
		in := painted[0]
		if in.Menu != nil || in.Dialog != nil || in.Help != nil || in.Settings != nil || in.Navigator != nil {
			view.PaneIDs = []string{}
		}
	}
	m.publishClientView(view)
}

func (m *machineClient) reportClientFocus() {
	// A snapshot can arrive while painting is fenced. A terminal focus report
	// updates the last painted view, never that not-yet-visible snapshot.
	view := api.ClientView{PaneIDs: []string{}}
	if m.lastClientView != nil {
		view = *m.lastClientView
	}
	if m.focusKnown {
		focused := !m.blurred
		view.Focused = &focused
	}
	m.publishClientView(view)
}

func (m *machineClient) publishClientView(view api.ClientView) {
	local := m.endpoints["local"]
	if local == nil || local.connection == nil {
		return
	}
	c := local.connection
	if !endpoint.Allows(c.writer.available, 1, c.writer.methods, "client_view") {
		return
	}
	if c == m.clientViewConnection && reflect.DeepEqual(m.lastClientView, &view) {
		return
	}
	if c.enqueue(msg{Type: "client_view", ClientView: &view}) == nil {
		m.lastClientView = &view
		m.clientViewConnection = c
	}
}
