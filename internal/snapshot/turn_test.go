package snapshot

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestLastTurnAgentPrompts(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		agent, prompt, final string
	}{
		{"Claude", "❯\u00a0inspect the diff", "> "},
		{"Codex", "› inspect the diff", "›\u00a0Ask Codex to do anything"},
		{"Gemini", "> inspect the diff", "> Type your message or @path/to/file"},
		{"agy", "> inspect the diff", "❯ "},
		{"zsh", "repo % git status", "repo % "},
		{"zsh with theme", "~/src/ikesu❯ git status", "❯ "},
		{"bash", "me@host:~/ikesu$ git status", "$ "},
		{"root shell", "[root@host ~]# ls", "# "},
	} {
		t.Run(tt.agent, func(t *testing.T) {
			want := Clean([]string{tt.prompt, "The result", tt.final, "? for shortcuts"})
			lines := Clean([]string{"> old request", "old response", "\x1b[1m" + tt.prompt + "\x1b[0m", "⠋ Thinking…", "The result", tt.final, "? for shortcuts"})
			agent := strings.ToLower(tt.agent)
			if agent != "claude" && agent != "codex" && agent != "agy" && agent != "gemini" {
				agent = ""
			}
			if got, found := LastTurn(lines, agent, 1); !found || !slices.Equal(got, want) {
				t.Fatalf("LastTurn() = %q, %v, want %q, true", got, found, want)
			}
		})
	}
}

func TestLastTurnFallback(t *testing.T) {
	t.Parallel()
	lines := make([]string, 150)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	lines[len(lines)-1] = "› Ask Codex to do anything"
	for _, tt := range []struct{ arg, want int }{{0, 120}, {9, 9}, {500, 150}} {
		got, found := LastTurn(lines, "codex", tt.arg)
		if found || !slices.Equal(got, lines[len(lines)-tt.want:]) {
			t.Fatalf("LastTurn(%d) = %d rows, found=%t, want %d rows and no boundary", tt.arg, len(got), found, tt.want)
		}
	}
}

func TestLastTurnCodexIgnoresMarkdownInAnswer(t *testing.T) {
	t.Parallel()
	lines := []string{"old", "› 調査して", "前提条件", "# 結果", "> 引用", "詳細"}
	got, found := LastTurn(lines, "codex", 1)
	if !found || !slices.Equal(got, lines[1:]) {
		t.Fatalf("got %q found=%t", got, found)
	}
	if got, found := LastTurn(lines[2:], "unknown", 1); found || !slices.Equal(got, []string{"詳細"}) {
		t.Fatalf("unknown agent must fall back: %q found=%t", got, found)
	}
}
