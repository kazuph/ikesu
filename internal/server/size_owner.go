package server

func (s *Server) displayClientAttached(c *clientConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clients[c]
}

// A draw can discover a broken connection on the app loop itself. Enqueue
// release from a separate goroutine so it cannot wait on that same loop.
func (s *Server) releaseSizeViewer(c *clientConn) {
	s.app.Do(func() {
		s.emitEvent("client_view_changed", ev("client_view_changed", "client_id", c.id, "disconnected", true))
		if c.viewer != nil {
			c.viewer.Detach(s.app)
		}
	})
}
