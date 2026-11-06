package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestLegacyAnsweredQuestionClosesFromRollout(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 11, 5, 1, 3, 23, 641000000, time.UTC)
	label := "Question 1/1"
	params := api.DecisionCreateParams{Kind: "agent_prompt", Title: "色は？", QuestionLabel: &label, Origin: &api.DecisionOrigin{PaneID: "p1", Agent: "codex", Reason: "screen_prompt"}, Options: []api.DecisionOption{{ID: "1", Label: "青", Role: "other"}}}
	d, err := store.CreateDecision(params, created)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	raw := `{"type":"session_meta","payload":{"id":"session-1"}}
{"timestamp":"2026-11-05T01:03:23.183Z","type":"response_item","payload":{"type":"function_call","name":"request_user_input","call_id":"call-1","arguments":"{\"questions\":[{\"id\":\"a\",\"question\":\"色は？\",\"options\":[{\"label\":\"青\"}]}]}"}}
{"type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"{\"answers\":{\"a\":{\"answers\":[\"None of the above\",\"user_note: 試験の回答\"]}}}"}}
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	questions, err := session.ReadCodexQuestions(path, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	q, ok := restoredCodexMatchAtProcessStart(d, questions, "", created.Add(-time.Minute))
	if !ok || q.State != session.CodexAnswered {
		t.Fatalf("answered question not matched: %+v", q)
	}
	if _, ok := restoredCodexMatchAtProcessStart(d, questions, "", created.Add(time.Second)); ok {
		t.Fatal("process started after decision")
	}
	tooOld := q
	tooOld.Timestamp = created.Add(-16 * time.Second)
	if _, ok := restoredCodexMatchAtProcessStart(d, []session.CodexQuestion{tooOld}, "", created.Add(-time.Minute)); ok {
		t.Fatal("out-of-window question accepted")
	}
	duplicate := q
	duplicate.CallID = "another"
	if _, ok := restoredCodexMatchAtProcessStart(d, []session.CodexQuestion{q, duplicate}, "", created.Add(-time.Minute)); ok {
		t.Fatal("ambiguous answered questions accepted")
	}
	wrong := q
	wrong.Options = []session.CodexQuestionOption{{Label: "赤"}}
	if _, ok := restoredCodexMatchAtProcessStart(d, []session.CodexQuestion{wrong}, "", created.Add(-time.Minute)); ok {
		t.Fatal("different options accepted")
	}
	w := &screenPromptWatch{decisionID: d.DecisionID, prompt: &screenprompt.Prompt{CodexQuestion: true, Params: params}, codex: &codexBinding{question: q}}
	s.app.Do(func() {
		s.app.SetReported(1, "codex", "working")
		s.syncDecisionAttention()
		if s.app.PaneStatusKey(1) != "blocked" {
			t.Fatal("pending decision did not block pane")
		}
		s.finishRestoredCodexQuestion(1, w)
		if s.app.PaneStatusKey(1) != "working" {
			t.Fatal("answered decision did not restore pane state")
		}
	})
	got, err := store.Decision(d.DecisionID)
	if err != nil || got.Status != "cancelled" || got.Body == nil || *got.Body != "ほかで回答済み" {
		t.Fatalf("decision=%+v err=%v", got, err)
	}
}
