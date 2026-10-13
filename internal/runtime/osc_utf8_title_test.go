package runtime

import (
	"strings"
	"testing"
)

func TestRecordedTitleDoesNotOverwriteQuestionAcrossChunks(t *testing.T) {
	for _, title := range []string{"Report format choice", "✳ AskUserQuestion で検証済みセクション選択"} {
		stream := []byte("\x1b[2J\x1b[HKEEP\x1b]0;" + title + "\a")
		direct := NewSupervisor().newPane(1, "/tmp", 80, 4)
		_, _ = direct.emu.Write(stream)
		t.Logf("direct emulator title %q screen=%q callback=%q", title, strings.TrimSpace(direct.emu.String()), direct.oscTitle)
		if got := strings.TrimSpace(direct.emu.String()); got != "KEEP" || direct.oscTitle != title {
			t.Fatalf("direct title %q: screen=%q callback=%q", title, got, direct.oscTitle)
		}
		for split := 1; split < len(stream); split++ {
			s := NewSupervisor()
			p := s.newPane(1, "/tmp", 80, 4)
			p.flushPTYBatch(stream[:split])
			p.flushPTYBatch(stream[split:])
			if got := strings.TrimSpace(p.ScreenText()); got != "KEEP" {
				t.Fatalf("title %q split %d appeared in question screen: %q", title, split, got)
			}
			if p.Snapshot().OSCTitle != title {
				t.Fatalf("title %q split %d changed: %q", title, split, p.Snapshot().OSCTitle)
			}
		}
	}
}

func TestRecordedUTF8TitleSingleByteChunks(t *testing.T) {
	const title = "✳ AskUserQuestion で検証済みセクション選択"
	for _, command := range []string{"0", "2"} {
		for _, end := range []string{"\a", "\x1b\\"} {
			stream := []byte("\x1b[2J\x1b[HKEEP\x1b]" + command + ";" + title + end)
			p := NewSupervisor().newPane(1, "/tmp", 80, 4)
			for i := range stream {
				p.flushPTYBatch(stream[i : i+1])
			}
			if got := strings.TrimSpace(p.ScreenText()); got != "KEEP" || p.Snapshot().OSCTitle != title {
				t.Fatalf("command %s end %q: screen=%q title=%q", command, end, got, p.Snapshot().OSCTitle)
			}
		}
	}
}
