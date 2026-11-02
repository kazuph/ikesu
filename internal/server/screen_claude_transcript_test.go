package server

import (
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestClaudeTranscriptScreenMatching(t *testing.T) {
	p := &screenprompt.Prompt{Params: api.DecisionCreateParams{Kind: "agent_prompt", Title: "長い質問の後半", Options: []api.DecisionOption{{ID: "1", Label: "選択肢"}}}}
	q := session.ClaudeQuestion{Question: "画面に入りきらない長い質問の後半", Options: []session.ClaudeQuestionOption{{Label: "選択肢", Description: "全文"}}}
	if !matchClaudeQuestion(p, q) {
		t.Fatal("visible part should match")
	}
	p.Params.Title = "画面に入りきらない長い質問の後半"
	if !matchClaudeQuestion(p, q) {
		t.Fatal("wider screen should identify the same question")
	}
	q.Options[0].Label = "別の選択肢"
	if matchClaudeQuestion(p, q) {
		t.Fatal("different labels must not match")
	}
	q.Options[0].Label = "無関係"
	if matchClaudeQuestion(p, q) {
		t.Fatal("different choices must not match")
	}
	q.Options[0].Label = "選択肢"
	p.Total, p.Question, q.Index = 2, 1, 0
	if matchClaudeQuestion(p, q) {
		t.Fatal("different question positions must not match")
	}
}

func TestScreenQuestionIdentityReusesPendingDecision(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	original := screenRecord(t, s)
	identity := `["session","tool",0]`
	var reused bool
	var reusedID string
	s.app.Do(func() {
		if err := s.store.BindScreenQuestion(original.DecisionID, "p1", identity); err != nil {
			t.Error(err)
			return
		}
		p := s.readScreenPrompt(1)
		w := &screenPromptWatch{prompt: p, signature: p.Signature(), transcriptID: identity}
		reused = s.reuseScreenPrompt(1, w)
		reusedID = w.decisionID
	})
	if !reused || reusedID != original.DecisionID {
		t.Fatalf("reused=%v id=%s original=%s", reused, reusedID, original.DecisionID)
	}
}

func TestClaudeDisplayRetainsScreenPreview(t *testing.T) {
	preview, short, full := "実際のプレビュー", "短い説明", "記録から得た全文"
	w := &screenPromptWatch{prompt: &screenprompt.Prompt{Params: api.DecisionCreateParams{Title: "画面の一部", Options: []api.DecisionOption{{ID: "1", Label: "選択", Description: &short, Preview: &preview}}}}, displayParams: &api.DecisionCreateParams{Title: "質問全文", Options: []api.DecisionOption{{ID: "1", Label: "選択", Description: &full}}}}
	got := screenDisplayParams(w)
	if got.Title != "質問全文" || got.Options[0].Preview == nil || *got.Options[0].Preview != preview || *got.Options[0].Description != full {
		t.Fatal(got)
	}
}

func TestClaudeTranscriptPresentationRemovesPartialScreenBody(t *testing.T) {
	partial, heading := "全文の後半", "質問 1/2"
	p := api.DecisionCreateParams{Title: "先頭", Body: &partial, QuestionLabel: &heading}
	got := claudeQuestionPresentation(p, session.ClaudeQuestion{Question: "質問の全文。\n\n全文の後半"})
	if got.Title != "質問の全文。\n\n全文の後半" || got.Body != nil || got.QuestionLabel == nil || *got.QuestionLabel != heading {
		t.Fatalf("old partial body remained: %+v", got)
	}
}

func TestClaudeCompletedAndAmbiguousQuestionsAreNotPresented(t *testing.T) {
	p := &screenprompt.Prompt{Params: api.DecisionCreateParams{Kind: "agent_prompt", Title: "質問", Options: []api.DecisionOption{{ID: "1", Label: "はい"}}}}
	q := session.ClaudeQuestion{ToolID: "old", Question: "質問", Options: []session.ClaudeQuestionOption{{Label: "はい"}}, Answered: true}
	if match, _, resolved := chooseClaudeQuestion(p, []session.ClaudeQuestion{q}); match != nil || !resolved {
		t.Fatal("completed question shown")
	}
	newQuestion := q
	newQuestion.ToolID = "new"
	newQuestion.Answered = false
	if match, unsafe, resolved := chooseClaudeQuestion(p, []session.ClaudeQuestion{q, newQuestion}); match == nil || match.ToolID != "new" || unsafe || resolved {
		t.Fatal("new call not distinguished")
	}
	other := newQuestion
	other.ToolID = "other"
	if match, unsafe, _ := chooseClaudeQuestion(p, []session.ClaudeQuestion{newQuestion, other}); match != nil || !unsafe {
		t.Fatal("ambiguous calls accepted")
	}
}
