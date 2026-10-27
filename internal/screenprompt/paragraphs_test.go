package screenprompt

import (
	"os"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
)

func TestLongQuestionKeepsParagraphsAndShortHeading(t *testing.T) {
	screen := fixture(t, "claude-long-paragraphs")
	data, err := os.ReadFile("testdata/claude-long-paragraphs-body.txt")
	if err != nil {
		t.Fatal(err)
	}
	full := string(data)
	p := Parse("claude", screen)
	if p == nil {
		t.Fatal("long question was not parsed")
	}
	if p.Params.Body == nil || *p.Params.Body != full {
		t.Errorf("paragraph body differs: got=%v want=%q", p.Params.Body, full)
	}
	sentence := strings.SplitN(full, "。", 2)[0] + "。"
	if p.Params.Title != sentence || len([]rune(p.Params.Title)) > 80 {
		t.Errorf("heading=%q want=%q", p.Params.Title, sentence)
	}
	if strings.Contains(p.Params.Title, "│") || p.Params.Body != nil && strings.Contains(*p.Params.Body, "│") {
		t.Error("frame leaked into question")
	}
	plain := strings.ReplaceAll(strings.Join(screen, "\n"), "│\n", "\n")
	q := Parse("claude", strings.Split(plain, "\n"))
	if q == nil || q.Signature() != p.Signature() {
		t.Fatal("frame/paragraph rendering changed question identity")
	}
	compact := strings.ReplaceAll(strings.Join(screen, "\n"), "│\n", "")
	if q := Parse("claude", strings.Split(compact, "\n")); q == nil || q.Signature() != p.Signature() {
		if q == nil {
			t.Fatal("compact question not parsed")
		}
		a, b := []rune(questionIdentity(p.Params)), []rune(questionIdentity(q.Params))
		i := 0
		for i < len(a) && i < len(b) && a[i] == b[i] {
			i++
		}
		t.Errorf("paragraph identity differs at %d: %q / %q", i, string(a[max(0, i-25):min(len(a), i+45)]), string(b[max(0, i-25):min(len(b), i+45)]))
	}
	saved := api.Decision{Title: p.Params.Title, Body: p.Params.Body, Options: p.Params.Options, AllowText: p.Params.AllowText, MultiSelect: p.Params.MultiSelect, QuestionLabel: p.Params.QuestionLabel}
	if FromDecision(saved) != p.Signature() {
		t.Error("saved title/body lost question identity")
	}
	changed := strings.ReplaceAll(strings.Join(screen, "\n"), "あわせて、", "さらに、")
	if q := Parse("claude", strings.Split(changed, "\n")); q == nil || q.Params.Title != p.Params.Title || q.Signature() == p.Signature() {
		t.Error("changed question body retained old identity")
	}
}

func TestShortQuestionKeepsTitleOnly(t *testing.T) {
	p := Parse("claude", []string{"─", "│ 短い質問の", "│ 折り返しです。", "❯ 1. はい", "  2. いいえ", "Enter to select · ↑/↓ to navigate · Esc to cancel"})
	if p == nil || p.Params.Title != "短い質問の折り返しです。" || p.Params.Body != nil {
		t.Fatal(p)
	}
}
