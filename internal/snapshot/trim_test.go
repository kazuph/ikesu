package snapshot

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTrimRunes(t *testing.T) {
	t.Parallel()
	in := "先" + strings.Repeat("日本語🙂", 90) + "末"
	for limit := range utf8.RuneCountInString(in) + 1 {
		got := Trim(in, limit)
		if !utf8.ValidString(got) || utf8.RuneCountInString(got) > limit {
			t.Fatalf("limit %d produced %q", limit, got)
		}
	}
	const limit = 50
	got := Trim(in, limit)
	if !strings.HasPrefix(got, "先") || !strings.HasSuffix(got, "末") || !strings.Contains(got, omission(326)) {
		t.Fatalf("unexpected retained text or omission count: %q", got)
	}
	if got := Trim(in, 500); got != in {
		t.Fatalf("input under limit changed: %q", got)
	}
}

func TestBuildLargeScrollback(t *testing.T) {
	t.Parallel()
	lines := make([]string, 200000)
	for i := range lines {
		lines[i] = "⠋ Working…"
	}
	lines[0] = "must not appear"
	lines[len(lines)-MaxLines-1] = "> out of range"
	lines[len(lines)-MaxLines] = "> last retained prompt"
	lines[len(lines)-1] = "finished"
	start := time.Now()
	got := Build(context.Background(), Input{Lines: lines, Mode: "last_turn", Agent: "claude"})
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("Build took %s (limit 1s)", elapsed)
	} else {
		t.Logf("Build of %d lines took %s", len(lines), elapsed)
	}
	if !got.BoundaryFound || got.LastTurn != "> last retained prompt\nfinished" {
		t.Fatalf("unexpected retained tail: %+v", got)
	}
}

func TestBuildTrimsExtremeMetadata(t *testing.T) {
	t.Parallel()
	got := Build(context.Background(), Input{
		Workspace: "work", PaneID: "p1", Agent: strings.Repeat("長", 600),
		Lines: []string{"> prompt", "ANSWER"}, Mode: "handoff", MaxChars: 500,
	})
	if utf8.RuneCountInString(got.Markdown) > 500 || !strings.HasPrefix(got.Markdown, "# ikesu handoff: work / p1") || !strings.Contains(got.Markdown, "文字省略") {
		t.Fatalf("Markdown did not fall back to Trim: %q", got.Markdown)
	}
}
