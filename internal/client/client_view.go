package client

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
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
	if e == nil || e.connection == nil || e.snapshot == nil {
		return view
	}
	view.Session = e.profile.Session
	if m.active == "local" {
		view.Session = m.session
	}
	if ws := e.snapshot.State.ActiveWorkspace(); ws != nil {
		view.WorkspaceID = fmt.Sprintf("s%d", ws.ID)
		for _, tab := range ws.Tabs {
			for _, pid := range tab.Layout.PaneIDs() {
				view.PaneIDs = append(view.PaneIDs, fmt.Sprintf("p%d", pid))
			}
		}
		sort.Strings(view.PaneIDs)
	}
	return view
}
func (m *machineClient) reportClientView() {
	local := m.endpoints["local"]
	if local == nil || local.connection == nil {
		return
	}
	c := local.connection
	if !endpoint.Allows(c.writer.available, 1, c.writer.methods, "client_view") {
		return
	}
	view := m.currentClientView()
	if c == m.clientViewConnection && reflect.DeepEqual(m.lastClientView, &view) {
		return
	}
	if c.enqueue(msg{Type: "client_view", ClientView: &view}) == nil {
		m.lastClientView = &view
		m.clientViewConnection = c
	}
}
