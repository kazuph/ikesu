package screenprompt

import (
	"strings"
	"testing"
)

func TestCompletedQuestionAboveAgentInputIsNotCurrent(t *testing.T) {
	for _, tc := range []struct{ agent, fixture, after string }{
		{"claude", "claude-single", "⏺ Answer received.\n❯ "},
		{"claude", "claude-permission", "⏺ Command completed.\n❯ "},
		{"codex", "codex-question", "• Answer received.\n› "},
		{"codex", "codex-permission", "• Command completed.\n› "},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			lines := fixture(t, tc.fixture)
			if Parse(tc.agent, lines) == nil {
				t.Fatal("active question was not recognized")
			}
			completed := append(append([]string{}, lines...), strings.Split(tc.after, "\n")...)
			if p := Parse(tc.agent, completed); p != nil {
				t.Fatalf("completed history recognized as current: %+v", p)
			}
		})
	}
}
