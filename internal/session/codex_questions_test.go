package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/platform"
)

func TestCodexRolloutRequiresOpenProcessFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\"}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	job := platform.ForegroundJob{Processes: []platform.ForegroundProcess{{PID: os.Getpid()}}}
	gotPath, id, err := OpenCodexRollout(job)
	canonical, canonicalErr := filepath.EvalSymlinks(path)
	if err != nil || canonicalErr != nil || gotPath != canonical || id != "session-1" {
		t.Fatalf("rollout: %q %q %v", gotPath, id, err)
	}
	if _, _, err := OpenCodexRollout(platform.ForegroundJob{}); err == nil {
		t.Fatal("no process accepted")
	}
}

func TestCodexQuestionsDistinguishesMissingAndAnswered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	data := `{"type":"session_meta","payload":{"id":"session-1"}}
{"type":"response_item","payload":{"type":"other","output":[{"type":"text","text":"unrelated"}]}}
{"type":"response_item","payload":{"type":"function_call","name":"request_user_input","call_id":"call-1","arguments":"{\"questions\":[{\"id\":\"a\",\"question\":\"色は？\",\"options\":[{\"label\":\"青\"}]},{\"id\":\"b\",\"question\":\"形は？\",\"options\":[{\"label\":\"丸\"}]}]}"}}
{"type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"{\"answers\":{\"a\":{\"answers\":[\"青\",\"user_note: 濃い青\"]}}}"}}
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	questions, err := ReadCodexQuestions(path, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 2 || questions[0].ID != "a" || questions[0].State != CodexAnswered || questions[1].ID != "b" || questions[1].State != CodexEmpty {
		t.Fatalf("question states: %+v", questions)
	}
}

func TestCodexQuestionsRejectsIncompleteAndForeignSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"other\"}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCodexQuestions(path, "session-1"); err == nil {
		t.Fatal("foreign session accepted")
	}
	if err := os.WriteFile(path, []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\"}}\n{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCodexQuestions(path, "session-1"); err == nil {
		t.Fatal("incomplete record accepted")
	}
}
