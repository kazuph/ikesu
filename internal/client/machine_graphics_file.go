package client

import (
	"fmt"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
)

func (m *machineClient) graphicsFileEvent(id string, message msg) error {
	e := m.endpoints[id]
	if e == nil || e.connection == nil {
		return nil
	}
	if message.Type == endpoint.GraphicsRetiredMessage {
		if id == "local" && message.GraphicsTransmission != nil {
			return m.cancelNativeTransfer(*message.GraphicsTransmission)
		}
		return nil
	}
	if message.GraphicsFile == nil {
		return fmt.Errorf("invalid graphics_file message: missing file")
	}
	file := *message.GraphicsFile
	var asset endpoint.GraphicsSurfaceAsset
	var assetErr error
	if e.snapshot != nil {
		asset, assetErr = decodeNativeAsset(file, *e.snapshot)
	}
	allowed := id == "local" && id == m.active && !m.blurred && e.snapshot != nil && message.FocusEpoch == e.epoch && assetErr == nil && m.nativeTransfer == nil && graphicsFileTransport(m.in, m.out, false) == endpoint.DirectKittyTransport
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
	m.nativeTransfer = &nativeViewerTransfer{identity: identity, asset: asset, epoch: e.epoch, connection: e.connection}
	if err := e.connection.enqueue(msg{Type: endpoint.GraphicsStartedMessage, GraphicsTransmission: &identity}); err != nil {
		_ = m.cancelNativeTransfer(identity)
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
	if result.Success {
		pending := m.nativeTransfer
		if pending == nil || pending.identity != result.GraphicsTransmission || pending.connection != connection {
			return fmt.Errorf("native graphics result has no matching surface")
		}
		pending.result = result
		return m.paint()
	}
	if !result.Success {
		if err := m.cancelNativeTransfer(result.GraphicsTransmission); err != nil {
			return err
		}
	}
	return connection.enqueue(msg{Type: endpoint.GraphicsResultMessage, GraphicsResult: result})
}
