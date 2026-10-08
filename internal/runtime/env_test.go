package runtime

import (
	"slices"
	"testing"
)

// SPEC G6: NO_COLOR is never inherited by pane children, while TERM and
// COLORTERM-style variables survive.
func TestCleanEnvStripsNoColor(t *testing.T) {
	in := []string{
		"NO_COLOR=1",
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"PATH=/bin",
		"NO_COLORFUL_EXTRA=keep",
	}
	out := cleanEnv(in)
	if slices.Contains(out, "NO_COLOR=1") {
		t.Fatalf("NO_COLOR survived: %v", out)
	}
	for _, want := range []string{"TERM=xterm-256color", "COLORTERM=truecolor", "PATH=/bin", "NO_COLORFUL_EXTRA=keep"} {
		if !slices.Contains(out, want) {
			t.Fatalf("%q dropped: %v", want, out)
		}
	}
}
