package server

import "net"

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
