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
			if m.fileGraphics.retired(*message.GraphicsTransmission, time.Now()) {
				_, err := m.out.Write(m.frames.ReleaseNativeImage(message.GraphicsTransmission.ImageID))
				return err
			}
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
	reserved := false
	if allowed {
		if err := m.frames.ReserveNativeImage(file.ImageID); err != nil {
			allowed = false
		} else {
			reserved = true
		}
	}
	identity, err := m.fileGraphics.begin(m.out, file, allowed, time.Now())
	if err != nil {
		if reserved {
			if _, writeErr := m.out.Write(m.frames.ReleaseNativeImage(file.ImageID)); writeErr != nil {
				return writeErr
			}
		}
		return e.connection.enqueue(msg{Type: endpoint.GraphicsResultMessage,
			GraphicsResult: &endpoint.GraphicsTransmissionResult{GraphicsTransmission: identity, Success: false}})
	}
	if err := e.connection.enqueue(msg{Type: endpoint.GraphicsStartedMessage, GraphicsTransmission: &identity}); err != nil {
		m.fileGraphics.retired(identity, time.Now())
		_, _ = m.out.Write(m.frames.ReleaseNativeImage(file.ImageID))
		return err
	}
	if result := m.fileGraphics.started(identity, time.Now()); result != nil {
		return m.sendGraphicsFileResult(e.connection, result)
	}
	return nil
}

func (m *machineClient) graphicsFileReply(raw []byte) (bool, error) {
	consumed, result := m.fileGraphics.reply(raw, time.Now())
	if result != nil {
		local := m.endpoints["local"]
		if local != nil && local.connection != nil {
			return consumed, m.sendGraphicsFileResult(local.connection, result)
		}
	}
	return consumed, nil
}

func (m *machineClient) sendGraphicsFileResult(connection *endpointConnection, result *endpoint.GraphicsTransmissionResult) error {
	if !result.Success {
		if _, err := m.out.Write(m.frames.ReleaseNativeImage(result.ImageID)); err != nil {
			return err
		}
	}
	return connection.enqueue(msg{Type: endpoint.GraphicsResultMessage, GraphicsResult: result})
}
