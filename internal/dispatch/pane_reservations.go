package dispatch

import (
	"errors"
	"strconv"
	"strings"
)

// ReservedPaneIDs returns durable references that prevent a pane ID from being reused.
func (s *Store) ReservedPaneIDs(session string) (map[int]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("dispatch database is closed")
	}
	st, err := s.queryLocked(`SELECT ta.name FROM dispatches d JOIN actors ta ON ta.id=d.to_actor WHERE d.kind='message' AND d.status='queued'
		UNION SELECT caller_pane FROM dispatches WHERE kind='command' AND status IN ('queued','running','cancelling')
		UNION SELECT 'p' || pane_id FROM pane_sessions WHERE session=?`, session)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	out := map[int]bool{}
	for st.Step() {
		value := st.ColumnText(0)
		if !IsGlobalPaneID(value) {
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(value, "p"))
		if err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, st.Err()
}
