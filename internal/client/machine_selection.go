package client

import (
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/render"
)

type paintedSelection struct {
	target, session string
	value           *endpoint.ViewSelection
}

func (m *machineClient) selectionSession(id string) string {
	if id == "local" {
		return m.session
	}
	return m.endpoints[id].profile.Session
}

// Retained snapshots may still belong to the previous connection target.
// Only a painted snapshot received in the current attempt owns a selection.
func (m *machineClient) rememberSelection(id string, in render.Input) {
	e := m.endpoints[id]
	if e.snapshotAttempt != e.attempt {
		return
	}
	e.selection = &paintedSelection{target: e.profile.Target, session: m.selectionSession(id), value: endpoint.CaptureSelection(in.State)}
}

func (m *machineClient) reconnectSelection(id string) *endpoint.ViewSelection {
	e := m.endpoints[id]
	saved := e.selection
	if saved == nil {
		return nil
	}
	if saved.target != e.profile.Target || saved.session != m.selectionSession(id) {
		e.selection = nil
		return nil
	}
	return saved.value
}
