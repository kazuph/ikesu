package dispatch

import "time"

// CompleteJob commits the terminal state and its durable notice together.
// The expected state arbitrates completion against cancellation and replay.
func (s *Store) CompleteJob(id, expected, status string, exit *int64, finishedMs int64, caller, body string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.db.Exec("BEGIN IMMEDIATE"); err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.db.Exec("ROLLBACK")
		}
	}()
	if err := s.execLocked(`UPDATE dispatches SET status=?,finished_at=?,exit_code=? WHERE kind='command' AND external_id=? AND status=?`, status, time.UnixMilli(finishedMs).UTC().Format(tsLayout), exit, id, expected); err != nil {
		return false, err
	}
	changed := s.db.Changes() > 0
	if changed && body != "" {
		fa, err := s.agentActorLocked(RunShellActor)
		if err != nil {
			return false, err
		}
		ta, err := s.agentActorLocked(caller)
		if err != nil {
			return false, err
		}
		noticeStatus, reason := "queued", ""
		if !IsGlobalPaneID(caller) {
			noticeStatus, reason = "failed", "job notification has no immutable caller pane"
		}
		if err = s.execLocked(`INSERT INTO dispatches(kind,room,project,from_actor,to_actor,body,created_at,status,completion) VALUES('message',?,'',?,?,?,?,?,?)`, JobsRoom, fa, ta, body, nowTS(), noticeStatus, reason); err != nil {
			return false, err
		}
	}
	if err := s.db.Exec("COMMIT"); err != nil {
		return false, err
	}
	committed = true
	return changed, nil
}
