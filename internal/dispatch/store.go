// Package dispatch is gtx's durable mailbox and job ledger: one SQLite
// database (WAL) records agent-to-agent messages, `gtx run` command
// dispatches, and the pane↔agent-session ledger used by restart restore.
// Table and column names follow docs/ref/herdr-db-schema.sql so the same
// DB can be inspected with `sqlite3 "$(gtx log --db)"`.
package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	sqlite3 "github.com/ncruces/go-sqlite3"
)

const DefaultRoom = "default"
const JobsRoom = "gtx-jobs"
const RunShellActor = "gtx-run"
const DBPathEnvVar = "GTX_DB_PATH"

// tsLayout is the millisecond UTC timestamp format stored in *_at columns.
const tsLayout = "2006-01-02T15:04:05.000Z"

func nowTS() string { return time.Now().UTC().Format(tsLayout) }

func tsToMs(s string) *int64 {
	t, err := time.Parse(tsLayout, s)
	if err != nil {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

// DBPath is the dispatch database location: $GTX_DB_PATH when set, else
// gtx.db under the state dir.
func DBPath() string {
	if p := os.Getenv(DBPathEnvVar); p != "" {
		return p
	}
	return filepath.Join(api.StateDir(), "gtx.db")
}

// DBExists reports whether the database file exists.
func DBExists() bool {
	_, err := os.Stat(DBPath())
	return err == nil
}

// JobLogDir is where `gtx run` job logs live.
func JobLogDir() string {
	return filepath.Join(api.StateDir(), "job-logs")
}

// JobLogPath is the log file name for a job: <id>-<label>.log, or
// <id>.log when the label is empty. `gtx log <job>` looks logs up under
// this scheme.
func JobLogPath(jobID, label string) string {
	if safe := sanitizeLabel(label); safe != "" {
		return filepath.Join(JobLogDir(), jobID+"-"+safe+".log")
	}
	return filepath.Join(JobLogDir(), jobID+".log")
}

// sanitizeLabel keeps a label usable inside a file name.
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// schemaSQL is the DDL executed on open. Column names and status values
// match the reference schema; pane_sessions (the restore ledger) and
// dispatches.caller_pane are gtx additions.
const schemaSQL = `CREATE TABLE IF NOT EXISTS actors (
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
  started_at    TEXT,
  finished_at   TEXT,
  exit_code     INTEGER,
  replied_at    TEXT,
  status        TEXT NOT NULL,
  completion    TEXT,
  runner_pid    INTEGER,
  log_path      TEXT,
  read_at       TEXT,
  caller_pane   TEXT
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
CREATE TABLE IF NOT EXISTS pane_sessions (
  session      TEXT NOT NULL,
  pane_id      INTEGER NOT NULL,
  terminal_id  TEXT,
  workspace_id INTEGER,
  tab_id       INTEGER,
  cwd          TEXT,
  agent        TEXT,
  session_kind TEXT,
  session_id   TEXT,
  title        TEXT,
  source       TEXT,
  observed_at  TEXT,
  PRIMARY KEY (session, pane_id)
);`

// Schema returns the DDL applied on open — `gtx log --schema` prints it.
func Schema() string { return schemaSQL + "\n" }

// Message is one dispatch row of kind 'message' as callers see it.
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

// Job is one dispatch row of kind 'command'.
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

// PendingNudge summarizes one room's queued mail to a recipient.
type PendingNudge struct {
	Room       string
	ToAgent    string
	Count      int
	LatestFrom string
}

// PaneSession is the pane ↔ agent-session ledger row (SPEC G3).
type PaneSession struct {
	Session     string
	PaneID      int
	TerminalID  string
	WorkspaceID int
	TabID       int
	Cwd         string
	Agent       string
	SessionKind string
	SessionID   string
	Title       string
	Source      string
	ObservedAt  string
}

// Store is a handle on the dispatch database. One connection serves all
// callers; the mutex serializes access.
type Store struct {
	db *sqlite3.Conn
	mu sync.Mutex
}

// Open opens the database read-write, creating it (and its directory)
// when absent. WAL mode, a 5 s busy timeout, and synchronous=NORMAL keep
// concurrent readers fast and safe.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	conn, err := sqlite3.OpenFlags(path,
		sqlite3.OPEN_READWRITE|sqlite3.OPEN_CREATE|sqlite3.OPEN_URI)
	if err != nil {
		return nil, err
	}
	s := &Store{db: conn}
	fail := func(err error) (*Store, error) {
		conn.Close()
		return nil, err
	}
	if err := conn.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return fail(err)
	}
	if err := conn.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return fail(err)
	}
	if err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fail(err)
	}
	if err := conn.Exec(schemaSQL); err != nil {
		return fail(err)
	}
	if err := s.ensureColumn("dispatches", "read_at", "read_at TEXT"); err != nil {
		return fail(err)
	}
	if err := s.ensureColumn("dispatches", "caller_pane", "caller_pane TEXT"); err != nil {
		return fail(err)
	}
	return s, nil
}

