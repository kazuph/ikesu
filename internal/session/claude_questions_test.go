package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeQuestionRecordsQuestionAndAnswerTimes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := "12345678-1234-1234-1234-123456789abc"
	rows := `{"timestamp":"2026-11-06T14:33:01Z","sessionId":"` + id + `","message":{"content":[{"type":"tool_use","name":"AskUserQuestion","id":"q","input":{"questions":[{"question":"質問","options":[{"label":"はい"}]}]}}]}}` + "\n" +
		`{"timestamp":"2026-11-06T14:34:01Z","sessionId":"` + id + `","message":{"content":[{"type":"tool_result","tool_use_id":"q"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(rows), 0600); err != nil {
		t.Fatal(err)
	}
	got, ok := new(ClaudeQuestionCache).ReadAll(root, id)
	start, _ := time.Parse(time.RFC3339, "2026-11-06T14:33:01Z")
	end, _ := time.Parse(time.RFC3339, "2026-11-06T14:34:01Z")
	if !ok || len(got) != 1 || !got[0].Answered || !got[0].StartedAt.Equal(start) || !got[0].AnsweredAt.Equal(end) {
		t.Fatalf("question times: %+v %v", got, ok)
	}
}

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
	var cache ClaudeQuestionCache
	if got, ok := cache.Read(root, id); !ok || len(got) != 2 {
		t.Fatal("initial cached read", got, ok)
	}
	all, ok := cache.ReadAll(root, id)
	if !ok || len(all) != 3 {
		t.Fatal("all calls missing", all, ok)
	}
	for _, q := range all {
		if q.ToolID == "old" && !q.Answered || q.ToolID == "new" && q.Answered {
			t.Fatal("tool_result did not mark the correct call", q)
		}
	}
	resolved := strings.TrimSuffix(content, `{"sessionId":`) + `{"sessionId":"` + id + `","message":{"content":[{"type":"tool_result","tool_use_id":"new"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(resolved), 0600); err != nil {
		t.Fatal(err)
	}
	if got, ok := cache.Read(root, id); !ok || len(got) != 0 {
		t.Fatal("resolved tool appeared", got, ok)
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

func TestClaudeQuestionsDoNotMixSessionsInSameDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "same-cwd")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ids := []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"}
	for _, id := range ids {
		line := `{"sessionId":"` + id + `","cwd":"/same","message":{"content":[{"type":"tool_use","id":"tool-` + id + `","name":"AskUserQuestion","input":{"questions":[{"question":"question-` + id + `","options":[{"label":"yes"}]}]}}]}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range ids {
		got, ok := PendingClaudeQuestions(root, id)
		if !ok || len(got) != 1 || got[0].SessionID != id || got[0].ToolID != "tool-"+id || got[0].Question != "question-"+id {
			t.Fatalf("%s: %+v ok=%v", id, got, ok)
		}
	}
}

func TestClaudeQuestionArrivingAfterInitialRead(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := "33333333-3333-3333-3333-333333333333"
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(`{"sessionId":"`+id+`","message":{"content":"start"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var cache ClaudeQuestionCache
	if got, ok := cache.Read(root, id); !ok || len(got) != 0 {
		t.Fatal(got, ok)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"sessionId":"` + id + `","message":{"content":[{"type":"tool_use","name":"AskUserQuestion","id":"late","input":{"questions":[{"question":"全文","options":[{"label":"はい","description":"長い説明"}]}]}}]}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.Read(root, id)
	if !ok || len(got) != 1 || got[0].ToolID != "late" || got[0].Options[0].Description != "長い説明" {
		t.Fatal(got, ok)
	}
}
