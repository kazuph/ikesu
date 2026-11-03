package client

import (
	"context"
	"fmt"

	"github.com/kazuph/ikesu/internal/endpoint"
)

// A slow SSH writer must never block terminal input to another endpoint.
// The single consumer preserves resize/select/visibility/input ordering.
func (c *endpointConnection) enqueue(message msg) error {
	if !endpoint.Allows(c.writer.available, endpoint.RequiredGeneration(message.Type), c.writer.methods, message.Type) {
		return fmt.Errorf("endpoint_action_unsupported: %s", message.Type)
	}
	c.queueMu.Lock()
	if c.writeErr != nil {
		err := c.writeErr
		c.queueMu.Unlock()
		if message.Type == "rendered" && isClosedRenderAck(err) {
			return nil
		}
		return err
	}
	if (message.Type == "input" || message.Type == "paste" || message.Type == "workspace_select") && endpoint.Allows(c.writer.available, endpoint.InputReceiptGeneration, c.writer.methods, "input_receipt") {
		c.sentInput++
		message.InputSequence = c.sentInput
	}
	c.queue = append(c.queue, message)
	c.queueMu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

func (c *endpointConnection) writeQueued(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.wake:
		}
		for {
			c.queueMu.Lock()
			if len(c.queue) == 0 {
				c.queueMu.Unlock()
				break
			}
			message := c.queue[0]
			c.queueMu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err := c.writer.send(message); err != nil {
				c.queueMu.Lock()
				c.writeErr = err
				if isClosedRenderAck(err) {
					kept := c.queue[:0]
					for _, queued := range c.queue[1:] {
						if queued.Type == "input" || queued.Type == "paste" || queued.Type == "workspace_select" {
							kept = append(kept, queued)
						}
					}
					c.queue = kept
				}
				c.queueMu.Unlock()
				return err
			}
			c.queueMu.Lock()
			c.queue[0] = msg{}
			c.queue = c.queue[1:]
			c.queueMu.Unlock()
		}
	}
}

func (c *endpointConnection) lastSentInput() uint64 {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	return c.sentInput
}

// saveUpgradeQueues runs after all connection writers have stopped.
func (m *machineClient) saveUpgradeQueues() {
	m.settings.retryQueued = make(map[string]queuedUpgradeInput)
	for id, e := range m.endpoints {
		if e.connection == nil {
			continue
		}
		e.connection.queueMu.Lock()
		var pending []msg
		for _, message := range e.connection.queue {
			if message.Type == "input" || message.Type == "paste" || message.Type == "workspace_select" {
				pending = append(pending, message)
			}
		}
		e.connection.queueMu.Unlock()
		if len(pending) != 0 {
			m.settings.retryQueued[id] = queuedUpgradeInput{target: e.profile.Target, session: m.selectionSession(id), messages: pending}
		}
	}
}

func (m *machineClient) replayUpgradeQueue(id string) error {
	pending, ok := m.settings.retryQueued[id]
	if !ok {
		return nil
	}
	e := m.endpoints[id]
	if pending.target != e.profile.Target || pending.session != m.selectionSession(id) {
		return nil
	}
	for len(pending.messages) != 0 {
		if err := e.connection.enqueue(pending.messages[0]); err != nil {
			return err
		}
		pending.messages = pending.messages[1:]
		m.settings.retryQueued[id] = pending
	}
	delete(m.settings.retryQueued, id)
	return nil
}
