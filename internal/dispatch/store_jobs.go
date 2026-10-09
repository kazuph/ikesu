package dispatch

import (
	"fmt"
	sqlite3 "github.com/ncruces/go-sqlite3"
	"regexp"
	"strconv"
	"strings"
)

// --- jobs (command dispatches) ---

// InsertCommand records a `gtx run` job: a 'command' dispatch in the jobs
// room from gtx-run to the caller's mailbox, queued until the runner starts.
func (s *Store) InsertCommand(id, label, command, cwd, callerPane, callerAgent, completion, logPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fa, err := s.agentActorLocked(RunShellActor)
	if err != nil {
		return err
	}
	ta, err := s.agentActorLocked(callerAgent)
	if err != nil {
		return err
	}
	return s.execLocked(
		`INSERT INTO dispatches(external_id,kind,room,project,from_actor,to_actor,body,label,created_at,status,completion,runner_pid,log_path,caller_pane)
		 VALUES(?,'command',?,?,?,?,?,?,?,'queued',?,NULL,?,?)`,
		id, JobsRoom, cwd, fa, ta, command, label, nowTS(), completion, logPath, callerPane)
}

var jobIDRe = regexp.MustCompile(`^j(\d+)$`)

// MaxJobSeq returns the largest jNNNN external id's number (0 when none).
func (s *Store) MaxJobSeq() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.queryLocked(
		`SELECT external_id FROM dispatches WHERE kind='command' AND external_id IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	defer st.Close()
	var max int64
	for st.Step() {
		if m := jobIDRe.FindStringSubmatch(st.ColumnText(0)); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n > max {
				max = n
			}
		}
	}
	return max, st.Err()
}

// jobCond is the external_id-or-log-name lookup shared by CommandRow and
// CommandDispatchID: a job whose log file name ends in "<id>.log" is found
// too, which covers pane-mode jobs named only by their log file.
func jobCond(id string) (string, []any) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(id)
	return `d.kind='command' AND (d.external_id=? OR d.log_path LIKE ? ESCAPE '\')`,
		[]any{id, "%" + esc + ".log"}
}

const jobCols = `d.id, COALESCE(d.external_id,''), COALESCE(d.label,''), d.body,
	d.project, COALESCE(d.caller_pane,''), COALESCE(ta.name,''), COALESCE(d.completion,''),
	d.status, d.runner_pid, d.exit_code, d.started_at, d.finished_at, COALESCE(d.log_path,'')`

func scanJob(st *sqlite3.Stmt) Job {
	j := Job{
		ID:          st.ColumnText(1),
		Label:       st.ColumnText(2),
		Command:     st.ColumnText(3),
		Cwd:         st.ColumnText(4),
		CallerPane:  st.ColumnText(5),
		CallerAgent: st.ColumnText(6),
		Completion:  st.ColumnText(7),
		Status:      st.ColumnText(8),
		LogPath:     st.ColumnText(13),
	}
	if st.ColumnType(9) != sqlite3.NULL {
		v := st.ColumnInt64(9)
		j.RunnerPID = &v
	}
	if st.ColumnType(10) != sqlite3.NULL {
		v := st.ColumnInt64(10)
		j.ExitCode = &v
	}
	if st.ColumnType(11) != sqlite3.NULL {
		j.StartedMs = tsToMs(st.ColumnText(11))
	}
	if st.ColumnType(12) != sqlite3.NULL {
		j.FinishedMs = tsToMs(st.ColumnText(12))
	}
	return j
}

// CommandRows lists jobs newest first (limit > 0 caps the count).
func (s *Store) CommandRows(limit int) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := `SELECT ` + jobCols + ` FROM dispatches d
		LEFT JOIN actors ta ON ta.id = d.to_actor
		WHERE d.kind='command' ORDER BY d.id DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	st, err := s.queryLocked(q)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	var jobs []Job
	for st.Step() {
		jobs = append(jobs, scanJob(st))
	}
	return jobs, st.Err()
}

// CommandRow finds a job by external id or by its "<id>.log" log name.
func (s *Store) CommandRow(id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cond, args := jobCond(id)
	st, err := s.queryLocked(`SELECT `+jobCols+` FROM dispatches d
		LEFT JOIN actors ta ON ta.id = d.to_actor
		WHERE `+cond+` ORDER BY d.id DESC LIMIT 1`, args...)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	if st.Step() {
		j := scanJob(st)
		return &j, st.Err()
	}
	return nil, st.Err()
}

// CommandDispatchID returns the dispatch row id of a job (for reply_to).
func (s *Store) CommandDispatchID(id string) (*int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cond, args := jobCond(id)
	st, err := s.queryLocked(`SELECT d.id FROM dispatches d WHERE `+cond+
		` ORDER BY d.id DESC LIMIT 1`, args...)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	if st.Step() {
		v := st.ColumnInt64(0)
		return &v, st.Err()
	}
	return nil, st.Err()
}

// MarkCommandRunning moves a queued job to running with its runner pid
// and start time.
func (s *Store) MarkCommandRunning(id string, pid int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.execLocked(
		`UPDATE dispatches SET status='running', runner_pid=?, started_at=?
		 WHERE kind='command' AND external_id=? AND status='queued'`,
		pid, nowTS(), id)
}

// MarkCommandFinished moves a running job to exited. Any other state
// (cancelling, cancelled, exited) wins and the mark is a no-op.
func (s *Store) MarkCommandFinished(id string, exitCode *int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.execLocked(
		`UPDATE dispatches SET status='exited', finished_at=?, exit_code=?
		 WHERE kind='command' AND external_id=? AND status='running'`,
		nowTS(), exitCode, id); err != nil {
		return false, err
	}
	return s.db.Changes() > 0, nil
}

// MarkCommandCancelling moves a running job to cancelling (cancel granted).
func (s *Store) MarkCommandCancelling(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.execLocked(
		`UPDATE dispatches SET status='cancelling'
		 WHERE kind='command' AND external_id=? AND status='running'`, id); err != nil {
		return false, err
	}
	return s.db.Changes() > 0, nil
}

// MarkCommandCancelled moves a non-terminal job to cancelled.
func (s *Store) MarkCommandCancelled(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.execLocked(
		`UPDATE dispatches SET status='cancelled', finished_at=?
		 WHERE kind='command' AND external_id=? AND status IN ('queued','running','cancelling')`,
		nowTS(), id); err != nil {
		return false, err
	}
	return s.db.Changes() > 0, nil
}
