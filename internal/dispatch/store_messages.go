package dispatch

// InsertMessage stores a queued message, creating sender/recipient agent
// rows as needed. A reply stamps replied_at on the referenced dispatch
// without touching its status (a job row stays a job).
func (s *Store) InsertMessage(room, project, from, to, body string, replyTo *int64, expectsReply bool) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fa, err := s.agentActorLocked(from)
	if err != nil {
		return Message{}, err
	}
	ta, err := s.agentActorLocked(to)
	if err != nil {
		return Message{}, err
	}
	now := nowTS()
	if err := s.execLocked(
		`INSERT INTO dispatches(kind,room,project,from_actor,to_actor,reply_to,expects_reply,body,created_at,status)
		 VALUES('message',?,?,?,?,?,?,?,?,'queued')`,
		room, project, fa, ta, replyTo, expectsReply, body, now); err != nil {
		return Message{}, err
	}
	if replyTo != nil {
		if err := s.execLocked(`UPDATE dispatches SET replied_at=? WHERE id=?`, now, *replyTo); err != nil {
			return Message{}, err
		}
	}
	return Message{
		ID: s.db.LastInsertRowID(), Room: room, Project: project,
		FromAgent: from, ToAgent: to, Body: body, CreatedAt: now,
	}, nil
}
