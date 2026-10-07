// Package dispatch is the durable mailbox and job store shared by every
// gtx session. It keeps the same actors/dispatches schema as herdr's
// DispatchStore (docs/ref/herdr-db-schema.sql) so pane mail and background
// jobs survive server restarts and stay queryable with sqlite3 while the
// server runs (WAL mode).
package dispatch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/kazuph/goterrace/internal/api"
)

// DefaultRoom is the room plain `gtx send`/`gtx inbox` use.
const DefaultRoom = "default"

// JobsRoom carries job completion notices; it bypasses the regular mail
// status gate (delivered to working agents too, but not blocked ones).
const JobsRoom = "gtx-jobs"

// RunShellActor is the sender name on job completion nudges.
const RunShellActor = "gtx-run"

// DBPathEnvVar overrides the database location (tests and tooling).
const DBPathEnvVar = "GTX_DB_PATH"

// DBPath is the shared mailbox/job database: one file across all gtx
// sessions ($GTX_DB_PATH, else $GTX_STATE_DIR/gtx.db).
func DBPath() string {
	if p := os.Getenv(DBPathEnvVar); p != "" {
		return p
	}
	return filepath.Join(api.StateDir(), "gtx.db")
}

// DBExists reports whether the store file has ever been created. Boot-time
// sweeps (orphaned jobs, queued mail) are no-ops when it is absent, so they
// can skip opening the store — and the wasm sqlite engine — entirely.
func DBExists() bool {
	_, err := os.Stat(DBPath())
	return err == nil
}

// JobLogDir holds per-job output logs ($GTX_STATE_DIR/job-logs).
func JobLogDir() string {
	return filepath.Join(api.StateDir(), "job-logs")
}

var jobLogUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// JobLogPath is the log path for a job id/label pair
// ($GTX_STATE_DIR/job-logs/<id>-<safe-label>.log). Both the server-side
// background runner and the in-pane __pane-run wrapper derive paths with
// this helper so `gtx log` can find either kind.
func JobLogPath(jobID, label string) string {
	safe := jobLogUnsafe.ReplaceAllString(strings.TrimSpace(label), "-")
	safe = strings.Trim(safe, "-.")
	if safe == "" {
		safe = "job"
	}
	return filepath.Join(JobLogDir(), jobID+"-"+safe+".log")
}

