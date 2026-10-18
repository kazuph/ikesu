package screenprompt

import (
	"strings"
	"testing"
)

func TestCodexQuestionBoundaryAndFreeInput(t *testing.T) {
	lines := []string{"Earlier work: allow file writes", "Question 1/2 (2 unanswered)", "Which format should the report use?", "", "› 1. Plain text", "     A readable report", "  2. JSON", "     A structured report", "  3. None of the above", "", "  auto-resolves in 1:23", "  tab to add notes · enter to submit answer"}
	p := Parse("codex", lines)
	if p == nil || !p.CodexQuestion || p.Params.Kind != "agent_prompt" || p.Params.Title != "Which format should the report use?" {
		t.Fatalf("prompt=%+v", p)
	}
	if p.Params.QuestionLabel == nil || *p.Params.QuestionLabel != "Question 1/2 (2 unanswered)" {
		t.Fatal(p.Params.QuestionLabel)
	}
	if !p.Params.AllowText || p.TextID != "3" || len(p.Params.Options) != 2 || p.Cursor != 0 {
		t.Fatal(p)
	}
	if p.Params.Body == nil || strings.ContainsAny(*p.Params.Body, "0123456789") || !strings.Contains(*p.Params.Body, "auto-resolves") {
		t.Fatal(p.Params.Body)
	}
	changed := strings.ReplaceAll(strings.Join(lines, "\n"), "1:23", "0:42")
	changed = strings.ReplaceAll(changed, "› 1.", "  1.")
	changed = strings.ReplaceAll(changed, "  2.", "› 2.")
	q := Parse("codex", strings.Split(changed, "\n"))
	if q == nil || p.Signature() != q.Signature() || q.Cursor != 1 {
		t.Fatal("countdown or cursor changed identity")
	}
}

func TestCodexQuestionIncompleteReadAndQuestionIdentity(t *testing.T) {
	lines := []string{"Question 1/2 (2 unanswered)", "Which format?", "› 1. Text", "  2. JSON", "  3. None of the above", "tab to add notes | enter to submit answer"}
	p := Parse("codex", lines)
	if p == nil {
		t.Fatal("complete question missing")
	}
	for _, cut := range [][]string{lines[:4], lines[:5]} {
		if Parse("codex", cut) != nil {
			t.Fatal("partial question was treated as complete")
		}
	}
	changed := append([]string{}, lines...)
	changed[0] = "Question 2/2 (1 unanswered)"
	q := Parse("codex", changed)
	if q == nil || q.Signature() == p.Signature() {
		t.Fatal("another question reused identity")
	}
}

func TestObservedCodexQuestionStates(t *testing.T) {
	var signature string
	for _, name := range []string{"codex-question", "codex-question-cursor", "codex-question-free", "codex-question-notes"} {
		p := Parse("codex", fixture(t, name))
		if p == nil || p.Params.Title != "Report format?" || p.Params.Kind != "agent_prompt" || !p.Params.AllowText || len(p.Params.Options) != 2 || p.TextID != "3" {
			t.Fatalf("%s: %+v", name, p)
		}
		if signature != "" && signature != p.Signature() {
			t.Fatalf("%s changed identity", name)
		}
		signature = p.Signature()
		if p.TextOpen != (name == "codex-question-notes") {
			t.Fatalf("%s notes state", name)
		}
		if p.Params.Options[0].Label != "Plain text" || p.Params.Options[0].Description == nil || *p.Params.Options[0].Description != "Use plain text." {
			t.Fatal(p.Params.Options)
		}
	}
}

func TestObservedCodexLastQuestionSubmitAll(t *testing.T) {
	p := Parse("codex", fixture(t, "codex-question-last"))
	if p == nil || p.Params.Title != "Scope?" || p.Params.QuestionLabel == nil || *p.Params.QuestionLabel != "Question 2/2 (1 unanswered)" || !p.Params.AllowText || !p.CodexQuestion || p.Final {
		t.Fatalf("last question: %+v", p)
	}
}
