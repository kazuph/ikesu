package notify

import (
	"strings"
	"testing"
)

func TestBodyLastClaudeMarker(t *testing.T) {
	lines := []string{
		"⏺ First response",
		"───────",
		"⏺ The actual answer",
		"  continuation line",
		"❯",
		"─────────────────────────────",
	}
	got := Body(lines, "2 ws")
	if got != "The actual answer continuation line" {
		t.Fatalf("body = %q", got)
	}
}

func TestBodyLastCodexMarker(t *testing.T) {
	lines := []string{
		"• done with edits",
		"gpt-5 Context 71% left",
		"› type here",
	}
	got := Body(lines, "t")
	if got != "done with edits" {
		t.Fatalf("body = %q", got)
	}
}

func TestBodyChromeOnlyIsEmpty(t *testing.T) {
	lines := []string{
		"─────────",
		"❯",
		"gpt-5 Context 71% left",
		"worked for 12s",
	}
	if got := Body(lines, "t"); got != "" {
		t.Fatalf("body = %q, want empty", got)
	}
}

func TestBodyPlainFallback(t *testing.T) {
	lines := []string{
		"some output",
		"final result here",
		"",
		"─────────",
	}
	if got := Body(lines, "t"); got != "final result here" {
		t.Fatalf("body = %q", got)
	}
}

func TestBodyCap120(t *testing.T) {
	long := strings.Repeat("あ", 200)
	lines := []string{"⏺ " + long}
	got := Body(lines, "t")
	if n := len([]rune(got)); n != 120 {
		t.Fatalf("len = %d, want 120", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("no ellipsis: %q", got[len(got)-4:])
	}
}

func TestMarkerBodyStopsAtNextMarker(t *testing.T) {
	lines := []string{
		"⏺ older answer",
		"⏺ newest answer",
		"trailing",
	}
	if got := Body(lines, "t"); got != "newest answer trailing" {
		t.Fatalf("body = %q", got)
	}
}