// schemaDDL mirrors herdr's DispatchStore schema verbatim (same table and
// view names so `gtx log --schema` output and ad-hoc sqlite3 queries carry
// over), minus only the literal 'herdr-run' actor names handled in code.
const schemaDDL = `PRAGMA journal_mode=WAL;

CREATE TABLE IF NOT EXISTS actors (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  kind          TEXT NOT NULL CHECK (kind IN ('agent','shell','human')),
  name          TEXT NOT NULL,
  source        TEXT,
  model         TEXT,
  session_id    TEXT,
  pane_id       TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_actors_identity ON actors(kind, name);

CREATE TABLE IF NOT EXISTS dispatches (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  external_id   TEXT,
  kind          TEXT NOT NULL CHECK (kind IN ('message','command')),
  room          TEXT NOT NULL DEFAULT 'default',
  project       TEXT NOT NULL DEFAULT '',
  from_actor    INTEGER NOT NULL REFERENCES actors(id),
  to_actor      INTEGER NOT NULL REFERENCES actors(id),
  reply_to      INTEGER REFERENCES dispatches(id),
  expects_reply INTEGER NOT NULL DEFAULT 0,
  body          TEXT NOT NULL,
  label         TEXT,
  created_at    TEXT NOT NULL,
  delivered_at  TEXT,
  read_at       TEXT,
  started_at    TEXT,
  finished_at   TEXT,
  exit_code     INTEGER,
  replied_at    TEXT,
  status        TEXT NOT NULL,
  completion    TEXT,
  runner_pid    INTEGER,
  log_path      TEXT
);
CREATE INDEX IF NOT EXISTS idx_dispatch_pending ON dispatches(to_actor, status);
CREATE INDEX IF NOT EXISTS idx_dispatch_room_time ON dispatches(room, created_at);
CREATE INDEX IF NOT EXISTS idx_dispatch_reply ON dispatches(reply_to);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dispatch_external ON dispatches(kind, external_id);

CREATE VIEW IF NOT EXISTS v_reply_latency AS
SELECT d.id, d.room, d.project,
       fa.name AS asked_by,  fa.model AS asked_model,
       ta.name AS answered_by, ta.model AS answered_model,
       d.created_at, d.replied_at,
       CAST((julianday(d.replied_at) - julianday(d.created_at)) * 86400 AS INTEGER) AS reply_seconds
FROM dispatches d
JOIN actors fa ON fa.id = d.from_actor
JOIN actors ta ON ta.id = d.to_actor
WHERE d.kind = 'message' AND d.replied_at IS NOT NULL;

CREATE VIEW IF NOT EXISTS v_unanswered AS
SELECT d.id, d.room,
       fa.name AS asked_by, ta.name AS asked_to,
       d.body, d.created_at,
       CAST((julianday('now') - julianday(d.created_at)) * 86400 AS INTEGER) AS waiting_seconds
FROM dispatches d
JOIN actors fa ON fa.id = d.from_actor
JOIN actors ta ON ta.id = d.to_actor
WHERE d.expects_reply = 1 AND d.replied_at IS NULL;

CREATE VIEW IF NOT EXISTS v_command_stats AS
SELECT COALESCE(label, body) AS label, COUNT(*) AS runs,
       AVG(CASE WHEN exit_code = 0 THEN 1.0 ELSE 0.0 END) AS success_rate,
       CAST(AVG((julianday(finished_at) - julianday(started_at)) * 86400) AS INTEGER) AS avg_seconds,
       CAST(MAX((julianday(finished_at) - julianday(started_at)) * 86400) AS INTEGER) AS max_seconds
FROM dispatches
WHERE kind = 'command' AND finished_at IS NOT NULL
GROUP BY COALESCE(label, body);

CREATE VIEW IF NOT EXISTS v_model_matrix AS
SELECT fa.model AS asked_model, ta.model AS answered_model, COUNT(*) AS dispatches
FROM dispatches d
JOIN actors fa ON fa.id = d.from_actor
JOIN actors ta ON ta.id = d.to_actor
GROUP BY fa.model, ta.model;

-- pane\u2194agent-session ledger (SPEC G3): the durable record of which exact
-- agent session ran in which pane. One row per (session, pane); deleted
-- when the pane is permanently closed.
CREATE TABLE IF NOT EXISTS pane_sessions (
  session      TEXT NOT NULL,
  pane_id      INTEGER NOT NULL,
  terminal_id  TEXT NOT NULL DEFAULT '',
  workspace_id INTEGER NOT NULL DEFAULT 0,
  tab_id       INTEGER NOT NULL DEFAULT 0,
  cwd          TEXT NOT NULL DEFAULT '',
  agent        TEXT NOT NULL DEFAULT '',
  session_kind TEXT NOT NULL DEFAULT 'id',
  session_id   TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL DEFAULT '',
  source       TEXT NOT NULL DEFAULT '',
  observed_at  TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (session, pane_id)
);
`

// Schema returns the DDL applied at open, for `gtx log --schema`.
func Schema() string { return schemaDDL }

// Store wraps the shared SQLite database. All methods are safe for
// concurrent use; access is serialized through a single connection.
type Store struct {
	db  *sql.DB
	mux sync.Mutex
}

// Open opens (creating if needed) the store at path in read-write mode.
func Open(path string) (*Store, error) {
	return open(path, false)
}

// OpenReadOnly opens the store without creating it; it fails when the
// file does not exist. Used by read-only CLI commands like `gtx run list`.
func OpenReadOnly(path string) (*Store, error) {
	return open(path, true)
}

