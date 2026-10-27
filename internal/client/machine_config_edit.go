package client

import "github.com/kazuph/ikesu/internal/config"

type pendingConfigEdit struct {
	endpoint string
	attempt  uint64
	id       uint64
	edit     config.Edit
}

func (m *machineClient) queueConfigEdit(id string, message msg) {
	e := m.endpoints[id]
	m.configEdits = append(m.configEdits, pendingConfigEdit{id, e.attempt, message.ConfigID, *message.ConfigEdit})
	if len(m.configEdits) == 1 {
		m.startConfigEdit()
	}
}

// Keep filesystem work off the input/paint loop while preserving edit order.
func (m *machineClient) startConfigEdit() {
	if len(m.configEdits) == 0 {
		return
	}
	request := m.configEdits[0]
	var current config.Config
	if m.settings.config != nil {
		current = *m.settings.config
	}
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		cfg, err := request.edit.ApplyTo(current)
		m.post(m.ctx, endpointEvent{formUpdate: func() {
			result := msg{Type: "config_result", ConfigID: request.id}
			if err != nil {
				result.Reason = err.Error()
			} else {
				m.settings.config = &cfg
				result.Config = &cfg
				m.configureOtherEndpoints(request.endpoint)
			}
			if e := m.endpoints[request.endpoint]; e != nil && e.attempt == request.attempt && e.connection != nil {
				_ = e.connection.enqueue(result)
			}
			m.configEdits = m.configEdits[1:]
			m.startConfigEdit()
		}})
	}()
}