// OpenReadOnly opens an existing database without creating or migrating
// it; a missing file is an error.
func OpenReadOnly(path string) (*Store, error) {
	conn, err := sqlite3.OpenFlags(path, sqlite3.OPEN_READONLY|sqlite3.OPEN_URI)
	if err != nil {
		return nil, err
	}
	if err := conn.Exec("PRAGMA busy_timeout=5000"); err != nil {
		conn.Close()
		return nil, err
	}
	return &Store{db: conn}, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// ensureColumn adds a column introduced after the first schema version.
func (s *Store) ensureColumn(table, column, decl string) error {
	st, _, err := s.db.Prepare("SELECT name FROM pragma_table_info(?)")
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.BindText(1, table); err != nil {
		return err
	}
	for st.Step() {
		if st.ColumnText(0) == column {
			return nil
		}
	}
	if err := st.Err(); err != nil {
		return err
	}
	return s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + decl)
}

// --- low-level helpers (callers must hold s.mu) ---

func bindArgs(st *sqlite3.Stmt, args []any) error {
	for i, a := range args {
		var err error
		switch v := a.(type) {
		case nil:
			err = st.BindNull(i + 1)
		case string:
			err = st.BindText(i+1, v)
		case int:
			err = st.BindInt(i+1, v)
		case int64:
			err = st.BindInt64(i+1, v)
		case bool:
			if v {
				err = st.BindInt(i+1, 1)
			} else {
				err = st.BindInt(i+1, 0)
			}
		case *string:
			if v == nil {
				err = st.BindNull(i + 1)
			} else {
				err = st.BindText(i+1, *v)
			}
		case *int64:
			if v == nil {
				err = st.BindNull(i + 1)
			} else {
				err = st.BindInt64(i+1, *v)
			}
		default:
			err = fmt.Errorf("unsupported bind type %T", a)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) execLocked(sql string, args ...any) error {
	st, _, err := s.db.Prepare(sql)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := bindArgs(st, args); err != nil {
		return err
	}
	return st.Exec()
}

func (s *Store) queryLocked(sql string, args ...any) (*sqlite3.Stmt, error) {
	st, _, err := s.db.Prepare(sql)
	if err != nil {
		return nil, err
	}
	if err := bindArgs(st, args); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

// exec is execLocked with the store mutex held.
func (s *Store) exec(sql string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.execLocked(sql, args...)
}

const msgCols = `d.id, d.room, d.project, fa.name, ta.name, d.body,
	d.created_at, d.delivered_at, d.read_at`
const msgJoin = `dispatches d
	JOIN actors fa ON fa.id = d.from_actor
	JOIN actors ta ON ta.id = d.to_actor`

func scanMessage(st *sqlite3.Stmt) Message {
	m := Message{
		ID:        st.ColumnInt64(0),
		Room:      st.ColumnText(1),
		Project:   st.ColumnText(2),
		FromAgent: st.ColumnText(3),
		ToAgent:   st.ColumnText(4),
		Body:      st.ColumnText(5),
		CreatedAt: st.ColumnText(6),
	}
	if st.ColumnType(7) != sqlite3.NULL {
		if t := st.ColumnText(7); t != "" {
			m.DeliveredAt = &t
		}
	}
	if st.ColumnType(8) != sqlite3.NULL {
		if t := st.ColumnText(8); t != "" {
			m.ReadAt = &t
		}
	}
	return m
}

// --- actors ---

// UpsertActor returns the id of the (kind, name) actor row, creating it
// when missing and refreshing the mutable fields when present.
func (s *Store) UpsertActor(kind, name, source, model, sessionID, paneID *string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upsertActorLocked(kind, name, source, model, sessionID, paneID)
}

func (s *Store) upsertActorLocked(kind, name, source, model, sessionID, paneID *string) (int64, error) {
	now := nowTS()
	st, err := s.queryLocked(`SELECT id FROM actors WHERE kind=? AND name=?`, kind, name)
	if err != nil {
		return 0, err
	}
	var id int64
	found := false
	if st.Step() {
		id, found = st.ColumnInt64(0), true
	}
	err = st.Err()
	st.Close()
	if err != nil {
		return 0, err
	}
	if found {
		return id, s.execLocked(
			`UPDATE actors SET source=?, model=?, session_id=?, pane_id=?, last_seen_at=? WHERE id=?`,
			source, model, sessionID, paneID, now, id)
	}
	if err := s.execLocked(
		`INSERT INTO actors(kind,name,source,model,session_id,pane_id,first_seen_at,last_seen_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		kind, name, source, model, sessionID, paneID, now, now); err != nil {
		return 0, err
	}
	return s.db.LastInsertRowID(), nil
}

func (s *Store) agentActorLocked(name string) (int64, error) {
	kind := "agent"
	return s.upsertActorLocked(&kind, &name, nil, nil, nil, nil)
}

// --- messages ---

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

// MessagesForInboxRecipients fetches queued messages to any of the
// recipients (oldest first) and marks them delivered in the same
// transaction. In-flight ids are neither returned nor marked.
func (s *Store) MessagesForInboxRecipients(room string, recipients []string, inFlight map[int64]bool) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(recipients) == 0 {
		return nil, nil
	}
	var q strings.Builder
	q.WriteString(`SELECT ` + msgCols + ` FROM ` + msgJoin +
		` WHERE d.kind='message' AND d.status='queued' AND ta.name IN (`)
	args := make([]any, 0, len(recipients)+1)
	for i, r := range recipients {
		if i > 0 {
			q.WriteByte(',')
		}
		q.WriteByte('?')
		args = append(args, r)
	}
	q.WriteByte(')')
	if room != "" {
		q.WriteString(` AND d.room=?`)
		args = append(args, room)
	}
	q.WriteString(` ORDER BY d.id`)
	if err := s.execLocked("BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.execLocked("ROLLBACK")
		}
	}()
	st, err := s.queryLocked(q.String(), args...)
	if err != nil {
		return nil, err
	}
	var msgs []Message
	for st.Step() {
		m := scanMessage(st)
		if inFlight[m.ID] {
			continue
		}
		msgs = append(msgs, m)
	}
	err = st.Err()
	st.Close()
	if err != nil {
		return nil, err
	}
	now := nowTS()
	for i := range msgs {
		if err := s.execLocked(
			`UPDATE dispatches SET status='delivered', delivered_at=?, read_at=COALESCE(read_at,?)
			 WHERE id=? AND status='queued'`, now, now, msgs[i].ID); err != nil {
			return nil, err
		}
	}
	if err := s.execLocked("COMMIT"); err != nil {
		return nil, err
	}
	committed = true
	for i := range msgs {
		d := now
		msgs[i].DeliveredAt = &d
		if msgs[i].ReadAt == nil {
			r := now
			msgs[i].ReadAt = &r
		}
	}
	return msgs, nil
}

// PendingMessagesForAgent returns queued messages to the recipient in one
// room, oldest first.
func (s *Store) PendingMessagesForAgent(room, to string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingLocked(`d.room=? AND ta.name=?`, room, to)
}

// PendingForAgentCreationOrder returns queued messages to the recipient
// from every room but the jobs room, in creation order.
func (s *Store) PendingForAgentCreationOrder(to string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingLocked(`ta.name=? AND d.room<>? ORDER BY d.created_at, d.id`, to, JobsRoom)
}

// pendingLocked is the shared queued-message scan; cond is the WHERE
// fragment and may append an ORDER BY clause.
func (s *Store) pendingLocked(cond string, args ...any) ([]Message, error) {
	q := `SELECT ` + msgCols + ` FROM ` + msgJoin +
		` WHERE d.kind='message' AND d.status='queued' AND ` + cond
	if !strings.Contains(strings.ToUpper(cond), "ORDER BY") {
		q += ` ORDER BY d.id`
	}
	st, err := s.queryLocked(q, args...)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	var msgs []Message
	for st.Step() {
		msgs = append(msgs, scanMessage(st))
	}
	return msgs, st.Err()
}

// PendingNudgesForAgent groups queued messages to the recipient by room;
// rooms order by their oldest undelivered message.
func (s *Store) PendingNudgesForAgent(to string) ([]PendingNudge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.queryLocked(
		`SELECT d.room, COUNT(*), MIN(d.id) FROM `+msgJoin+
			` WHERE d.kind='message' AND d.status='queued' AND ta.name=?
		 GROUP BY d.room ORDER BY MIN(d.id)`, to)
	if err != nil {
		return nil, err
	}
	type rk struct {
		room  string
		count int
	}
	var rows []rk
	for st.Step() {
		rows = append(rows, rk{st.ColumnText(0), int(st.ColumnInt64(1))})
	}
	err = st.Err()
	st.Close()
	if err != nil {
		return nil, err
	}
	out := make([]PendingNudge, 0, len(rows))
	for _, r := range rows {
		n, err := s.pendingNudgeLocked(r.room, to)
		if err != nil {
			return nil, err
		}
		n.Count = r.count
		out = append(out, *n)
	}
	return out, nil
}

// PendingNudgeFor summarizes one room's queued mail to the recipient; nil
// when there is none.
func (s *Store) PendingNudgeFor(room, to string) (*PendingNudge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingNudgeLocked(room, to)
}

func (s *Store) pendingNudgeLocked(room, to string) (*PendingNudge, error) {
	st, err := s.queryLocked(
		`SELECT COUNT(*), fa.name FROM `+msgJoin+
			` WHERE d.kind='message' AND d.status='queued' AND d.room=? AND ta.name=?
		 GROUP BY d.id ORDER BY d.id DESC`, room, to)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	n := &PendingNudge{Room: room, ToAgent: to}
	for st.Step() {
		n.Count++
		if n.Count == 1 {
			n.LatestFrom = st.ColumnText(1)
		}
	}
	if err := st.Err(); err != nil {
		return nil, err
	}
	if n.Count == 0 {
		return nil, nil
	}
	return n, nil
}

// History returns the newest limit dispatches of a room (filtered by
// project when given), oldest first.
func (s *Store) History(room string, project *string, limit int) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := `SELECT ` + msgCols + ` FROM ` + msgJoin + ` WHERE d.room=?`
	args := []any{room}
	if project != nil {
		q += ` AND d.project=?`
		args = append(args, *project)
	}
	q += ` ORDER BY d.id DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	st, err := s.queryLocked(q, args...)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	var msgs []Message
	for st.Step() {
		msgs = append(msgs, scanMessage(st))
	}
	if err := st.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// Rooms lists rooms that hold dispatches, most recent activity first.
func (s *Store) Rooms() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.queryLocked(
		`SELECT room FROM dispatches GROUP BY room ORDER BY MIN(id)`)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	var rooms []string
	for st.Step() {
		rooms = append(rooms, st.ColumnText(0))
	}
	return rooms, st.Err()
}

// --- delivery marks ---

// markDelivered runs the queued→delivered transition and reports how many
// rows changed.
func (s *Store) markDelivered(cond string, args ...any) (int, error) {
	now := nowTS()
	sql := `UPDATE dispatches SET status='delivered', delivered_at=?,
		read_at=COALESCE(read_at,?) WHERE status='queued' AND ` + cond
	all := append([]any{now, now}, args...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.execLocked(sql, all...); err != nil {
		return 0, err
	}
	return int(s.db.Changes()), nil
}

// MarkMessageDelivered marks one queued dispatch delivered; it is a no-op
// once the row left 'queued'.
func (s *Store) MarkMessageDelivered(id int64) (bool, error) {
	n, err := s.markDelivered(`id=?`, id)
	return n > 0, err
}

// MarkDelivered marks every queued message to a recipient in one room.
func (s *Store) MarkDelivered(room, to string) (int, error) {
	return s.markDelivered(
		`kind='message' AND room=? AND to_actor=(SELECT id FROM actors WHERE kind='agent' AND name=?)`,
		room, to)
}

// MarkDeliveredByIDs marks exactly the listed ids still queued — a late
// arrival keeps waiting for its own write.
func (s *Store) MarkDeliveredByIDs(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var q strings.Builder
	q.WriteString(`kind='message' AND id IN (`)
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			q.WriteByte(',')
		}
		q.WriteByte('?')
		args[i] = id
	}
	q.WriteByte(')')
	return s.markDelivered(q.String(), args...)
}

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
