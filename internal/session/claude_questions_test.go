package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPendingClaudeQuestions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	const id = "12345678-1234-1234-1234-123456789abc"
	path := filepath.Join(dir, id+".jsonl")
	content := `{"sessionId":"` + id + `","message":{"content":[{"type":"tool_use","id":"old","name":"AskUserQuestion","input":{"questions":[{"question":"old","options":[{"label":"old"}]}]}}]}}
{"sessionId":"` + id + `","message":{"content":[{"type":"tool_result","tool_use_id":"old"},{"type":"tool_use","id":"new","name":"AskUserQuestion","input":{"questions":[{"header":"one","question":"long question","options":[{"label":"yes","description":"full description"}]},{"header":"two","question":"second","multiSelect":true,"options":[{"label":"other","description":"detail"}]}]}}]}}
{"sessionId":`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got, ok := PendingClaudeQuestions(root, id)
	if !ok || len(got) != 2 || got[0].ToolID != "new" || got[0].Question != "long question" || got[0].Options[0].Description != "full description" || got[1].Index != 1 || !got[1].Multi {
		t.Fatalf("questions=%+v ok=%v", got, ok)
	}
	other := filepath.Join(root, "projects", "other")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, id+".jsonl"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := PendingClaudeQuestions(root, id); ok {
		t.Fatal("ambiguous session files accepted")
	}
	if _, ok := PendingClaudeQuestions(root, "missing"); ok {
		t.Fatal("missing session accepted")
	}
}
