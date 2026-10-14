package screenprompt

import (
	"strings"
	"testing"
)

func TestVolatileSignatureAndWrappedDenialNotice(t *testing.T) {
	lines := []string{"⏺ Bash(echo test)", "────────────────", "Bash command", "echo test", "Do you want to proceed?", "⚠ Claude Code will automatically deny this request in 1:23,", "to avoid blocking progress on an unattended session", "❯ 1. Yes (12s) ⠋", "  2. No"}
	a := Parse("claude", lines)
	if a == nil || a.Params.Body == nil || strings.Contains(*a.Params.Body, "1:23") || strings.Contains(a.Params.Title, "automatically deny") || !strings.Contains(a.Params.Title, "⏺ Bash(echo test)") {
		t.Fatal(a)
	}
	b := Parse("claude", strings.Split(strings.NewReplacer("1:23", "1:22", "12s", "13s", "⠋", "⠙").Replace(strings.Join(lines, "\n")), "\n"))
	if b == nil || a.Signature() != b.Signature() || a.Params.Title != b.Params.Title {
		t.Fatal("volatile identity", a, b)
	}
	single := Parse("claude", strings.Split(strings.ReplaceAll(strings.Join(lines, "\n"), ",\nto avoid", ", to avoid"), "\n"))
	if single == nil || *single.Params.Body != *a.Params.Body {
		t.Fatal(single)
	}
	ask := Parse("claude", append([]string{"⏺ Bash(previous command)"}, fixture(t, "claude-single")...))
	if ask == nil || strings.Contains(ask.Params.Title, "⏺") {
		t.Fatal(ask)
	}
}

func TestFinalScreensAndPositionBasedFreeInput(t *testing.T) {
	for _, lines := range [][]string{fixture(t, "claude-final-confirm"), {"Ready to submit your answers?", " 1. Submit answers", " 2. Cancel"}} {
		p := Parse("claude", lines)
		if p == nil || !p.Final {
			t.Fatal(p)
		}
	}
	original := Parse("claude", fixture(t, "claude-multi-text"))
	edited := Parse("claude", fixture(t, "claude-multi-text-edited"))
	if edited == nil || original.Signature() != edited.Signature() || len(edited.Params.Options) != 2 {
		t.Fatal(edited)
	}
	chat := strings.Split(strings.ReplaceAll(strings.Join(fixture(t, "claude-multi-text-edited"), "\n"), "Chat about this", "more typing"), "\n")
	p := Parse("claude", chat)
	if p == nil || p.Signature() != original.Signature() {
		t.Fatal(p)
	}
	if original.PreviewSignature() != edited.PreviewSignature() {
		t.Fatal("preview would rescan")
	}
}

func TestFreeInputAttachedSeparatorKeepsIdentity(t *testing.T) {
	before := Parse("claude", fixture(t, "claude-multi-text"))
	text := strings.Join(fixture(t, "claude-multi-text-edited"), "\n")
	text = strings.ReplaceAll(text, "Risks section.\n\n     Submit\n────────────────", "Risks section.────────────────")
	after := Parse("claude", strings.Split(text, "\n"))
	if after == nil || before.Signature() != after.Signature() {
		t.Fatal(after)
	}
}

func TestPermissionTitleIgnoresResponseRowsAndPreservesWrappedCommand(t *testing.T) {
	lines := []string{"⏺ User declined to answer questions", "⏺ Writing proof file", "────────────────", "Bash command", "╌╌╌╌", ` │ python3 -c "Path('/private/tmp/gtx-accept/delta-c-`, ` │ proof.txt').write_text('value')"`, "╌╌╌╌", "Do you want to proceed?", "❯ 1. Yes", " 2. No"}
	p := Parse("claude", lines)
	if p == nil || strings.Contains(p.Params.Title, "⏺") || !strings.Contains(p.Params.Title, `Path('/private/tmp/gtx-accept/delta-c-proof.txt')`) {
		t.Fatal(p)
	}
	changed := append([]string{}, lines[1:]...)
	if q := Parse("claude", changed); q == nil || q.Signature() != p.Signature() {
		t.Fatal("response line changed permission identity", q)
	}
}

func TestPermissionDoesNotUseToolFromEarlierConversation(t *testing.T) {
	lines := []string{"⏺ Bash(old command)", "⏺ Ordinary response", "────────────────", "Run this command?", "❯ 1. Yes", " 2. No"}
	p := Parse("claude", lines)
	if p == nil || strings.Contains(p.Params.Title, "old command") {
		t.Fatal(p)
	}
}
