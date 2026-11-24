package guard

import (
	"strings"
	"testing"
)

// Unquoted brace sequences such as JS/JSON snippets or `{1..999999999}` must
// never trigger mvdan.cc/sh/v3/expand.Braces exponential memory explosion in
// guard redaction or approval matching (2026-11-25, 129GB OOM in claude-prefix).
func TestRedactAndWholeCommandDoNotExpandBraces(t *testing.T) {
	t.Parallel()
	cartesian := strings.Repeat("{a,b}", 30) // 2^30 combinations if brace-expanded
	hugeRange := "{1..999999999}"
	for _, payload := range []string{cartesian, hugeRange} {
		i := Input{
			Command: "echo",
			Args:    []string{payload},
			RawLine: "echo " + payload,
			Env:     map[string]string{"API_TOKEN": "supersecretvalue12345"},
		}
		got := i.redact(i.RawLine)
		if got != i.RawLine {
			t.Fatalf("unexpected redact output len=%d want=%d", len(got), len(i.RawLine))
		}
		if !wholeCommand(i.RawLine, i) {
			t.Fatalf("wholeCommand returned false for %q", payload)
		}
	}

	// Quoted braces with secrets must still be redacted.
	secret := "sk-live-secret-token-987654321"
	i := Input{
		Command: "curl",
		Args:    []string{"-d", `{"token":"` + secret + `"}`},
		RawLine: `curl -d '{"token":"` + secret + `"}'`,
		Env:     map[string]string{"API_TOKEN": secret},
	}
	redacted := i.redact(i.RawLine)
	if strings.Contains(redacted, secret) {
		t.Fatalf("quoted brace secret leaked: %q", redacted)
	}
}
