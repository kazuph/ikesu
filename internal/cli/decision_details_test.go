package cli

import (
	"strings"
	"testing"
)

func TestDecisionDescriptionRejectsUnknownOptionBeforeConnecting(t *testing.T) {
	code, _, errs := runCLI(t, Options{}, "decision", "ask", "--title", "Choice", "--option", "one:One:other", "--option-desc", "missing:detail")
	if code != 2 || !strings.Contains(errs, "unknown option id") {
		t.Fatalf("%d %s", code, errs)
	}
	code, _, errs = runCLI(t, Options{}, "decision", "ask", "--title", "Choice", "--option", "one:One:other", "--option-desc", "bad")
	if code != 2 || !strings.Contains(errs, "id:text") {
		t.Fatalf("%d %s", code, errs)
	}
}
