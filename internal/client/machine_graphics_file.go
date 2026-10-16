package client

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kazuph/goterrace/internal/endpoint"
)

func (m *machineClient) graphicsFileEvent(id string, message msg) error {
	e := m.endpoints[id]
	if e == nil || e.connection == nil {
		return nil
	}
	if message.Type == endpoint.GraphicsRetiredMessage {
		if id == "local" && message.GraphicsTransmission != nil {
			m.fileGraphics.retired(*message.GraphicsTransmission, time.Now())
		}
		return nil
	}
	if message.GraphicsFile == nil {
		return fmt.Errorf("invalid graphics_file message: missing file")
	}
	file := *message.GraphicsFile
	var asset map[string]json.RawMessage
	assetValid := json.Unmarshal(file.SurfaceAsset, &asset) == nil && asset != nil
	allowed := id == "local" && id == m.active && !m.blurred && e.snapshot != nil && message.FocusEpoch == e.epoch && assetValid && graphicsFileTransport(m.in, m.out, false) == endpoint.DirectKittyTransport
	identity, err := m.fileGraphics.begin(m.out, file, allowed, time.Now())
	if err != nil {
		return e.connection.enqueue(msg{Type: endpoint.GraphicsResultMessage,
			GraphicsResult: &endpoint.GraphicsTransmissionResult{GraphicsTransmission: identity, Success: false}})
	}
	if err := e.connection.enqueue(msg{Type: endpoint.GraphicsStartedMessage, GraphicsTransmission: &identity}); err != nil {
		m.fileGraphics.retired(identity, time.Now())
		return err
	}
	if result := m.fileGraphics.started(identity, time.Now()); result != nil {
		return e.connection.enqueue(msg{Type: endpoint.GraphicsResultMessage, GraphicsResult: result})
	}
	return nil
}

func (m *machineClient) graphicsFileReply(raw []byte) (bool, error) {
	consumed, result := m.fileGraphics.reply(raw, time.Now())
	if result != nil {
		local := m.endpoints["local"]
		if local != nil && local.connection != nil {
			return consumed, local.connection.enqueue(msg{Type: endpoint.GraphicsResultMessage, GraphicsResult: result})
		}
	}
	return consumed, nil
}