func open(path string, readOnly bool) (*Store, error) {
	if !readOnly {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	if readOnly {
		dsn += "&_pragma=query_only(true)&mode=ro"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if !readOnly {
		if err := s.migrate(); err != nil {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) migrate() error {
	s.mux.Lock()
	defer s.mux.Unlock()
	if _, err := s.db.Exec(schemaDDL); err != nil {
		return fmt.Errorf("dispatch schema: %w", err)
	}
	// Column migrations mirror herdr: add columns when missing; the
	// read_at backfill only runs when the column had to be added, matching
	// herdr's migrate_read_at one-shot semantics.
	for _, col := range [][2]string{
		{"external_id", "TEXT"},
		{"completion", "TEXT"},
		{"runner_pid", "INTEGER"},
		{"read_at", "TEXT"},
	} {
		exists, err := s.hasColumnLocked("dispatches", col[0])
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE dispatches ADD COLUMN %s %s", col[0], col[1])); err != nil {
				return err
			}
			if col[0] == "read_at" {
				if _, err := s.db.Exec("UPDATE dispatches SET read_at=delivered_at WHERE delivered_at IS NOT NULL AND status IN ('delivered','replied')"); err != nil {
					return err
				}
			}
		}
	}
	if _, err := s.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_dispatch_external ON dispatches(kind, external_id)"); err != nil {
		return err
	}
	return nil
}

func (s *Store) hasColumnLocked(table, col string) (bool, error) {
	rows, err := s.db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// nowStamp formats UTC timestamps like herdr's strftime('%Y-%m-%dT%H:%M:%fZ').
func nowStamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// upsertActorLocked inserts or refreshes an actor and returns its id.
// Actor identity is (kind, name); session/pane are refreshed attributes.
func (s *Store) upsertActorLocked(kind, name string, source, model, sessionID, paneID *string) (int64, error) {
	now := nowStamp()
	_, err := s.db.Exec(`
		INSERT INTO actors (kind, name, source, model, session_id, pane_id, first_seen_at, last_seen_at)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?7)
		ON CONFLICT(kind, name) DO UPDATE SET
			source = COALESCE(?3, actors.source),
			model = COALESCE(?4, actors.model),
			session_id = COALESCE(?5, actors.session_id),
			pane_id = COALESCE(?6, actors.pane_id),
			last_seen_at = ?7
	`, kind, name, source, model, sessionID, paneID, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRow("SELECT id FROM actors WHERE kind=?1 AND name=?2", kind, name).Scan(&id)
	return id, err
}

// UpsertActor records an agent or user actor (name/pane identity).
func (s *Store) UpsertActor(kind, name string, source, model, sessionID, paneID *string) (int64, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	return s.upsertActorLocked(kind, name, source, model, sessionID, paneID)
}

// Message is one dispatch row of kind='message' with actor names joined.
type Message struct {
	ID          int64   `json:"id"`
	Room        string  `json:"room"`
	Project     string  `json:"project"`
	FromAgent   string  `json:"from_agent"`
	ToAgent     string  `json:"to_agent"`
	Body        string  `json:"body"`
	CreatedAt   string  `json:"created_at"`
	DeliveredAt *string `json:"delivered_at"`
	ReadAt      *string `json:"read_at"`
}

// Job is one dispatch row of kind='command'.
type Job struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Command     string `json:"command"`
	Cwd         string `json:"cwd"`
	CallerPane  string `json:"caller_pane"`
	CallerAgent string `json:"caller_agent"`
	Completion  string `json:"completion"`
	Status      string `json:"status"`
	RunnerPID   *int64 `json:"runner_pid"`
	ExitCode    *int64 `json:"exit_code"`
	StartedMs   *int64 `json:"started_unix_ms"`
	FinishedMs  *int64 `json:"finished_unix_ms"`
	LogPath     string `json:"log_path"`
}

const messageSelect = `
SELECT d.id, d.room, d.project, fa.name, ta.name, d.body, d.created_at, d.delivered_at, d.read_at
FROM dispatches d
JOIN actors fa ON fa.id=d.from_actor
JOIN actors ta ON ta.id=d.to_actor
`

func scanMessage(rows *sql.Rows) (Message, error) {
	var m Message
	err := rows.Scan(&m.ID, &m.Room, &m.Project, &m.FromAgent, &m.ToAgent, &m.Body,
		&m.CreatedAt, &m.DeliveredAt, &m.ReadAt)
	return m, err
}

// InsertMessage stores a message addressed to toAgent and returns its row.
// replyTo links the new row to an earlier dispatch and marks that dispatch
// replied (without overwriting terminal job status).
func (s *Store) InsertMessage(room, project, fromAgent, toAgent, body string, replyTo *int64, expectsReply bool) (Message, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	from, err := s.upsertActorLocked("agent", fromAgent, nil, nil, nil, nil)
	if err != nil {
		return Message{}, err
	}
	to, err := s.upsertActorLocked("agent", toAgent, nil, nil, nil, nil)
	if err != nil {
		return Message{}, err
	}
	res, err := s.db.Exec(`
		INSERT INTO dispatches (kind, room, project, from_actor, to_actor, reply_to, expects_reply, body, created_at, status)
		VALUES ('message', ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, 'queued')
	`, room, project, from, to, replyTo, b2i(expectsReply), body, nowStamp())
	if err != nil {
		return Message{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Message{}, err
	}
	if replyTo != nil {
		if _, err := s.db.Exec(`
			UPDATE dispatches
			SET replied_at = COALESCE(replied_at, ?2),
			    status = CASE WHEN kind='message' THEN 'replied' ELSE status END
			WHERE id = ?1 AND replied_at IS NULL
		`, *replyTo, nowStamp()); err != nil {
			return Message{}, err
		}
	}
	var m Message
	err = s.db.QueryRow(messageSelect+"WHERE d.id=?", id).Scan(
		&m.ID, &m.Room, &m.Project, &m.FromAgent, &m.ToAgent, &m.Body,
		&m.CreatedAt, &m.DeliveredAt, &m.ReadAt)
	return m, err
}

// MessagesForInboxRecipients returns queued messages for any of the
// recipient aliases, marking them delivered+read in the same transaction.
// inFlight ids are excluded (a write is still on its way to the pane).
func (s *Store) MessagesForInboxRecipients(room string, recipients []string, inFlight map[int64]bool) ([]Message, error) {
	if len(recipients) == 0 {
		return nil, nil
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	args := make([]any, 0, len(recipients)+1)
	args = append(args, room)
	for _, r := range recipients {
		args = append(args, r)
	}
	var out []Message
	err := s.tx(func(tx *sql.Tx) error {
		rows, err := tx.Query(fmt.Sprintf(`
			SELECT d.id FROM dispatches d
			JOIN actors t ON t.id=d.to_actor
			WHERE d.kind='message' AND d.status='queued' AND d.room=?1 AND t.kind='agent' AND t.name IN (%s)
			ORDER BY d.id
		`, placeholdersAt(len(recipients), 2)), args...)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !inFlight[id] {
				ids = append(ids, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if _, err := tx.Exec(fmt.Sprintf(`
			UPDATE dispatches
			SET status='delivered', delivered_at=?1, read_at=COALESCE(read_at, ?1)
			WHERE id IN (%s)
		`, placeholdersAt(len(ids), 2)), append([]any{nowStamp()}, int64Args(ids)...)...); err != nil {
			return err
		}
		qrows, err := tx.Query(fmt.Sprintf(messageSelect+"WHERE d.id IN (%s) ORDER BY d.id", placeholdersAt(len(ids), 1)), int64Args(ids)...)
		if err != nil {
			return err
		}
		defer qrows.Close()
		for qrows.Next() {
			m, err := scanMessage(qrows)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return qrows.Err()
	})
	return out, err
}

// PendingMessagesForAgent lists queued messages for toAgent in a room,
// oldest first.
func (s *Store) PendingMessagesForAgent(room, toAgent string) ([]Message, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	return s.selectMessages(messageSelect+`
		WHERE d.kind='message' AND d.status='queued' AND d.room=?1 AND ta.name=?2
		ORDER BY d.created_at, d.id
	`, room, toAgent)
}

// PendingForAgentCreationOrder lists queued messages for toAgent across
// all rooms except the jobs room (regular mail delivery order).
func (s *Store) PendingForAgentCreationOrder(toAgent string) ([]Message, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	return s.selectMessages(messageSelect+`
		WHERE d.kind='message' AND d.status='queued' AND ta.name=?1 AND d.room != ?2
		ORDER BY d.created_at, d.id
	`, toAgent, JobsRoom)
}

// PendingNudgesForAgent groups the agent's queued messages by room,
// ordered by first pending id per room.
func (s *Store) PendingNudgesForAgent(toAgent string) ([]PendingNudge, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	messages, err := s.selectMessages(messageSelect+`
		WHERE d.kind='message' AND d.status='queued' AND ta.name=?1
		ORDER BY d.id
	`, toAgent)
	if err != nil {
		return nil, err
	}
	var nudges []PendingNudge
	seen := map[string]int{}
	for _, m := range messages {
		i, ok := seen[m.Room]
		if !ok {
			nudges = append(nudges, PendingNudge{Room: m.Room, ToAgent: m.ToAgent, Count: 1, LatestFrom: m.FromAgent})
			seen[m.Room] = len(nudges) - 1
			continue
		}
		nudges[i].Count++
		nudges[i].LatestFrom = m.FromAgent
	}
	return nudges, nil
}

// PendingNudge summarizes queued messages for one room/agent pair.
type PendingNudge struct {
	Room       string
	ToAgent    string
	Count      int
	LatestFrom string
}

// PendingNudgeFor returns the nudge for one room/agent pair, if any.
func (s *Store) PendingNudgeFor(room, toAgent string) (*PendingNudge, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	messages, err := s.selectMessages(messageSelect+`
		WHERE d.kind='message' AND d.status='queued' AND d.room=?1 AND ta.name=?2
		ORDER BY d.id
	`, room, toAgent)
	if err != nil || len(messages) == 0 {
		return nil, err
	}
	return &PendingNudge{
		Room:       room,
		ToAgent:    toAgent,
		Count:      len(messages),
		LatestFrom: messages[len(messages)-1].FromAgent,
	}, nil
}

// History lists messages in a room (optionally one project), oldest first,
// limited to the newest `limit` rows.
func (s *Store) History(room string, project *string, limit int) ([]Message, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	messages, err := s.selectMessages(messageSelect+`
		WHERE d.kind='message' AND d.room=?1 AND (?2 IS NULL OR d.project=?2)
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT ?3
	`, room, project, limit)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// Rooms lists distinct rooms with messages, in activity order.
func (s *Store) Rooms() ([]string, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	rows, err := s.db.Query(`
		SELECT DISTINCT room FROM dispatches
		WHERE kind='message'
		ORDER BY room
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		rooms = append(rooms, r)
	}
	return rooms, rows.Err()
}

// MarkMessageDelivered marks one queued message delivered+read. It returns
// false when the row was no longer pending (idempotent delivery).
func (s *Store) MarkMessageDelivered(id int64) (bool, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	res, err := s.db.Exec(`
		UPDATE dispatches
		SET status='delivered', delivered_at=?2, read_at=COALESCE(read_at, ?2)
		WHERE id=?1 AND status='queued'
	`, id, nowStamp())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// MarkDelivered marks every queued message for room/agent delivered+read.
func (s *Store) MarkDelivered(room, toAgent string) (int, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	res, err := s.db.Exec(`
		UPDATE dispatches
		SET status='delivered', delivered_at=?3, read_at=COALESCE(read_at, ?3)
		WHERE kind='message' AND status='queued' AND room=?1
		  AND to_actor IN (SELECT id FROM actors WHERE name=?2)
	`, room, toAgent, nowStamp())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// MarkDeliveredByIDs marks only the listed queued messages delivered —
// used by the jobs-room batch writer so a message that arrives between
// payload assembly and write completion is not lost (it stays queued and
// is delivered by the next flush).
func (s *Store) MarkDeliveredByIDs(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	args := make([]any, 0, len(ids)+1)
	args = append(args, nowStamp())
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := s.db.Exec(fmt.Sprintf(`
		UPDATE dispatches
		SET status='delivered', delivered_at=?1, read_at=COALESCE(read_at, ?1)
		WHERE kind='message' AND status='queued' AND id IN (%s)
	`, placeholdersAt(len(ids), 2)), args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// InsertCommand stores a background job dispatch row.
func (s *Store) InsertCommand(id, label, command, cwd, callerPane, callerAgent, completion, logPath string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	from, err := s.upsertActorLocked("agent", callerAgent, nil, nil, nil, &callerPane)
	if err != nil {
		return err
	}
	to, err := s.upsertActorLocked("shell", "gtx-run", nil, nil, nil, nil)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO dispatches (external_id, kind, room, project, from_actor, to_actor, body, label, created_at, status, completion, log_path)
		VALUES (?1, 'command', 'default', ?2, ?3, ?4, ?5, ?6, ?7, 'queued', ?8, ?9)
	`, id, cwd, from, to, command, label, nowStamp(), completion, logPath)
	return err
}

const jobSelect = `
SELECT d.external_id, d.label, d.body, d.project, d.status, d.exit_code,
       CAST((julianday(d.started_at) - 2440587.5) * 86400000 AS INTEGER),
       CAST((julianday(d.finished_at) - 2440587.5) * 86400000 AS INTEGER),
       d.log_path, fa.name, fa.pane_id, d.completion, d.runner_pid
FROM dispatches d
JOIN actors fa ON fa.id=d.from_actor
WHERE d.kind='command'
`

func scanJob(rows *sql.Rows) (Job, error) {
	var j Job
	var extID, label, callerPane, completion, logPath *string
	err := rows.Scan(&extID, &label, &j.Command, &j.Cwd, &j.Status, &j.ExitCode,
		&j.StartedMs, &j.FinishedMs, &logPath, &j.CallerAgent, &callerPane,
		&completion, &j.RunnerPID)
	if err != nil {
		return j, err
	}
	j.Label = deref(label)
	j.CallerPane = deref(callerPane)
	j.Completion = deref(completion)
	if j.Completion == "" {
		j.Completion = "summary"
	}
	j.LogPath = deref(logPath)
	if extID != nil {
		j.ID = *extID
	} else {
		base := filepath.Base(j.LogPath)
		j.ID = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return j, nil
}

// MaxJobSeq returns the highest numeric suffix among jNNNN external ids
// (0 when none), used to seed the server's job-id counter.
func (s *Store) MaxJobSeq() (int64, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	var n int64
	err := s.db.QueryRow(`
		SELECT COALESCE(MAX(CAST(substr(external_id, 2) AS INTEGER)), 0)
		FROM dispatches WHERE kind='command' AND external_id GLOB 'j[0-9]*'
	`).Scan(&n)
	return n, err
}

// CommandRows lists jobs newest first (bounded by limit when > 0).
func (s *Store) CommandRows(limit int) ([]Job, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	q := jobSelect + " ORDER BY d.id DESC"
	args := []any{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// CommandRow finds one job by external id or log-file stem.
func (s *Store) CommandRow(id string) (*Job, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	rows, err := s.db.Query(jobSelect+`
		AND (d.external_id=?1 OR d.log_path LIKE '%' || ?1 || '.log')
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	j, err := scanJob(rows)
	if err != nil {
		return nil, err
	}
	return &j, rows.Err()
}

// CommandDispatchID returns the dispatch row id for a job, for reply_to.
func (s *Store) CommandDispatchID(id string) (*int64, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	var did int64
	err := s.db.QueryRow(`
		SELECT d.id FROM dispatches d
		WHERE d.kind='command' AND (d.external_id=?1 OR d.log_path LIKE '%' || ?1 || '.log')
	`, id).Scan(&did)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &did, nil
}

// MarkCommandRunning transitions a job queued→running.
func (s *Store) MarkCommandRunning(id string, runnerPID int64) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	_, err := s.db.Exec(`
		UPDATE dispatches
		SET status='running', started_at=?2, runner_pid=?3
		WHERE kind='command' AND (external_id=?1 OR log_path LIKE '%' || ?1 || '.log')
	`, id, nowStamp(), runnerPID)
	return err
}

// MarkCommandFinished transitions running→exited; false when not running.
func (s *Store) MarkCommandFinished(id string, exitCode *int64) (bool, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	res, err := s.db.Exec(`
		UPDATE dispatches
		SET status='exited', finished_at=?2, exit_code=?3
		WHERE kind='command' AND (external_id=?1 OR log_path LIKE '%' || ?1 || '.log') AND status='running'
	`, id, nowStamp(), exitCode)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// MarkCommandCancelling transitions running→cancelling; false otherwise.
func (s *Store) MarkCommandCancelling(id string) (bool, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	res, err := s.db.Exec(`
		UPDATE dispatches
		SET status='cancelling'
		WHERE kind='command' AND (external_id=?1 OR log_path LIKE '%' || ?1 || '.log') AND status='running'
	`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// MarkCommandCancelled transitions queued/running/cancelling→cancelled.
func (s *Store) MarkCommandCancelled(id string) (bool, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	res, err := s.db.Exec(`
		UPDATE dispatches
		SET status='cancelled', finished_at=?2
		WHERE kind='command' AND (external_id=?1 OR log_path LIKE '%' || ?1 || '.log') AND status IN ('queued','running','cancelling')
	`, id, nowStamp())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) selectMessages(query string, args ...any) ([]Message, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) tx(fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func placeholdersAt(n, start int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "?%d", start+i)
		if i+1 < n {
			b.WriteByte(',')
		}
	}
	return b.String()
}

func b2i(v bool) int {
	if v {
		return 1
	}
	return 0
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// PaneSession is one row of the pane↔agent-session ledger (SPEC G3). It
// keeps the exact session id a pane last ran so restart/restore can resume
// that conversation instead of guessing a cwd-latest one.
type PaneSession struct {
	Session     string // gtx session name (the db is shared across sessions)
	PaneID      int
	TerminalID  string
	WorkspaceID int
	TabID       int
	Cwd         string
	Agent       string
	SessionKind string // "id" | "path"
	SessionID   string
	Title       string
	Source      string
	ObservedAt  string
}

// PutPaneSession upserts the ledger row for a pane.
func (s *Store) PutPaneSession(rec PaneSession) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	_, err := s.db.Exec(`INSERT INTO pane_sessions
		(session, pane_id, terminal_id, workspace_id, tab_id, cwd, agent,
		 session_kind, session_id, title, source, observed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(session, pane_id) DO UPDATE SET
			terminal_id=excluded.terminal_id,
			workspace_id=excluded.workspace_id,
			tab_id=excluded.tab_id,
			cwd=excluded.cwd,
			agent=excluded.agent,
			session_kind=excluded.session_kind,
			session_id=excluded.session_id,
			title=excluded.title,
			source=excluded.source,
			observed_at=excluded.observed_at`,
		rec.Session, rec.PaneID, rec.TerminalID, rec.WorkspaceID, rec.TabID,
		rec.Cwd, rec.Agent, rec.SessionKind, rec.SessionID, rec.Title,
		rec.Source, rec.ObservedAt)
	return err
}

// PaneSessions returns every ledger row for a gtx session, ordered by pane.
func (s *Store) PaneSessions(session string) ([]PaneSession, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	rows, err := s.db.Query(`SELECT pane_id, terminal_id, workspace_id, tab_id,
		cwd, agent, session_kind, session_id, title, source, observed_at
		FROM pane_sessions WHERE session=? ORDER BY pane_id`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaneSession
	for rows.Next() {
		var r PaneSession
		r.Session = session
		if err := rows.Scan(&r.PaneID, &r.TerminalID, &r.WorkspaceID,
			&r.TabID, &r.Cwd, &r.Agent, &r.SessionKind, &r.SessionID,
			&r.Title, &r.Source, &r.ObservedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeletePaneSession removes the ledger row for one pane. Called when the
// pane is permanently closed (explicit close, PaneDied removal, or the
// closing of its owning tab/workspace).
func (s *Store) DeletePaneSession(session string, paneID int) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	_, err := s.db.Exec(`DELETE FROM pane_sessions WHERE session=? AND pane_id=?`, session, paneID)
	return err
}
