package guard

import "testing"

func TestExtGlobDisplayDoesNotPanic(t *testing.T) {
	for _, op := range []string{"+", "@", "!", "?", "*"} {
		t.Run(op, func(t *testing.T) {
			line := "echo " + op + "(foo|bar)"
			if got := (Input{}).redact(line); got != line {
				t.Fatalf("got %q, want %q", got, line)
			}
		})
	}
}
