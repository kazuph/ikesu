package client

import (
	"io"

	"github.com/kazuph/ikesu/internal/render"
)

type windowTitleState struct {
	shared  render.WindowTitleState
	session string
	groups  []render.MachineGroup
}

func (title *windowTitleState) painted(out io.Writer, frame render.Input) error {
	return title.shared.Painted(out, frame, title.session, title.groups)
}

// Keep retained offline pictures in the sidebar, but never count their status.
func (m *machineClient) titleGroups() []render.MachineGroup {
	groups := make([]render.MachineGroup, 0, len(m.order))
	for _, id := range m.order {
		e := m.endpoints[id]
		group := render.MachineGroup{ID: id}
		if e != nil && e.profile.Enabled && e.connection != nil && e.snapshotAttempt == e.attempt {
			group.Input = e.snapshot
		}
		groups = append(groups, group)
	}
	return groups
}
