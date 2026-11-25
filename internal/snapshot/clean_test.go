package snapshot

import (
	"slices"
	"testing"
)

func TestCleanTerminalRows(t *testing.T) {
	t.Parallel()
	input := []string{
		"\x1b[32m❯\x1b[0m fix the bug   ",
		"⠋ Working… (2s)",
		"⠙ Thinking (1s)",
		"⠹ Waiting for results",
		"✻ Running… (esc to interrupt)",
		"\x1b]8;;https://example.invalid\x1b\\link\x1b]8;;\a\x1bPignored\x1b\\\x1b(Bliteral\x00\x7f\u202e\u200b",
		"\tindent",
		"", "", " \t",
		"⠋ artistic text",
		"", "",
		"> ",
	}
	want := []string{"❯ fix the bug", "linkliteral", "\tindent", "", "⠋ artistic text", "", "", ">"}
	if got := Clean(input); !slices.Equal(got, want) {
		t.Fatalf("Clean() = %q, want %q", got, want)
	}
}
