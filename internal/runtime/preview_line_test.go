package runtime

import (
	"strings"
	"testing"
)

func TestPreviewLineReadsTextAboveAgentInput(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, screen, want string
	}{
		{"claude", "old line\n✻ Cogitated for 1m 39s\n\n──────────\n❯ pushしてPR更新して\n──────────\n  ⏵⏵ auto mode on", "✻ Cogitated for 1m 39s"},
		{"codex", "  書き出しはキャンセル済みです。\n\n  Worked for 45s\n\n\n› Ask Codex to do anything\n\n  GPT-6.1 Terra medium\n  ? for shortcuts", "Worked for 45s"},
		{"shell", "$ go test ./...\nok  ikesu 0.3s\n\n", "ok  ikesu 0.3s"},
		{"blank", "\n\n", ""},
	} {
		p := NewSupervisor().newPane(1, "/tmp", 60, 12)
		p.flushPTYBatch([]byte(strings.ReplaceAll(c.screen, "\n", "\r\n")))
		if got := p.PreviewLine(); got != c.want {
			t.Fatalf("%s: got %q want %q\n%s", c.name, got, c.want, p.ScreenText())
		}
	}
}
