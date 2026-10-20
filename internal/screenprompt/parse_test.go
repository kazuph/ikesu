package screenprompt

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
)

func fixture(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(b), "\n")
}

// Fixtures are the public dialogs observed in real Claude 2.1.296 / Codex 0.162.1,
// paired with the JSON emitted from those same screens by isolated herdr 0.2.40.
func TestObservedScreenContractsMatchHerdr0240(t *testing.T) {
	for _, name := range []string{"claude-single", "claude-permission", "codex-permission", "claude-multi-wide", "claude-second-tab", "claude-final-confirm"} {
		t.Run(name, func(t *testing.T) {
			agent := "claude"
			if strings.HasPrefix(name, "codex") {
				agent = "codex"
			}
			p := Parse(agent, fixture(t, name))
			if p == nil {
				t.Fatal("not parsed")
			}
			b, err := os.ReadFile("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want api.Decision
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}
			// E1 corrects inserted wrap spaces in this historical observation.
			if name == "codex-permission" {
				value := strings.ReplaceAll(*want.Options[1].Description, "/ outside.txt", "/outside.txt")
				want.Options[1].Description = &value
			}
			got := p.Params
			if got.Title != want.Title || got.Kind != want.Kind || got.AllowText != want.AllowText || got.MultiSelect != want.MultiSelect || !reflect.DeepEqual(got.Options, want.Options) || !reflect.DeepEqual(got.QuestionLabel, want.QuestionLabel) || !reflect.DeepEqual(got.Body, want.Body) {
				t.Fatalf("got %+v\nwant %+v", got, want)
			}
			if p.Cursor != 0 {
				t.Fatalf("cursor=%d", p.Cursor)
			}
		})
	}
}

func TestScreenSignaturesAndShortcuts(t *testing.T) {
	lines := fixture(t, "claude-multi-wide")
	a := Parse("claude", lines)
	b := Parse("claude", strings.Split(strings.ReplaceAll(strings.Join(lines, "\n"), "☐ Sections", "☒ Sections"), "\n"))
	if a.Signature() != b.Signature() {
		t.Fatal("tab progress changed identity")
	}
	b = Parse("claude", strings.Split(strings.ReplaceAll(strings.Join(lines, "\n"), "[ ] Summary", "[✔] Summary"), "\n"))
	if a.Signature() != b.Signature() || !b.Checked["1"] {
		t.Fatal("check state changed identity or was lost")
	}
	for _, replacement := range []string{"Which sections should the report exclude?", "A changed overview."} {
		from := "Which sections should the report include?"
		if strings.HasPrefix(replacement, "A ") {
			from = "A concise overview."
		}
		b = Parse("claude", strings.Split(strings.ReplaceAll(strings.Join(lines, "\n"), from, replacement), "\n"))
		if a.Signature() == b.Signature() {
			t.Fatal("changed question/detail kept identity")
		}
	}
	p := Parse("codex", fixture(t, "codex-permission"))
	if p.Keys["1"] != "y" || p.Keys["2"] != "p" || p.Keys["3"] != "\x1b" {
		t.Fatal(p.Keys)
	}
}

func TestHighlightGluedRowsAndUnsupportedScreens(t *testing.T) {
	lines := fixture(t, "claude-single")
	a := Parse("claude", lines)
	for _, cut := range []string{"❯ 1. Plain\n     A compact plain text", "  2. Markdown\n     A structured Markdown"} {
		glue := strings.Replace(cut, "\n", strings.Repeat(" ", 80), 1)
		b := Parse("claude", strings.Split(strings.Replace(strings.Join(lines, "\n"), cut, glue, 1), "\n"))
		if b == nil || a.Signature() != b.Signature() {
			t.Fatalf("glued row changed meaning: %+v", b)
		}
	}
	if Parse("other", lines) != nil || Parse("claude", []string{"ordinary idle screen"}) != nil {
		t.Fatal("unsupported screen accepted")
	}
}

func TestDevinNumberedConfirmationFromSpecification(t *testing.T) {
	p := Parse("devin", []string{"Run this command?", "❯ 1. Yes", "  2. No", "Enter to confirm · Esc to cancel"})
	if p == nil || p.Params.Kind != "tool_permission" || p.Keys["1"] != "1" {
		t.Fatal(p)
	}
}

func TestObservedPreviewFocusedOptionAndMonospaceSpacing(t *testing.T) {
	p := Parse("claude", fixture(t, "claude-preview"))
	if p == nil || !p.HasPreview || p.Cursor != 0 || len(p.Params.Options) != 2 {
		t.Fatal(p)
	}
	if p.Params.Options[0].Preview == nil || *p.Params.Options[0].Preview != " Hello team.\n A short report." || !p.Params.Options[1].PreviewUnavailable {
		t.Fatal(p.Params.Options)
	}
	if p.Params.Options[0].Description != nil || p.Params.AllowText {
		t.Fatal("preview UI invented invisible details")
	}
}

func TestAskUserQuestionKindDoesNotDependOnQuestionVocabulary(t *testing.T) {
	for _, name := range []string{"claude-single", "claude-multi-wide", "claude-preview"} {
		lines := fixture(t, name)
		for _, word := range []string{"permission", "allow", "trust", "許可"} {
			p := Parse("claude", lines)
			title := p.Params.Title
			changed := strings.Split(strings.Replace(strings.Join(lines, "\n"), title, "Which "+word+" setting should the report describe?", 1), "\n")
			got := Parse("claude", changed)
			if got == nil || got.Params.Kind != "agent_prompt" {
				t.Fatalf("%s %s: %+v", name, word, got)
			}
		}
	}
}
