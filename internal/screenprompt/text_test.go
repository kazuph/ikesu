package screenprompt

import (
	"strings"
	"testing"
)

func TestObservedSingleMultiSelectAndEditedFreeRow(t *testing.T) {
	p := Parse("claude", fixture(t, "claude-multi-text"))
	if p == nil || p.Params.QuestionLabel == nil || *p.Params.QuestionLabel != "質問 1/1 Extra" || !p.Params.MultiSelect || !p.Params.AllowText {
		t.Fatalf("single multi-select label: %+v", p)
	}
	edited := fixture(t, "claude-multi-text-edited")
	q := ParseWithTextID("claude", edited, p.TextID)
	if q == nil || len(q.Params.Options) != 2 || q.Signature() != p.Signature() || !q.Checked[p.TextID] {
		t.Fatalf("known free row changed identity: %+v", q)
	}
	unknown := Parse("claude", edited)
	if unknown == nil || unknown.Signature() != p.Signature() {
		t.Fatal("position did not identify edited free input")
	}
	changed := strings.Split(strings.ReplaceAll(strings.Join(edited, "\n"), "A concise overview.", "A changed overview."), "\n")
	if q := ParseWithTextID("claude", changed, p.TextID); q == nil || q.Signature() == p.Signature() {
		t.Fatal("regular description change was hidden by free-row identity")
	}
}
