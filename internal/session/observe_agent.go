package session

import (
	"bufio"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/platform"
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
	return observeClaudeRoot(filepath.Join(home, ".claude"), cwd, start)
}

func observeClaudeRoot(configRoot, cwd string, start time.Time) (string, bool) {
	root := filepath.Join(configRoot, "projects")
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

// ObserveCodexSessionFile correlates only recent, fully readable rollouts.
func ObserveCodexSessionFile(cwd string, start time.Time) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return observeCodexFiles(filepath.Join(home, ".codex", "sessions"), cwd, start, nil)
}

// ObserveProcessSession additionally uses rollouts opened by the live process.
func ObserveProcessSession(agent, cwd string, start time.Time, job platform.ForegroundJob, environments ...[]string) (string, bool) {
	env := os.Environ()
	if len(environments) > 0 {
		env = environments[0]
	}
	values := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			values[k] = v
		}
	}
	home := values["HOME"]
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	switch agent {
	case "claude":
		root := values["CLAUDE_CONFIG_DIR"]
		if root == "" {
			root = filepath.Join(home, ".claude")
		}
		if id, ok := observeClaudeRuntime(root, cwd, start, job); ok {
			return id, true
		}
		return observeClaudeRoot(root, cwd, start)
	case "codex":
		root := values["CODEX_HOME"]
		if root == "" {
			root = filepath.Join(home, ".codex")
		}
		var paths []string
		for _, process := range job.Processes {
			paths = append(paths, platform.OpenJSONLFiles(process.PID)...)
		}
		return observeCodexFiles(filepath.Join(root, "sessions"), cwd, start, paths)
	}
	return "", false
}
func observeCodexFiles(root, cwd string, start time.Time, opened []string) (string, bool) {
	if cwd == "" {
		return "", false
	}
	paths := map[string]bool{}
	for _, path := range opened {
		paths[path] = true
	}
	uncertain := false
	cutoff := start.Add(-sessionFileWindow)
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if !os.IsNotExist(err) {
				uncertain = true
			}
			return nil
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			paths[path] = true
		}
		return nil
	}); err != nil {
		uncertain = true
	}
	ids := map[string]bool{}
	for path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			uncertain = true
			continue
		}
		if info.ModTime().Before(cutoff) {
			continue
		}
		id, fcwd, ts, ok := codexSessionMeta(path)
		if !ts.IsZero() && ts.Before(cutoff) {
			continue
		}
		if !ok || fcwd == "" {
			uncertain = true
			continue
		}
		if ts.Before(cutoff) || fcwd != cwd {
			continue
		}
		ids[id] = true
	}
	if uncertain || len(ids) != 1 {
		return "", false
	}
	for id := range ids {
		return id, true
	}
	return "", false
}

// observeCmdline reads the resume-shape argv of the pane's running
// processes: `X --resume <id>`, `X --resume=<id>`, `X resume <id>`. It is
// a launch-shape fallback only — the caller never lets it overwrite a
// file-observed or reported id. All candidate processes must agree on one
// id or the observation is ambiguous.
func ObserveCmdline(job platform.ForegroundJob) (string, bool) {
	found := ""
	for _, pr := range job.Processes {
		if id, native, ok := ObserveNativeResume(pr); native {
			if !ok {
				return "", false
			}
			if id != "" {
				if found != "" && found != id {
					return "", false
				}
				found = id
			}
			continue
		}
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
			if v == "" || strings.HasPrefix(v, "-") {
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
