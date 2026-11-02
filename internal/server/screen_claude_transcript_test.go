package server

import (
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
)

func TestClaudeTranscriptScreenMatching(t *testing.T) {
	p := &screenprompt.Prompt{Params: api.DecisionCreateParams{Kind: "agent_prompt", Title: "長い質問の後半", Options: []api.DecisionOption{{ID: "1", Label: "選択肢"}}}}
	q := session.ClaudeQuestion{Question: "画面に入りきらない長い質問の後半", Options: []session.ClaudeQuestionOption{{Label: "選択肢", Description: "全文"}}}
	if !matchClaudeQuestion(p, q) {
		t.Fatal("visible part should match")
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
