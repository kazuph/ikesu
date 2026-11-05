package guard

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

func TestUnsupportedParamExpansionRequiresApproval(t *testing.T) {
	for _, op := range []string{"P", "A", "a", "k", "u", "K", "L", "U"} {
		t.Run(op, func(t *testing.T) {
			line := `bash -c 'echo "${HOME@` + op + `}"'`
			i := Input{Cwd: t.TempDir(), Env: map[string]string{"HOME": "/tmp/home", "PATH": "/usr/bin:/bin"}}
			_, v, err := lineVerdict(Config{}, i, line)
			if err == nil || v.Decision != "ask" || v.Rule != "shell-line" || !strings.Contains(v.Reason, "展開") {
				t.Fatalf("verdict=%+v err=%v", v, err)
			}
			display := `echo "${HOME@` + op + `}"`
			if got := i.redact(display); got != display {
				t.Fatalf("display=%q want %q", got, display)
			}
		})
	}
}

func TestSupportedParamExpansionsRemainUsable(t *testing.T) {
	for _, tc := range []struct{ expression, want string }{
		{`${HOME@Q}`, "/tmp/home"},
		{`${HOME@E}`, "/tmp/home"},
		{`${HOME}`, "/tmp/home"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			tree, err := syntax.NewParser().Parse(strings.NewReader("echo "+tc.expression), "test")
			if err != nil {
				t.Fatal(err)
			}
			word := tree.Stmts[0].Cmd.(*syntax.CallExpr).Args[1]
			got, err := shellLiteral(&expand.Config{Env: expand.ListEnviron("HOME=/tmp/home"), NoUnset: true}, word)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, err=%v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestParamQuoteWithNULRequiresApproval(t *testing.T) {
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"HOME": "bad\x00value", "PATH": "/usr/bin:/bin"}}
	_, v, err := lineVerdict(Config{}, i, `echo "${HOME@Q}"`)
	if err == nil || v.Decision != "ask" || v.Rule != "shell-line" {
		t.Fatalf("verdict=%+v err=%v", v, err)
	}
}
