package server

// enqueueAppCleanup keeps cleanup ordered while allowing disconnected API
// connections to close even when the app loop is temporarily busy.
func (s *Server) enqueueAppCleanup(fn func()) {
	s.cleanupMu.Lock()
	s.cleanupQueue = append(s.cleanupQueue, fn)
	if !s.cleanupRunning {
		s.cleanupRunning = true
		go s.runAppCleanups()
	}
	s.cleanupMu.Unlock()
}

func (s *Server) runAppCleanups() {
	for {
		s.cleanupMu.Lock()
		if len(s.cleanupQueue) == 0 {
			s.cleanupRunning = false
			s.cleanupMu.Unlock()
			return
		}
		fn := s.cleanupQueue[0]
		s.cleanupQueue[0] = nil
		s.cleanupQueue = s.cleanupQueue[1:]
		s.cleanupMu.Unlock()
		s.app.Do(fn)
	}
}
