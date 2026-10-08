package detect

import "testing"

func TestG2ObservedTerminalTitleStripping(t *testing.T) {
	for _, r := range "·✳✶✻✽✢⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏" {
		if got := StripTerminalTitle(string(r) + "  planner"); got != "planner" {
			t.Fatal(r, got)
		}
		if got := StripTerminalTitle(string(r)); got != "" {
			t.Fatal(r, got)
		}
	}
	for _, s := range []string{"● planner", "✻✻ planner", "🙂 hello", "› title", "▶ hello", "✣ planner", "✺ planner", "✻planner", "⠴✻ planner", "user@host:foo"} {
		if got := StripTerminalTitle(s); got != s {
			t.Fatal(s, got)
		}
	}
	if StripTerminalTitle("  planner  ") != "planner" {
		t.Fatal("whitespace")
	}
}
