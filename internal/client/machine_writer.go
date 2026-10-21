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
			c.queue[0] = msg{}
			c.queue = c.queue[1:]
			c.queueMu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err := c.writer.send(message); err != nil {
				c.queueMu.Lock()
				c.writeErr = err
				c.queue = nil
				c.queueMu.Unlock()
				return err
			}
		}
	}
}
