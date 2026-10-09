package server

// Restore callbacks originate on the app loop; defer the shell wait to a worker.
func (s *Server) restoreAgentsUI(dryRun bool) {
	if dryRun {
		s.runAgentRestore(true)
		return
	}
	go func() {
		err := s.sup.WaitLoginPath()
		s.app.Do(func() {
			if err != nil {
				s.app.ShowToast("agent restore", err.Error(), "error", -1)
				return
			}
			s.runAgentRestore(false)
		})
	}()
}
