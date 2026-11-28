package client

import (
	"fmt"
	"os"

	"github.com/kazuph/ikesu/internal/endpoint"
)

const machineSidebarUnsupported = "endpoint_action_unsupported: machine sidebar requires a Local server with client rendering; Local input remains available"

func (m *machineClient) legacyLocal() bool {
	e := m.endpoints["local"]
	return m.active == "local" && e.connection != nil && !endpoint.Allows(e.connection.writer.available, 1, e.connection.writer.methods, "rendered")
}

func (m *machineClient) configureMachineDisplay() {
	if !m.legacyLocal() {
		for _, id := range m.order[1:] {
			if m.endpoints[id].status == "unsupported" && m.endpoints[id].profile.Enabled {
				m.start(id, nil)
			}
		}
		return
	}
	if !hasEnabledMachine(savedMachines(m.session)) {
		return
	}
	fmt.Fprintln(os.Stderr, "ikesu:", machineSidebarUnsupported)
	for _, id := range m.order[1:] {
		e := m.endpoints[id]
		if !e.profile.Enabled {
			continue
		}
		if e.cancel != nil {
			e.cancel()
		}
		// Ignore events from the cancelled attempt and stop retries for
		// the display feature that the negotiated Local server lacks.
		e.attempt++
		e.connection = nil
		e.retry = retryPolicy{}
		e.status, e.reason = "unsupported", machineSidebarUnsupported
	}
	held := m.held
	m.held = nil
	for _, event := range held {
		if err := m.input(event); err != nil {
			fmt.Fprintln(os.Stderr, "ikesu:", err)
		}
	}
}
