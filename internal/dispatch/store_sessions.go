package dispatch

// --- pane session ledger ---

// PutPaneSession upserts the (session, pane) ledger row.
func (s *Store) PutPaneSession(r PaneSession) error {
	return s.exec(
		`INSERT OR REPLACE INTO pane_sessions
		 (session,pane_id,terminal_id,workspace_id,tab_id,cwd,agent,session_kind,session_id,title,source,observed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.Session, r.PaneID, r.TerminalID, r.WorkspaceID, r.TabID, r.Cwd,
		r.Agent, r.SessionKind, r.SessionID, r.Title, r.Source, r.ObservedAt)
}

// PaneSessions lists every ledger row of a session.
func (s *Store) PaneSessions(session string) ([]PaneSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.queryLocked(
		`SELECT session,pane_id,terminal_id,workspace_id,tab_id,cwd,agent,
		 session_kind,session_id,title,source,observed_at
		 FROM pane_sessions WHERE session=? ORDER BY pane_id`, session)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	var rows []PaneSession
	for st.Step() {
		rows = append(rows, PaneSession{
			Session:     st.ColumnText(0),
			PaneID:      int(st.ColumnInt64(1)),
			TerminalID:  st.ColumnText(2),
			WorkspaceID: int(st.ColumnInt64(3)),
			TabID:       int(st.ColumnInt64(4)),
			Cwd:         st.ColumnText(5),
			Agent:       st.ColumnText(6),
			SessionKind: st.ColumnText(7),
			SessionID:   st.ColumnText(8),
			Title:       st.ColumnText(9),
			Source:      st.ColumnText(10),
			ObservedAt:  st.ColumnText(11),
		})
	}
	return rows, st.Err()
}

// DeletePaneSession drops one (session, pane) ledger row.
func (s *Store) DeletePaneSession(session string, paneID int) error {
	return s.exec(`DELETE FROM pane_sessions WHERE session=? AND pane_id=?`,
		session, paneID)
}
