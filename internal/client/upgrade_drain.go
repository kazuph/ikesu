package client

import (
	"context"
	"fmt"

	"github.com/kazuph/ikesu/internal/input"
)

type upgradeWait string

func (w upgradeWait) Error() string { return string(w) }

// prepareUpgrade runs after the terminal reader has stopped, while writers
// and the selected endpoints are still alive.
func (m *machineClient) prepareUpgrade(ctx context.Context, reader *machineInputState, incoming <-chan input.Event) error {
	pending := append([]input.Event(nil), m.held...)
	m.held = nil
	for event := range incoming {
		pending = append(pending, event)
	}
	pending = append(pending, reader.unsent...)
	reader.unsent = nil
	if m.pending != nil || len(m.settings.retryQueued) != 0 || len(m.endpoints["local"].restartInput) != 0 || (len(pending) != 0 && (m.regions == nil || m.endpoints[m.active].connection == nil)) {
		m.held = pending
		return upgradeWait("cannot send pending input before client upgrade")
	}
	for i, event := range pending {
		if err := ctx.Err(); err != nil {
			m.held = append(m.held, pending[i:]...)
			return err
		}
		if err := m.input(event); err != nil {
			m.held = append(m.held, pending[i:]...)
			return fmt.Errorf("send input before client upgrade: %w", err)
		}
		if m.pending != nil {
			m.held = append(m.held, pending[i+1:]...)
			return upgradeWait("input destination is changing during client upgrade")
		}
	}
	local := m.endpoints["local"]
	if local.restarting || len(local.restartInput) != 0 {
		return upgradeWait("local input is waiting for reconnection before client upgrade")
	}
	for id, endpoint := range m.endpoints {
		if endpoint.paste != nil {
			return upgradeWait("clipboard input is pending before client upgrade")
		}
		if endpoint.connection != nil {
			if err := endpoint.connection.waitEmpty(ctx); err != nil {
				return fmt.Errorf("drain %s input before client upgrade: %w", id, err)
			}
		}
	}
	return nil
}
