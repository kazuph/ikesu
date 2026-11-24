package guard

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

func parseSingleWord(t *testing.T, src string) *syntax.Word {
	t.Helper()
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "word")
	if err != nil || len(tree.Stmts) != 1 {
		t.Fatalf("parse %q: %v", src, err)
	}
	call, ok := tree.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) != 1 {
		t.Fatalf("not a single word: %q", src)
	}
	return call.Args[0]
}

// Unquoted brace sequences such as JS/JSON snippets or `{1..N}` must never
// trigger mvdan.cc/sh/v3/expand.Braces exponential memory explosion in guard
// redaction or approval matching (2026-11-25, 129GB OOM in claude-prefix).
func TestBraceExpansionPreventedWithoutRegressingRedaction(t *testing.T) {
	t.Parallel()

	// 1. Helper directly rejects unquoted braces for singleExpandedField while
	//    singleDisplayField decodes them literally without brace expansion.
	for _, expr := range []string{"{a,b}{c,d}", "{1..50}", strings.Repeat("{a,b}", 12)} {
		w := parseSingleWord(t, expr)
		if !wordHasUnquotedBrace(w) {
			t.Fatalf("wordHasUnquotedBrace(%q) = false, want true", expr)
		}
		if _, ok := singleExpandedField(&expand.Config{NoUnset: true}, w); ok {
			t.Fatalf("singleExpandedField(%q) unexpectedly succeeded", expr)
		}
		got, ok := singleDisplayField(&expand.Config{NoUnset: true}, w)
		if !ok || got != expr {
			t.Fatalf("singleDisplayField(%q) = (%q, %v), want (%q, true)", expr, got, ok, expr)
		}
	}

	// 2. Quoted ordinary words remain accepted by singleExpandedField.
	quoted := parseSingleWord(t, `'{a,b}'`)
	if wordHasUnquotedBrace(quoted) {
		t.Fatal("quoted brace flagged as unquoted")
	}
	if got, ok := singleExpandedField(&expand.Config{NoUnset: true}, quoted); !ok || got != "{a,b}" {
		t.Fatalf("singleExpandedField(quoted) = (%q, %v)", got, ok)
	}

	// 3. wholeCommand second path (when shellLiteral differs from argv due to
	//    unquoted backslash) rejects unquoted brace words instead of expanding.
	iBrace := Input{Command: "echo", Args: []string{"{a,b} x"}, Env: map[string]string{}}
	if wholeCommand(`echo {a,b}\ x`, iBrace) {
		t.Fatal("wholeCommand unexpectedly matched unquoted brace word in second path")
	}

	// 4. Redaction must NOT regress when a secret value spans quote boundaries
	//    inside a word containing ordinary unquoted braces.
	secret := "opaque{mid}value98765"
	iSecret := Input{
		Command: "curl",
		Args:    []string{"-d", secret},
		RawLine: `curl -d opaque{mid}'value98765'`,
		Env:     map[string]string{"TEST_TOKEN": secret},
	}
	redacted := iSecret.redact(iSecret.RawLine)
	if !strings.Contains(redacted, "$TEST_TOKEN") || strings.Contains(redacted, "value98765") {
		t.Fatalf("unquoted-brace secret spanning quote boundary not redacted: %q", redacted)
	}

	// 5. Escaped braces (`\{` and `\\{`) must leave ZERO top-level '{' in the
	//    transformed AST while preserving exact shell decoding and secret masking.
	for _, tc := range []struct{ raw, want string }{
		{`lbl\{x`, `lbl{x`},
		{`lbl\\{x`, `lbl\{x`},
		{`\{a,b\}\{c,d\}`, `{a,b}{c,d}`},
	} {
		w := parseSingleWord(t, tc.raw)
		safe := escapeUnquotedBraces(w)
		if safe == nil || wordHasUnquotedBrace(safe) {
			t.Fatalf("escapeUnquotedBraces(%q) left unquoted brace in AST", tc.raw)
		}
		got, ok := singleDisplayField(&expand.Config{NoUnset: true}, w)
		if !ok || got != tc.want {
			t.Fatalf("singleDisplayField(%q) = (%q, %v), want (%q, true)", tc.raw, got, ok, tc.want)
		}
	}
	iEscapedSecret := Input{
		Command: "curl",
		Args:    []string{"-d", secret},
		RawLine: `curl -d opaque\{mid\}'value98765'`,
		Env:     map[string]string{"TEST_TOKEN": secret},
	}
	redactedEsc := iEscapedSecret.redact(iEscapedSecret.RawLine)
	if !strings.Contains(redactedEsc, "$TEST_TOKEN") || strings.Contains(redactedEsc, "value98765") {
		t.Fatalf("escaped-brace secret not redacted: %q", redactedEsc)
	}

	// 6. Realistic JS heredoc snippet with many brace pairs redacts cleanly without expanding.
	jsLine := `python3 - build.py <<'PYEOF'` + "\n" +
		`const WHO = {'0-0':'q','0-1':'z'}; const S = [{a:1,b:2},{c:3,d:4},{e:5,f:6},{g:7,h:8}];` + "\n" +
		`PYEOF`
	iJS := Input{Command: "python3", Args: []string{"-", "build.py"}, RawLine: jsLine, Env: map[string]string{"TEST_TOKEN": secret}}
	if got := iJS.redact(jsLine); got != jsLine {
		t.Fatalf("unexpected JS heredoc redaction change: %q", got)
	}
}
