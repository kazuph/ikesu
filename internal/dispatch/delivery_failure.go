package dispatch

import "regexp"

var globalPaneID = regexp.MustCompile(`^p[1-9][0-9]*$`)

func IsGlobalPaneID(value string) bool { return globalPaneID.MatchString(value) }

// InsertFailedMessage preserves the rejected body and target in the existing
// ledger, with the reason in completion; no delivery timestamp is written.
func (s *Store) InsertFailedMessage(room, project, from, to, body, reason string, reply *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fa, err := s.agentActorLocked(from)
	if err != nil {
		return err
	}
	ta, err := s.agentActorLocked(to)
	if err != nil {
		return err
	}
	return s.execLocked(`INSERT INTO dispatches(kind,room,project,from_actor,to_actor,body,created_at,status,completion,reply_to) VALUES('message',?,?,?,?,?,?,'failed',?,?)`, room, project, fa, ta, body, nowTS(), reason, reply)
}
func (s *Store) FailMessage(id int64, reason string) error {
	return s.exec(`UPDATE dispatches SET status='failed',completion=? WHERE id=? AND kind='message' AND status='queued'`, reason, id)
}

// MigrateJobRecipients never uses mutable actor pane metadata as caller proof.
// Repeated opens preserve both failed history and exact pane-addressed queues.
func (s *Store) MigrateJobRecipients() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.queryLocked(`SELECT d.id,ta.name FROM dispatches d JOIN actors ta ON ta.id=d.to_actor WHERE d.kind='message' AND d.room=? AND d.status='queued'`, JobsRoom)
	if err != nil {
		return err
	}
	var failed []int64
	for st.Step() {
		if !IsGlobalPaneID(st.ColumnText(1)) {
			failed = append(failed, st.ColumnInt64(0))
		}
	}
	err = st.Err()
	st.Close()
	if err != nil {
		return err
	}
	for _, id := range failed {
		if err := s.execLocked(`UPDATE dispatches SET status='failed',completion='job notification has no immutable caller pane' WHERE id=? AND status='queued'`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PendingJobMessages() ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingLocked(`d.room=?`, JobsRoom)
}
