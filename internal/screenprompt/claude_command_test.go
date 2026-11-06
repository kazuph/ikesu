package screenprompt

import (
	"strings"
	"testing"
)

func TestClaudeCommandVisibleRowsDetermineIdentity(t *testing.T) {
	screen := "Bash command\nReason\n╌╌╌╌\n│ echo one\n│ echo two\n╌╌╌╌\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel"
	a := Parse("claude", strings.Split(screen, "\n"))
	b := Parse("claude", strings.Split(strings.Replace(screen, "│ echo one\n│ echo two", "│ echo one echo two", 1), "\n"))
	if a.Params.Origin == nil || a.Params.Origin.Command != "echo one\necho two" || b.Params.Origin == nil || a.Signature() == b.Signature() {
		t.Fatal("different visible rows must have different identities")
	}
	a.Params.Origin.ScreenColumns = 80
	b = Parse("claude", strings.Split(screen, "\n"))
	b.Params.Origin.ScreenColumns = 81
	if a.Signature() == b.Signature() {
		t.Fatal("terminal width must be part of the identity")
	}
}
func TestClaudeCommandQuotedBlankLine(t *testing.T) {
	screen := "Bash command\nReason\n╌╌╌╌\n│ printf 'one\n│\n│ two'\n╌╌╌╌\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel"
	p := Parse("claude", strings.Split(screen, "\n"))
	if p.Params.Origin == nil || p.Params.Origin.Command != "printf 'one\n\ntwo'" {
		t.Fatalf("quoted command changed: %+v", p.Params.Origin)
	}
}

func TestClaudeCommandDeadlineIgnoresCommandText(t *testing.T) {
	screen := "Bash command\nReason\n╌╌╌╌\n│ echo \"automatically deny this request in 0:01\"\n╌╌╌╌\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel"
	_, timeout := claudeCommand(strings.Split(screen, "\n"))
	if timeout != nil {
		t.Fatalf("command created a deadline: %d", *timeout)
	}
	screen = strings.Replace(screen, "Do you want to proceed?", "⚠ Claude Code will automatically block this request in 0:38, to avoid blocking progress\nDo you want to proceed?", 1)
	_, timeout = claudeCommand(strings.Split(screen, "\n"))
	if timeout == nil || *timeout != 38000 {
		t.Fatalf("wrong actual deadline: %v", timeout)
	}
}

func TestClaudeCommandDetailsAndIdentity(t *testing.T) {
	lines := fixture(t, "claude-permission")
	p := Parse("claude", lines)
	if p == nil || p.Params.Origin == nil {
		t.Fatal("command not parsed")
	}
	o := p.Params.Origin
	if o.Tool != "Bash command" || o.Explanation != "Write observation text to file" || o.Command != "printf observation >\n/tmp/gs-j0m1z8np/work/permission-observation.txt" {
		t.Fatalf("details: %+v", o)
	}
	changed := Parse("claude", strings.Split(strings.ReplaceAll(strings.Join(lines, "\n"), "printf observation", "printf replacement"), "\n"))
	if changed == nil || changed.Signature() == p.Signature() {
		t.Fatal("changed command matched")
	}
	partial := append([]string(nil), lines...)
	partial[8] = ""
	if origin, _ := claudeCommand(partial); origin != nil {
		t.Fatal("partial frame was structured")
	}
}

func TestClaudeCommandCountdown(t *testing.T) {
	screen := strings.Join(fixture(t, "claude-permission"), "\n")
	screen = strings.Replace(screen, " Do you want to proceed?", " ⚠ Claude Code will automatically block this request in 0:38, to avoid blocking progress\n Do you want to proceed?", 1)
	a := Parse("claude", strings.Split(screen, "\n"))
	b := Parse("claude", strings.Split(strings.Replace(screen, "0:38", "0:37", 1), "\n"))
	if a == nil || b == nil || a.Params.TimeoutMS == nil || *a.Params.TimeoutMS != 38000 || a.Signature() != b.Signature() {
		t.Fatal("countdown parsing or identity")
	}
}

func TestClaudePermissionWarningsAndCountdownVariants(t *testing.T) {
	for _, verb := range []string{"deny", "block"} {
		screen := "Bash command\nTip: auto mode handles these prompts for you — choose \"switch to auto mode\"\nbelow\nCheck credentials\n╌╌╌╌╌╌\n│ echo Tip: keep\n╌╌╌╌╌╌\n│ Dangerous operation not ruled out\n⚠ Claude Code will automatically " + verb + "\n this request in 0:38, to avoid blocking progress\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel · Tab to amend"
		p := Parse("claude", strings.Split(screen, "\n"))
		if p == nil || p.Params.Origin == nil {
			t.Fatalf("%s: no structured command", verb)
		}
		o := p.Params.Origin
		if o.Explanation != "Check credentials" || o.Command != "echo Tip: keep" || o.Warning != "Dangerous operation not ruled out" || p.Params.TimeoutMS == nil || *p.Params.TimeoutMS != 38000 {
			t.Fatalf("%s: origin=%+v timeout=%v", verb, o, p.Params.TimeoutMS)
		}
	}
}

func TestClaudePermissionTipWrappedInsideOptionName(t *testing.T) {
	screen := "Bash command\nTip: auto mode handles these prompts for you — choose \"switch to\n auto mode\" below\nActual reason\n╌╌╌╌\necho safe\n╌╌╌╌\nDo you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel"
	p := Parse("claude", strings.Split(screen, "\n"))
	if p == nil || p.Params.Origin == nil || p.Params.Origin.Explanation != "Actual reason" {
		t.Fatalf("wrapped tip leaked into reason: %+v", p)
	}
}
