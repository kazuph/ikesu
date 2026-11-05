package guard

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

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

func TestExtGlobExpansionPaths(t *testing.T) {
	for _, op := range []string{"+", "@", "!", "?", "*"} {
		t.Run(op, func(t *testing.T) {
			line := "echo " + op + "(foo|bar)"
			tree, err := syntax.NewParser().Parse(strings.NewReader(line), "test")
			if err != nil {
				t.Fatal(err)
			}
			word := tree.Stmts[0].Cmd.(*syntax.CallExpr).Args[1]
			if _, err := shellLiteral(&expand.Config{NoUnset: true}, word); err == nil {
				t.Fatal("shell literal accepted extglob")
			}
			if _, ok := claudeLiteral(word); ok {
				t.Fatal("Claude literal accepted extglob")
			}
			if _, err := expand.Fields(&expand.Config{NoUnset: true}, word); err == nil {
				t.Fatal("Fields accepted extglob")
			}
			i := Input{Command: "echo", Args: []string{op + "(foo|bar)"}, Env: map[string]string{"PATH": "/usr/bin:/bin"}, Cwd: t.TempDir()}
			if wholeCommand(line, i) {
				t.Fatal("whole command accepted extglob")
			}
			for _, runtime := range []bool{false, true} {
				i.runtimeLine = runtime
				_, verdict, err := lineVerdict(Config{}, i, line)
				if verdict.Decision != "ask" || verdict.Rule != "shell-line" || err == nil || !strings.Contains(err.Error(), "拡張グロブ") || !strings.Contains(verdict.Reason, "拡張グロブ") {
					t.Fatalf("runtime=%v: %+v %v", runtime, verdict, err)
				}
			}
			for _, literal := range []string{`"` + op + `(foo|bar)"`, `\` + op + `\(foo\|bar\)`} {
				quoted := "echo " + literal
				_, verdict, err := lineVerdict(Config{}, Input{Cwd: t.TempDir(), Env: i.Env}, quoted)
				if err != nil || verdict.Decision != "allow" {
					t.Fatalf("quoted %q: %+v %v", quoted, verdict, err)
				}
			}
		})
	}
}

func TestQuotedExtGlobRemainsLiteral(t *testing.T) {
	line := `gh release view ikesu-v0.4.74 --json isDraft -q '"d="+(.isDraft|tostring)'`
	i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin"}}
	_, verdict, err := lineVerdict(Config{}, i, line)
	if err != nil || verdict.Decision != "allow" {
		t.Fatalf("quoted command: %+v %v", verdict, err)
	}
	if got := i.redact(line); got != line {
		t.Fatalf("got %q", got)
	}
}
