package server

import (
	"os"
	"strings"
	"testing"
	"time"

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

func TestClaudeTranscriptRejectsConflictingVisibleBodyAndDescription(t *testing.T) {
	body, description := "同じ先頭\n\n別の操作を実行", "別の説明"
	p := &screenprompt.Prompt{Params: api.DecisionCreateParams{Kind: "agent_prompt", Title: "同じ先頭", Body: &body, Options: []api.DecisionOption{{Label: "はい"}}}}
	q := session.ClaudeQuestion{Question: "同じ先頭\n\n安全な操作を実行", Options: []session.ClaudeQuestionOption{{Label: "はい", Description: "正しい説明"}}}
	if matchClaudeQuestion(p, q) {
		t.Fatal("conflicting second paragraph matched")
	}
	body = q.Question
	p.Params.Options[0].Description = &description
	if matchClaudeQuestion(p, q) {
		t.Fatal("conflicting option description matched")
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

func TestScreenQuestionRecreationClosesUntrackedDuplicate(t *testing.T) {
	teststate.Lock(t)
	s, p, _ := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	old := screenRecord(t, s)
	identity := `["session","tool",0]`
	var current api.Decision
	s.app.Do(func() {
		prompt := s.readScreenPrompt(1)
		params := prompt.Params
		params.Origin = old.Origin
		var err error
		current, err = s.store.CreateScreenDecision(params, time.Now(), "p1", identity)
		if err != nil {
			t.Error(err)
			return
		}
		s.screenPrompts.watches = map[int]*screenPromptWatch{}
		w := &screenPromptWatch{prompt: prompt, signature: prompt.Signature(), transcriptID: identity}
		s.screenPrompts.watches[1] = w
		s.beginScreenPrompt(1, w)
	})
	if current.DecisionID == "" {
		t.Fatal("missing recreated decision")
	}
	if got := screenRecord(t, s); got.DecisionID != current.DecisionID {
		t.Fatalf("reused %s, want %s", got.DecisionID, current.DecisionID)
	}
	paintScreen(p, "")
	scanScreen(s)
	scanScreen(s)
	got, err := s.store.Decision(old.DecisionID)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("old decision remained: %+v %v", got, err)
	}
}

func TestScreenQuestionReconciliationPreservesOtherDecisions(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	old := screenRecord(t, s)
	var other, permission, differentPane api.Decision
	s.app.Do(func() {
		p := s.readScreenPrompt(1)
		params := p.Params
		origin := *old.Origin
		params.Origin = &origin
		var err error
		other, err = s.store.CreateScreenDecision(params, time.Now(), "p1", ` ["other"]`)
		if err != nil {
			t.Error(err)
			return
		}
		params.Kind = "tool_permission"
		permission, err = s.store.CreateDecision(params, time.Now())
		if err != nil {
			t.Error(err)
			return
		}
		params.Kind = "agent_prompt"
		origin.PaneID = "p2"
		differentPane, err = s.store.CreateDecision(params, time.Now())
		if err != nil {
			t.Error(err)
			return
		}
		w := &screenPromptWatch{prompt: p, signature: p.Signature(), transcriptID: `["current"]`}
		s.beginScreenPrompt(1, w)
		if w.decisionID != old.DecisionID {
			t.Errorf("selected %s, want %s", w.decisionID, old.DecisionID)
		}
	})
	for _, d := range []api.Decision{other, permission, differentPane} {
		got, err := s.store.Decision(d.DecisionID)
		if err != nil || got.Status != "pending" {
			t.Fatalf("unrelated decision changed: %+v %v", got, err)
		}
	}
}

func TestScreenQuestionDuplicateClosedBeforeAnswer(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	old := screenRecord(t, s)
	var duplicate api.Decision
	s.app.Do(func() {
		params := s.readScreenPrompt(1).Params
		params.Origin = old.Origin
		var err error
		duplicate, err = s.store.CreateDecision(params, time.Now())
		if err != nil {
			t.Error(err)
		}
		s.screenPrompts.watches = nil
	})
	scanScreen(s)
	current := screenRecord(t, s)
	if response := answerScreen(s, current, "1", nil); response.Err != nil {
		t.Fatal(response.Err)
	}
	waitPromptCondition(t, func() bool { b, _ := os.ReadFile(input); return len(b) > 0 })
	paintScreen(p, "")
	scanScreen(s)
	scanScreen(s)
	for _, id := range []string{old.DecisionID, duplicate.DecisionID} {
		d, err := s.store.Decision(id)
		if err != nil || d.Status == "pending" {
			t.Fatalf("pending after answer: %+v %v", d, err)
		}
	}
}

func TestScreenQuestionReconcilesSummarizedLegacyRecord(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	first := screenRecord(t, s)
	var legacy, current api.Decision
	s.app.Do(func() {
		p := s.readScreenPrompt(1)
		full := p.Params.Title + "\n\n" + strings.Repeat("Long question details. ", 20)
		params := p.Params
		params.Origin = first.Origin
		params.Body = &full
		var err error
		legacy, err = s.store.CreateDecision(params, time.Now())
		if err != nil {
			t.Error(err)
			return
		}
		display := p.Params
		display.Title = full
		display.Origin = first.Origin
		current, err = s.store.CreateScreenDecision(display, time.Now(), "p1", `["session","call",0]`)
		if err != nil {
			t.Error(err)
			return
		}
		w := &screenPromptWatch{prompt: p, signature: p.Signature(), transcriptID: `["session","call",0]`, displayParams: &display}
		s.beginScreenPrompt(1, w)
		if w.decisionID != current.DecisionID {
			t.Errorf("selected %s, want %s", w.decisionID, current.DecisionID)
		}
	})
	got, err := s.store.Decision(legacy.DecisionID)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("summarized duplicate: %+v %v", got, err)
	}
}

func TestClaudeUnknownIdentityDoesNotReuseOrCancelSameText(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	old := screenRecord(t, s)
	s.app.Do(func() {
		if err := s.store.BindScreenQuestion(old.DecisionID, "p1", `["session","old",0]`); err != nil {
			t.Error(err)
			return
		}
		p := s.readScreenPrompt(1)
		params := p.Params
		params.Origin = old.Origin
		current, err := s.store.CreateDecision(params, time.Now())
		if err != nil {
			t.Error(err)
			return
		}
		w := &screenPromptWatch{prompt: p, signature: p.Signature()}
		s.screenPrompts.watches[1] = w
		s.beginScreenPrompt(1, w)
		if w.decisionID == old.DecisionID || w.decisionID == current.DecisionID {
			t.Errorf("unverified decision reused: %s", w.decisionID)
		}
		for _, d := range []api.Decision{old, current} {
			got, e := s.store.Decision(d.DecisionID)
			if e != nil || got.Status != "pending" {
				t.Errorf("unverified decision changed: %+v %v", got, e)
			}
		}
	})
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

func TestClaudeUnverifiedDoesNotExpire(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	s.app.Do(func() {
		p := s.readScreenPrompt(1)
		w := &screenPromptWatch{prompt: p, transcriptDeadline: time.Now().Add(-time.Second)}
		if !s.waitClaudeQuestion(1, w, false, false, true) {
			t.Error("unverified question escaped after deadline")
		}
	})
}

func TestWaitingWatchCollectsPreview(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-preview", "claude")
	s.app.Do(func() {
		p := s.readScreenPrompt(1)
		s.screenPrompts.watches = map[int]*screenPromptWatch{1: {signature: p.Signature(), prompt: p, lastMatched: true}}
		s.scanScreenPrompts()
		w := s.screenPrompts.watches[1]
		if w.preview == nil || w.decisionID != "" {
			t.Errorf("preview skipped: %+v", w)
		}
	})
}
