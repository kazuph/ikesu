package session

import (
	"bufio"
	"encoding/json"
	"github.com/kazuph/goterrace/internal/platform"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The existing SPEC G3 window requires a unique cwd/start-time match.
const sessionFileWindow = 120 * time.Second

// procStartOf returns the start time of the foreground job's leader.
func ProcessStart(job platform.ForegroundJob) (time.Time, bool) {
	pid := job.ProcessGroupID
	if pid <= 0 && len(job.Processes) > 0 {
		pid = job.Processes[0].PID
	}
	if pid <= 0 {
		return time.Time{}, false
	}
	return platform.ProcStart(pid)
}

// observeSessionFile finds the agent's session file written near the
// process start, restricted to the pane's cwd. Exactly one candidate
// within the window is required; zero or many means "not determined".
func ObserveSessionFile(agent, cwd string, start time.Time) (string, bool) {
	if cwd == "" {
		return "", false
	}
	switch agent {
	case "claude":
		return ObserveClaudeSessionFile(cwd, start)
	case "codex":
		return ObserveCodexSessionFile(cwd, start)
	}
	return "", false
}

// jsonStringField reads a string field from one complete JSON record.
func jsonStringField(line, key string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(line), &fields) != nil {
		return ""
	}
	var value string
	if json.Unmarshal(fields[key], &value) != nil {
		return ""
	}
	return value
}

func payloadField(line, key string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(line), &fields) != nil {
		return ""
	}
	return jsonStringField(string(fields["payload"]), key)
}

// parseSessionTS parses an RFC3339 timestamp from a session record.
func parseSessionTS(v string) (time.Time, bool) {
	ts, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		if ts, err = time.Parse("2006-01-02T15:04:05.000Z", v); err != nil {
			return time.Time{}, false
		}
	}
	return ts, true
}

// headLines calls fn for each of the first n lines of path, stopping
// early when fn returns false.
func headLines(path string, n int, fn func(line string) bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 4<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n > 0 && sc.Scan() {
		n--
		if !fn(sc.Text()) {
			return
		}
	}
}

// observeClaudeSessionFile scans ~/.claude/projects/**/*.jsonl for the
// unique file whose first 64 lines record this cwd and a timestamp
// within the window of the agent process's start (SPEC G3). The project
// folder name is not trusted — distinct cwds slugify to the same dir —
// so the recorded cwd field decides, and the id is the recorded
// sessionId, never the file name.
func ObserveClaudeSessionFile(cwd string, start time.Time) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	root := filepath.Join(home, ".claude", "projects")
	// A session file's recorded timestamp precedes its last write, so a
	// file untouched since before the window cannot qualify; checking
	// mtime first keeps the scan cheap.
	cutoff := start.Add(-sessionFileWindow)
	found := ""
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if info, err := d.Info(); err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		var id, fcwd string
		var ts time.Time
		headLines(path, 64, func(line string) bool {
			if id == "" {
				id = jsonStringField(line, "sessionId")
			}
			if fcwd == "" {
				fcwd = jsonStringField(line, "cwd")
			}
			if ts.IsZero() {
				if v := jsonStringField(line, "timestamp"); v != "" {
					if t, ok := parseSessionTS(v); ok {
						ts = t
					}
				}
			}
			return id == "" || fcwd == "" || ts.IsZero()
		})
		if id == "" || fcwd != cwd || ts.IsZero() || absDur(ts.Sub(start)) > sessionFileWindow {
			return nil
		}
		count++
		found = id
		return nil
	})
	if count != 1 {
		return "", false
	}
	return found, true
}

// codexSessionMeta reads a rollout file's session_meta record (the first
// line carrying one) and returns payload.id (or payload.session_id),
// payload.cwd, and payload.timestamp.
func codexSessionMeta(path string) (id, cwd string, ts time.Time, ok bool) {
	headLines(path, 8, func(line string) bool {
		if jsonStringField(line, "type") != "session_meta" {
			return true
		}
		id = payloadField(line, "id")
		if id == "" {
			id = payloadField(line, "session_id")
		}
		cwd = payloadField(line, "cwd")
		ts, _ = parseSessionTS(payloadField(line, "timestamp"))
		ok = true
		return false
	})
	return id, cwd, ts, ok && id != "" && !ts.IsZero()
}

// observeCodexSessionFile scans the day folders sessions/YYYY/MM/DD
// around the process start for the unique rollout whose session_meta
// records this cwd and a timestamp within the window (SPEC G3). A meta
// record without payload.cwd never counts.
func ObserveCodexSessionFile(cwd string, start time.Time) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	root := filepath.Join(home, ".codex", "sessions")
	found := ""
	count := 0
	for _, day := range []time.Time{start.AddDate(0, 0, -1), start, start.AddDate(0, 0, 1)} {
		dir := filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02"))
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			id, fcwd, ts, ok := codexSessionMeta(filepath.Join(dir, name))
			if !ok || fcwd != cwd || absDur(ts.Sub(start)) > sessionFileWindow {
				continue
			}
			count++
			found = id
		}
	}
	if count != 1 {
		return "", false
	}
	return found, true
}

// observeCmdline reads the resume-shape argv of the pane's running
// processes: `X --resume <id>`, `X --resume=<id>`, `X resume <id>`. It is
// a launch-shape fallback only — the caller never lets it overwrite a
// file-observed or reported id. All candidate processes must agree on one
// id or the observation is ambiguous.
func ObserveCmdline(job platform.ForegroundJob) (string, bool) {
	found := ""
	for _, pr := range job.Processes {
		for i, arg := range pr.Argv {
			var v string
			switch {
			case arg == "--resume" && i+1 < len(pr.Argv):
				v = pr.Argv[i+1]
			case strings.HasPrefix(arg, "--resume="):
				v = strings.TrimPrefix(arg, "--resume=")
			case arg == "resume" && i > 0 && i+1 < len(pr.Argv):
				// codex-style `… resume <id>`; argv[i-1] is the binary.
				v = pr.Argv[i+1]
			}
			if v == "" {
				continue
			}
			if found == "" {
				found = v
			} else if found != v {
				return "", false
			}
		}
	}
	return found, found != ""
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
