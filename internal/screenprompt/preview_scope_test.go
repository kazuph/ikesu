package screenprompt

import (
	"strings"
	"testing"
)

func TestPreviewOnlyActiveOptionsAndNoPreviewSentinel(t *testing.T) {
	picture := strings.Join(fixture(t, "claude-preview"), "\n")
	for _, prefix := range []string{
		"A table in conversation\n┌────┐\n│old │\n└────┘\n",
		picture + "\n",
	} {
		p := Parse("claude", strings.Split(prefix+picture, "\n"))
		if p == nil || p.Preview == nil || *p.Preview != " Hello team.\n A short report." || p.Params.Title != "Which draft should we keep?" {
			t.Fatalf("%+v", p)
		}
	}
	none := strings.ReplaceAll(picture, " Hello team.                              ", " No preview available                     ")
	none = strings.ReplaceAll(none, "│ A short report.                          │\n", "│                                          │\n")
	p := Parse("claude", strings.Split(none, "\n"))
	if p == nil || !p.HasPreview || p.Preview == nil || *p.Preview != "" {
		t.Fatalf("sentinel: %+v", p)
	}
	without := strings.ReplaceAll(picture, "Enter to select · ↑/↓ to navigate · n to add notes · Esc to cancel", "")
	p = Parse("claude", strings.Split(without, "\n"))
	if p != nil && p.HasPreview {
		t.Fatal("box without active navigation footer adopted")
	}
}
