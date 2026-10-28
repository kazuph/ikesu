package guard

import (
	"bytes"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const crashedGuardLine = `git fetch -q origin && git log --oneline HEAD..origin/main; grep 'const version' <(git show origin/main:cmd/ikesu/main.go)`

func TestGuardProcessSubstitutionDoesNotPanicOrAllow(t *testing.T) {
	for _, line := range []string{crashedGuardLine, `cat <(printf x)`, `printf x > >(cat)`, `X=<(printf x) printf ok`, `cat <(cat >(printf x))`} {
		t.Run(line, func(t *testing.T) {
			for _, runtime := range []bool{false, true} {
				i := Input{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin"}, runtimeLine: runtime}
				_, v, _ := lineVerdict(Config{}, i, line)
				if v.Decision != "ask" && v.Decision != "block" {
					t.Errorf("runtime=%v unexpected allow: %+v", runtime, v)
				}
				_ = i.redact(line)
				if wholeCommand(line, i) {
					t.Fatal("unsupported substitution matched whole command")
				}
			}
		})
	}
}

func TestForwardingShimProcessSubstitutionRejected(t *testing.T) {
	for _, body := range []string{`<(printf exec) /bin/ikesu guard exec --via shim -- rm "$@"`, `exec /bin/ikesu <(printf guard) exec --via shim -- rm "$@"`} {
		path := filepath.Join(t.TempDir(), "shim")
		if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if isForwardingShim(path) {
			t.Fatal("nonliteral forwarding shim accepted")
		}
	}
}

func TestGuardSubstitutionFailsClosedWithoutExecution(t *testing.T) {
	root := t.TempDir()
	a := NewAssets(root, "p1", "/usr/bin/true", Config{})
	env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/socket", "AI_GUARD_REQUEST_REASON=対象プロジェクト: ikesu。必要な理由: プロセス置換の拒否を確認します。"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Sync("claude"); err != nil {
		t.Fatal(err)
	}
	i := Input{Cwd: root, Env: EnvMap(env)}
	marker := filepath.Join(root, "never-created")
	for _, line := range []string{crashedGuardLine, "cat <(touch " + quote(marker) + ")", "printf x > >(touch " + quote(marker) + ")"} {
		var out bytes.Buffer
		if AuthorizeLine(Config{}, root, i, line, &out, nil) {
			t.Fatalf("unsupported substitution allowed: %s", line)
		}
		if !strings.Contains(out.String(), "プロセス置換") {
			t.Fatalf("missing safe rejection: %s", out.String())
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("analysis executed substitution", err)
	}
	_, v, err := lineVerdict(Config{}, i, `printf '%s' '<(literal)'`)
	if err != nil || v.Decision != "allow" {
		t.Fatal("quoted literal regression", v, err)
	}
}

func TestShellLiteralRejectsUnsupportedOrMissingWords(t *testing.T) {
	if _, err := shellLiteral(nil, nil); err == nil {
		t.Fatal("missing word accepted")
	}
	if _, err := snapshotLiteral(nil); err == nil {
		t.Fatal("missing snapshot word accepted")
	}
	for _, line := range []string{`cat <(printf x)`, `cat >(printf x)`, `cat pre<(printf x)post`, `cat <(cat <(printf x))`, `cat $(printf x)`, `cat ` + "`printf x`"} {
		tree, err := syntax.NewParser().Parse(strings.NewReader(line), "")
		if err != nil {
			t.Fatal(err)
		}
		call := tree.Stmts[0].Cmd.(*syntax.CallExpr)
		if _, err := shellLiteral(&expand.Config{NoUnset: true}, call.Args[1]); err == nil {
			t.Errorf("unsupported expansion accepted: %s", line)
		}
	}
}

func TestGuardProcessSubstitutionRedactionKeepsSecretsMasked(t *testing.T) {
	line := `API_TOKEN='outside-secret' cat <(printf '%s' OTHER_TOKEN='inside-secret')`
	i := Input{RawLine: line, Env: map[string]string{}}
	safe := i.redact(line)
	if strings.Contains(safe, "outside-secret") || strings.Contains(safe, "inside-secret") || !strings.Contains(safe, "***") {
		t.Fatalf("substitution redaction exposed secret: %s", safe)
	}
}
