package detect

import "testing"

// While teammates exist, Claude Code draws "──── View teammates: … ─" under
// a question form, as the screen's last rule with only blank rows below.
// The form above it must still read as blocked (2026-11-11: a question in a
// pane with teammates stayed idle instead of turning red).
const claudeQuestionUnderTeamRule = "⏺ Bash(git status)\n" +
	"  ⎿  Allowed by auto mode\n\n" +
	"────────────────────────────────────────────────────────────\n" +
	" ☐ 置き場\n\n" +
	"│ どちらに置くか決めてほしい。\n\n" +
	"❯ 1. GitHub (推奨)\n" +
	"     リポジトリにコミットする。\n" +
	"  2. R2\n" +
	"     staging の R2 に上げる。\n" +
	"  3. Type something.\n" +
	"────────────────────────────────────────────────────────────\n" +
	"  4. Chat about this\n\n" +
	"Enter to select · ↑/↓ to navigate · Esc to cancel\n" +
	"──────────────────── View teammates: `tmux -L claude-swarm-8791 a` ─\n" +
	"\n\n"

func TestClaudeQuestionUnderTeammateRuleIsBlocked(t *testing.T) {
	t.Parallel()
	// Claude shows the idle title while it waits for this answer.
	d := Detect("claude", Input{Screen: claudeQuestionUnderTeamRule, OSCTitle: "✳ task"})
	if d.State != Blocked || d.RuleID != "live_blocked_form" || !d.VisibleBlocker {
		t.Fatalf("got %+v, want visible blocked from live_blocked_form", d)
	}
}

func TestTrailingEmptyRuleDoesNotHideBlockBelowPreviousRule(t *testing.T) {
	t.Parallel()
	in := Input{Screen: "text\n──────────\nEnter to confirm · Esc to cancel\n──────────\n\n"}
	if got := regionText(in, "after_last_horizontal_rule"); got != "Enter to confirm · Esc to cancel\n──────────\n\n" && got != "Enter to confirm · Esc to cancel\n──────────\n" {
		t.Fatalf("region = %q", got)
	}
	// A last rule with content below keeps the old meaning.
	in = Input{Screen: "──────────\n❯ \n──────────\n  ⏵⏵ auto mode on\n"}
	if got := regionText(in, "after_last_horizontal_rule"); got != "  ⏵⏵ auto mode on\n" && got != "  ⏵⏵ auto mode on" {
		t.Fatalf("region with status below = %q", got)
	}
}
