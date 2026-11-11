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
	for _, tc := range []struct{ name, screen, want string }{
		{"trailing rule", "text\n──────────\nEnter to confirm · Esc to cancel\n──────────\n\n", "Enter to confirm · Esc to cancel\n──────────\n"},
		{"status below the last rule keeps the old meaning", "──────────\n❯ \n──────────\n  ⏵⏵ auto mode on\n", "  ⏵⏵ auto mode on"},
		{"only rule has nothing below", "☐ x\nEnter to confirm · Esc to cancel\n──── View teammates: `tmux -L s a` ─\n\n", "☐ x\nEnter to confirm · Esc to cancel\n──── View teammates: `tmux -L s a` ─\n\n"},
	} {
		if got := regionText(Input{Screen: tc.screen}, "after_last_horizontal_rule"); got != tc.want {
			t.Errorf("%s: region = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The form's only rule is the teammate line (its top scrolled away).
func TestClaudeQuestionWithOnlyTeammateRuleIsBlocked(t *testing.T) {
	t.Parallel()
	screen := " ☐ x\n\n❯ 1. A\n  2. B\n\nEnter to select · ↑/↓ to navigate · Esc to cancel\n" +
		"──────────────────── View teammates: `tmux -L claude-swarm-8791 a` ─\n\n"
	d := Detect("claude", Input{Screen: screen, OSCTitle: "✳ task"})
	if d.State != Blocked || d.RuleID != "live_blocked_form" || !d.VisibleBlocker {
		t.Fatalf("got %+v, want visible blocked from live_blocked_form", d)
	}
}

// A permission dialog above the teammate line is a visible blocker too.
func TestClaudePermissionUnderTeammateRuleIsBlocked(t *testing.T) {
	t.Parallel()
	screen := "⏺ Write(notes.txt)\n\n" +
		"────────────────────────────────────────────────────────────\n" +
		" Create file\n\n" +
		" Do you want to proceed?\n" +
		" ❯ 1. Yes\n   2. No, and tell Claude what to do differently (esc)\n\n" +
		" Esc to cancel\n" +
		"──────────────────── View teammates: `tmux -L claude-swarm-8791 a` ─\n\n"
	d := Detect("claude", Input{Screen: screen, OSCTitle: "✳ task"})
	if d.State != Blocked || d.RuleID != "generic_permission_prompt" || !d.VisibleBlocker {
		t.Fatalf("got %+v, want visible blocked from generic_permission_prompt", d)
	}
}

// Waiting for input with a rule at the very bottom must stay idle: the
// back-off stops at the prompt box and never reaches an answered question.
func TestClaudeIdleWithTrailingRuleAfterAnsweredQuestionStaysIdle(t *testing.T) {
	t.Parallel()
	screen := "Enter to select · ↑/↓ to navigate · Esc to cancel\n" +
		"⏺ 回答を受け取ったよ。\n\n" +
		"────────────────────────────────────────────────────────────\n" +
		"❯ \n" +
		"──────────────────── View teammates: `tmux -L claude-swarm-8791 a` ─\n\n"
	d := Detect("claude", Input{Screen: screen, OSCTitle: "✳ task"})
	if d.State != Idle {
		t.Fatalf("got %+v, want idle", d)
	}
}
