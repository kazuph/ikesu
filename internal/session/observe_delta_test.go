package session

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/platform"
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
	stale := rolloutFile(t, root, "rollout-stale-incomplete-id.jsonl", "", "", start.Add(-121*time.Second))
	if id, ok := ObserveCodexSessionFile("/work", start); !ok || id != "current" {
		t.Fatal("known old metadata must not hide current session", id, ok)
	}
	os.Remove(stale)
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

func TestPaneLaunchSessionRootsOverrideServerHome(t *testing.T) {
	server := t.TempDir()
	t.Setenv("HOME", server)
	child := t.TempDir()
	start := time.Now()
	root := filepath.Join(child, ".codex", "sessions")
	rolloutFile(t, root, "rollout-child.jsonl", "child-id", "/work", start)
	id, ok := ObserveProcessSession("codex", "/work", start, platform.ForegroundJob{}, []string{"HOME=" + child})
	if !ok || id != "child-id" {
		t.Fatal(id, ok)
	}
	custom := t.TempDir()
	rolloutFile(t, filepath.Join(custom, "sessions"), "rollout-custom.jsonl", "configured-id", "/work", start)
	id, ok = ObserveProcessSession("codex", "/work", start, platform.ForegroundJob{}, []string{"HOME=" + child, "CODEX_HOME=" + custom})
	if !ok || id != "configured-id" {
		t.Fatal(id, ok)
	}
	claude := t.TempDir()
	projects := filepath.Join(claude, "projects", "project")
	os.MkdirAll(projects, 0700)
	data, _ := json.Marshal(map[string]string{"sessionId": "claude-current", "cwd": "/work", "timestamp": start.Format(time.RFC3339Nano)})
	os.WriteFile(filepath.Join(projects, "session.jsonl"), append(data, '\n'), 0600)
	id, ok = ObserveProcessSession("claude", "/work", start, platform.ForegroundJob{}, []string{"HOME=" + child, "CLAUDE_CONFIG_DIR=" + claude})
	if !ok || id != "claude-current" {
		t.Fatal(id, ok)
	}
}

func TestClaudeRuntimePIDAndStartAreExact(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sessions"), 0700)
	start := time.Now()
	job := platform.ForegroundJob{Processes: []platform.ForegroundProcess{{PID: 42}}}
	file := filepath.Join(root, "sessions", "42.json")
	for _, test := range []struct {
		pid    int
		cwd    string
		offset time.Duration
		want   bool
	}{{42, "/work", 0, true}, {43, "/work", 0, false}, {42, "/other", 0, false}, {42, "/work", -121 * time.Second, false}} {
		raw, _ := json.Marshal(map[string]any{"pid": test.pid, "cwd": test.cwd, "startedAt": start.Add(test.offset).UnixMilli(), "sessionId": "current-runtime"})
		os.WriteFile(file, raw, 0600)
		id, ok := observeClaudeRuntime(root, "/work", start, job)
		if ok != test.want || ok && id != "current-runtime" {
			t.Fatal(test, id, ok)
		}
	}
}
