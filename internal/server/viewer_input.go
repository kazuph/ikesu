package server

func (s *Server) viewerInput(c *clientConn, m clientMsg) {
	if m.EndPrefix {
		s.app.Do(func() {
			if c.viewer != nil {
				c.viewer.EndPrefix(s.app)
			}
		})
	}
	if data, err := decode64(m.Data); err == nil && len(data) > 0 {
		s.app.Do(func() {
			s.mu.Lock()
			s.client = c
			s.lastUse++
			c.lastUse = s.lastUse
			s.mu.Unlock()
			if m.Type == "paste" {
				c.viewer.Paste(s.app, string(data))
			} else {
				c.viewer.Input(s.app, data)
			}
		})
	}
}
