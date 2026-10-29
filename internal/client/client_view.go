package client

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/layout"
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
func (m *machineClient) reportClientView(in render.Input) {
	view := m.currentClientView()
	covers := []layout.Rect{}
	if m.regions != nil {
		covers = append(covers, m.regions.MenuRect, m.regions.DialogRect, m.regions.HelpRect)
	}
	if in.Settings != nil {
		covers = append(covers, render.SettingsRect(in.State.Cols, in.State.Rows))
	}
	if in.Navigator != nil {
		covers = append(covers, render.NavigatorRect(in.State.Cols, in.State.Rows))
	}
	// Project exactly the panes drawn by drawPanes, then the popup drawn
	// above them. Fullscreen copy replaces even a zoom/overlay projection.
	panes := in.State.DisplayPaneRects()
	if in.Copy != nil && in.Copy.Fullscreen {
		panes = nil
		if in.Panes[in.Copy.Pane] != nil {
			panes = []layout.PaneRect{{ID: in.Copy.Pane, Rect: in.State.PanesArea()}}
		}
	}
	popup := in.State.PopupRect()
	popupVisible := in.State.PopupVisible() && popup.W > 0 && popup.H > 0 && popup.W <= in.State.Cols && popup.H <= in.State.Rows
	if popupVisible {
		panes = append(panes, layout.PaneRect{ID: in.State.Popup.PaneID, Rect: popup})
	}
	view.PaneIDs = []string{}
	if view.WorkspaceID != "" {
		for _, pane := range panes {
			r := pane.Rect
			if r.W <= 0 || r.H <= 0 {
				continue
			}
			covered := false
			layers := covers
			if popupVisible && pane.ID != in.State.Popup.PaneID {
				layers = append(append([]layout.Rect{}, covers...), popup)
			}
			for _, cover := range layers {
				if !image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(image.Rect(cover.X, cover.Y, cover.X+cover.W, cover.Y+cover.H)).Empty() {
					covered = true
					break
				}
			}
			if !covered {
				view.PaneIDs = append(view.PaneIDs, fmt.Sprintf("p%d", pane.ID))
			}
		}
	}
	sort.Strings(view.PaneIDs)
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
