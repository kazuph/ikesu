CREATE TABLE actors (
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
CREATE TABLE sqlite_sequence(name,seq);
CREATE UNIQUE INDEX idx_actors_identity ON actors(kind, name);
CREATE TABLE dispatches (
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
  log_path      TEXT
, read_at TEXT);
CREATE INDEX idx_dispatch_pending ON dispatches(to_actor, status);
CREATE INDEX idx_dispatch_room_time ON dispatches(room, created_at);
CREATE INDEX idx_dispatch_reply ON dispatches(reply_to);
CREATE UNIQUE INDEX idx_dispatch_external ON dispatches(kind, external_id);
CREATE VIEW v_reply_latency AS
SELECT d.id, d.room, d.project,
       fa.name AS asked_by,  fa.model AS asked_model,
       ta.name AS answered_by, ta.model AS answered_model,
       d.created_at, d.replied_at,
       CAST((julianday(d.replied_at) - julianday(d.created_at)) * 86400 AS INTEGER) AS reply_seconds
FROM dispatches d
JOIN actors fa ON fa.id = d.from_actor
JOIN actors ta ON ta.id = d.to_actor
WHERE d.kind = 'message' AND d.replied_at IS NOT NULL
/* v_reply_latency(id,room,project,asked_by,asked_model,answered_by,answered_model,created_at,replied_at,reply_seconds) */;
CREATE VIEW v_unanswered AS
SELECT d.id, d.room,
       fa.name AS asked_by, ta.name AS asked_to,
       d.body, d.created_at,
       CAST((julianday('now') - julianday(d.created_at)) * 86400 AS INTEGER) AS waiting_seconds
FROM dispatches d
JOIN actors fa ON fa.id = d.from_actor
JOIN actors ta ON ta.id = d.to_actor
WHERE d.expects_reply = 1 AND d.replied_at IS NULL
/* v_unanswered(id,room,asked_by,asked_to,body,created_at,waiting_seconds) */;
CREATE VIEW v_command_stats AS
SELECT COALESCE(label, body) AS label, COUNT(*) AS runs,
       AVG(CASE WHEN exit_code = 0 THEN 1.0 ELSE 0.0 END) AS success_rate,
       CAST(AVG((julianday(finished_at) - julianday(started_at)) * 86400) AS INTEGER) AS avg_seconds,
       CAST(MAX((julianday(finished_at) - julianday(started_at)) * 86400) AS INTEGER) AS max_seconds
FROM dispatches
WHERE kind = 'command' AND finished_at IS NOT NULL
GROUP BY COALESCE(label, body)
/* v_command_stats(label,runs,success_rate,avg_seconds,max_seconds) */;
CREATE VIEW v_model_matrix AS
SELECT fa.model AS asked_model, ta.model AS answered_model, COUNT(*) AS dispatches
FROM dispatches d
JOIN actors fa ON fa.id = d.from_actor
JOIN actors ta ON ta.id = d.to_actor
GROUP BY fa.model, ta.model
/* v_model_matrix(asked_model,answered_model,dispatches) */;
