package server

// Persist the replacement generation before binding old durable references.
func (s *Server) migrateLegacyPaneGenerations() {
	state := s.app.State()
	if len(state.LegacyPaneGenerations) == 0 || !s.saveSession() {
		return
	}
	if store, err := s.db(); err == nil {
		for id := range state.LegacyPaneGenerations {
			if s.sup.Get(id) != nil {
				if err := store.BindLegacyPane(fmtPaneID(id), state.PaneGeneration(id)); err == nil {
					delete(state.LegacyPaneGenerations, id)
				}
			}
		}
	}
	s.saveSession()
}
