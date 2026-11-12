package detect

import (
	"fmt"
	"testing"
)

const leadBusyFooter = "────────────────────────────────────────\n" +
	"❯\n" +
	"────────────────────────────────────────\n" +
	"  ⚠ compact-handoff: 引き継ぎ文書なし\n" +
	"  ⚠ cache-warm: ☕ cache 残り54分\n" +
	"  Opus 5.5 |  main | ~/s/g/g/m\n" +
	"  ¥101171 / $674.48 ctx 32%%\n" +
	"%s\n" +
	"\n" +
	"  ⏺ main\n" +
	"  ◯ pr4-fix          Fix stickman PASS failure\n" +
	"  ◯ pr3-fix          Fix PR3 script-to-bubble test\n" +
	"  ↓ 16 more\n"

func TestClaudeLeadBusy(t *testing.T) {
	t.Parallel()
	footer := func(status string) string {
		return fmt.Sprintf(leadBusyFooter, status)
	}
	for _, tc := range []struct {
		name   string
		screen string
		want   bool
	}{
		{"spinner right above the box", "⏺ Bash(go test)\n\n✻ Effecting… (6m 7s · ↓ 4.8k tokens)\n\n" + footer("  ⏵⏵ auto mode on"), true},
		{"tip under the spinner", "✻ Effecting… (6m 7s)\n  ⎿  Tip: press ctrl+o\n\n" + footer("  ⏵⏵ auto mode on"), true},
		{"interrupt hint in the status block", "⏺ 返事を書き終えたよ。\n\n" + footer("  ⏵⏵ auto mode on · esc to interrupt"), true},
		{"idle with a finished summary", "⏺ 返事を書き終えたよ。\n\n✻ Cooked for 6m 7s\n\n" + footer("  ⏵⏵ auto mode on"), false},
		// Conversation text further up must not keep the lead busy.
		{"old text far above", "· Thinking… (old)\nthe footer says esc to interrupt while working\n⏺ a\n⏺ b\n⏺ c\n⏺ d\n⏺ 返事を書き終えたよ。\n\n" + footer("  ⏵⏵ auto mode on"), false},
		// The panel under the status block is not the status block.
		{"interrupt text inside the agent panel", "⏺ 返事を書き終えたよ。\n\n" + footer("  ⏵⏵ auto mode on") + "  ◯ x   explain esc to interrupt\n", false},
	} {
		if got := ClaudeLeadBusy(tc.screen); got != tc.want {
			t.Errorf("%s: ClaudeLeadBusy = %v, want %v", tc.name, got, tc.want)
		}
	}
}
