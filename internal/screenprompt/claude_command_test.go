package screenprompt

import (
	"strings"
	"testing"
)

func TestClaudeCommandDetailsAndIdentity(t *testing.T) {
	lines := fixture(t, "claude-permission")
	p := Parse("claude", lines)
	if p == nil || p.Params.Origin == nil {
		t.Fatal("command not parsed")
	}
	o := p.Params.Origin
	if o.Tool != "Bash command" || o.Explanation != "Write observation text to file" || o.Command != "printf observation > /tmp/gs-j0m1z8np/work/permission-observation.txt" {
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
