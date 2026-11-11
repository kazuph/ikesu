package detect

import "testing"

func TestLabelledRuleIsAHorizontalRule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"────────────", true},
		{"──── View teammates: `tmux -L claude-swarm-92227 a` ─", true},
		{"  ──── label ────", true},
		{"── short label ─", false},       // fewer than three dashes before the label
		{"──── label without end", false}, // the label must close with a dash
		{"────label ─", false},            // the label is set off by a space
		{"text ──── ─", false},
	} {
		if got := isHorizontalRule(tc.line); got != tc.want {
			t.Errorf("isHorizontalRule(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// Claude Code puts "View teammates" into the prompt box top while
// split-pane teammates run; the prompt must still read as the input box.
func TestClaudePromptUnderTeammateLabelIsIdle(t *testing.T) {
	t.Parallel()
	screen := "⏺ 返事を書き終えたよ。\n\n" +
		"──────────────────── View teammates: `tmux -L claude-swarm-92227 a` ─\n" +
		"❯ 今日のissueのネタ候補を見て\n" +
		"──────────────────────────────────────────\n" +
		"  ⏵⏵ auto mode on (shift+tab to cycle) · ← 4 agents\n\n" +
		"  ⏺ main\n" +
		"  ◯ chromatic-short   Build zundamon Chromatic short   1d 5h 28m\n"
	d := Detect("claude", Input{Screen: screen})
	if d.State != Idle || d.RuleID != "live_prompt_box" || !d.VisibleIdle {
		t.Fatalf("got %+v, want visible idle from live_prompt_box", d)
	}
}
