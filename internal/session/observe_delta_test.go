package session

import (
	"encoding/json"
	"github.com/kazuph/goterrace/internal/platform"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rolloutFile(t *testing.T, root, name, id, cwd string, ts time.Time) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": id, "cwd": cwd, "timestamp": ts.Format(time.RFC3339Nano)}})
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestCodexRecentRolloutsFailClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	start := time.Now()
	root := filepath.Join(home, ".codex", "sessions", "older-folder")
	valid := rolloutFile(t, root, "rollout-valid.jsonl", "current", "/work", start.Add(5*time.Minute))
	if id, ok := ObserveCodexSessionFile("/work", start); !ok || id != "current" {
		t.Fatal(id, ok)
	}
	bad := filepath.Join(root, "rollout-incomplete.jsonl")
	os.WriteFile(bad, []byte("{incomplete"), 0600)
	if _, ok := ObserveCodexSessionFile("/work", start); ok {
		t.Fatal("incomplete candidate hidden")
	}
	old := start.Add(-sessionFileWindow - time.Second)
	os.Chtimes(bad, old, old)
	if _, ok := ObserveCodexSessionFile("/work", start); !ok {
		t.Fatal("old mtime must be excluded")
	}
	other := rolloutFile(t, root, "rollout-other.jsonl", "second", "/work", start)
	if _, ok := ObserveCodexSessionFile("/work", start); ok {
		t.Fatal("ambiguous sessions")
	}
	os.Remove(other)
	os.Chtimes(valid, old, old)
	if _, ok := ObserveCodexSessionFile("/work", start); ok {
		t.Fatal("valid meta cannot override old mtime")
	}
}
func TestCodexOpenJSONLCorrelation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	start := time.Now()
	path := rolloutFile(t, t.TempDir(), "active.jsonl", "opened-id", "/work", start)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	job := platform.ForegroundJob{Processes: []platform.ForegroundProcess{{PID: os.Getpid()}}}
	id, ok := ObserveProcessSession("codex", "/work", start, job)
	if !ok || id != "opened-id" {
		t.Fatal(id, ok)
	}
	if _, ok := ObserveProcessSession("codex", "/else", start, job); ok {
		t.Fatal("foreign cwd")
	}
}
func TestCodexResumeLastIsNotID(t *testing.T) {
	job := platform.ForegroundJob{Processes: []platform.ForegroundProcess{{Argv: []string{"codex", "resume", "--last"}}}}
	if id, ok := ObserveCmdline(job); ok {
		t.Fatal(id)
	}
}
