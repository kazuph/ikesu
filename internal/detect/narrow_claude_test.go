package detect

import (
	"os"
	"strings"
	"testing"
)

func TestClaudeNarrowRealQuestionIsBlocked(t *testing.T) {
	b, err := os.ReadFile("../screenprompt/testdata/claude-multi-narrow.txt")
	if err != nil {
		t.Fatal(err)
	}
	d := Detect("claude", Input{Screen: string(b)})
	if d.State != Blocked || d.RuleID != "live_blocked_form" {
		t.Fatal(d)
	}
	if m := manifestFor("claude"); m.version != "2026.10.14.1" {
		t.Fatal(m.version)
	}
}

func TestClaudeCapturedMCPConfirmationIsBlocked(t *testing.T) {
	for _, cursor := range []string{"accept", "decline"} {
		t.Run(cursor, func(t *testing.T) {
			data, err := os.ReadFile("../screenprompt/testdata/claude-mcp-" + cursor + ".txt")
			if err != nil {
				t.Fatal(err)
			}
			d := Detect("claude", Input{Screen: string(data)})
			if d.State != Blocked || d.RuleID != "mcp_elicitation_prompt" {
				t.Fatal(d)
			}
		})
	}
}

func TestClaudeCompleteFooterAcceptsWhitespaceWrapOnly(t *testing.T) {
	base := "Question?\n──────────\n❯ 1. First\n  2. Second\n──────────\n"
	for _, footer := range []string{"Enter to select · Tab/Arrow keys to navigate · Esc to cancel", "Enter to select · ↑/↓ to navigate · Esc to cancel", "Enter to confirm · Esc to cancel"} {
		for at, r := range footer {
			if r != ' ' {
				continue
			}
			wrapped := footer[:at] + "\n" + footer[at+1:]
			d := Detect("claude", Input{Screen: base + wrapped + "\n"})
			if d.State != Blocked || d.RuleID != "live_blocked_form" {
				t.Fatalf("footer=%q: %+v", wrapped, d)
			}
		}
	}
	for _, footer := range []string{
		"Enter to select · Tab/Arrow keys to navigate · Esc to\n",
		"Enter to select · Tab/Arrow keys to navigate\nThe prose mentions esc to\nUnrelated prose has cancel here",
		"The text quotes Enter to select · Tab/Arrow keys to navigate · Esc to\ncancel as an example",
		"Enter to select · Tab/Arrow keys to navi\ngate · Esc to\ncancel",
	} {
		d := Detect("claude", Input{Screen: base + footer + "\n"})
		if d.State != Idle {
			t.Fatalf("partial/prose=%q: %+v", footer, d)
		}
	}
	// The existing one-line rule also accepts footer controls with extra hints.
	d := Detect("claude", Input{Screen: base + strings.Join([]string{"Enter to select", "↑/↓ to navigate", "n to add notes", "Esc to cancel"}, " · ")})
	if d.State != Blocked {
		t.Fatal(d)
	}
}
