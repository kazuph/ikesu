package childenv

import (
	"context"
	"strings"
	"testing"
)

func TestCommandAndContextScrubRealChildren(t *testing.T) {
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CODEX_THREAD_ID", "outer")
	t.Setenv("IKESU_GUARD_ENFORCING", "1")
	t.Setenv("CLAUDE_EFFORT", "high")
	commands := []struct {
		name string
		run  func() ([]byte, error)
	}{
		{"command", func() ([]byte, error) { return Command("/usr/bin/env").Output() }},
		{"context", func() ([]byte, error) { return CommandContext(context.Background(), "/usr/bin/env").Output() }},
	}
	for _, entry := range commands {
		t.Run(entry.name, func(t *testing.T) {
			out, err := entry.run()
			if err != nil {
				t.Fatal(err)
			}
			text := string(out)
			for _, key := range []string{"CLAUDE_CODE_CHILD_SESSION", "CODEX_THREAD_ID", "IKESU_GUARD_ENFORCING"} {
				if strings.Contains(text, key+"=") {
					t.Fatal(text)
				}
			}
			if !strings.Contains(text, "CLAUDE_EFFORT=high") {
				t.Fatal(text)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CommandContext(ctx, "/usr/bin/env").Run(); err == nil {
		t.Fatal("cancelled command started")
	}
}
