package dispatch

// BindLegacyPane binds pre-generation queued records to a restored live pane.
func (s *Store) BindLegacyPane(pane, generation string) error {
	return s.exec(`UPDATE dispatches SET pane_generation=? WHERE COALESCE(pane_generation,'')='' AND
		((kind='message' AND status='queued' AND to_actor IN (SELECT id FROM actors WHERE kind='agent' AND name=?))
		OR (kind='command' AND status IN ('queued','running','cancelling') AND caller_pane=?))`, generation, pane, pane)
}
