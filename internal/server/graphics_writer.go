package server

import (
	"fmt"
	"net"
)

// Native delivery waits outside the app loop and never holds wmu during I/O.
func (c *clientConn) queueGraphicsControl(message clientMsg) (<-chan clientWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return nil, net.ErrClosed
	}
	c.startWriter()
	request := clientWrite{message: message, reply: make(chan clientWriteResult, 1)}
	select {
	case c.control <- request:
		return request.reply, nil
	case <-c.done:
		return nil, net.ErrClosed
	default:
		return nil, fmt.Errorf("endpoint control delivery is busy")
	}
}

// One queued frame corresponds to the graphics flow's one in-flight write.
// Encoding the next frame waits for drain, so gob/grid deltas cannot be lost.
func (c *clientConn) queueGraphicsFrame(message clientMsg) (<-chan clientWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return nil, net.ErrClosed
	}
	c.startWriter()
	request := clientWrite{message: message, reply: make(chan clientWriteResult, 1)}
	select {
	case c.render <- request:
		return request.reply, nil
	case <-c.done:
		return nil, net.ErrClosed
	}
}
