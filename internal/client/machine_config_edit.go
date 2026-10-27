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
	m.configEditMu.Lock()
	m.configEdits = append(m.configEdits, pendingConfigEdit{id, e.attempt, message.ConfigID, *message.ConfigEdit})
	if m.configEditing {
		m.configEditMu.Unlock()
		return
	}
	m.configEditing = true
	var current config.Config
	if m.settings.config != nil {
		current = *m.settings.config
	}
	m.workers.Add(1)
	m.configEditMu.Unlock()
	go m.saveConfigEdits(current)
}

// Accepted edits drain independently of UI result delivery, including shutdown.
func (m *machineClient) saveConfigEdits(current config.Config) {
	defer m.workers.Done()
	for {
		m.configEditMu.Lock()
		if len(m.configEdits) == 0 {
			m.configEditing = false
			m.configEditMu.Unlock()
			return
		}
		request := m.configEdits[0]
		m.configEdits[0] = pendingConfigEdit{}
		m.configEdits = m.configEdits[1:]
		m.configEditMu.Unlock()
		cfg, err := request.edit.ApplyTo(current)
		if err == nil {
			current = cfg
		}
		m.post(m.ctx, endpointEvent{formUpdate: func() {
			result := msg{Type: "config_result", ConfigID: request.id}
			if err != nil {
				result.Reason = err.Error()
			} else {
				m.settings.config = &cfg
				m.configRevision++
				result.Config = &cfg
				source := ""
				if e := m.endpoints[request.endpoint]; e != nil && e.attempt == request.attempt {
					source = request.endpoint
				}
				m.configureOtherEndpoints(source)
			}
			if e := m.endpoints[request.endpoint]; e != nil && e.attempt == request.attempt && e.connection != nil {
				_ = e.connection.enqueue(result)
			}
		}})
	}
}
